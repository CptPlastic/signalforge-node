package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const (
	listSourceSharesFailedMessage = "list source shares failed"
	querySourceSharesMessage      = "query source shares"
	missingSourceIDMessage        = "missing source id"
	loadIngestionSourceFailed     = "load ingestion source failed"
	loadSourceMessage             = "load source"
	sourceNotFoundMessage         = "source not found"
)

func (h *handler) handleListIngestionSources(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	sources, err := h.db.ListIngestionSources(false)
	if err != nil {
		h.logger.Error("list ingestion sources failed", "error", err)
		http.Error(w, "query ingestion sources", http.StatusInternalServerError)
		return
	}
	if user, ok := getAuthUser(r.Context()); ok {
		if !isAdmin(user) {
			sharedSourceIDs, shareErr := h.db.ListSharedSourceIDsForUser(user.ID)
			if shareErr != nil {
				h.logger.Error(listSourceSharesFailedMessage, "error", shareErr)
				http.Error(w, querySourceSharesMessage, http.StatusInternalServerError)
				return
			}
			sources = slices.DeleteFunc(sources, func(source database.IngestionSource) bool {
				return source.UserID != user.ID && !source.IsShared && !sharedSourceIDs[source.ID]
			})
		}
	} else {
		sources = slices.DeleteFunc(sources, func(source database.IngestionSource) bool {
			return source.UserID != ""
		})
	}
	if sources == nil {
		sources = make([]database.IngestionSource, 0)
	}
	writeJSON(w, http.StatusOK, sources)
}

type updateIngestionSourceRequest struct {
	Label    *string `json:"label"`
	Enabled  *bool   `json:"enabled"`
	IsShared *bool   `json:"isShared"`
}

func (h *handler) handleUpsertIngestionSource(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}

	var req updateIngestionSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s := database.IngestionSource{ID: id}
	if user, ok := getAuthUser(r.Context()); ok {
		if isGuest(user) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.UserID = user.ID
	}
	existing, found, err := h.db.GetIngestionSource(id)
	if err != nil {
		h.logger.Error(loadIngestionSourceFailed, "error", err)
		http.Error(w, loadSourceMessage, http.StatusInternalServerError)
		return
	}

	if found {
		if !h.canManageSource(w, r, existing) {
			return
		}
		s = existing
	} else {
		s.Label = id
		s.Enabled = true
	}

	if req.Label != nil {
		s.Label = strings.TrimSpace(*req.Label)
		if s.Label == "" {
			s.Label = id
		}
	}
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	if req.IsShared != nil {
		user, ok := getAuthUser(r.Context())
		if !ok || !isAdmin(user) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.IsShared = *req.IsShared
	}

	if user, ok := getAuthUser(r.Context()); ok {
		if s.UserID == "" {
			s.UserID = user.ID
		}
	}
	if err := h.db.UpsertIngestionSource(s); err != nil {
		h.logger.Error("upsert ingestion source failed", "error", err)
		http.Error(w, "save ingestion source", http.StatusInternalServerError)
		return
	}

	// Return the full record (with live metrics) rather than the partial request body.
	full, found, err := h.db.GetIngestionSource(id)
	if err != nil || !found {
		writeJSON(w, http.StatusOK, s)
		return
	}
	writeJSON(w, http.StatusOK, full)
}

// handleDeleteIngestionSource deletes an ingestion source and its associated API keys.
func (h *handler) handleDeleteIngestionSource(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	sourceID := r.PathValue("id")
	if sourceID == "" {
		http.Error(w, missingSourceIDMessage, http.StatusBadRequest)
		return
	}
	source, found, err := h.db.GetIngestionSource(sourceID)
	if err != nil {
		h.logger.Error(loadIngestionSourceFailed, "error", err)
		http.Error(w, loadSourceMessage, http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, sourceNotFoundMessage, http.StatusNotFound)
		return
	}
	if !h.canManageSource(w, r, source) {
		return
	}

	deleted, err := h.db.DeleteIngestionSource(sourceID)
	if err != nil {
		h.logger.Error("delete ingestion source failed", "source_id", sourceID, "error", err)
		http.Error(w, "delete source", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, sourceNotFoundMessage, http.StatusNotFound)
		return
	}
	if user, ok := getAuthUser(r.Context()); ok {
		_ = h.db.AppendAuditLog(user.ID, "source.soft_deleted", "source", sourceID, map[string]any{})
	}

	type wsEvent struct {
		Type     string `json:"type"`
		SourceID string `json:"sourceId"`
	}
	h.hub.broadcast(wsEvent{Type: "source_deleted", SourceID: sourceID})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
