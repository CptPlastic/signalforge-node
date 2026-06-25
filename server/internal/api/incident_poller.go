package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const (
	incidentPollInterval = 90 * time.Second
	nwsUserAgent         = "SignalForge-Hub/1.0 (incident-management; contact=info@projectseven.us)"
)

type nwsAlertsFeed struct {
	Features []nwsAlertFeature `json:"features"`
}

type nwsAlertFeature struct {
	Properties nwsAlertProperties `json:"properties"`
}

type nwsAlertProperties struct {
	ID       string `json:"id"`
	Event    string `json:"event"`
	Headline string `json:"headline"`
	Severity string `json:"severity"`
	AreaDesc string `json:"areaDesc"`
}

type iemLSRFeature struct {
	Properties map[string]any `json:"properties"`
}

type iemLSRFeed struct {
	Features []iemLSRFeature `json:"features"`
}

type nwsRawProperties struct {
	Polygon       any      `json:"polygon"`
	AffectedZones []string `json:"affectedZones"`
}

func (h *handler) startIncidentSignalPoller() {
	go func() {
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		for {
			<-timer.C
			if _, err := h.pollIncidentSignalsOnce(); err != nil {
				h.logger.Warn("incident signal poll failed", "error", err)
			}
			timer.Reset(incidentPollInterval)
		}
	}()
}

func (h *handler) pollIncidentSignalsOnce() (int, error) {
	identity, err := h.ensureHubIdentity()
	if err != nil {
		return 0, err
	}
	if !identity.IncidentManagementEnabled {
		return 0, nil
	}
	if !identity.IncidentAutoSuggest && !identity.IncidentAutoOpen {
		return 0, nil
	}

	processed := 0
	activeExternalIDs := make(map[string]bool)
	for _, area := range identity.IncidentWatchAreas {
		area = strings.TrimSpace(area)
		if area == "" {
			continue
		}
		n, ids, err := h.pollNWSAlerts(area, identity)
		if err != nil {
			return processed, err
		}
		processed += n
		for _, id := range ids {
			activeExternalIDs[id] = true
		}
	}

	if len(activeExternalIDs) > 0 {
		if err := h.cleanupExpiredWeatherIncidents(activeExternalIDs); err != nil {
			h.logger.Warn("expired weather incident cleanup failed", "error", err)
		}
	}

	if len(identity.IncidentWatchAreas) > 0 {
		state := strings.ToUpper(strings.TrimSpace(identity.IncidentWatchAreas[0]))
		if len(state) == 2 {
			n, err := h.pollIEMLSR(state, identity)
			if err != nil {
				h.logger.Warn("iem lsr poll failed", "state", state, "error", err)
			} else {
				processed += n
			}
		}
	}
	return processed, nil
}

