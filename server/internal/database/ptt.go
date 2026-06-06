package database

import (
	"database/sql"
	"errors"
	"time"
)

// GetRadioSetForPTT returns a radio set by ID with no user-ownership scoping.
// PTT lets any authenticated hub user with tx_enabled transmit into any radio
// set on the hub, so this read is intentionally not user-scoped.
func (d *DB) GetRadioSetForPTT(id string) (RadioSet, bool, error) {
	row := d.db.QueryRow(`
		SELECT id, user_id, name, selection_mode, talkgroups, talkgroup_groups, share_token, ptt_talkgroup, created_at, updated_at
		FROM radio_sets WHERE id = $1`, id)
	rs, err := scanRadioSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RadioSet{}, false, nil
		}
		return RadioSet{}, false, err
	}
	return rs, true, nil
}

// GetPTTUploadCallID looks up an existing PTT upload by clientId for idempotency.
// Returns (callID, true, nil) on hit, (0, false, nil) on miss.
func (d *DB) GetPTTUploadCallID(clientID string) (int64, bool, error) {
	var callID int64
	err := d.db.QueryRow(`SELECT call_id FROM ptt_uploads WHERE client_id = $1`, clientID).Scan(&callID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return callID, true, nil
}

// RecordPTTUpload writes the idempotency record linking clientId -> callId.
// Returns an error if clientId is already taken (race with a concurrent retry).
func (d *DB) RecordPTTUpload(clientID string, callID int64, userID string) error {
	_, err := d.db.Exec(`
		INSERT INTO ptt_uploads (client_id, call_id, user_id, created_at)
		VALUES ($1, $2, $3, $4)`,
		clientID, callID, userID, time.Now().Unix())
	return err
}
