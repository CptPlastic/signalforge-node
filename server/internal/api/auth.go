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
	ID                string `json:"id"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	TxEnabled         bool   `json:"txEnabled"`
	DispatcherEnabled bool   `json:"dispatcherEnabled"`
}

type requestMagicLinkBody struct {
	Email string `json:"email"`
}

type verifyMagicCodeBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type passwordLoginBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func toAuthUser(user database.User) authUser {
	return authUser{
		ID:                user.ID,
		Email:             user.Email,
		Role:              user.Role,
		TxEnabled:         user.TxEnabled,
		DispatcherEnabled: user.DispatcherEnabled,
	}
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

func (h *handler) handleAuthCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"passwordLoginEnabled":        h.cfg.AuthPasswordLoginEnabled,
		"magicLinkEnabled":            true,
		"emailDeliveryConfigured":     h.emailDeliveryConfigured(),
		"autoApproveUsers":            h.cfg.AuthAutoApproveUsers,
		"bootstrapEmailConfigured":    strings.TrimSpace(h.cfg.AuthBootstrapEmail) != "",
		"bootstrapPasswordConfigured": strings.TrimSpace(h.cfg.AuthBootstrapPassword) != "",
	})
}

func (h *handler) emailDeliveryConfigured() bool {
	return strings.TrimSpace(h.cfg.MailjetAPIKey) != "" &&
		strings.TrimSpace(h.cfg.MailjetSecretKey) != "" &&
		strings.TrimSpace(h.cfg.MailFromEmail) != ""
}

func (h *handler) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.AuthPasswordLoginEnabled {
		http.Error(w, "password login disabled", http.StatusForbidden)
		return
	}

	var req passwordLoginBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	password := strings.TrimSpace(req.Password)
	if email == "" || !strings.Contains(email, "@") || password == "" {
		http.Error(w, "invalid email or password", http.StatusBadRequest)
		return
	}

	user, err := h.db.VerifyUserPassword(email, password)
	if err != nil {
		h.writePasswordLoginError(w, err)
		return
	}

	sessionToken, err := h.db.CreateUserSession(user.ID, 24*time.Hour)
	if err != nil {
		h.logger.Error("create password login session failed", "error", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}

	h.issueSession(w, user, sessionToken)
}

func (h *handler) writePasswordLoginError(w http.ResponseWriter, err error) {
	switch err {
	case database.ErrPendingUser:
		http.Error(w, "account pending approval", http.StatusForbidden)
	case database.ErrInactiveUser:
		http.Error(w, "account disabled", http.StatusForbidden)
	case sql.ErrNoRows:
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
	default:
		h.logger.Error("password login failed", "error", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
	}
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

	token, code, user, err := h.db.CreateMagicLinkToken(email, 15*time.Minute)
	if err != nil {
		h.writeCreateMagicLinkError(w, user, err)
		return
	}
	verifyURL := h.magicLinkVerifyURL(r, token)
	if err := h.sendMagicLinkEmail(r.Context(), user.Email, verifyURL, token, code); err != nil {
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
		response["code"] = code
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
		h.writeVerifyError(w, err)
		return
	}

	h.issueSession(w, user, sessionToken)
}

// handleVerifyMagicCode verifies a short 6-digit code entered in-app, avoiding
// the email round-trip of clicking the magic link.
func (h *handler) handleVerifyMagicCode(w http.ResponseWriter, r *http.Request) {
	var req verifyMagicCodeBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)
	if email == "" || code == "" {
		http.Error(w, "missing email or code", http.StatusBadRequest)
		return
	}

	user, sessionToken, err := h.db.VerifyMagicLinkCode(email, code, 24*time.Hour)
	if err != nil {
		h.writeVerifyError(w, err)
		return
	}

	h.issueSession(w, user, sessionToken)
}

// writeVerifyError maps verification errors to HTTP responses shared by the
// link- and code-based sign-in handlers.
func (h *handler) writeVerifyError(w http.ResponseWriter, err error) {
	switch err {
	case database.ErrPendingUser:
		http.Error(w, "account pending approval", http.StatusForbidden)
	case database.ErrInactiveUser:
		http.Error(w, "account disabled", http.StatusForbidden)
	case sql.ErrNoRows:
		http.Error(w, "invalid or expired code", http.StatusUnauthorized)
	default:
		h.logger.Error("verify sign-in failed", "error", err)
		http.Error(w, "verify failed", http.StatusInternalServerError)
	}
}

// issueSession sets the session cookie, records the login, and returns the
// session payload shared by the link- and code-based sign-in handlers.
func (h *handler) issueSession(w http.ResponseWriter, user database.User, sessionToken string) {
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

// handleDeleteMyAccount permanently deletes the signed-in user and revokes the session.
func (h *handler) handleDeleteMyAccount(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	if user.Role == "admin" {
		if !h.ensureMoreThanOneActiveAdmin(w, "delete account", "cannot delete the last active admin account") {
			return
		}
	}

	_ = h.db.AppendAuditLog(user.ID, "auth.account_deleted", "user", user.ID, map[string]any{
		"email": user.Email,
	})

	deleted, err := h.db.DeleteUser(user.ID)
	if err != nil {
		h.logger.Error("delete my account failed", "user_id", user.ID, "error", err)
		http.Error(w, "delete account", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	if token := sessionTokenFromRequest(r); token != "" {
		_ = h.db.RevokeSession(token)
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

// handleRefreshSession extends the caller's active session by a fresh TTL.
// Used by the web client's "RE-AUTH" banner to keep a session alive without
// going through the full magic-link round-trip.
func (h *handler) handleRefreshSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := getAuthUser(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token := sessionTokenFromRequest(r)
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	newExpiresAt, ok, err := h.db.ExtendSession(token, 24*time.Hour)
	if err != nil {
		h.logger.Error("extend session failed", "error", err)
		http.Error(w, "refresh failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "session not found", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessionExpiresAt": newExpiresAt,
	})
}

// sessionTokenFromRequest returns the active session token sourced from either
// the session cookie or an Authorization: Bearer header.
func sessionTokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if v := strings.TrimSpace(cookie.Value); v != "" {
			return v
		}
	}
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}