func (h *handler) pollNWSAlerts(area string, identity *database.HubIdentity) (int, []string, error) {
	url := fmt.Sprintf("https://api.weather.gov/alerts/active?area=%s", strings.ToUpper(area))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/geo+json")
	req.Header.Set("User-Agent", nwsUserAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, nil, fmt.Errorf("nws alerts HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var feed nwsAlertsFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return 0, nil, err
	}

	processed := 0
	activeIDs := make([]string, 0, len(feed.Features))
	for _, feature := range feed.Features {
		props := feature.Properties
		id := strings.TrimSpace(props.ID)
		if id == "" {
			continue
		}
		activeIDs = append(activeIDs, id)

		if !h.alertAffectsWatchArea(props, identity) {
			continue
		}

		tmpl, hasTmpl, _ := h.db.MatchIncidentTemplateByNWSEvent(props.Event)
		templateID := ""
		if hasTmpl {
			templateID = tmpl.ID
		}
		title := strings.TrimSpace(props.Headline)
		if title == "" {
			title = strings.TrimSpace(props.Event)
		}
		detail := strings.TrimSpace(props.AreaDesc)

		signal := database.IncidentSignal{
			Source:     "nws",
			ExternalID: props.ID,
			EventType:  props.Event,
			Severity:   mapNWSSeverity(props.Severity, props.Event),
			Title:      title,
			Detail:     detail,
			TemplateID: templateID,
		}
		raw, _ := json.Marshal(props)
		signal.Raw = raw

		insertedSignal, isNew, err := h.db.UpsertIncidentSignal(signal)
		if err != nil {
			return processed, activeIDs, err
		}
		if !isNew {
			continue
		}
		processed++

		if err := h.maybeAutoCreateIncidentFromSignal(identity, insertedSignal, tmpl, hasTmpl); err != nil {
			h.logger.Warn("auto incident from nws signal failed", "signal_id", insertedSignal.ID, "error", err)
		}
	}
	return processed, activeIDs, nil
}

func (h *handler) alertAffectsWatchArea(props nwsAlertProperties, identity *database.HubIdentity) bool {
	if identity.IncidentWatchRadiusKm <= 0 {
		return true
	}
	if identity.IncidentWatchPointLat == 0 && identity.IncidentWatchPointLon == 0 {
		return true
	}

	var raw nwsRawProperties
	if err := json.Unmarshal([]byte(`{"polygon":`+extractRawField(props, "polygon")+`,"affectedZones":`+extractRawField(props, "affectedZones")+`}`), &raw); err != nil {
		return true
	}

	if raw.Polygon != nil {
		coords := flattenPolygon(raw.Polygon)
		if len(coords) >= 3 && pointInPolygon(coords, identity.IncidentWatchPointLon, identity.IncidentWatchPointLat) {
			return true
		}
		if len(coords) >= 2 && distanceToPolygonMeters(coords, identity.IncidentWatchPointLat, identity.IncidentWatchPointLon) <= identity.IncidentWatchRadiusKm*1000 {
			return true
		}
		return false
	}

	if len(raw.AffectedZones) > 0 && len(identity.IncidentWatchAreas) > 0 {
		for _, zone := range raw.AffectedZones {
			zone = strings.ToUpper(strings.TrimSpace(zone))
			for _, area := range identity.IncidentWatchAreas {
				area = strings.ToUpper(strings.TrimSpace(area))
				if strings.Contains(zone, "/"+area) || strings.HasSuffix(zone, area) {
					return true
				}
			}
		}
		return false
	}

	return true
}

func extractRawField(props nwsAlertProperties, field string) string {
	raw, err := json.Marshal(props)
	if err != nil {
		return "null"
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return "null"
	}
	val, ok := data[field]
	if !ok || val == nil {
		return "null"
	}
	b, err := json.Marshal(val)
	if err != nil {
		return "null"
	}
	return string(b)
}

func flattenPolygon(poly any) [][2]float64 {
	switch v := poly.(type) {
	case []any:
		if len(v) == 0 {
			return nil
		}
		if first, ok := v[0].([]any); ok && len(first) > 0 {
			if _, ok := first[0].([]any); ok {
				return flattenPolygon(first)
			}
			return flattenCoords(first)
		}
		return flattenCoords(v)
	}
	return nil
}

func flattenCoords(arr []any) [][2]float64 {
	coords := make([][2]float64, 0, len(arr))
	for _, elem := range arr {
		pair, ok := elem.([]any)
		if !ok || len(pair) < 2 {
			continue
		}
		lon, _ := pair[0].(float64)
		lat, _ := pair[1].(float64)
		coords = append(coords, [2]float64{lon, lat})
	}
	return coords
}

func pointInPolygon(coords [][2]float64, lon, lat float64) bool {
	inside := false
	n := len(coords)
	j := n - 1
	for i := 0; i < n; i++ {
		if (coords[i][1] > lat) != (coords[j][1] > lat) &&
			lon < (coords[j][0]-coords[i][0])*(lat-coords[i][1])/(coords[j][1]-coords[i][1])+coords[i][0] {
			inside = !inside
		}
		j = i
	}
	return inside
}

func distanceToPolygonMeters(coords [][2]float64, lat, lon float64) float64 {
	minDist := math.MaxFloat64
	for _, coord := range coords {
		d := haversineMeters(lat, lon, coord[1], coord[0])
		if d < minDist {
			minDist = d
		}
	}
	return minDist
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func (h *handler) pollIEMLSR(state string, identity *database.HubIdentity) (int, error) {
	url := fmt.Sprintf("https://mesonet.agron.iastate.edu/cgi-bin/request/gis/lsr.py?state=%s&hours=6&fmt=geojson", state)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", nwsUserAgent)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("iem lsr HTTP %d", resp.StatusCode)
	}

	var feed iemLSRFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return 0, err
	}

	processed := 0
	for _, feature := range feed.Features {
		props := feature.Properties
		externalID := fmt.Sprintf("%v", props["id"])
		if externalID == "" || externalID == "<nil>" {
			externalID = fmt.Sprintf("%v-%v", props["valid"], props["type"])
		}
		eventType := fmt.Sprintf("%v", props["type"])
		title := strings.TrimSpace(fmt.Sprintf("%s — %v", eventType, props["city"]))
		detail := strings.TrimSpace(fmt.Sprintf("%v", props["remark"]))

		signal := database.IncidentSignal{
			Source:     "iem_lsr",
			ExternalID: externalID,
			EventType:  eventType,
			Severity:   "normal",
			Title:      title,
			Detail:     detail,
			TemplateID: "weather-severe",
		}
		raw, _ := json.Marshal(props)
		signal.Raw = raw

		insertedSignal, isNew, err := h.db.UpsertIncidentSignal(signal)
		if err != nil {
			return processed, err
		}
		if !isNew {
			continue
		}
		processed++

		tmpl, hasTmpl, _ := h.db.GetIncidentTemplate("weather-severe")
		if err := h.maybeAutoCreateIncidentFromSignal(identity, insertedSignal, tmpl, hasTmpl); err != nil {
			h.logger.Warn("auto incident from lsr signal failed", "signal_id", insertedSignal.ID, "error", err)
		}
	}
	return processed, nil
}

