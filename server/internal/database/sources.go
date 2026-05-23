package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GetIngestionSource returns a single ingestion source by ID.
// The second return value is false when no row was found.
func (d *DB) GetIngestionSource(id string) (IngestionSource, bool, error) {
	var s IngestionSource
	err := d.db.QueryRow(`
		SELECT id, COALESCE(user_id, ''), label, enabled, is_shared, deleted_at, system_id, system_label,
		       last_seen_unix, error_count, calls_received, updated_at
		FROM ingestion_sources WHERE id = $1`, id).Scan(
		&s.ID, &s.UserID, &s.Label, &s.Enabled, &s.IsShared, &s.DeletedAt, &s.SystemID, &s.SystemLabel,
		&s.LastSeenUnix, &s.ErrorCount, &s.CallsReceived, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s, false, nil
		}
		return s, false, err
	}
	return s, true, nil
}

// ListIngestionSources returns configured ingestion sources.
func (d *DB) ListIngestionSources(includeDeleted bool) ([]IngestionSource, error) {
	query := `
		SELECT id, COALESCE(user_id, ''), label, enabled, is_shared, deleted_at, system_id, system_label,
		       last_seen_unix, error_count, calls_received, updated_at
		FROM ingestion_sources`
	if !includeDeleted {
		query += ` WHERE deleted_at = 0`
	}
	query += ` ORDER BY id ASC`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sources := make([]IngestionSource, 0)
	for rows.Next() {
		var s IngestionSource
		if err := rows.Scan(&s.ID, &s.UserID, &s.Label, &s.Enabled, &s.IsShared, &s.DeletedAt, &s.SystemID, &s.SystemLabel,
			&s.LastSeenUnix, &s.ErrorCount, &s.CallsReceived, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

// ListSharedIngestionSources returns sources intentionally published to peer hubs.
func (d *DB) ListSharedIngestionSources() ([]IngestionSource, error) {
	rows, err := d.db.Query(`
		SELECT id, COALESCE(user_id, ''), label, enabled, is_shared, deleted_at, system_id, system_label,
		       last_seen_unix, error_count, calls_received, updated_at
		FROM ingestion_sources
		WHERE deleted_at = 0 AND enabled = TRUE AND is_shared = TRUE
		  AND id NOT LIKE 'remote\_%' ESCAPE '\'
		ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sources := make([]IngestionSource, 0)
	for rows.Next() {
		var s IngestionSource
		if err := rows.Scan(&s.ID, &s.UserID, &s.Label, &s.Enabled, &s.IsShared, &s.DeletedAt, &s.SystemID, &s.SystemLabel,
			&s.LastSeenUnix, &s.ErrorCount, &s.CallsReceived, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

// CountImportedFederatedSources returns enabled remote sources imported from peer hubs.
func (d *DB) CountImportedFederatedSources() (int64, error) {
	var count int64
	err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM ingestion_sources
		WHERE deleted_at = 0
		  AND enabled = TRUE
		  AND id LIKE 'remote\_%' ESCAPE '\'`).Scan(&count)
	return count, err
}

// UpsertIngestionSource creates or updates an ingestion source.
func (d *DB) UpsertIngestionSource(s IngestionSource) error {
	_, err := d.db.Exec(`
		INSERT INTO ingestion_sources
			(id, user_id, label, enabled, is_shared, deleted_at, system_id, system_label,
			 last_seen_unix, error_count, calls_received, updated_at)
		VALUES ($1, $2, $3, $4, $5, 0, $6, $7, $8, $9, $10, $11)
		ON CONFLICT(id) DO UPDATE SET
			user_id = COALESCE(ingestion_sources.user_id, excluded.user_id),
			label = excluded.label,
			enabled = excluded.enabled,
			is_shared = excluded.is_shared,
			deleted_at = 0,
			system_id = excluded.system_id,
			system_label = excluded.system_label,
			last_seen_unix = excluded.last_seen_unix,
			error_count = excluded.error_count,
			calls_received = excluded.calls_received,
			updated_at = excluded.updated_at
	`, s.ID, nullableString(s.UserID), s.Label, s.Enabled, s.IsShared, s.SystemID, s.SystemLabel,
		s.LastSeenUnix, s.ErrorCount, s.CallsReceived, time.Now().Unix())
	return err
}

// IncrementSourceMetrics upserts metrics for a source. If the source doesn't
// exist yet it is created with enabled=true and sensible defaults.
func (d *DB) IncrementSourceMetrics(sourceID string, success bool) error {
	now := time.Now().Unix()
	if success {
		_, err := d.db.Exec(`
			INSERT INTO ingestion_sources
				(id, user_id, label, enabled, deleted_at, system_id, system_label, calls_received, last_seen_unix, updated_at)
			VALUES ($1, NULL, '', true, 0, 0, '', 1, $2, $3)
			ON CONFLICT(id) DO UPDATE SET
			    calls_received = ingestion_sources.calls_received + 1,
			    last_seen_unix = excluded.last_seen_unix,
			    updated_at    = excluded.updated_at`,
			sourceID, now, now)
		return err
	}
	_, err := d.db.Exec(`
		INSERT INTO ingestion_sources
			(id, user_id, label, enabled, deleted_at, system_id, system_label, error_count, updated_at)
		VALUES ($1, NULL, '', true, 0, 0, '', 1, $2)
		ON CONFLICT(id) DO UPDATE SET
		    error_count = ingestion_sources.error_count + 1,
		    updated_at  = excluded.updated_at`,
		sourceID, now)
	return err
}

// generateRandomKey creates a random API key (32-byte hex string).
func generateRandomKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand.Read failed: %v", err))
	}
	return "sk_live_" + hex.EncodeToString(b)
}

// GenerateSourceKey creates a new API key for a source.
func (d *DB) GenerateSourceKey(source IngestionSource) (SourceAPIKey, error) {
	keyID := fmt.Sprintf("key_%d", time.Now().UnixMilli())
	apiKey := generateRandomKey()
	now := time.Now().Unix()

	_, err := d.db.Exec(`
		INSERT INTO ingestion_source_keys (id, source_id, user_id, api_key, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		keyID, source.ID, nullableString(source.UserID), apiKey, now)
	if err != nil {
		return SourceAPIKey{}, err
	}

	return SourceAPIKey{
		ID:         keyID,
		SourceID:   source.ID,
		UserID:     source.UserID,
		APIKey:     apiKey,
		CreatedAt:  now,
		LastUsedAt: 0,
	}, nil
}

// GetSourceByAPIKey looks up a source by its API key.
// Returns the source and whether it was found.
func (d *DB) GetSourceByAPIKey(apiKey string) (IngestionSource, bool, error) {
	var sourceID string
	var keyUserID string
	err := d.db.QueryRow(`
		SELECT source_id, COALESCE(user_id, '') FROM ingestion_source_keys WHERE api_key = $1`,
		apiKey).Scan(&sourceID, &keyUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IngestionSource{}, false, nil
		}
		return IngestionSource{}, false, err
	}

	// Now fetch the source
	source, found, err := d.GetIngestionSource(sourceID)
	if err != nil || !found {
		return source, found, err
	}
	if source.DeletedAt > 0 {
		return IngestionSource{}, false, nil
	}
	if source.UserID == "" && keyUserID != "" {
		source.UserID = keyUserID
		_ = d.UpsertIngestionSource(source)
	}
	return source, true, nil
}

// ListSourceKeys returns all API keys for a source.
func (d *DB) ListSourceKeys(sourceID string) ([]SourceAPIKey, error) {
	rows, err := d.db.Query(`
		SELECT id, source_id, COALESCE(user_id, ''), api_key, created_at, last_used_at
		FROM ingestion_source_keys
		WHERE source_id = $1
		ORDER BY created_at DESC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []SourceAPIKey
	for rows.Next() {
		var k SourceAPIKey
		if err := rows.Scan(&k.ID, &k.SourceID, &k.UserID, &k.APIKey, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListSourceIDsForOwner returns source IDs owned directly by a user or by one of their source keys.
func (d *DB) ListSourceIDsForOwner(userID string) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT DISTINCT id
		FROM (
			SELECT id
			FROM ingestion_sources
			WHERE user_id = $1 AND deleted_at = 0
			UNION
			SELECT source_id AS id
			FROM ingestion_source_keys
			WHERE user_id = $1
		) owned_sources
		WHERE id <> ''
		ORDER BY id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sourceIDs := make([]string, 0)
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			return nil, err
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	return sourceIDs, rows.Err()
}

// ListReadableSourceIDsForUser returns source IDs whose calls are visible to a user.
func (d *DB) ListReadableSourceIDsForUser(userID string) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT DISTINCT id
		FROM (
			SELECT id
			FROM ingestion_sources
			WHERE user_id = $1 AND deleted_at = 0
			UNION
			SELECT source_id AS id
			FROM ingestion_source_keys
			WHERE user_id = $1
			UNION
			SELECT source_id AS id
			FROM ingestion_source_user_shares
			WHERE user_id = $1
			UNION
			SELECT id
			FROM ingestion_sources
			WHERE is_shared = TRUE AND deleted_at = 0
		) readable_sources
		WHERE id <> ''
		ORDER BY id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sourceIDs := make([]string, 0)
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			return nil, err
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	return sourceIDs, rows.Err()
}

// RevokeSourceKey deletes an API key that belongs to the given source.
func (d *DB) RevokeSourceKey(sourceID, keyID string) error {
	_, err := d.db.Exec(`DELETE FROM ingestion_source_keys WHERE source_id = $1 AND id = $2`, sourceID, keyID)
	return err
}

// ListSharedSourceIDsForUser returns source IDs explicitly shared with the given user.
func (d *DB) ListSharedSourceIDsForUser(userID string) (map[string]bool, error) {
	rows, err := d.db.Query(`
		SELECT source_id
		FROM ingestion_source_user_shares
		WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shared := make(map[string]bool)
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			return nil, err
		}
		shared[sourceID] = true
	}
	return shared, rows.Err()
}

// ListSourceShareUserIDs returns user IDs that can access the given source through an explicit share.
func (d *DB) ListSourceShareUserIDs(sourceID string) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT user_id
		FROM ingestion_source_user_shares
		WHERE source_id = $1
		ORDER BY user_id ASC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	userIDs := make([]string, 0)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, rows.Err()
}

// SetSourceShareUserIDs replaces the explicit user share list for a source.
func (d *DB) SetSourceShareUserIDs(sourceID string, userIDs []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM ingestion_source_user_shares WHERE source_id = $1`, sourceID); err != nil {
		return err
	}
	now := time.Now().Unix()
	seen := make(map[string]bool, len(userIDs))
	for _, rawUserID := range userIDs {
		userID := strings.TrimSpace(rawUserID)
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		if _, err := tx.Exec(`
			INSERT INTO ingestion_source_user_shares (source_id, user_id, created_at)
			VALUES ($1, $2, $3)`, sourceID, userID, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteIngestionSource soft-deletes a source and removes associated API keys.
// The second return value is false when an active source row does not exist.
func (d *DB) DeleteIngestionSource(sourceID string) (bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM ingestion_source_keys WHERE source_id = $1`, sourceID); err != nil {
		return false, err
	}
	now := time.Now().Unix()

	result, err := tx.Exec(`
		UPDATE ingestion_sources
		SET enabled = false,
			deleted_at = $2,
			updated_at = $2,
			label = CASE
				WHEN label LIKE '[deleted] %' THEN label
				ELSE '[deleted] ' || label
			END
		WHERE id = $1 AND deleted_at = 0
	`, sourceID, now)
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

// UpdateKeyLastUsed updates the last_used_at timestamp for a key.
func (d *DB) UpdateKeyLastUsed(apiKey string) error {
	_, err := d.db.Exec(`
		UPDATE ingestion_source_keys SET last_used_at = $1 WHERE api_key = $2`,
		time.Now().Unix(), apiKey)
	return err
}
