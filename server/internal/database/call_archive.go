package database

import (
	"fmt"
	"strings"
)

// CallStorageStats summarizes call table size for admin retention decisions.
type CallStorageStats struct {
	CallCount    int64 `json:"callCount"`
	AudioBytes   int64 `json:"audioBytes"`
	OldestCallAt int64 `json:"oldestCallAt"`
	NewestCallAt int64 `json:"newestCallAt"`
}

// CallArchiveRecord is a call row including audio bytes for export before deletion.
type CallArchiveRecord struct {
	Call           Call
	Audio          []byte
	TranscriptText string
}

// GetCallStorageStats returns aggregate call/audio size metrics.
func (d *DB) GetCallStorageStats() (CallStorageStats, error) {
	var stats CallStorageStats
	err := d.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(octet_length(audio)), 0),
		       COALESCE(MIN(datetime), 0),
		       COALESCE(MAX(datetime), 0)
		FROM calls`).Scan(&stats.CallCount, &stats.AudioBytes, &stats.OldestCallAt, &stats.NewestCallAt)
	return stats, err
}

// CountCallsOlderThan returns how many calls have datetime strictly before cutoffUnix.
func (d *DB) CountCallsOlderThan(cutoffUnix int64) (int64, error) {
	var count int64
	err := d.db.QueryRow(`SELECT COUNT(*) FROM calls WHERE datetime < $1`, cutoffUnix).Scan(&count)
	return count, err
}

// SumAudioBytesOlderThan returns total audio bytes for calls older than cutoffUnix.
func (d *DB) SumAudioBytesOlderThan(cutoffUnix int64) (int64, error) {
	var total int64
	err := d.db.QueryRow(
		`SELECT COALESCE(SUM(octet_length(audio)), 0) FROM calls WHERE datetime < $1`,
		cutoffUnix,
	).Scan(&total)
	return total, err
}

// ListCallsForArchive returns calls (with audio) older than cutoffUnix, oldest first.
func (d *DB) ListCallsForArchive(cutoffUnix int64, limit int) ([]CallArchiveRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.Query(`
		SELECT c.id, COALESCE(c.user_id, ''), COALESCE(c.source_id, ''), c.datetime, c.system, c.system_label,
		       c.talkgroup, c.talkgroup_label, c.talkgroup_group, c.talkgroup_tag, c.frequency, c.duration,
		       c.audio_name, c.audio_type, COALESCE(ct.transcript, ''), COALESCE(ct.status, ''), COALESCE(ct.provider, ''),
		       COALESCE(c.origin, 'rf'), COALESCE(c.sender_user_id, ''), c.created_at, c.audio
		FROM calls c
		LEFT JOIN call_transcripts ct ON ct.call_id = c.id
		WHERE c.datetime < $1
		ORDER BY c.datetime ASC, c.id ASC
		LIMIT $2`, cutoffUnix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CallArchiveRecord, 0)
	for rows.Next() {
		var rec CallArchiveRecord
		if err := rows.Scan(
			&rec.Call.ID, &rec.Call.UserID, &rec.Call.SourceID, &rec.Call.DateTime, &rec.Call.System, &rec.Call.SystemLabel,
			&rec.Call.Talkgroup, &rec.Call.TalkgroupLabel, &rec.Call.TalkgroupGroup, &rec.Call.TalkgroupTag,
			&rec.Call.Frequency, &rec.Call.Duration, &rec.Call.AudioName, &rec.Call.AudioType,
			&rec.TranscriptText, &rec.Call.TranscriptStatus, &rec.Call.TranscriptProvider,
			&rec.Call.Origin, &rec.Call.SenderUserID, &rec.Call.CreatedAt, &rec.Audio,
		); err != nil {
			return nil, err
		}
		rec.Call.TranscriptText = rec.TranscriptText
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteCallsByIDs removes calls and dependent rows (transcripts, ptt uploads, federation imports).
func (d *DB) DeleteCallsByIDs(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := `DELETE FROM calls WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	result, err := d.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
