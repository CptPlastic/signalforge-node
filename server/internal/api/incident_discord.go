package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

type incidentDiscordIntegrationResponse struct {
	Integration *database.IncidentIntegration `json:"integration"`
}

type completeDiscordTaskRequest struct {
	VoiceChannelID string `json:"voiceChannelId"`
	TextChannelID  string `json:"textChannelId"`
	CategoryID     string `json:"categoryId"`
}

type failDiscordTaskRequest struct {
	Error string `json:"error"`
}

func (h *handler) buildIncidentDiscordConfig(incident database.Incident) (json.RawMessage, error) {
	cfg := database.IncidentIntegrationConfig{
		IncidentTitle: incident.Title,
		Exposure:      incident.Exposure,
	}
	if incident.Exposure == "community" {
		cfg.PublicPlayerURL = h.incidentPublicPlayerURL(incident)
		if cfg.PublicPlayerURL == "" && incident.RadioSetID != "" {
			shareURL, err := h.ensureCommunityShareToken(incident)
			if err != nil {
				return nil, err
			}
			cfg.PublicPlayerURL = shareURL
		}
	}
	if incident.RadioSetID != "" {
		token, err := h.ensureIncidentStreamToken(incident)
		if err != nil {
			return nil, err
		}
		cfg.StreamToken = token
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (h *handler) ensureIncidentStreamToken(incident database.Incident) (string, error) {
	if incident.RadioSetID == "" {
		return "", nil
	}
	rs, found, err := h.db.GetRadioSetForPTT(incident.RadioSetID)
	if err != nil || !found {
		return "", err
	}
	if rs.ShareToken != nil && *rs.ShareToken != "" {
		return *rs.ShareToken, nil
	}
	token := database.NewShareToken()
	if err := h.db.SetRadioSetShareToken(rs.ID, rs.UserID, token); err != nil {
		return "", err
	}
	return token, nil
}

func (h *handler) handleGetIncidentDiscordIntegration(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}
	incidentID := chi.URLParam(r, "id")
	integration, found, err := h.db.GetIncidentIntegration(incidentID, "discord")
	if err != nil {
		h.logger.Error("load incident discord integration failed", "error", err, "incidentId", incidentID)
		http.Error(w, "load integration", http.StatusInternalServerError)
		return
	}
	resp := incidentDiscordIntegrationResponse{}
	if found {
		resp.Integration = &integration
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleCreateIncidentDiscordIntegration(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(h.cfg.DiscordBotWorkerToken) == "" {
		http.Error(w, "discord bot worker not configured", http.StatusServiceUnavailable)
		return
	}

	incidentID := chi.URLParam(r, "id")
	incident, found, err := h.db.GetIncident(incidentID)
	if err != nil || !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if incident.Exposure == "internal" {
		http.Error(w, "internal incidents cannot use discord integration", http.StatusBadRequest)
		return
	}
	if incident.Status != "active" && incident.Status != "monitoring" {
		http.Error(w, "incident must be active or monitoring", http.StatusBadRequest)
		return
	}

	existing, hasExisting, err := h.db.GetIncidentIntegration(incidentID, "discord")
	if err != nil {
		h.logger.Error("load existing discord integration failed", "error", err)
		http.Error(w, "load integration", http.StatusInternalServerError)
		return
	}
	if hasExisting {
		switch existing.Status {
		case "active", "pending":
			writeJSON(w, http.StatusOK, incidentDiscordIntegrationResponse{Integration: &existing})
			return
		case "stopping":
			http.Error(w, "discord integration is stopping", http.StatusConflict)
			return
		}
	}

	config, err := h.buildIncidentDiscordConfig(incident)
	if err != nil {
		h.logger.Error("build discord integration config failed", "error", err)
		http.Error(w, "build integration config", http.StatusInternalServerError)
		return
	}

	integration := database.IncidentIntegration{
		ID:         existing.ID,
		IncidentID: incidentID,
		Kind:       "discord",
		Status:     "pending",
		Config:     config,
	}
	saved, err := h.db.UpsertIncidentIntegration(integration)
	if err != nil {
		h.logger.Error("save discord integration failed", "error", err)
		http.Error(w, "save integration", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(user.ID, "incident.discord_integration_requested", "incident", incidentID, map[string]any{
		"integrationId": saved.ID,
	})
	writeJSON(w, http.StatusCreated, incidentDiscordIntegrationResponse{Integration: &saved})
}

func (h *handler) handleDeleteIncidentDiscordIntegration(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	incidentID := chi.URLParam(r, "id")
	integration, found, err := h.db.GetIncidentIntegration(incidentID, "discord")
	if err != nil {
		h.logger.Error("load discord integration failed", "error", err)
		http.Error(w, "load integration", http.StatusInternalServerError)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if integration.Status == "stopped" || integration.Status == "failed" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.db.MarkDiscordIntegrationsStopping(incidentID); err != nil {
		h.logger.Error("mark discord integration stopping failed", "error", err)
		http.Error(w, "stop integration", http.StatusInternalServerError)
		return
	}
	_ = h.db.AppendAuditLog(user.ID, "incident.discord_integration_stopping", "incident", incidentID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleListDiscordIncidentTasks(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiscordBotWorker(w, r) {
		return
	}
	tasks, err := h.db.ListPendingDiscordIntegrationTasks(20)
	if err != nil {
		h.logger.Error("list discord incident tasks failed", "error", err)
		http.Error(w, "list tasks", http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []database.IncidentIntegration{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *handler) handleListActiveDiscordVoiceBridges(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiscordBotWorker(w, r) {
		return
	}
	integrations, err := h.db.ListActiveDiscordIntegrations(50)
	if err != nil {
		h.logger.Error("list active discord integrations failed", "error", err)
		http.Error(w, "list bridges", http.StatusInternalServerError)
		return
	}
	bridges := make([]map[string]string, 0)
	for _, item := range integrations {
		var cfg database.IncidentIntegrationConfig
		if len(item.Config) > 0 {
			_ = json.Unmarshal(item.Config, &cfg)
		}
		if cfg.VoiceChannelID == "" || cfg.StreamToken == "" {
			continue
		}
		bridges = append(bridges, map[string]string{
			"integrationId":  item.ID,
			"incidentId":     item.IncidentID,
			"voiceChannelId": cfg.VoiceChannelID,
			"streamToken":    cfg.StreamToken,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"bridges": bridges})
}

func (h *handler) handleCompleteDiscordIncidentTask(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiscordBotWorker(w, r) {
		return
	}
	taskID := chi.URLParam(r, "id")
	var req completeDiscordTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}

	existing, found, err := h.db.GetIncidentIntegrationByID(taskID)
	if err != nil || !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var cfg database.IncidentIntegrationConfig
	if len(existing.Config) > 0 {
		_ = json.Unmarshal(existing.Config, &cfg)
	}
	cfg.VoiceChannelID = strings.TrimSpace(req.VoiceChannelID)
	cfg.TextChannelID = strings.TrimSpace(req.TextChannelID)
	cfg.CategoryID = strings.TrimSpace(req.CategoryID)
	cfg.Error = ""
	raw, _ := json.Marshal(cfg)

	updated, err := h.db.UpdateIncidentIntegrationStatus(taskID, "active", raw)
	if err != nil {
		h.logger.Error("complete discord task failed", "error", err)
		http.Error(w, "complete task", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *handler) handleFailDiscordIncidentTask(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiscordBotWorker(w, r) {
		return
	}
	taskID := chi.URLParam(r, "id")
	var req failDiscordTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}

	existing, found, err := h.db.GetIncidentIntegrationByID(taskID)
	if err != nil || !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var cfg database.IncidentIntegrationConfig
	if len(existing.Config) > 0 {
		_ = json.Unmarshal(existing.Config, &cfg)
	}
	cfg.Error = strings.TrimSpace(req.Error)
	if cfg.Error == "" {
		cfg.Error = "discord integration failed"
	}
	raw, _ := json.Marshal(cfg)

	updated, err := h.db.UpdateIncidentIntegrationStatus(taskID, "failed", raw)
	if err != nil {
		h.logger.Error("fail discord task failed", "error", err)
		http.Error(w, "fail task", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *handler) handleStopDiscordIncidentTask(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiscordBotWorker(w, r) {
		return
	}
	taskID := chi.URLParam(r, "id")
	updated, err := h.db.UpdateIncidentIntegrationStatus(taskID, "stopped", nil)
	if err != nil {
		h.logger.Error("stop discord task failed", "error", err)
		http.Error(w, "stop task", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
