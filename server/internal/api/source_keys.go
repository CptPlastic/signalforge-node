package api

import (
	"net/http"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

// handleGenerateSourceKey creates a new API key for a source.
func (h *handler) handleGenerateSourceKey(w http.ResponseWriter, r *http.Request) {
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

	key, err := h.db.GenerateSourceKey(source)
	if err != nil {
		h.logger.Error("generate source key failed", "error", err)
		http.Error(w, "generate key", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, key)
}

// handleListSourceKeys returns all API keys for a source.
func (h *handler) handleListSourceKeys(w http.ResponseWriter, r *http.Request) {
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

	keys, err := h.db.ListSourceKeys(sourceID)
	if err != nil {
		h.logger.Error("list source keys failed", "error", err)
		http.Error(w, "list keys", http.StatusInternalServerError)
		return
	}
	if keys == nil {
		keys = make([]database.SourceAPIKey, 0)
	}

	writeJSON(w, http.StatusOK, keys)
}

// handleRevokeSourceKey deletes an API key.
func (h *handler) handleRevokeSourceKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	sourceID := r.PathValue("id")
	keyID := r.PathValue("keyId")
	if sourceID == "" || keyID == "" {
		http.Error(w, "missing source id or key id", http.StatusBadRequest)
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

	if err := h.db.RevokeSourceKey(sourceID, keyID); err != nil {
		h.logger.Error("revoke source key failed", "error", err)
		http.Error(w, "revoke key", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
