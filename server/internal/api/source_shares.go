package api

import (
	"encoding/json"
	"net/http"
)

type sourceSharesRequest struct {
	UserIDs []string `json:"userIds"`
}

func (h *handler) handleListSourceShares(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	sourceID := r.PathValue("id")
	if sourceID == "" {
		http.Error(w, missingSourceIDMessage, http.StatusBadRequest)
		return
	}
	if _, found, err := h.db.GetIngestionSource(sourceID); err != nil {
		h.logger.Error(loadIngestionSourceFailed, "error", err)
		http.Error(w, loadSourceMessage, http.StatusInternalServerError)
		return
	} else if !found {
		http.Error(w, sourceNotFoundMessage, http.StatusNotFound)
		return
	}
	userIDs, err := h.db.ListSourceShareUserIDs(sourceID)
	if err != nil {
		h.logger.Error(listSourceSharesFailedMessage, "error", err)
		http.Error(w, querySourceSharesMessage, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"userIds": userIDs})
}

func (h *handler) handleUpdateSourceShares(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	sourceID := r.PathValue("id")
	if sourceID == "" {
		http.Error(w, missingSourceIDMessage, http.StatusBadRequest)
		return
	}
	if _, found, err := h.db.GetIngestionSource(sourceID); err != nil {
		h.logger.Error(loadIngestionSourceFailed, "error", err)
		http.Error(w, loadSourceMessage, http.StatusInternalServerError)
		return
	} else if !found {
		http.Error(w, sourceNotFoundMessage, http.StatusNotFound)
		return
	}
	var req sourceSharesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.db.SetSourceShareUserIDs(sourceID, req.UserIDs); err != nil {
		h.logger.Error("update source shares failed", "error", err)
		http.Error(w, "save source shares", http.StatusInternalServerError)
		return
	}
	_ = h.db.AppendAuditLog(admin.ID, "source.shares_updated", "source", sourceID, map[string]any{"userIds": req.UserIDs})
	userIDs, err := h.db.ListSourceShareUserIDs(sourceID)
	if err != nil {
		h.logger.Error(listSourceSharesFailedMessage, "error", err)
		http.Error(w, querySourceSharesMessage, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"userIds": userIDs})
}
