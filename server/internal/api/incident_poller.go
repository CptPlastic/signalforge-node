package api

import (
	"encoding/json"
	"fmt"
	"io"
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
	for _, area := range identity.IncidentWatchAreas {
		area = strings.TrimSpace(area)
		if area == "" {
			continue
		}
		n, err := h.pollNWSAlerts(area, identity)
		if err != nil {
			return processed, err
		}
		processed += n
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

func (h *handler) pollNWSAlerts(area string, identity *database.HubIdentity) (int, error) {
	url := fmt.Sprintf("https://api.weather.gov/alerts/active?area=%s", strings.ToUpper(area))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/geo+json")
	req.Header.Set("User-Agent", nwsUserAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("nws alerts HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var feed nwsAlertsFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return 0, err
	}

	processed := 0
	for _, feature := range feed.Features {
		props := feature.Properties
		if strings.TrimSpace(props.ID) == "" {
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
			return processed, err
		}
		if !isNew {
			continue
		}
		processed++

		if err := h.maybeAutoCreateIncidentFromSignal(identity, insertedSignal, tmpl, hasTmpl); err != nil {
			h.logger.Warn("auto incident from nws signal failed", "signal_id", insertedSignal.ID, "error", err)
		}
	}
	return processed, nil
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

	// Use system user path — incidents from automation use opened_by empty; find first admin
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
		if _, err := h.db.ActivateIncident(incident.ID); err != nil {
			return err
		}
		if exposure == "community" {
			token := database.NewShareToken()
			_ = h.db.SetRadioSetShareToken(rs.ID, adminID, token)
		}
	}
	return h.db.LinkIncidentSignal(signal.ID, incident.ID)
}
