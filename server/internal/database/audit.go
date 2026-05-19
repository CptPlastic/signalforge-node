package database

import (
	"encoding/json"
	"time"
)

// AppendAuditLog writes an account/security audit event.
func (d *DB) AppendAuditLog(userID, action, targetType, targetID string, metadata map[string]any) error {
	payload, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`
		INSERT INTO audit_log (user_id, action, target_type, target_id, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, nullableString(userID), action, targetType, targetID, string(payload), time.Now().Unix())
	return err
}

// ListAuditLogs returns recent audit events for admin visibility.
func (d *DB) ListAuditLogs(limit int) ([]AuditLogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := d.db.Query(`
		SELECT id, COALESCE(user_id, ''), action, target_type, target_id, metadata, created_at
		FROM audit_log
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]AuditLogEntry, 0)
	for rows.Next() {
		var entry AuditLogEntry
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.Action, &entry.TargetType, &entry.TargetID, &entry.Metadata, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
