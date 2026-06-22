package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

type incidentSettingsRequest struct {
	IncidentManagementEnabled bool     `json:"incidentManagementEnabled"`
	IncidentHandlerHubID      string   `json:"incidentHandlerHubId"`
	IncidentAutoSuggest       bool     `json:"incidentAutoSuggest"`
	IncidentAutoOpen          bool     `json:"incidentAutoOpen"`
	IncidentWatchAreas        []string `json:"incidentWatchAreas"`
	IncidentWatchPointLat     float64  `json:"incidentWatchPointLat"`
	IncidentWatchPointLon     float64  `json:"incidentWatchPointLon"`
}

type createIncidentRequest struct {
	Title      string `json:"title"`
	TemplateID string `json:"templateId"`
	Type       string `json:"type"`
	Priority   string `json:"priority"`
	Exposure   string `json:"exposure"`
	Notes      string `json:"notes"`
	Activate   bool   `json:"activate"`
}

type incidentResponse struct {
	Incident       database.Incident  `json:"incident"`
	RadioSet       *database.RadioSet `json:"radioSet,omitempty"`
	ShareURL       string             `json:"shareUrl,omitempty"`
	DiscordQueued  bool               `json:"discordQueued,omitempty"`
}

type incidentListItem struct {
	database.Incident
	ShareURL string `json:"shareUrl,omitempty"`
}

func (h *handler) incidentPublicPlayerURL(incident database.Incident) string {
	if incident.Exposure != "community" || incident.Status != "active" || incident.RadioSetID == "" {
		return ""
	}
	rs, found, err := h.db.GetRadioSetForPTT(incident.RadioSetID)
	if err != nil || !found || rs.ShareToken == nil || *rs.ShareToken == "" {
		return ""
	}
	base := strings.TrimRight(h.cfg.HubPublicURL, "/")
	if base == "" {
		return ""
	}
	return base + "/public/player/" + *rs.ShareToken
}

func (h *handler) ensureCommunityShareToken(incident database.Incident) (string, error) {
	if incident.Exposure != "community" || incident.RadioSetID == "" {
		return "", nil
	}
	rs, found, err := h.db.GetRadioSetForPTT(incident.RadioSetID)
	if err != nil || !found {
		return "", err
	}
	if rs.ShareToken != nil && *rs.ShareToken != "" {
		return h.incidentPublicPlayerURL(incident), nil
	}
	token := database.NewShareToken()
	if err := h.db.SetRadioSetShareToken(rs.ID, rs.UserID, token); err != nil {
		return "", err
	}
	base := strings.TrimRight(h.cfg.HubPublicURL, "/")
	if base == "" {
		return "", nil
	}
	return base + "/public/player/" + token, nil
}

func canManageIncidents(user authUser) bool {
	return isAdmin(user) || user.DispatcherEnabled
}

func (h *handler) incidentManagementAvailable(identity *database.HubIdentity) bool {
	if identity == nil {
		return false
	}
	if !identity.IncidentManagementEnabled {
		return false
	}
	handlerID := strings.TrimSpace(identity.IncidentHandlerHubID)
	if handlerID == "" {
		return true
	}
	peers, err := h.db.ListHubPeers()
	if err != nil {
		h.logger.Error("list peers for incident management check failed", "error", err)
		return false
	}
	for _, peer := range peers {
		if peer.HubID == handlerID && peer.Status == "connected" {
			return true
		}
	}
	return false
}

func (h *handler) requireIncidentManager(w http.ResponseWriter, r *http.Request) (authUser, *database.HubIdentity, bool) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return authUser{}, nil, false
	}
	if !canManageIncidents(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return authUser{}, nil, false
	}
	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity for incidents failed", "error", err)
		http.Error(w, "load hub identity", http.StatusInternalServerError)
		return authUser{}, nil, false
	}
	if !h.incidentManagementAvailable(identity) {
		http.Error(w, "incident management not enabled or handler peer not connected", http.StatusForbidden)
		return authUser{}, nil, false
	}
	return user, identity, true
}

func (h *handler) handleGetIncidentSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}
	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity failed", "error", err)
		http.Error(w, "load hub identity", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"incidentManagementEnabled": identity.IncidentManagementEnabled,
		"incidentHandlerHubId":      identity.IncidentHandlerHubID,
		"incidentAutoSuggest":       identity.IncidentAutoSuggest,
		"incidentAutoOpen":          identity.IncidentAutoOpen,
		"incidentWatchAreas":        identity.IncidentWatchAreas,
		"incidentWatchPointLat":     identity.IncidentWatchPointLat,
		"incidentWatchPointLon":     identity.IncidentWatchPointLon,
	})
}

