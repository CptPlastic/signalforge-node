package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const stalePendingTaskSeconds = 300

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

func (h *handler) nextDiscordBotInstance() string {
	instances := h.cfg.DiscordBotInstances
	if len(instances) == 0 {
		return "signal1"
	}
	n := h.discordBotCounter.Add(1) - 1
	return instances[n%int64(len(instances))]
}

func (h *handler) upsertDiscordIntegration(incident database.Incident, existing database.IncidentIntegration, hasExisting bool, botInstanceID string) (database.IncidentIntegration, error) {
	config, err := h.buildIncidentDiscordConfig(incident)
	if err != nil {
		return database.IncidentIntegration{}, err
	}
	integration := database.IncidentIntegration{
		ID:            existing.ID,
		IncidentID:    incident.ID,
		Kind:          "discord",
		Status:        "pending",
		BotInstanceID: botInstanceID,
		Config:        config,
	}
	if hasExisting && existing.Status == "failed" {
		integration.ID = existing.ID
	}
	return h.db.UpsertIncidentIntegration(integration)
}

func (h *handler) discordQueueSkipReason(incident database.Incident) string {
	if strings.TrimSpace(h.cfg.DiscordBotWorkerToken) == "" {
		return "DISCORD_BOT_WORKER_TOKEN not set on api — add same secret to api + discord-bot in Portainer, then redeploy"
	}
	if incident.Exposure == "internal" {
		return "internal incidents skip Discord"
	}
	if incident.Status != "active" && incident.Status != "monitoring" {
		return "incident must be active before Discord rooms can sync"
	}
	return ""
}

func (h *handler) shouldAutoQueueDiscord(incident database.Incident) bool {
	return h.discordQueueSkipReason(incident) == ""
}

