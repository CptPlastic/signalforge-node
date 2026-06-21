package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

type discordHeartbeatRequest struct {
	BotUserTag     string `json:"botUserTag"`
	GuildID        string `json:"guildId"`
	GuildName      string `json:"guildName"`
	CommandCount   int    `json:"commandCount"`
	WelcomeEnabled bool   `json:"welcomeEnabled"`
}

type discordStatusResponse struct {
	Configured bool                      `json:"configured"`
	Online     bool                      `json:"online"`
	Status     *database.DiscordBotStatus `json:"status,omitempty"`
}

func (h *handler) requireDiscordBotWorker(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(h.cfg.DiscordBotWorkerToken)
	if expected == "" {
		http.Error(w, "discord bot worker disabled", http.StatusNotFound)
		return false
	}
	actual := strings.TrimSpace(r.Header.Get("X-Discord-Bot-Token"))
	if actual == "" {
		actual = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	}
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (h *handler) handleDiscordHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiscordBotWorker(w, r) {
		return
	}
	var req discordHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}
	status := database.DiscordBotStatus{
		BotUserTag:     strings.TrimSpace(req.BotUserTag),
		GuildID:        strings.TrimSpace(req.GuildID),
		GuildName:      strings.TrimSpace(req.GuildName),
		CommandCount:   req.CommandCount,
		WelcomeEnabled: req.WelcomeEnabled,
		LastSeenAt:     time.Now().Unix(),
	}
	if err := h.db.UpsertDiscordBotStatus(status); err != nil {
		h.logger.Error("discord heartbeat failed", "error", err)
		http.Error(w, "save status", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleGetDiscordStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	resp := discordStatusResponse{
		Configured: strings.TrimSpace(h.cfg.DiscordBotWorkerToken) != "",
	}
	status, err := h.db.GetDiscordBotStatus()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		h.logger.Error("load discord status failed", "error", err)
		http.Error(w, "load discord status", http.StatusInternalServerError)
		return
	}
	resp.Status = status
	if status.LastSeenAt > 0 {
		resp.Online = time.Now().Unix()-status.LastSeenAt <= 120
	}
	writeJSON(w, http.StatusOK, resp)
}
