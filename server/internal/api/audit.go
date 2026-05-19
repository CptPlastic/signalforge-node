package api

import (
	"net/http"
	"strconv"
)

func (h *handler) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			limit = parsed
		}
	}

	logs, err := h.db.ListAuditLogs(limit)
	if err != nil {
		h.logger.Error("list audit logs failed", "error", err)
		http.Error(w, "list audit logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, logs)
}
