package database

import (
	"encoding/json"
	"time"
)

func metadataString(meta map[string]any, key string) string {
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func applyAuditEmailFallbacks(entry *AuditLogEntry) {
	if len(entry.Metadata) == 0 || (entry.UserEmail != "" && entry.TargetEmail != "") {
		return
	}

	meta := map[string]any{}
	if err := json.Unmarshal(entry.Metadata, &meta); err != nil {
		return
	}
	if entry.UserEmail == "" {
		entry.UserEmail = metadataString(meta, "actorEmail")
	}
	if entry.TargetEmail == "" {
		entry.TargetEmail = metadataString(meta, "targetEmail")
	}
}

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
		SELECT
			a.id,
			COALESCE(a.user_id, ''),
			COALESCE(actor.email, ''),
			a.action,
			a.target_type,
			a.target_id,
			COALESCE(target.email, ''),
			a.metadata,
			a.created_at
		FROM audit_log a
		LEFT JOIN users actor ON actor.id = a.user_id
		LEFT JOIN users target ON a.target_type = 'user' AND target.id = a.target_id
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
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.UserEmail,
			&entry.Action,
			&entry.TargetType,
			&entry.TargetID,
			&entry.TargetEmail,
			&entry.Metadata,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		applyAuditEmailFallbacks(&entry)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