func (h *handler) queueDiscordIntegrationForIncident(incidentID, userID string) (bool, string) {
	incident, found, err := h.db.GetIncident(incidentID)
	if err != nil || !found {
		return false, "incident not found"
	}
	if reason := h.discordQueueSkipReason(incident); reason != "" {
		h.logger.Debug("discord integration not queued", "incidentId", incidentID, "reason", reason)
		return false, reason
	}
	existing, hasExisting, err := h.db.GetIncidentIntegration(incidentID, "discord")
	if err != nil {
		h.logger.Error("load discord integration for auto queue failed", "error", err, "incidentId", incidentID)
		return false, "could not load discord integration"
	}
	if hasExisting {
		switch existing.Status {
		case "active":
			return true, ""
		case "stopping":
			return false, ""
		case "pending":
			staleCutoff := time.Now().Unix() - stalePendingTaskSeconds
			if existing.UpdatedAt > staleCutoff {
				// Still fresh — another bot should pick it up.
				return true, ""
			}
			// Stale pending — bot may be down; fall through to reassign.
			h.logger.Info("reassigning stale pending discord integration",
				"incidentId", incidentID, "integrationId", existing.ID,
				"updatedAt", existing.UpdatedAt, "botInstanceId", existing.BotInstanceID)
		}
	}
	botInstanceID := h.nextDiscordBotInstance()
	saved, err := h.upsertDiscordIntegration(incident, existing, hasExisting, botInstanceID)
	if err != nil {
		h.logger.Error("auto queue discord integration failed", "error", err, "incidentId", incidentID)
		return false, "could not save discord integration"
	}
	if userID != "" {
		_ = h.db.AppendAuditLog(userID, "incident.discord_integration_auto_queued", "incident", incidentID, map[string]any{
			"integrationId": saved.ID,
		})
	}
	h.logger.Info("discord integration queued", "incidentId", incidentID, "integrationId", saved.ID)
	return true, ""
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
		case "active":
			writeJSON(w, http.StatusOK, incidentDiscordIntegrationResponse{Integration: &existing})
			return
		case "stopping":
			http.Error(w, "discord integration is stopping", http.StatusConflict)
			return
		case "pending":
			staleCutoff := time.Now().Unix() - stalePendingTaskSeconds
			if existing.UpdatedAt > staleCutoff {
				writeJSON(w, http.StatusOK, incidentDiscordIntegrationResponse{Integration: &existing})
				return
			}
			h.logger.Info("reassigning stale pending discord integration on user request",
				"incidentId", incidentID, "integrationId", existing.ID,
				"updatedAt", existing.UpdatedAt, "botInstanceId", existing.BotInstanceID)
		}
	}

	botInstanceID := h.nextDiscordBotInstance()
	saved, err := h.upsertDiscordIntegration(incident, existing, hasExisting, botInstanceID)
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
	botInstanceID := strings.TrimSpace(r.URL.Query().Get("bot_instance_id"))
	tasks, err := h.db.ListPendingDiscordIntegrationTasks(botInstanceID, 20)
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
	botInstanceID := strings.TrimSpace(r.URL.Query().Get("bot_instance_id"))
	integrations, err := h.db.ListActiveDiscordIntegrations(botInstanceID, 50)
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
		if cfg.StreamToken == "" && item.IncidentID != "" {
			incident, found, incErr := h.db.GetIncident(item.IncidentID)
			if incErr == nil && found {
				token, tokenErr := h.ensureIncidentStreamToken(incident)
				if tokenErr != nil {
					h.logger.Warn("ensure stream token for voice bridge failed", "error", tokenErr, "incidentId", item.IncidentID)
				} else if token != "" {
					cfg.StreamToken = token
					raw, _ := json.Marshal(cfg)
					if _, updErr := h.db.UpdateIncidentIntegrationStatus(item.ID, item.Status, raw); updErr != nil {
						h.logger.Warn("persist stream token on integration failed", "error", updErr, "integrationId", item.ID)
					}
				}
			}
		}
		if cfg.VoiceChannelID == "" {
			h.logger.Warn("discord voice bridge skipped — no voice channel", "integrationId", item.ID, "incidentId", item.IncidentID)
			continue
		}
		if cfg.StreamToken == "" {
			h.logger.Warn("discord voice bridge skipped — no stream token", "integrationId", item.ID, "incidentId", item.IncidentID)
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
	if cfg.StreamToken == "" && existing.IncidentID != "" {
		incident, found, incErr := h.db.GetIncident(existing.IncidentID)
		if incErr == nil && found {
			token, tokenErr := h.ensureIncidentStreamToken(incident)
			if tokenErr != nil {
				h.logger.Error("ensure stream token on discord complete failed", "error", tokenErr, "incidentId", existing.IncidentID)
			} else {
				cfg.StreamToken = token
			}
		}
	}
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

func (h *handler) handleReconcileDiscordIncidents(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireIncidentManager(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(h.cfg.DiscordBotWorkerToken) == "" {
		http.Error(w, "discord bot worker not configured", http.StatusServiceUnavailable)
		return
	}

	missing, err := h.db.ListActiveIncidentsMissingDiscord(100)
	if err != nil {
		h.logger.Error("list incidents missing discord failed", "error", err)
		http.Error(w, "list incidents", http.StatusInternalServerError)
		return
	}

	queued := 0
	skipped := 0
	for _, incident := range missing {
		ok, _ := h.queueDiscordIntegrationForIncident(incident.ID, user.ID)
		if ok {
			queued++
		} else {
			skipped++
		}
	}

	retried := 0
	failedItems, err := h.db.ListFailedDiscordIntegrations(100)
	if err != nil {
		h.logger.Error("list failed discord integrations failed", "error", err)
	} else {
		for _, item := range failedItems {
			incident, found, incErr := h.db.GetIncident(item.IncidentID)
			if incErr != nil || !found {
				continue
			}
			if ok, _ := h.queueDiscordIntegrationForIncident(incident.ID, user.ID); ok {
				retried++
			}
		}
	}

	pending, _ := h.db.CountDiscordIntegrationsByStatus("pending")
	failed, _ := h.db.CountDiscordIntegrationsByStatus("failed")
	active, _ := h.db.CountDiscordIntegrationsByStatus("active")

	_ = h.db.AppendAuditLog(user.ID, "incident.discord_reconcile", "hub", "", map[string]any{
		"queued":  queued,
		"retried": retried,
		"skipped": skipped,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"queued":       queued,
		"retried":      retried,
		"skipped":      skipped,
		"pendingTasks": pending,
		"activeTasks":  active,
		"failedTasks":  failed,
	})
}
