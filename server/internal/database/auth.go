package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EnsureUserByEmail finds or creates a user account for the given email.
func (d *DB) EnsureUserByEmail(email string) (User, error) {
	now := time.Now().Unix()
	userID := fmt.Sprintf("usr_%d", time.Now().UnixMilli())
	role := "user"
	var userCount int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return User{}, err
	}
	if userCount == 0 {
		role = "admin"
	}
	_, err := d.db.Exec(`
		INSERT INTO users (id, email, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', $4, $5)
		ON CONFLICT (email) DO NOTHING
	`, userID, strings.ToLower(strings.TrimSpace(email)), role, now, now)
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
func (d *DB) CreateMagicLinkToken(email string, ttl time.Duration) (string, User, error) {
	user, err := d.EnsureUserByEmail(email)
	if err != nil {
		return "", User{}, err
	}

	token := randomToken("ml_")
	now := time.Now().Unix()
	expiresAt := time.Now().Add(ttl).Unix()

	_, err = d.db.Exec(`
		INSERT INTO auth_magic_links (token, user_id, email, expires_at, used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, $5)
	`, token, user.ID, user.Email, expiresAt, now)
	if err != nil {
		return "", User{}, err
	}

	return token, user, nil
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

	if err := tx.Commit(); err != nil {
		return User{}, "", err
	}

	return user, sessionToken, nil
}

// GetUserBySessionToken returns the active user for a non-revoked, non-expired session.
func (d *DB) GetUserBySessionToken(token string) (User, bool, error) {
	var user User
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.role, u.status, u.created_at, u.updated_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = $1
		  AND s.revoked_at = 0
		  AND s.expires_at >= $2
		  AND u.status = 'active'
	`, token, time.Now().Unix()).Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt)
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

// ListUsers returns all users for admin management.
func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.db.Query(`
		SELECT id, email, role, status, created_at, updated_at
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
		if err := rows.Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
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

	if _, err := tx.Exec(`DELETE FROM auth_sessions WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM auth_magic_links WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM ingestion_source_keys WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM ingestion_source_user_shares WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM ingestion_sources WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM calls WHERE user_id = $1`, userID); err != nil {
		return false, err
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
