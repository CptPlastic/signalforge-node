package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInactiveUser = errors.New("inactive user")
	ErrPendingUser  = errors.New("pending user")
)

// EnsureUserByEmail finds or creates a user account for the given email.
func (d *DB) EnsureUserByEmail(email string) (User, error) {
	now := time.Now().Unix()
	userID := fmt.Sprintf("usr_%d", time.Now().UnixMilli())
	role := "user"
	status := "pending"
	if d.autoApproveUsers {
		status = "active"
	}
	var userCount int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return User{}, err
	}
	if userCount == 0 {
		role = "admin"
		status = "active"
	}
	_, err := d.db.Exec(`
		INSERT INTO users (id, email, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO NOTHING
	`, userID, strings.ToLower(strings.TrimSpace(email)), role, status, now, now)
	if err != nil {
		return User{}, err
	}

	var user User
	err = d.db.QueryRow(`
		SELECT id, email, role, status, created_at, updated_at
		FROM users WHERE email = $1
	`, strings.ToLower(strings.TrimSpace(email))).Scan(
		&user.ID, &user.Email, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

// CreateMagicLinkToken creates a one-time magic-link token for a user's email.
// It also generates a short 6-digit code stored on the same row so the caller
// can offer an in-app code entry as an alternative to clicking the link.
func (d *DB) CreateMagicLinkToken(email string, ttl time.Duration) (string, string, User, error) {
	user, err := d.EnsureUserByEmail(email)
	if err != nil {
		return "", "", User{}, err
	}
	if user.Status == "pending" {
		return "", "", user, ErrPendingUser
	}
	if user.Status != "active" {
		return "", "", User{}, ErrInactiveUser
	}

	token := randomToken("ml_")
	code := randomNumericCode(6)
	now := time.Now().Unix()
	expiresAt := time.Now().Add(ttl).Unix()

	_, err = d.db.Exec(`
		INSERT INTO auth_magic_links (token, user_id, email, code, expires_at, used_at, created_at)
		VALUES ($1, $2, $3, $4, $5, 0, $6)
	`, token, user.ID, user.Email, code, expiresAt, now)
	if err != nil {
		return "", "", User{}, err
	}

	return token, code, user, nil
}

// VerifyMagicLinkCode validates and consumes a one-time 6-digit code for the
// given email, then creates a session. This lets a user finish sign-in without
// leaving the app to click the emailed link.
func (d *DB) VerifyMagicLinkCode(email, code string, sessionTTL time.Duration) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	code = strings.TrimSpace(code)
	if email == "" || code == "" {
		return User{}, "", sql.ErrNoRows
	}

	tx, err := d.db.Begin()
	if err != nil {
		return User{}, "", err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var token string
	var userID string
	var expiresAt int64
	var usedAt int64
	err = tx.QueryRow(`
		SELECT token, user_id, expires_at, used_at
		FROM auth_magic_links
		WHERE email = $1 AND code = $2
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE
	`, email, code).Scan(&token, &userID, &expiresAt, &usedAt)
	if err != nil {
		return User{}, "", err
	}

	now := time.Now().Unix()
	if usedAt > 0 || expiresAt < now {
		return User{}, "", sql.ErrNoRows
	}

	if _, err := tx.Exec(`UPDATE auth_magic_links SET used_at = $1 WHERE token = $2`, now, token); err != nil {
		return User{}, "", err
	}

	sessionToken := randomToken("sess_")
	sessionExpiresAt := time.Now().Add(sessionTTL).Unix()
	if _, err := tx.Exec(`
		INSERT INTO auth_sessions (token, user_id, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, 0, $4)
	`, sessionToken, userID, sessionExpiresAt, now); err != nil {
		return User{}, "", err
	}

	var user User
	err = tx.QueryRow(`
		SELECT id, email, role, status, created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, "", err
	}
	if user.Status == "pending" {
		return User{}, "", ErrPendingUser
	}
	if user.Status != "active" {
		return User{}, "", ErrInactiveUser
	}

	if err := tx.Commit(); err != nil {
		return User{}, "", err
	}

	return user, sessionToken, nil
}

// VerifyMagicLinkToken validates and consumes a one-time magic-link token, then creates a session.
func (d *DB) VerifyMagicLinkToken(token string, sessionTTL time.Duration) (User, string, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return User{}, "", err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var userID string
	var expiresAt int64
	var usedAt int64
	err = tx.QueryRow(`
		SELECT user_id, expires_at, used_at
		FROM auth_magic_links
		WHERE token = $1
		FOR UPDATE
	`, token).Scan(&userID, &expiresAt, &usedAt)
	if err != nil {
		return User{}, "", err
	}

	now := time.Now().Unix()
	if usedAt > 0 || expiresAt < now {
		return User{}, "", sql.ErrNoRows
	}

	if _, err := tx.Exec(`UPDATE auth_magic_links SET used_at = $1 WHERE token = $2`, now, token); err != nil {
		return User{}, "", err
	}

	sessionToken := randomToken("sess_")
	sessionExpiresAt := time.Now().Add(sessionTTL).Unix()
	if _, err := tx.Exec(`
		INSERT INTO auth_sessions (token, user_id, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, 0, $4)
	`, sessionToken, userID, sessionExpiresAt, now); err != nil {
		return User{}, "", err
	}

	var user User
	err = tx.QueryRow(`
		SELECT id, email, role, status, created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, "", err
	}
	if user.Status == "pending" {
		return User{}, "", ErrPendingUser
	}
	if user.Status != "active" {
		return User{}, "", ErrInactiveUser
	}

	if err := tx.Commit(); err != nil {
		return User{}, "", err
	}

	return user, sessionToken, nil
}

// GetUserBySessionToken returns the active user for a non-revoked, non-expired session.
func (d *DB) GetUserBySessionToken(token string) (User, bool, error) {
	var user User
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.role, u.status,
		       COALESCE(u.tx_enabled, FALSE),
		       COALESCE(u.dispatcher_enabled, FALSE),
		       u.created_at, u.updated_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = $1
		  AND s.revoked_at = 0
		  AND s.expires_at >= $2
		  AND u.status = 'active'
	`, token, time.Now().Unix()).Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.TxEnabled, &user.DispatcherEnabled, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, false, nil
		}
		return User{}, false, err
	}

	return user, true, nil
}

// RevokeSession invalidates an auth session token.
func (d *DB) RevokeSession(token string) error {
	_, err := d.db.Exec(`UPDATE auth_sessions SET revoked_at = $1 WHERE token = $2`, time.Now().Unix(), token)
	return err
}

// ExtendSession sets a session's expires_at to now+ttl, returning the new
// expiration. Returns (0, false, nil) if the session is unknown or already
// revoked — callers should require re-auth in that case.
func (d *DB) ExtendSession(token string, ttl time.Duration) (int64, bool, error) {
	newExpiresAt := time.Now().Add(ttl).Unix()
	result, err := d.db.Exec(`
		UPDATE auth_sessions
		SET expires_at = $1
		WHERE token = $2
		  AND revoked_at = 0
	`, newExpiresAt, token)
	if err != nil {
		return 0, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if rows == 0 {
		return 0, false, nil
	}
	return newExpiresAt, true, nil
}

// GetSessionExpiresAt returns the expiration for an active session token.
func (d *DB) GetSessionExpiresAt(token string) (int64, bool, error) {
	var expiresAt int64
	err := d.db.QueryRow(`
		SELECT expires_at
		FROM auth_sessions
		WHERE token = $1
		  AND revoked_at = 0
	`, token).Scan(&expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return expiresAt, true, nil
}

// GetUserByID returns a single user by primary key.
func (d *DB) GetUserByID(userID string) (User, bool, error) {
	var user User
	err := d.db.QueryRow(`
		SELECT id, email, role, status,
		       COALESCE(tx_enabled, FALSE),
		       COALESCE(dispatcher_enabled, FALSE),
		       (password_hash IS NOT NULL AND BTRIM(password_hash) <> ''),
		       created_at, updated_at
		FROM users WHERE id = $1`, userID).Scan(
		&user.ID, &user.Email, &user.Role, &user.Status,
		&user.TxEnabled, &user.DispatcherEnabled, &user.PasswordConfigured,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, false, nil
		}
		return User{}, false, err
	}
	return user, true, nil
}

// ListUsers returns all users for admin management.
func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.db.Query(`
		SELECT id, email, role, status,
		       COALESCE(tx_enabled, FALSE),
		       COALESCE(dispatcher_enabled, FALSE),
		       (password_hash IS NOT NULL AND BTRIM(password_hash) <> ''),
		       created_at, updated_at
		FROM users
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var user User
		if err := rows.Scan(
			&user.ID, &user.Email, &user.Role, &user.Status,
			&user.TxEnabled, &user.DispatcherEnabled, &user.PasswordConfigured,
			&user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// UpdateUserRoleStatus updates a user's role and/or status.
// The second return value is false when the user does not exist.
func (d *DB) UpdateUserRoleStatus(userID, role, status string) (bool, error) {
	result, err := d.db.Exec(`
		UPDATE users
		SET role = $2, status = $3, updated_at = $4
		WHERE id = $1
	`, userID, role, status, time.Now().Unix())
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// SetUserTxEnabled toggles a user's PTT transmit capability.
// The second return value is false when the user does not exist.
func (d *DB) SetUserTxEnabled(userID string, enabled bool) (bool, error) {
	result, err := d.db.Exec(`
		UPDATE users
		SET tx_enabled = $2, updated_at = $3
		WHERE id = $1
	`, userID, enabled, time.Now().Unix())
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// SetUserDispatcherEnabled toggles a user's multi-set broadcast capability.
// The second return value is false when the user does not exist.
func (d *DB) SetUserDispatcherEnabled(userID string, enabled bool) (bool, error) {
	result, err := d.db.Exec(`
		UPDATE users
		SET dispatcher_enabled = $2, updated_at = $3
		WHERE id = $1
	`, userID, enabled, time.Now().Unix())
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// DeleteUser deletes a user and all account-owned auth/session and scanner records.
// The second return value is false when the user does not exist.
func (d *DB) DeleteUser(userID string) (bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	steps := []string{
		`DELETE FROM auth_sessions WHERE user_id = $1`,
		`DELETE FROM auth_magic_links WHERE user_id = $1`,
		`UPDATE audit_log SET user_id = NULL WHERE user_id = $1`,
		`UPDATE hub_invites SET created_by_user_id = NULL WHERE created_by_user_id = $1`,
		`DELETE FROM ingestion_source_keys WHERE user_id = $1`,
		`DELETE FROM ingestion_source_user_shares WHERE user_id = $1`,
		`DELETE FROM radio_sets WHERE user_id = $1`,
		`DELETE FROM ptt_uploads WHERE user_id = $1`,
		`DELETE FROM ingestion_sources WHERE user_id = $1`,
		`DELETE FROM calls WHERE sender_user_id = $1`,
		`DELETE FROM calls WHERE user_id = $1`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(q, userID); err != nil {
			return false, err
		}
	}
	result, err := tx.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, nil
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SetUserPassword hashes and stores a password for the user.
func (d *DB) SetUserPassword(userID, password string) (bool, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return false, err
	}
	return d.SetUserPasswordHash(userID, hash)
}

// SetUserPasswordHash stores a bcrypt hash for the user and revokes existing sessions.
func (d *DB) SetUserPasswordHash(userID, passwordHash string) (bool, error) {
	passwordHash = strings.TrimSpace(passwordHash)
	if userID == "" || passwordHash == "" {
		return false, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.Exec(`
		UPDATE users
		SET password_hash = $2, updated_at = $3
		WHERE id = $1
	`, userID, passwordHash, time.Now().Unix())
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, nil
	}
	if _, err := tx.Exec(`
		UPDATE auth_sessions
		SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at = 0
	`, userID, time.Now().Unix()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// CreateUserSession issues a new auth session for an active user.
func (d *DB) CreateUserSession(userID string, sessionTTL time.Duration) (string, error) {
	sessionToken := randomToken("sess_")
	sessionExpiresAt := time.Now().Add(sessionTTL).Unix()
	now := time.Now().Unix()
	_, err := d.db.Exec(`
		INSERT INTO auth_sessions (token, user_id, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, 0, $4)
	`, sessionToken, userID, sessionExpiresAt, now)
	if err != nil {
		return "", err
	}
	return sessionToken, nil
}

// VerifyUserPassword returns the user when email and password match an active account.
func (d *DB) VerifyUserPassword(email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return User{}, sql.ErrNoRows
	}

	var user User
	var passwordHash sql.NullString
	err := d.db.QueryRow(`
		SELECT id, email, role, status,
		       COALESCE(tx_enabled, FALSE),
		       COALESCE(dispatcher_enabled, FALSE),
		       password_hash, created_at, updated_at
		FROM users
		WHERE email = $1
	`, email).Scan(
		&user.ID, &user.Email, &user.Role, &user.Status,
		&user.TxEnabled, &user.DispatcherEnabled,
		&passwordHash, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return User{}, err
	}
	if user.Status == "pending" {
		return User{}, ErrPendingUser
	}
	if user.Status != "active" {
		return User{}, ErrInactiveUser
	}
	if !passwordHash.Valid || strings.TrimSpace(passwordHash.String) == "" {
		return User{}, sql.ErrNoRows
	}
	if err := comparePasswordHash(passwordHash.String, password); err != nil {
		return User{}, sql.ErrNoRows
	}
	return user, nil
}

// BootstrapAuthUser creates or updates the bootstrap admin with a password hash.
func (d *DB) BootstrapAuthUser(email, passwordHash string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	passwordHash = strings.TrimSpace(passwordHash)
	if email == "" || passwordHash == "" {
		return nil
	}
	now := time.Now().Unix()

	var existingID string
	err := d.db.QueryRow(`SELECT id FROM users WHERE email = $1`, email).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		userID := fmt.Sprintf("usr_%d", time.Now().UnixMilli())
		_, err = d.db.Exec(`
			INSERT INTO users (id, email, role, status, password_hash, created_at, updated_at)
			VALUES ($1, $2, 'admin', 'active', $3, $4, $4)
		`, userID, email, passwordHash, now)
		return err
	}
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`
		UPDATE users
		SET role = 'admin', status = 'active', password_hash = $2, updated_at = $3
		WHERE id = $1
	`, existingID, passwordHash, now)
	return err
}

// CountActiveAdmins returns the number of active admin users.
func (d *DB) CountActiveAdmins() (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM users
		WHERE role = 'admin' AND status = 'active'
	`).Scan(&count)
	return count, err
}
