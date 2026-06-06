package database

import "time"

// ListTalkgroupSettings returns all saved talkgroup preferences.
func (d *DB) ListTalkgroupSettings() ([]TalkgroupSetting, error) {
	rows, err := d.db.Query(`
		SELECT talkgroup, favorite, muted, transcribe, updated_at
		FROM talkgroup_settings
		ORDER BY talkgroup ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make([]TalkgroupSetting, 0)
	for rows.Next() {
		var s TalkgroupSetting
		if err := rows.Scan(&s.Talkgroup, &s.Favorite, &s.Muted, &s.Transcribe, &s.UpdatedAt); err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	return settings, rows.Err()
}

// ListTalkgroupGroups returns visible distinct non-empty talkgroup_group values, sorted alphabetically.
func (d *DB) ListTalkgroupGroups(userID string, includeAll bool) ([]string, error) {
	query := `
		SELECT DISTINCT c.talkgroup_group
		FROM calls c
		LEFT JOIN ingestion_sources s ON s.id = c.source_id
		WHERE c.talkgroup_group <> ''`
	args := []any{}
	if !includeAll {
		query += `
			AND (
				c.user_id = $1
				OR s.user_id = $1
				OR s.is_shared = TRUE
				OR EXISTS (
					SELECT 1 FROM ingestion_source_user_shares sh
					WHERE sh.source_id = c.source_id AND sh.user_id = $1
				)
			)`
		args = append(args, userID)
	}
	query += ` ORDER BY c.talkgroup_group ASC`
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]string, 0)
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// UpsertTalkgroupSetting creates or updates a talkgroup setting row.
func (d *DB) UpsertTalkgroupSetting(s TalkgroupSetting) error {
	_, err := d.db.Exec(`
		INSERT INTO talkgroup_settings (talkgroup, favorite, muted, transcribe, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(talkgroup) DO UPDATE SET
			favorite = excluded.favorite,
			muted = excluded.muted,
			transcribe = excluded.transcribe,
			updated_at = excluded.updated_at
	`, s.Talkgroup, s.Favorite, s.Muted, s.Transcribe, time.Now().Unix())
	return err
}

// ShouldTranscribeTalkgroup returns true only when the talkgroup has TX enabled.
func (d *DB) ShouldTranscribeTalkgroup(talkgroup int) (bool, error) {
	var allowed bool
	err := d.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM talkgroup_settings
			WHERE talkgroup = $1 AND transcribe = TRUE
		)`, talkgroup).Scan(&allowed)
	return allowed, err
}

// DeleteTalkgroup removes current stored calls and settings for a talkgroup.
func (d *DB) DeleteTalkgroup(talkgroup int) (int64, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM talkgroup_settings WHERE talkgroup = $1`, talkgroup); err != nil {
		return 0, err
	}

	result, err := tx.Exec(`DELETE FROM calls WHERE talkgroup = $1`, talkgroup)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// ListDistinctTalkgroups returns one visible entry per talkgroup ID using metadata from the most recent call.
func (d *DB) ListDistinctTalkgroups(userID string, includeAll bool) ([]TalkgroupInfo, error) {
	query := `
		SELECT DISTINCT ON (c.talkgroup) c.talkgroup, c.talkgroup_label, c.talkgroup_group, c.system_label
		FROM calls c
		LEFT JOIN ingestion_sources s ON s.id = c.source_id
		WHERE c.talkgroup > 0`
	args := []any{}
	if !includeAll {
		query += `
			AND (
				c.user_id = $1
				OR s.user_id = $1
				OR s.is_shared = TRUE
				OR EXISTS (
					SELECT 1 FROM ingestion_source_user_shares sh
					WHERE sh.source_id = c.source_id AND sh.user_id = $1
				)
			)`
		args = append(args, userID)
	}
	query += ` ORDER BY c.talkgroup, c.datetime DESC`
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tgs := make([]TalkgroupInfo, 0)
	for rows.Next() {
		var t TalkgroupInfo
		if err := rows.Scan(&t.Talkgroup, &t.TalkgroupLabel, &t.TalkgroupGroup, &t.SystemLabel); err != nil {
			return nil, err
		}
		tgs = append(tgs, t)
	}
	return tgs, rows.Err()
}
