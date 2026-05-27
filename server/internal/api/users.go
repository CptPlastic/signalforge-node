package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const (
	updateUserError  = "update user"
	deleteUserError  = "delete user"
	userNotFoundText = "user not found"
)

type updateUserRequest struct {
	Role              string `json:"role"`
	Status            string `json:"status"`
	TxEnabled         *bool  `json:"txEnabled,omitempty"`
	DispatcherEnabled *bool  `json:"dispatcherEnabled,omitempty"`
}

func normalizeUserRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "user", "guest":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return ""
	}
}

func normalizeUserStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "pending", "disabled":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
}

func findUserByID(users []database.User, userID string) (database.User, bool) {
	for _, user := range users {
		if user.ID == userID {
			return user, true
		}
	}
	return database.User{}, false
}

func (h *handler) ensureActiveAdminCanChange(w http.ResponseWriter, target database.User, role, status, operation string) bool {
	keepsActiveAdmin := role == "admin" && status == "active"
	if target.Role != "admin" || target.Status != "active" || keepsActiveAdmin {
		return true
	}
	return h.ensureMoreThanOneActiveAdmin(w, operation, "cannot remove the last active admin")
}

func (h *handler) ensureMoreThanOneActiveAdmin(w http.ResponseWriter, operation, message string) bool {
	adminCount, err := h.db.CountActiveAdmins()
	if err != nil {
		h.logger.Error("count admins failed", "error", err)
		http.Error(w, operation, http.StatusInternalServerError)
		return false
	}
	if adminCount <= 1 {
		http.Error(w, message, http.StatusBadRequest)
		return false
	}
	return true
}

func (h *handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	users, err := h.db.ListUsers()
	if err != nil {
		h.logger.Error("list users failed", "error", err)
		http.Error(w, "list users", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *handler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	userID := r.PathValue("id")
	if strings.TrimSpace(userID) == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	role := normalizeUserRole(req.Role)
	status := normalizeUserStatus(req.Status)
	if role == "" || status == "" {
		http.Error(w, "invalid role or status", http.StatusBadRequest)
		return
	}
	if admin.ID == userID && status != "active" {
		http.Error(w, "cannot disable current admin session user", http.StatusBadRequest)
		return
	}
	users, err := h.db.ListUsers()
	if err != nil {
		h.logger.Error("list users for validation failed", "error", err)
		http.Error(w, updateUserError, http.StatusInternalServerError)
		return
	}
	target, found := findUserByID(users, userID)
	if !found {
		http.Error(w, userNotFoundText, http.StatusNotFound)
		return
	}
	if !h.ensureActiveAdminCanChange(w, target, role, status, updateUserError) {
		return
	}

	updated, err := h.db.UpdateUserRoleStatus(userID, role, status)
	if err != nil {
		h.logger.Error("update user failed", "user_id", userID, "error", err)
		http.Error(w, updateUserError, http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, userNotFoundText, http.StatusNotFound)
		return
	}
	auditMeta := map[string]any{
		"role":   role,
		"status": status,
	}
	if req.TxEnabled != nil {
		if _, txErr := h.db.SetUserTxEnabled(userID, *req.TxEnabled); txErr != nil {
			h.logger.Error("update user tx_enabled failed", "user_id", userID, "error", txErr)
			http.Error(w, updateUserError, http.StatusInternalServerError)
			return
		}
		auditMeta["txEnabled"] = *req.TxEnabled
	}
	if req.DispatcherEnabled != nil {
		if _, dispErr := h.db.SetUserDispatcherEnabled(userID, *req.DispatcherEnabled); dispErr != nil {
			h.logger.Error("update user dispatcher_enabled failed", "user_id", userID, "error", dispErr)
			http.Error(w, updateUserError, http.StatusInternalServerError)
			return
		}
		auditMeta["dispatcherEnabled"] = *req.DispatcherEnabled
	}
	_ = h.db.AppendAuditLog(admin.ID, "admin.user_updated", "user", userID, auditMeta)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	userID := r.PathValue("id")
	if strings.TrimSpace(userID) == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	if admin.ID == userID {
		http.Error(w, "cannot delete current admin session user", http.StatusBadRequest)
		return
	}
	users, err := h.db.ListUsers()
	if err != nil {
		h.logger.Error("list users for deletion failed", "error", err)
		http.Error(w, deleteUserError, http.StatusInternalServerError)
		return
	}
	target, found := findUserByID(users, userID)
	if !found {
		http.Error(w, userNotFoundText, http.StatusNotFound)
		return
	}
	if target.Role == "admin" && target.Status == "active" {
		if !h.ensureMoreThanOneActiveAdmin(w, deleteUserError, "cannot delete the last active admin") {
			return
		}
	}

	deleted, err := h.db.DeleteUser(userID)
	if err != nil {
		h.logger.Error("delete user failed", "user_id", userID, "error", err)
		http.Error(w, deleteUserError, http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, userNotFoundText, http.StatusNotFound)
		return
	}
	_ = h.db.AppendAuditLog(admin.ID, "admin.user_deleted", "user", userID, map[string]any{})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
