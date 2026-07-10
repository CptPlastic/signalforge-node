package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

var (
	BuildVersion = "0.1.0"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

type handler struct {
	cfg                config.Config
	db                 *database.DB
	hub                *hub
	streamHub          *streamHub
	logger             *slog.Logger
	discordBotCounter  atomic.Int64
}

type updateManifest struct {
	Product        string            `json:"product"`
	Channel        string            `json:"channel"`
	Version        string            `json:"version"`
	Commit         string            `json:"commit"`
	ShortCommit    string            `json:"shortCommit"`
	ImageRegistry  string            `json:"imageRegistry"`
	ImageNamespace string            `json:"imageNamespace"`
	ImageTag       string            `json:"imageTag"`
	PublishedAt    string            `json:"publishedAt"`
	Images         map[string]string `json:"images"`
}

type updateCheckResponse struct {
	Current         map[string]string `json:"current"`
	Latest          *updateManifest   `json:"latest,omitempty"`
	UpdateAvailable bool              `json:"updateAvailable"`
	CheckURL        string            `json:"checkUrl"`
	Error           string            `json:"error,omitempty"`
}

func newHandler(cfg config.Config, db *database.DB, h *hub, sh *streamHub, logger *slog.Logger) *handler {
	return &handler{cfg: cfg, db: db, hub: h, streamHub: sh, logger: logger}
}

func (h *handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *handler) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":               BuildVersion,
		"commit":                BuildCommit,
		"buildDate":             BuildDate,
		"environment":           h.cfg.AppEnv,
		"stackId":               h.cfg.StackID,
		"deployTag":             h.cfg.DeployTag,
		"uploadKeyRequired":     uploadKeyRequired,
		"transcriptionEnabled":   strings.TrimSpace(h.cfg.TranscriptionWorkerToken) != "",
		"passwordLoginEnabled":    h.cfg.AuthPasswordLoginEnabled,
		"emailDeliveryConfigured": h.emailDeliveryConfigured(),
		"callRetentionDays":       h.cfg.CallRetentionDays,
		"callArchiveDir":          h.cfg.CallArchiveDir,
		"callArchiveS3Uri":        h.cfg.CallArchiveS3URI,
		"callArchiveLoopEnabled":  h.callArchiveLoopEnabled(),
		"incidentArchiveDays":     h.cfg.IncidentArchiveDays,
		"incidentCleanupEnabled":  h.incidentCleanupLoopEnabled(),
	})
}

func (h *handler) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	current := map[string]string{
		"version":   BuildVersion,
		"commit":    BuildCommit,
		"deployTag": h.cfg.DeployTag,
		"stackId":   h.cfg.StackID,
	}
	response := updateCheckResponse{
		Current:  current,
		CheckURL: h.cfg.UpdateCheckURL,
	}

	if h.cfg.UpdateCheckURL == "" {
		response.Error = "update checks disabled"
		writeJSON(w, http.StatusOK, response)
		return
	}

	manifest, err := fetchUpdateManifest(r, h.cfg.UpdateCheckURL)
	if err != nil {
		h.logger.Warn("update check failed", "url", h.cfg.UpdateCheckURL, "error", err)
		response.Error = err.Error()
		writeJSON(w, http.StatusOK, response)
		return
	}

	response.Latest = manifest
	response.UpdateAvailable = isUpdateAvailable(BuildCommit, h.cfg.DeployTag, manifest)
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}
	h.hub.handleWebSocket(h, w, r)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func fetchUpdateManifest(r *http.Request, checkURL string) (*updateManifest, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, checkURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("update manifest unavailable: %s", message)
	}

	var manifest updateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&manifest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifest.Commit) == "" && strings.TrimSpace(manifest.ImageTag) == "" {
		return nil, fmt.Errorf("update manifest missing commit and image tag")
	}
	return &manifest, nil
}

func isUpdateAvailable(currentCommit, currentTag string, manifest *updateManifest) bool {
	latestCommit := strings.TrimSpace(manifest.Commit)
	currentCommit = strings.TrimSpace(currentCommit)
	if latestCommit != "" && currentCommit != "" && currentCommit != "unknown" {
		return !strings.EqualFold(currentCommit, latestCommit)
	}

	latestTag := strings.TrimSpace(manifest.ImageTag)
	currentTag = strings.TrimSpace(currentTag)
	return latestTag != "" && currentTag != "" && currentTag != "unknown" && !strings.EqualFold(currentTag, latestTag)
}