func (h *handler) handleUpdateIncidentSettings(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	existing, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity failed", "error", err)
		http.Error(w, "load hub identity", http.StatusInternalServerError)
		return
	}

	var req incidentSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	updated := *existing
	updated.IncidentManagementEnabled = req.IncidentManagementEnabled
	updated.IncidentHandlerHubID = strings.TrimSpace(req.IncidentHandlerHubID)
	updated.IncidentAutoSuggest = req.IncidentAutoSuggest
	updated.IncidentAutoOpen = req.IncidentAutoOpen
	if req.IncidentWatchAreas != nil {
		updated.IncidentWatchAreas = req.IncidentWatchAreas
	}
	if req.IncidentWatchPointLat != 0 {
		updated.IncidentWatchPointLat = req.IncidentWatchPointLat
	}
	if req.IncidentWatchPointLon != 0 {
		updated.IncidentWatchPointLon = req.IncidentWatchPointLon
	}

	saved, err := h.db.UpdateHubIncidentSettings(updated)
	if err != nil {
		h.logger.Error("save incident settings failed", "error", err)
		http.Error(w, "save incident settings", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.incident_settings_updated", "hub", saved.HubID, map[string]any{
		"incidentManagementEnabled": saved.IncidentManagementEnabled,
		"incidentHandlerHubId":      saved.IncidentHandlerHubID,
	})
	writeJSON(w, http.StatusOK, saved)
}

func (h *handler) handleListIncidentTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}
	templates, err := h.db.ListIncidentTemplates()
	if err != nil {
		h.logger.Error("list incident templates failed", "error", err)
		http.Error(w, "list incident templates", http.StatusInternalServerError)
		return
	}
	if len(templates) == 0 {
		if err := h.db.SeedDefaultIncidentTemplates(); err != nil {
			h.logger.Error("seed incident templates failed", "error", err)
			http.Error(w, "seed incident templates", http.StatusInternalServerError)
			return
		}
		templates, err = h.db.ListIncidentTemplates()
		if err != nil {
			h.logger.Error("list incident templates after seed failed", "error", err)
			http.Error(w, "list incident templates", http.StatusInternalServerError)
			return
		}
	}
	if templates == nil {
		templates = []database.IncidentTemplate{}
	}
	writeJSON(w, http.StatusOK, templates)
}

func (h *handler) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}
	includeArchived := r.URL.Query().Get("archived") == "1"
	incidents, err := h.db.ListIncidents(includeArchived)
	if err != nil {
		h.logger.Error("list incidents failed", "error", err)
		http.Error(w, "list incidents", http.StatusInternalServerError)
		return
	}
	if incidents == nil {
		incidents = []database.Incident{}
	}
	items := make([]incidentListItem, 0, len(incidents))
	for _, inc := range incidents {
		items = append(items, incidentListItem{
			Incident: inc,
			ShareURL: h.incidentPublicPlayerURL(inc),
		})
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *handler) handleListIncidentSignals(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireIncidentManager(w, r); !ok {
		return
	}
	signals, err := h.db.ListPendingIncidentSignals(50)
	if err != nil {
		h.logger.Error("list incident signals failed", "error", err)
		http.Error(w, "list incident signals", http.StatusInternalServerError)
		return
	}
	if signals == nil {
		signals = []database.IncidentSignal{}
	}
	writeJSON(w, http.StatusOK, signals)
}

func (h *handler) createIncidentFromTemplate(user authUser, req createIncidentRequest, status string, activate bool) (incidentResponse, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return incidentResponse{}, fmt.Errorf("title is required")
	}

	var tmpl database.IncidentTemplate
	if req.TemplateID != "" {
		foundTmpl, ok, err := h.db.GetIncidentTemplate(req.TemplateID)
		if err != nil {
			return incidentResponse{}, err
		}
		if !ok {
			return incidentResponse{}, fmt.Errorf("template not found")
		}
		tmpl = foundTmpl
	}

	incidentType := strings.TrimSpace(req.Type)
	if incidentType == "" {
		incidentType = tmpl.IncidentType
	}
	if incidentType == "" {
		incidentType = "custom"
	}
	priority := req.Priority
	if priority == "" {
		priority = tmpl.DefaultPriority
	}
	exposure := req.Exposure
	if exposure == "" {
		exposure = tmpl.DefaultExposure
	}

	setName := fmt.Sprintf("INC · %s", title)
	if len(setName) > 120 {
		setName = setName[:120]
	}
	selectionMode := tmpl.SelectionMode
	if selectionMode == "" {
		selectionMode = "groups"
	}
	rs, err := h.db.CreateRadioSet(user.ID, setName, selectionMode, tmpl.Talkgroups, tmpl.TalkgroupGroups)
	if err != nil {
		return incidentResponse{}, err
	}

	incident, err := h.db.CreateIncident(database.Incident{
		Title:          title,
		IncidentType:   incidentType,
		Status:         status,
		Priority:       priority,
		Exposure:       exposure,
		RadioSetID:     rs.ID,
		TemplateID:     tmpl.ID,
		OpenedByUserID: user.ID,
		Notes:          strings.TrimSpace(req.Notes),
	})
	if err != nil {
		return incidentResponse{}, err
	}

	resp := incidentResponse{Incident: incident, RadioSet: &rs}
	if activate {
		activated, err := h.db.ActivateIncident(incident.ID)
		if err != nil {
			return incidentResponse{}, err
		}
		resp.Incident = activated
		shareURL, shareErr := h.ensureCommunityShareToken(activated)
		if shareErr != nil {
			h.logger.Error("ensure incident share token failed", "error", shareErr, "incidentId", activated.ID)
		} else if shareURL != "" {
			resp.ShareURL = shareURL
			if rs.ShareToken == nil {
				rs2, found, _ := h.db.GetRadioSetForPTT(rs.ID)
				if found {
					resp.RadioSet = &rs2
				}
			}
		}
		resp.DiscordQueued = h.queueDiscordIntegrationForIncident(activated.ID, user.ID)
	}
	return resp, nil
}

