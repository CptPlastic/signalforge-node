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
	cfg               config.Config
	db                *database.DB
	hub               *hub
	streamHub         *streamHub
	mp3Broker         *publicMP3Broker
	logger            *slog.Logger
	discordBotCounter atomic.Int64
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
	handle := &handler{cfg: cfg, db: db, hub: h, streamHub: sh, logger: logger}
	handle.mp3Broker = newPublicMP3Broker(sh, logger)
	return handle
}

func (h *handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *handler) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":                 BuildVersion,
		"commit":                  BuildCommit,
		"buildDate":               BuildDate,
		"environment":             h.cfg.AppEnv,
		"stackId":                 h.cfg.StackID,
		"deployTag":               h.cfg.DeployTag,
		"uploadKeyRequired":       uploadKeyRequired,
		"transcriptionEnabled":    strings.TrimSpace(h.cfg.TranscriptionWorkerToken) != "",
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
		"buildDate": BuildDate,
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
	response.UpdateAvailable = isUpdateAvailable(BuildCommit, h.cfg.DeployTag, BuildDate, manifest)
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

func isUpdateAvailable(currentCommit, currentTag, currentBuildDate string, manifest *updateManifest) bool {
	if manifest == nil || sameRelease(currentCommit, currentTag, manifest) {
		return false
	}
	publishedAt, ok := parseBuildTime(manifest.PublishedAt)
	if !ok {
		return false
	}
	builtAt, ok := parseBuildTime(currentBuildDate)
	if !ok {
		return false
	}
	// Short SHAs are not ordered. A manifest is an update only when it was
	// published after this image was built, so an older published tag cannot
	// prompt a downgrade.
	return publishedAt.After(builtAt)
}

func sameRelease(currentCommit, currentTag string, manifest *updateManifest) bool {
	latest := []string{manifest.Commit, manifest.ImageTag, manifest.ShortCommit, manifest.Version}
	for _, current := range []string{currentCommit, currentTag} {
		for _, candidate := range latest {
			if sameRevision(current, candidate) {
				return true
			}
		}
	}
	return false
}

func sameRevision(left, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if left == "" || right == "" || left == "unknown" || right == "unknown" || left == "latest" || right == "latest" {
		return false
	}
	if left == right {
		return true
	}
	shorter, longer := left, right
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	return len(shorter) >= 7 && strings.HasPrefix(longer, shorter)
}

func parseBuildTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "unknown") {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
