package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type updateUserRequest struct {
	Role   string `json:"role"`
	Status string `json:"status"`
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
	case "active", "disabled":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
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
		http.Error(w, "update user", http.StatusInternalServerError)
		return
	}
	var targetRole string
	var targetStatus string
	for _, user := range users {
		if user.ID == userID {
			targetRole = user.Role
			targetStatus = user.Status
			break
		}
	}
	if targetRole == "" {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if targetRole == "admin" && targetStatus == "active" && (role != "admin" || status != "active") {
		adminCount, err := h.db.CountActiveAdmins()
		if err != nil {
			h.logger.Error("count admins failed", "error", err)
			http.Error(w, "update user", http.StatusInternalServerError)
			return
		}
		if adminCount <= 1 {
			http.Error(w, "cannot remove the last active admin", http.StatusBadRequest)
			return
		}
	}

	updated, err := h.db.UpdateUserRoleStatus(userID, role, status)
	if err != nil {
		h.logger.Error("update user failed", "user_id", userID, "error", err)
		http.Error(w, "update user", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	_ = h.db.AppendAuditLog(admin.ID, "admin.user_updated", "user", userID, map[string]any{
		"role":   role,
		"status": status,
	})

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
		http.Error(w, "delete user", http.StatusInternalServerError)
		return
	}
	for _, user := range users {
		if user.ID != userID {
			continue
		}
		if user.Role == "admin" && user.Status == "active" {
			adminCount, err := h.db.CountActiveAdmins()
			if err != nil {
				h.logger.Error("count admins failed", "error", err)
				http.Error(w, "delete user", http.StatusInternalServerError)
				return
			}
			if adminCount <= 1 {
				http.Error(w, "cannot delete the last active admin", http.StatusBadRequest)
				return
			}
		}
		break
	}

	deleted, err := h.db.DeleteUser(userID)
	if err != nil {
		h.logger.Error("delete user failed", "user_id", userID, "error", err)
		http.Error(w, "delete user", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	_ = h.db.AppendAuditLog(admin.ID, "admin.user_deleted", "user", userID, map[string]any{})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
