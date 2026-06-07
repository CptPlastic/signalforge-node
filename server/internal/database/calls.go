package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// InsertCall stores a new call and returns its generated ID.
func (d *DB) InsertCall(c *Call, audio []byte) (int64, error) {
	var id int64
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	origin := c.Origin
	if origin == "" {
		origin = "rf"
	}
	err = tx.QueryRow(`
		INSERT INTO calls
			(user_id, source_id, datetime, system, system_label, talkgroup, talkgroup_label,
			 talkgroup_group, talkgroup_tag, frequency, duration,
			 audio_name, audio_type, audio, origin, sender_user_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id`,
		nullableString(c.UserID), nullableString(c.SourceID), c.DateTime, c.System, c.SystemLabel, c.Talkgroup, c.TalkgroupLabel,
		c.TalkgroupGroup, c.TalkgroupTag, c.Frequency, c.Duration,
		c.AudioName, c.AudioType, audio, origin, nullableString(c.SenderUserID), time.Now().Unix(),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	c.ID = id
	return id, nil
}

// ListFederatedCalls returns shared-source calls newer than the supplied local call ID.
func (d *DB) ListFederatedCalls(sinceID int64, limit int) ([]FederatedCall, error) {
	if limit <= 0 || limit > 250 {
		limit = 100
	}
	rows, err := d.db.Query(`
		SELECT c.id, COALESCE(c.user_id, ''), COALESCE(c.source_id, ''), c.datetime, c.system, c.system_label,
		       c.talkgroup, c.talkgroup_label, c.talkgroup_group, c.talkgroup_tag, c.frequency, c.duration,
		       c.audio_name, c.audio_type, c.created_at, c.audio
		FROM calls c
		JOIN ingestion_sources s ON s.id = c.source_id
		WHERE c.id > $1
		  AND s.is_shared = TRUE
		  AND s.enabled = TRUE
		  AND s.deleted_at = 0
		  AND s.id NOT LIKE 'remote\_%' ESCAPE '\'
		ORDER BY c.id ASC
		LIMIT $2`, sinceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	calls := make([]FederatedCall, 0)
	for rows.Next() {
		var item FederatedCall
		if err := rows.Scan(
			&item.Call.ID, &item.Call.UserID, &item.Call.SourceID, &item.Call.DateTime, &item.Call.System, &item.Call.SystemLabel,
			&item.Call.Talkgroup, &item.Call.TalkgroupLabel, &item.Call.TalkgroupGroup, &item.Call.TalkgroupTag,
			&item.Call.Frequency, &item.Call.Duration, &item.Call.AudioName, &item.Call.AudioType, &item.Call.CreatedAt, &item.Audio,
		); err != nil {
			return nil, err
		}
		item.Source = item.Call.SourceID
		calls = append(calls, item)
	}
	return calls, rows.Err()
}

// CountFederatedCalls returns the number of calls currently eligible for federation export.
func (d *DB) CountFederatedCalls() (int64, error) {
	var count int64
	err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM calls c
		JOIN ingestion_sources s ON s.id = c.source_id
		WHERE s.is_shared = TRUE
		  AND s.enabled = TRUE
		  AND s.deleted_at = 0
		  AND s.id NOT LIKE 'remote\_%' ESCAPE '\'`).Scan(&count)
	return count, err
}

// CountImportedFederatedCalls returns calls imported from remote hubs.
func (d *DB) CountImportedFederatedCalls() (int64, error) {
	var count int64
	err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM calls
		WHERE source_id LIKE 'remote\_%' ESCAPE '\'`).Scan(&count)
	return count, err
}

// CountImportedFederatedCallsFromPeer returns how many calls were imported from one peer hub.
func (d *DB) CountImportedFederatedCallsFromPeer(peerHubID string) (int64, error) {
	var count int64
	err := d.db.QueryRow(`SELECT COUNT(*) FROM federation_call_imports WHERE peer_hub_id = $1`, peerHubID).Scan(&count)
	return count, err
}

// MaxImportedRemoteCallID returns the highest remote call ID imported from a peer.
func (d *DB) MaxImportedRemoteCallID(peerHubID string) (int64, error) {
	var maxID sql.NullInt64
	err := d.db.QueryRow(`SELECT MAX(remote_call_id) FROM federation_call_imports WHERE peer_hub_id = $1`, peerHubID).Scan(&maxID)
	if err != nil {
		return 0, err
	}
	if !maxID.Valid {
		return 0, nil
	}
	return maxID.Int64, nil
}

// RecordFederatedCallImport stores the mapping between a peer call ID and the local call ID.
func (d *DB) RecordFederatedCallImport(peerHubID string, remoteCallID, localCallID int64) (bool, error) {
	result, err := d.db.Exec(`
		INSERT INTO federation_call_imports (peer_hub_id, remote_call_id, local_call_id, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (peer_hub_id, remote_call_id) DO NOTHING`, peerHubID, remoteCallID, localCallID, time.Now().Unix())
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// ListCalls returns calls without audio blobs using validated sort and optional search.
func (d *DB) ListCalls(params ListCallsParams) ([]Call, error) {
	limit, offset := normalizeListCallsPaging(params)
	orderByClause := normalizeListCallsSort(params)

	baseQuery := `
		SELECT c.id, COALESCE(c.user_id, ''), COALESCE(c.source_id, ''), c.datetime, c.system, c.system_label, c.talkgroup, c.talkgroup_label,
		       c.talkgroup_group, c.talkgroup_tag, c.frequency, c.duration,
		       c.audio_name, c.audio_type, COALESCE(ct.transcript, ''), COALESCE(ct.status, ''), COALESCE(ct.provider, ''),
		       COALESCE(c.origin, 'rf'), COALESCE(c.sender_user_id, ''), c.created_at
		FROM calls c
		LEFT JOIN call_transcripts ct ON ct.call_id = c.id`
	args := make([]any, 0, 4)
	argPos := 1
	filters := make([]string, 0, 2)
	if params.UserID != "" {
		filters = append(filters, fmt.Sprintf("(c.user_id = $%d OR c.user_id IS NULL)", argPos))
		args = append(args, params.UserID)
		argPos += 1
	} else if params.OnlyUnowned {
		filters = append(filters, "c.user_id IS NULL")
	}
	appendSearchFilter(params.Search, &filters, &args, &argPos)
	appendTalkgroupsFilter(params.Talkgroups, &filters, &args, &argPos)
	appendGroupsFilter(params.Groups, &filters, &args, &argPos)
	appendGroupFilter(params.Group, &filters, &args, &argPos)
	if len(filters) > 0 {
		baseQuery += " WHERE " + strings.Join(filters, " AND ")
	}

	query := fmt.Sprintf("%s ORDER BY %s LIMIT $%d OFFSET $%d", baseQuery, orderByClause, argPos, argPos+1)
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
			&c.Frequency, &c.Duration, &c.AudioName, &c.AudioType, &c.TranscriptText, &c.TranscriptStatus, &c.TranscriptProvider,
			&c.Origin, &c.SenderUserID, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}

// GetRecentCallsForTalkgroupGroups returns call metadata for recent calls whose
// talkgroup_group is in groups, oldest-first, for public player seeding.
func (d *DB) GetRecentCallsForTalkgroupGroups(userID string, groups []string, limit int) ([]Call, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(groups)+2)
	args = append(args, userID)
	ph := make([]string, len(groups))
	for i, group := range groups {
		ph[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, group)
	}
	args = append(args, limit)
	q := fmt.Sprintf(
		`SELECT DISTINCT c.id, COALESCE(c.user_id,''), COALESCE(c.source_id,''), c.datetime, c.system, c.system_label,
		        c.talkgroup, c.talkgroup_label, c.talkgroup_group, c.talkgroup_tag, c.frequency, c.duration,
		        c.audio_name, c.audio_type, c.created_at
		 FROM calls c
		 LEFT JOIN ingestion_sources s ON s.id = c.source_id
		 WHERE (c.user_id = $1 OR s.user_id = $1 OR s.is_shared = TRUE OR EXISTS (
			 SELECT 1 FROM ingestion_source_keys k
			 WHERE k.source_id = c.source_id AND k.user_id = $1
		 ) OR EXISTS (
			 SELECT 1 FROM ingestion_source_user_shares sh
			 WHERE sh.source_id = c.source_id AND sh.user_id = $1
		 ) OR (s.id LIKE 'remote\_%%' ESCAPE '\' AND s.enabled = TRUE AND s.deleted_at = 0))
		 AND c.talkgroup_group IN (%s)
		 ORDER BY c.datetime DESC
		 LIMIT $%d`,
		strings.Join(ph, ","), len(groups)+2,
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
			&c.ID, &c.UserID, &c.SourceID, &c.DateTime, &c.System, &c.SystemLabel,
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
	for i, j := 0, len(calls)-1; i < j; i, j = i+1, j-1 {
		calls[i], calls[j] = calls[j], calls[i]
	}
	return calls, nil
}

// GetRecentCallsForRadioSet seeds the public player with recent calls for a set.
func (d *DB) GetRecentCallsForRadioSet(userID string, rs RadioSet, limit int) ([]Call, error) {
	pttTalkgroups := make([]int, 0, 1)
	if rs.PTTTalkgroup != nil {
		pttTalkgroups = append(pttTalkgroups, *rs.PTTTalkgroup)
	}
	if rs.IsGroupsMode() {
		if len(rs.TalkgroupGroups) == 0 && len(pttTalkgroups) == 0 {
			return nil, nil
		}
		calls, err := d.GetRecentCallsForTalkgroupGroups(userID, rs.TalkgroupGroups, limit)
		if err != nil || len(pttTalkgroups) == 0 {
			return calls, err
		}
		pttCalls, err := d.GetRecentCallsForTalkgroups(userID, pttTalkgroups, limit)
		if err != nil {
			return calls, err
		}
		return mergeRecentCallsByDateTime(calls, pttCalls, limit), nil
	}
	talkgroups := append(append([]int(nil), rs.Talkgroups...), pttTalkgroups...)
	if len(talkgroups) == 0 {
		return nil, nil
	}
	return d.GetRecentCallsForTalkgroups(userID, talkgroups, limit)
}

func mergeRecentCallsByDateTime(primary, extra []Call, limit int) []Call {
	seen := make(map[int64]struct{}, limit)
	merged := make([]Call, 0, limit)
	appendCall := func(call Call) {
		if len(merged) >= limit {
			return
		}
		if _, ok := seen[call.ID]; ok {
			return
		}
		seen[call.ID] = struct{}{}
		merged = append(merged, call)
	}
	for _, call := range primary {
		appendCall(call)
	}
	for _, call := range extra {
		appendCall(call)
	}
	return merged
}

// ListCallsSince returns calls with id greater than sinceID ordered ascending by
// id (no audio blobs). It is used to replay missed calls to a reconnecting
// WebSocket client so no traffic is lost across a dropped connection.
func (d *DB) ListCallsSince(sinceID int64, limit int) ([]Call, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	rows, err := d.db.Query(`
		SELECT c.id, COALESCE(c.user_id, ''), COALESCE(c.source_id, ''), c.datetime, c.system, c.system_label, c.talkgroup, c.talkgroup_label,
		       c.talkgroup_group, c.talkgroup_tag, c.frequency, c.duration,
		       c.audio_name, c.audio_type, COALESCE(ct.transcript, ''), COALESCE(ct.status, ''), COALESCE(ct.provider, ''),
		       COALESCE(c.origin, 'rf'), COALESCE(c.sender_user_id, ''), c.created_at
		FROM calls c
		LEFT JOIN call_transcripts ct ON ct.call_id = c.id
		WHERE c.id > $1
		ORDER BY c.id ASC
		LIMIT $2`, sinceID, limit)
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
			&c.Frequency, &c.Duration, &c.AudioName, &c.AudioType, &c.TranscriptText, &c.TranscriptStatus, &c.TranscriptProvider,
			&c.Origin, &c.SenderUserID, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}

func normalizeListCallsPaging(params ListCallsParams) (int, int) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func normalizeListCallsSort(params ListCallsParams) string {
	sortKey := strings.ToLower(params.SortBy)
	sortBy := map[string]string{
		"datetime":  "c.datetime",
		"duration":  "c.duration",
		"frequency": "c.frequency",
		"talkgroup": "c.talkgroup",
	}[sortKey]
	if sortBy == "" {
		sortBy = "c.datetime"
	}

	if strings.ToUpper(params.Order) == "ASC" {
		switch sortKey {
		case "duration", "frequency", "talkgroup":
			return sortBy + " ASC"
		default:
			return "c.datetime ASC"
		}
	}

	switch sortKey {
	case "duration", "frequency", "talkgroup":
		return sortBy + " DESC"
	default:
		return "c.datetime DESC"
	}
}

func appendSearchFilter(search string, filters *[]string, args *[]any, argPos *int) {
	value := strings.TrimSpace(search)
	if value == "" {
		return
	}
	p1 := fmt.Sprintf("$%d", *argPos)
	p2 := fmt.Sprintf("$%d", *argPos+1)
	p3 := fmt.Sprintf("$%d", *argPos+2)
	p4 := fmt.Sprintf("$%d", *argPos+3)
	*filters = append(*filters,
		`(LOWER(c.system_label) LIKE LOWER(`+p1+`)
	   OR LOWER(c.talkgroup_label) LIKE LOWER(`+p2+`)
	   OR LOWER(c.talkgroup_group) LIKE LOWER(`+p3+`)
	   OR CAST(c.talkgroup AS TEXT) LIKE `+p4+`
	   OR LOWER(COALESCE(ct.transcript, '')) LIKE LOWER(`+p1+`))`)
	pattern := "%" + value + "%"
	*args = append(*args, pattern, pattern, pattern, pattern)
	*argPos += 4
}

func appendTalkgroupsFilter(talkgroups []int, filters *[]string, args *[]any, argPos *int) {
	if len(talkgroups) == 0 {
		return
	}
	placeholders := make([]string, len(talkgroups))
	for i, tg := range talkgroups {
		placeholders[i] = fmt.Sprintf("$%d", *argPos)
		*args = append(*args, tg)
		(*argPos)++
	}
	*filters = append(*filters, "c.talkgroup IN ("+strings.Join(placeholders, ",")+")")
}

func appendGroupsFilter(groups []string, filters *[]string, args *[]any, argPos *int) {
	if len(groups) == 0 {
		return
	}
	placeholders := make([]string, len(groups))
	for i, group := range groups {
		placeholders[i] = fmt.Sprintf("$%d", *argPos)
		*args = append(*args, group)
		*argPos++
	}
	*filters = append(*filters, fmt.Sprintf("c.talkgroup_group IN (%s)", strings.Join(placeholders, ",")))
}

func appendGroupFilter(group string, filters *[]string, args *[]any, argPos *int) {
	value := strings.TrimSpace(group)
	if value == "" {
		return
	}
	*filters = append(*filters, fmt.Sprintf("LOWER(c.talkgroup_group) LIKE LOWER($%d)", *argPos))
	*args = append(*args, "%"+value+"%")
	(*argPos)++
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
		`SELECT DISTINCT c.id, c.datetime
		 FROM calls c
		 LEFT JOIN ingestion_sources s ON s.id = c.source_id
		 WHERE (c.user_id = $1 OR s.user_id = $1 OR s.is_shared = TRUE OR EXISTS (
			 SELECT 1 FROM ingestion_source_keys k
			 WHERE k.source_id = c.source_id AND k.user_id = $1
		 ) OR EXISTS (
			 SELECT 1 FROM ingestion_source_user_shares sh
			 WHERE sh.source_id = c.source_id AND sh.user_id = $1
		 ) OR (s.id LIKE 'remote\_%%' ESCAPE '\' AND s.enabled = TRUE AND s.deleted_at = 0))
		 AND c.talkgroup IN (%s)
		 ORDER BY c.datetime DESC
		 LIMIT $%d`,
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
		var dateTime int64
		if err := rows.Scan(&id, &dateTime); err != nil {
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
		`SELECT DISTINCT c.id, COALESCE(c.user_id,''), COALESCE(c.source_id,''), c.datetime, c.system, c.system_label,
		        c.talkgroup, c.talkgroup_label, c.talkgroup_group, c.talkgroup_tag, c.frequency, c.duration,
		        c.audio_name, c.audio_type, c.created_at
		 FROM calls c
		 LEFT JOIN ingestion_sources s ON s.id = c.source_id
		 WHERE (c.user_id = $1 OR s.user_id = $1 OR s.is_shared = TRUE OR EXISTS (
			 SELECT 1 FROM ingestion_source_keys k
			 WHERE k.source_id = c.source_id AND k.user_id = $1
		 ) OR EXISTS (
			 SELECT 1 FROM ingestion_source_user_shares sh
			 WHERE sh.source_id = c.source_id AND sh.user_id = $1
		 ) OR (s.id LIKE 'remote\_%%' ESCAPE '\' AND s.enabled = TRUE AND s.deleted_at = 0))
		 AND c.talkgroup IN (%s)
		 ORDER BY c.datetime DESC
		 LIMIT $%d`,
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
			&c.ID, &c.UserID, &c.SourceID, &c.DateTime, &c.System, &c.SystemLabel,
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