func (h *handler) cleanupExpiredWeatherIncidents(activeExternalIDs map[string]bool) error {
	incidents, err := h.db.ListActiveWeatherIncidents()
	if err != nil {
		return err
	}

	for _, inc := range incidents {
		if inc.RadioSetID == "" {
			continue
		}
		externalID, found, err := h.db.GetNWSSignalExternalIDForIncident(inc.ID)
		if err != nil || !found {
			continue
		}
		if activeExternalIDs[externalID] {
			continue
		}

		closed, err := h.db.CloseIncident(inc.ID)
		if err != nil {
			h.logger.Warn("failed to close expired weather incident", "incidentId", inc.ID, "error", err)
			continue
		}
		_ = h.db.MarkDiscordIntegrationsStopping(closed.ID)
		if closed.RadioSetID != "" {
			rs, found, rsErr := h.db.GetRadioSetForPTT(closed.RadioSetID)
			if rsErr == nil && found {
				_ = h.db.ClearRadioSetShareToken(rs.ID, rs.UserID)
			}
		}
		h.logger.Info("closed expired weather incident", "incidentId", inc.ID, "title", inc.Title)
	}
	return nil
}

func mapNWSSeverity(severity, event string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "extreme", "severe":
		return "urgent"
	case "moderate":
		return "high"
	default:
		if strings.Contains(strings.ToLower(event), "warning") {
			return "high"
		}
		return "normal"
	}
}

func (h *handler) maybeAutoCreateIncidentFromSignal(identity *database.HubIdentity, signal database.IncidentSignal, tmpl database.IncidentTemplate, hasTmpl bool) error {
	if !identity.IncidentAutoSuggest && !identity.IncidentAutoOpen {
		return nil
	}

	autoOpen := identity.IncidentAutoOpen && signal.Severity == "urgent" && strings.Contains(strings.ToLower(signal.EventType), "tornado warning")
	status := "draft"
	activate := false
	if autoOpen {
		status = "active"
		activate = true
	} else if !identity.IncidentAutoSuggest {
		return nil
	}

	adminID, err := h.db.FirstAdminUserID()
	if err != nil {
		return err
	}

	selectionMode := "groups"
	tgs := []int{}
	groups := []string{}
	incidentType := "weather"
	priority := "high"
	exposure := "community"
	templateID := signal.TemplateID
	if hasTmpl {
		selectionMode = tmpl.SelectionMode
		tgs = tmpl.Talkgroups
		groups = tmpl.TalkgroupGroups
		incidentType = tmpl.IncidentType
		priority = tmpl.DefaultPriority
		exposure = tmpl.DefaultExposure
		templateID = tmpl.ID
	}

	setName := fmt.Sprintf("INC · %s", signal.Title)
	if len(setName) > 120 {
		setName = setName[:120]
	}
	rs, err := h.db.CreateRadioSet(adminID, setName, selectionMode, tgs, groups)
	if err != nil {
		return err
	}

	incident, err := h.db.CreateIncident(database.Incident{
		Title:          signal.Title,
		IncidentType:   incidentType,
		Status:         status,
		Priority:       priority,
		Exposure:       exposure,
		RadioSetID:     rs.ID,
		TemplateID:     templateID,
		OpenedByUserID: adminID,
		Notes:          signal.Detail,
	})
	if err != nil {
		return err
	}
	if activate {
		activated, err := h.db.ActivateIncident(incident.ID)
		if err != nil {
			return err
		}
		if exposure == "community" {
			token := database.NewShareToken()
			_ = h.db.SetRadioSetShareToken(rs.ID, adminID, token)
		}
		h.queueDiscordIntegrationForIncident(activated.ID, adminID)
	}
	return h.db.LinkIncidentSignal(signal.ID, incident.ID)
}
