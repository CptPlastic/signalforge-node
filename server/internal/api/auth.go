package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const (
	sessionCookieName = "p7_session"
)

type authContextKey string

const userContextKey authContextKey = "user"

type authUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	TxEnabled bool   `json:"txEnabled"`
}

type requestMagicLinkBody struct {
	Email string `json:"email"`
}

func toAuthUser(user database.User) authUser {
	return authUser{ID: user.ID, Email: user.Email, Role: user.Role, TxEnabled: user.TxEnabled}
}

func getAuthUser(ctx context.Context) (authUser, bool) {
	user, ok := ctx.Value(userContextKey).(authUser)
	return user, ok
}

func isAdmin(user authUser) bool {
	return user.Role == "admin"
}

func isGuest(user authUser) bool {
	return user.Role == "guest"
}

func (h *handler) requireAuthenticated(w http.ResponseWriter, r *http.Request) (authUser, bool) {
	user, ok := getAuthUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return authUser{}, false
	}
	return user, true
}

func (h *handler) requireAdmin(w http.ResponseWriter, r *http.Request) (authUser, bool) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return authUser{}, false
	}
	if !isAdmin(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return authUser{}, false
	}
	return user, true
}

func (h *handler) canManageSource(w http.ResponseWriter, r *http.Request, source database.IngestionSource) bool {
	user, ok := getAuthUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if strings.TrimSpace(source.UserID) == "" {
		if isAdmin(user) {
			return true
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	if user.ID != source.UserID && !isAdmin(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	if isGuest(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (h *handler) withUserContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Session token is delivered either as an HttpOnly cookie (web) or as
		// an `Authorization: Bearer <token>` header (native clients without a
		// cookie jar — currently the React Native mobile app).
		sessionToken := ""
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			sessionToken = strings.TrimSpace(cookie.Value)
		}
		if sessionToken == "" {
			if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
				sessionToken = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			}
		}
		if sessionToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		user, found, err := h.db.GetUserBySessionToken(sessionToken)
		if err != nil {
			h.logger.Error("session lookup failed", "error", err)
			next.ServeHTTP(w, r)
			return
		}
		if !found {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, toAuthUser(user))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) handleRequestMagicLink(w http.ResponseWriter, r *http.Request) {
	var req requestMagicLinkBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}

	token, user, err := h.db.CreateMagicLinkToken(email, 15*time.Minute)
	if err != nil {
		h.writeCreateMagicLinkError(w, user, err)
		return
	}
	verifyURL := h.magicLinkVerifyURL(r, token)
	if err := h.sendMagicLinkEmail(r.Context(), user.Email, verifyURL, token); err != nil {
		h.logger.Error("send magic link email failed", "error", err, "email", user.Email)
		http.Error(w, h.magicLinkSendErrorMessage(err), http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"status": "ok",
		"user":   toAuthUser(user),
	}
	_ = h.db.AppendAuditLog(user.ID, "auth.magic_link_requested", "user", user.ID, map[string]any{
		"email": user.Email,
	})
	if h.cfg.AppEnv != "production" {
		response["token"] = token
		response["verifyUrl"] = verifyURL
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *handler) writeCreateMagicLinkError(w http.ResponseWriter, user database.User, err error) {
	if err == database.ErrPendingUser {
		_ = h.db.AppendAuditLog(user.ID, "auth.approval_requested", "user", user.ID, map[string]any{
			"email": user.Email,
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "pending",
			"user":   toAuthUser(user),
		})
		return
	}
	if err == database.ErrInactiveUser {
		http.Error(w, "account disabled", http.StatusForbidden)
		return
	}
	h.logger.Error("create magic link failed", "error", err)
	http.Error(w, "create magic link", http.StatusInternalServerError)
}

func (h *handler) magicLinkSendErrorMessage(err error) string {
	if h.cfg.AppEnv != "production" {
		return "send magic link failed: " + err.Error()
	}
	if strings.Contains(err.Error(), "not configured") {
		return "send magic link failed: email delivery is not configured"
	}
	return "send magic link failed: email provider rejected request"
}

func (h *handler) handleVerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	user, sessionToken, err := h.db.VerifyMagicLinkToken(token, 24*time.Hour)
	if err != nil {
		if err == database.ErrPendingUser {
			http.Error(w, "account pending approval", http.StatusForbidden)
			return
		}
		if err == database.ErrInactiveUser {
			http.Error(w, "account disabled", http.StatusForbidden)
			return
		}
		if err == sql.ErrNoRows {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		h.logger.Error("verify magic link failed", "error", err)
		http.Error(w, "verify token", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cfg.AppEnv == "production",
		MaxAge:   int((24 * time.Hour).Seconds()),
	})
	_ = h.db.AppendAuditLog(user.ID, "auth.login", "session", sessionToken, map[string]any{
		"email": user.Email,
	})

	// Native clients (mobile) can't read the HttpOnly cookie, so we also
	// return the session token in the JSON body. Web ignores this and uses
	// the cookie as before.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"user":         toAuthUser(user),
		"sessionToken": sessionToken,
	})
}

func (h *handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if user, ok := getAuthUser(r.Context()); ok {
		_ = h.db.AppendAuditLog(user.ID, "auth.logout", "user", user.ID, map[string]any{
			"email": user.Email,
		})
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		_ = h.db.RevokeSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cfg.AppEnv == "production",
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := getAuthUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessionExpiresAt := int64(0)
	if cookie, err := r.Cookie(sessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		if expiresAt, found, lookupErr := h.db.GetSessionExpiresAt(cookie.Value); lookupErr == nil && found {
			sessionExpiresAt = expiresAt
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":             user,
		"sessionExpiresAt": sessionExpiresAt,
	})
}
