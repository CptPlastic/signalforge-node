package database

import (
	"database/sql"
	"time"
)

// EnsureTranscriptionQueueRows backfills missing queue rows for existing calls.
func (d *DB) EnsureTranscriptionQueueRows() error {
	now := time.Now().Unix()
	_, err := d.db.Exec(`
		INSERT INTO call_transcripts (call_id, status, created_at, updated_at)
		SELECT c.id, 'pending', $1, $1
		FROM calls c
		WHERE NOT EXISTS (
			SELECT 1 FROM call_transcripts ct WHERE ct.call_id = c.id
		)
		ON CONFLICT (call_id) DO NOTHING`, now)
	return err
}

// ClaimTranscriptionJob leases one pending call for a transcription worker.
func (d *DB) ClaimTranscriptionJob(workerID string, leaseSeconds int64) (*TranscriptionJob, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = 300
	}
	now := time.Now().Unix()
	claimedUntil := now + leaseSeconds

	if err := d.EnsureTranscriptionQueueRows(); err != nil {
		return nil, err
	}

	var job TranscriptionJob
	err := d.db.QueryRow(`
		WITH candidate AS (
			SELECT call_id
			FROM call_transcripts
			WHERE status = 'pending'
			   OR (status = 'processing' AND claimed_until < $1)
			ORDER BY call_id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE call_transcripts ct
			SET status = 'processing',
			    attempts = attempts + 1,
			    claimed_by = $2,
			    claimed_until = $3,
			    error = '',
			    updated_at = $1
			FROM candidate
			WHERE ct.call_id = candidate.call_id
			RETURNING ct.call_id, ct.attempts, ct.claimed_until
		)
		SELECT c.id, c.audio_name, c.audio_type, c.duration, c.system_label,
		       c.talkgroup, c.talkgroup_label, c.talkgroup_group,
		       claimed.attempts, claimed.claimed_until
		FROM claimed
		JOIN calls c ON c.id = claimed.call_id`, now, workerID, claimedUntil).Scan(
		&job.CallID, &job.AudioName, &job.AudioType, &job.Duration, &job.SystemLabel,
		&job.Talkgroup, &job.TalkgroupLabel, &job.TalkgroupGroup, &job.Attempts, &job.ClaimedUntil,
	)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// CompleteTranscriptionJob stores a transcript and marks the job complete.
func (d *DB) CompleteTranscriptionJob(callID int64, transcript, provider, language string, confidence float64) error {
	_, err := d.db.Exec(`
		UPDATE call_transcripts
		SET status = 'done',
		    transcript = $2,
		    provider = $3,
		    language = $4,
		    confidence = $5,
		    error = '',
		    claimed_by = '',
		    claimed_until = 0,
		    updated_at = $6
		WHERE call_id = $1`, callID, transcript, provider, language, confidence, time.Now().Unix())
	return err
}

// FailTranscriptionJob records a worker failure for a call.
func (d *DB) FailTranscriptionJob(callID int64, message string) error {
	_, err := d.db.Exec(`
		UPDATE call_transcripts
		SET status = 'failed',
		    error = $2,
		    claimed_by = '',
		    claimed_until = 0,
		    updated_at = $3
		WHERE call_id = $1`, callID, message, time.Now().Unix())
	return err
}

// IsNoTranscriptionJob reports whether a claim query found no available work.
func IsNoTranscriptionJob(err error) bool {
	return err == sql.ErrNoRows
}
