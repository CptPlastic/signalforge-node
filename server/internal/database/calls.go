package database

import (
	"fmt"
	"strings"
	"time"
)

// InsertCall stores a new call and returns its generated ID.
func (d *DB) InsertCall(c *Call, audio []byte) (int64, error) {
	var id int64
	err := d.db.QueryRow(`
		INSERT INTO calls
			(user_id, source_id, datetime, system, system_label, talkgroup, talkgroup_label,
			 talkgroup_group, talkgroup_tag, frequency, duration,
			 audio_name, audio_type, audio, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id`,
		nullableString(c.UserID), nullableString(c.SourceID), c.DateTime, c.System, c.SystemLabel, c.Talkgroup, c.TalkgroupLabel,
		c.TalkgroupGroup, c.TalkgroupTag, c.Frequency, c.Duration,
		c.AudioName, c.AudioType, audio, time.Now().Unix(),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListCalls returns calls without audio blobs using validated sort and optional search.
func (d *DB) ListCalls(params ListCallsParams) ([]Call, error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	sortBy := map[string]string{
		"datetime":  "datetime",
		"duration":  "duration",
		"frequency": "frequency",
		"talkgroup": "talkgroup",
	}[strings.ToLower(params.SortBy)]
	if sortBy == "" {
		sortBy = "datetime"
	}

	order := strings.ToUpper(params.Order)
	if order != "ASC" {
		order = "DESC"
	}

	baseQuery := `
		SELECT id, COALESCE(user_id, ''), COALESCE(source_id, ''), datetime, system, system_label, talkgroup, talkgroup_label,
		       talkgroup_group, talkgroup_tag, frequency, duration,
		       audio_name, audio_type, created_at
		FROM calls`
	args := make([]any, 0, 4)
	argPos := 1
	filters := make([]string, 0, 2)
	if params.UserID != "" {
		filters = append(filters, fmt.Sprintf("(user_id = $%d OR user_id IS NULL)", argPos))
		args = append(args, params.UserID)
		argPos += 1
	} else if params.OnlyUnowned {
		filters = append(filters, "user_id IS NULL")
	}
	if search := strings.TrimSpace(params.Search); search != "" {
		p1 := fmt.Sprintf("$%d", argPos)
		p2 := fmt.Sprintf("$%d", argPos+1)
		p3 := fmt.Sprintf("$%d", argPos+2)
		p4 := fmt.Sprintf("$%d", argPos+3)
		filters = append(filters,
			`(LOWER(system_label) LIKE LOWER(`+p1+`)
		   OR LOWER(talkgroup_label) LIKE LOWER(`+p2+`)
		   OR LOWER(talkgroup_group) LIKE LOWER(`+p3+`)
		   OR CAST(talkgroup AS TEXT) LIKE `+p4+`)`)
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern, pattern)
		argPos += 4
	}
	if len(params.Talkgroups) > 0 {
		placeholders := make([]string, len(params.Talkgroups))
		for i, tg := range params.Talkgroups {
			placeholders[i] = fmt.Sprintf("$%d", argPos)
			args = append(args, tg)
			argPos++
		}
		filters = append(filters, "talkgroup IN ("+strings.Join(placeholders, ",")+")")
	}
	if group := strings.TrimSpace(params.Group); group != "" {
		filters = append(filters, fmt.Sprintf("LOWER(talkgroup_group) LIKE LOWER($%d)", argPos))
		args = append(args, "%"+group+"%")
		argPos++
	}
	if len(filters) > 0 {
		baseQuery += " WHERE " + strings.Join(filters, " AND ")
	}

	query := fmt.Sprintf("%s ORDER BY %s %s LIMIT $%d OFFSET $%d", baseQuery, sortBy, order, argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	calls := make([]Call, 0)
	for rows.Next() {
		var c Call
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.SourceID, &c.DateTime, &c.System, &c.SystemLabel,
			&c.Talkgroup, &c.TalkgroupLabel, &c.TalkgroupGroup, &c.TalkgroupTag,
			&c.Frequency, &c.Duration, &c.AudioName, &c.AudioType, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}

// GetCallAudio returns the raw audio bytes, MIME type, filename, owning user, and source ID for a call.
func (d *DB) GetCallAudio(id int64) ([]byte, string, string, string, string, error) {
	var audio []byte
	var audioType string
	var audioName string
	var userID string
	var sourceID string
	err := d.db.QueryRow(
		`SELECT audio, audio_type, COALESCE(audio_name, ''), COALESCE(user_id, ''), COALESCE(source_id, '') FROM calls WHERE id = $1`, id,
	).Scan(&audio, &audioType, &audioName, &userID, &sourceID)
	return audio, audioType, audioName, userID, sourceID, err
}

// GetRecentCallIDsForTalkgroups returns call IDs (oldest-first) for stream seeding.
func (d *DB) GetRecentCallIDsForTalkgroups(userID string, talkgroups []int, limit int) ([]int64, error) {
	if len(talkgroups) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(talkgroups)+2)
	args = append(args, userID)
	ph := make([]string, len(talkgroups))
	for i, tg := range talkgroups {
		ph[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, tg)
	}
	args = append(args, limit)
	q := fmt.Sprintf(
		`SELECT id FROM calls WHERE user_id = $1 AND talkgroup IN (%s) ORDER BY datetime DESC LIMIT $%d`,
		strings.Join(ph, ","), len(talkgroups)+2,
	)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse so oldest plays first.
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	return ids, nil
}

// GetRecentCallsForTalkgroups returns call metadata (no audio) for the most recent
// calls across the given talkgroups, oldest-first, for SSE seeding.
func (d *DB) GetRecentCallsForTalkgroups(userID string, talkgroups []int, limit int) ([]Call, error) {
	if len(talkgroups) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(talkgroups)+2)
	args = append(args, userID)
	ph := make([]string, len(talkgroups))
	for i, tg := range talkgroups {
		ph[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, tg)
	}
	args = append(args, limit)
	q := fmt.Sprintf(
		`SELECT id, COALESCE(user_id,''), datetime, system, system_label, talkgroup, talkgroup_label,
		        talkgroup_group, talkgroup_tag, frequency, duration, audio_name, audio_type, created_at
		 FROM calls WHERE user_id = $1 AND talkgroup IN (%s) ORDER BY datetime DESC LIMIT $%d`,
		strings.Join(ph, ","), len(talkgroups)+2,
	)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var calls []Call
	for rows.Next() {
		var c Call
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.DateTime, &c.System, &c.SystemLabel,
			&c.Talkgroup, &c.TalkgroupLabel, &c.TalkgroupGroup, &c.TalkgroupTag,
			&c.Frequency, &c.Duration, &c.AudioName, &c.AudioType, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse so oldest displays first.
	for i, j := 0, len(calls)-1; i < j; i, j = i+1, j-1 {
		calls[i], calls[j] = calls[j], calls[i]
	}
	return calls, nil
}