func (h *handler) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	var req createIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	status := "draft"
	if req.Activate {
		status = "active"
	}
	resp, err := h.createIncidentFromTemplate(user, req, status, req.Activate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_ = h.db.AppendAuditLog(user.ID, "incident.created", "incident", resp.Incident.ID, map[string]any{
		"title":      resp.Incident.Title,
		"templateId": resp.Incident.TemplateID,
		"status":     resp.Incident.Status,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *handler) handleActivateIncident(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	incident, found, err := h.db.GetIncident(id)
	if err != nil || !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	activated, err := h.db.ActivateIncident(incident.ID)
	if err != nil {
		h.logger.Error("activate incident failed", "error", err)
		http.Error(w, "activate incident", http.StatusInternalServerError)
		return
	}
	shareURL, _ := h.ensureCommunityShareToken(activated)
	resp := incidentResponse{Incident: activated, ShareURL: shareURL}
	if activated.RadioSetID != "" {
		if rs, found, rsErr := h.db.GetRadioSetForPTT(activated.RadioSetID); rsErr == nil && found {
			resp.RadioSet = &rs
		}
	}
	resp.DiscordQueued = h.queueDiscordIntegrationForIncident(activated.ID, user.ID)
	_ = h.db.AppendAuditLog(user.ID, "incident.activated", "incident", activated.ID, nil)
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleCloseIncident(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	closed, err := h.db.CloseIncident(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.db.MarkDiscordIntegrationsStopping(closed.ID); err != nil {
		h.logger.Error("mark discord integrations stopping failed", "error", err, "incidentId", closed.ID)
	}
	if closed.RadioSetID != "" {
		rs, found, rsErr := h.db.GetRadioSetForPTT(closed.RadioSetID)
		if rsErr == nil && found {
			_ = h.db.ClearRadioSetShareToken(rs.ID, rs.UserID)
		}
	}
	_ = h.db.AppendAuditLog(user.ID, "incident.closed", "incident", closed.ID, nil)
	writeJSON(w, http.StatusOK, closed)
}

func (h *handler) handleArchiveIncident(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	archived, err := h.db.ArchiveIncident(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = h.db.AppendAuditLog(user.ID, "incident.archived", "incident", archived.ID, nil)
	writeJSON(w, http.StatusOK, archived)
}

func (h *handler) handlePromoteIncidentSignal(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	signalID := chi.URLParam(r, "id")

	signals, err := h.db.ListPendingIncidentSignals(200)
	if err != nil {
		http.Error(w, "list signals", http.StatusInternalServerError)
		return
	}
	var signal *database.IncidentSignal
	for i := range signals {
		if signals[i].ID == signalID {
			signal = &signals[i]
			break
		}
	}
	if signal == nil {
		http.Error(w, "signal not found", http.StatusNotFound)
		return
	}

	req := createIncidentRequest{
		Title:      signal.Title,
		TemplateID: signal.TemplateID,
		Notes:      signal.Detail,
		Activate:   true,
	}
	resp, err := h.createIncidentFromTemplate(user, req, "active", true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = h.db.LinkIncidentSignal(signal.ID, resp.Incident.ID)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *handler) handlePollIncidentSignals(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireIncidentManager(w, r); !ok {
		return
	}
	count, err := h.pollIncidentSignalsOnce()
	if err != nil {
		h.logger.Error("manual incident signal poll failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"processed": count})
}
