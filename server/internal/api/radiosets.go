package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

type radioSetRequest struct {
	Name       string `json:"name"`
	Talkgroups []int  `json:"talkgroups"`
}

// resolveRadioSet returns a set the caller may manage: own sets for everyone, any set for admins.
func (h *handler) resolveRadioSet(id string, user authUser) (database.RadioSet, bool, error) {
	if isAdmin(user) {
		return h.db.GetRadioSetForPTT(id)
	}
	return h.db.GetRadioSet(id, user.ID)
}

func (h *handler) handleListRadioSets(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	var sets []database.RadioSet
	var err error
	if isAdmin(user) {
		sets, err = h.db.ListAllRadioSets()
	} else {
		sets, err = h.db.ListRadioSets(user.ID)
	}
	if err != nil {
		h.logger.Error("list radio sets failed", "error", err)
		http.Error(w, "query radio sets", http.StatusInternalServerError)
		return
	}
	if isAdmin(user) {
		for i := range sets {
			sourceIDs, sourceErr := h.db.ListSourceIDsForTalkgroups(sets[i].Talkgroups)
			if sourceErr != nil {
				h.logger.Error("list radio set sources failed", "error", sourceErr)
				http.Error(w, "query radio set sources", http.StatusInternalServerError)
				return
			}
			sets[i].SourceIDs = sourceIDs
		}
	}
	writeJSON(w, http.StatusOK, sets)
}

func (h *handler) handleCreateRadioSet(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	var req radioSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Talkgroups == nil {
		req.Talkgroups = []int{}
	}
	rs, err := h.db.CreateRadioSet(user.ID, req.Name, req.Talkgroups)
	if err != nil {
		h.logger.Error("create radio set failed", "error", err)
		http.Error(w, "create radio set", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, rs)
}

func (h *handler) handleGetRadioSet(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	rs, found, err := h.resolveRadioSet(id, user)
	if err != nil {
		h.logger.Error("get radio set failed", "error", err)
		http.Error(w, "query radio set", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (h *handler) handleUpdateRadioSet(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req radioSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Talkgroups == nil {
		req.Talkgroups = []int{}
	}
	rs, found, err := h.resolveRadioSet(id, user)
	if err != nil {
		h.logger.Error("resolve radio set for update failed", "error", err)
		http.Error(w, "query radio set", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.db.UpdateRadioSet(id, rs.UserID, req.Name, req.Talkgroups); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.logger.Error("update radio set failed", "error", err)
		http.Error(w, "update radio set", http.StatusInternalServerError)
		return
	}
	rs, _, _ = h.resolveRadioSet(id, user)
	writeJSON(w, http.StatusOK, rs)
}

func (h *handler) handleDeleteRadioSet(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	rs, found, err := h.resolveRadioSet(id, user)
	if err != nil {
		h.logger.Error("resolve radio set for delete failed", "error", err)
		http.Error(w, "query radio set", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.db.DeleteRadioSet(id, rs.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.logger.Error("delete radio set failed", "error", err)
		http.Error(w, "delete radio set", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleGenerateShareToken creates a share token for a radio set, returning the updated set.
func (h *handler) handleGenerateShareToken(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	rs, found, err := h.resolveRadioSet(id, user)
	if err != nil {
		h.logger.Error("get radio set before share token failed", "error", err)
		http.Error(w, "query radio set", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if rs.ShareToken != nil && strings.TrimSpace(*rs.ShareToken) != "" {
		writeJSON(w, http.StatusOK, rs)
		return
	}

	token := database.NewShareToken()
	if err := h.db.SetRadioSetShareToken(id, rs.UserID, token); err != nil {
		h.logger.Error("set share token failed", "error", err)
		http.Error(w, "set share token", http.StatusInternalServerError)
		return
	}
	rs, ok2, err := h.resolveRadioSet(id, user)
	if err != nil || !ok2 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// handleRevokeShareToken removes the share token from a radio set.
func (h *handler) handleRevokeShareToken(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	rs, found, err := h.resolveRadioSet(id, user)
	if err != nil {
		h.logger.Error("resolve radio set for revoke failed", "error", err)
		http.Error(w, "query radio set", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.db.ClearRadioSetShareToken(id, rs.UserID); err != nil {
		h.logger.Error("clear share token failed", "error", err)
		http.Error(w, "clear share token", http.StatusInternalServerError)
		return
	}
	rs, ok2, err := h.resolveRadioSet(id, user)
	if err != nil || !ok2 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}
