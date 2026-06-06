package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func normalizeRadioSetSelectionMode(mode string) string {
	if strings.TrimSpace(mode) == "groups" {
		return "groups"
	}
	return "talkgroups"
}

// CreateRadioSet inserts a new radio set owned by the given user.
//
// Each radio set is auto-allocated a unique PTT talkgroup ID from the
// ptt_talkgroup_seq sequence (range 9_000_001+). This virtual TG is used by
// the in-hub push-to-talk feature; subscribers of the set receive PTT calls
// alongside real RF traffic.
func (d *DB) CreateRadioSet(userID, name, selectionMode string, talkgroups []int, talkgroupGroups []string) (RadioSet, error) {
	id := randomToken("rs_")
	now := time.Now().Unix()
	selectionMode = normalizeRadioSetSelectionMode(selectionMode)
	if talkgroups == nil {
		talkgroups = []int{}
	}
	if talkgroupGroups == nil {
		talkgroupGroups = []string{}
	}
	tgsJSON, err := json.Marshal(talkgroups)
	if err != nil {
		return RadioSet{}, err
	}
	groupsJSON, err := json.Marshal(talkgroupGroups)
	if err != nil {
		return RadioSet{}, err
	}
	var pttTG int
	if err := d.db.QueryRow(`SELECT nextval('ptt_talkgroup_seq')`).Scan(&pttTG); err != nil {
		return RadioSet{}, fmt.Errorf("allocate ptt talkgroup: %w", err)
	}
	_, err = d.db.Exec(`
		INSERT INTO radio_sets (id, user_id, name, selection_mode, talkgroups, talkgroup_groups, ptt_talkgroup, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, userID, name, selectionMode, string(tgsJSON), string(groupsJSON), pttTG, now, now)
	if err != nil {
		return RadioSet{}, err
	}
	return RadioSet{
		ID:              id,
		UserID:          userID,
		Name:            name,
		SelectionMode:   selectionMode,
		Talkgroups:      talkgroups,
		TalkgroupGroups: talkgroupGroups,
		PTTTalkgroup:    &pttTG,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// ListRadioSets returns all radio sets owned by the given user.
func (d *DB) ListRadioSets(userID string) ([]RadioSet, error) {
	rows, err := d.db.Query(`
		SELECT id, user_id, name, selection_mode, talkgroups, talkgroup_groups, share_token, ptt_talkgroup, created_at, updated_at
		FROM radio_sets
		WHERE user_id = $1
		ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sets := make([]RadioSet, 0)
	for rows.Next() {
		rs, err := scanRadioSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, rs)
	}
	return sets, rows.Err()
}

// ListAllRadioSets returns all radio sets for admin visibility.
func (d *DB) ListAllRadioSets() ([]RadioSet, error) {
	rows, err := d.db.Query(`
		SELECT id, user_id, name, selection_mode, talkgroups, talkgroup_groups, share_token, ptt_talkgroup, created_at, updated_at
		FROM radio_sets
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sets := make([]RadioSet, 0)
	for rows.Next() {
		rs, err := scanRadioSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, rs)
	}
	return sets, rows.Err()
}

// ListSourceIDsForTalkgroups returns source IDs that have produced calls for any of the given talkgroups.
func (d *DB) ListSourceIDsForTalkgroups(talkgroups []int) ([]string, error) {
	if len(talkgroups) == 0 {
		return []string{}, nil
	}
	args := make([]any, 0, len(talkgroups))
	placeholders := make([]string, len(talkgroups))
	for i, tg := range talkgroups {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, tg)
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT source_id
		FROM calls
		WHERE source_id <> '' AND talkgroup IN (%s)
		ORDER BY source_id ASC`, strings.Join(placeholders, ","))
	rows, err := d.db.Query(query, args...)
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

// ListSourceIDsForTalkgroupGroups returns source IDs that have produced calls for any of the given groups.
func (d *DB) ListSourceIDsForTalkgroupGroups(groups []string) ([]string, error) {
	if len(groups) == 0 {
		return []string{}, nil
	}
	args := make([]any, 0, len(groups))
	placeholders := make([]string, len(groups))
	for i, group := range groups {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, group)
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT source_id
		FROM calls
		WHERE source_id <> '' AND talkgroup_group IN (%s)
		ORDER BY source_id ASC`, strings.Join(placeholders, ","))
	rows, err := d.db.Query(query, args...)
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

// GetRadioSet returns a single radio set by ID, scoped to the given user.
func (d *DB) GetRadioSet(id, userID string) (RadioSet, bool, error) {
	row := d.db.QueryRow(`
		SELECT id, user_id, name, selection_mode, talkgroups, talkgroup_groups, share_token, ptt_talkgroup, created_at, updated_at
		FROM radio_sets WHERE id = $1 AND user_id = $2`, id, userID)
	rs, err := scanRadioSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RadioSet{}, false, nil
		}
		return RadioSet{}, false, err
	}
	return rs, true, nil
}

// UpdateRadioSet replaces the name and membership of an existing set.
func (d *DB) UpdateRadioSet(id, userID, name, selectionMode string, talkgroups []int, talkgroupGroups []string) error {
	selectionMode = normalizeRadioSetSelectionMode(selectionMode)
	if talkgroups == nil {
		talkgroups = []int{}
	}
	if talkgroupGroups == nil {
		talkgroupGroups = []string{}
	}
	tgsJSON, err := json.Marshal(talkgroups)
	if err != nil {
		return err
	}
	groupsJSON, err := json.Marshal(talkgroupGroups)
	if err != nil {
		return err
	}
	res, err := d.db.Exec(`
		UPDATE radio_sets
		SET name = $1, selection_mode = $2, talkgroups = $3, talkgroup_groups = $4, updated_at = $5
		WHERE id = $6 AND user_id = $7`,
		name, selectionMode, string(tgsJSON), string(groupsJSON), time.Now().Unix(), id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteRadioSet removes a set by ID scoped to the given user.
func (d *DB) DeleteRadioSet(id, userID string) error {
	res, err := d.db.Exec(`DELETE FROM radio_sets WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// scanRadioSet scans a row into a RadioSet, decoding JSONB membership columns.
type scannable interface {
	Scan(dest ...any) error
}

func scanRadioSet(row scannable) (RadioSet, error) {
	var rs RadioSet
	var tgsRaw string
	var groupsRaw string
	var shareToken sql.NullString
	var pttTalkgroup sql.NullInt64
	if err := row.Scan(
		&rs.ID, &rs.UserID, &rs.Name, &rs.SelectionMode, &tgsRaw, &groupsRaw,
		&shareToken, &pttTalkgroup, &rs.CreatedAt, &rs.UpdatedAt,
	); err != nil {
		return RadioSet{}, err
	}
	rs.SelectionMode = normalizeRadioSetSelectionMode(rs.SelectionMode)
	if shareToken.Valid {
		rs.ShareToken = &shareToken.String
	}
	if pttTalkgroup.Valid {
		v := int(pttTalkgroup.Int64)
		rs.PTTTalkgroup = &v
	}
	rs.Talkgroups = make([]int, 0)
	if err := json.Unmarshal([]byte(tgsRaw), &rs.Talkgroups); err != nil {
		return RadioSet{}, fmt.Errorf("decode talkgroups: %w", err)
	}
	rs.TalkgroupGroups = make([]string, 0)
	if err := json.Unmarshal([]byte(groupsRaw), &rs.TalkgroupGroups); err != nil {
		return RadioSet{}, fmt.Errorf("decode talkgroup groups: %w", err)
	}
	return rs, nil
}

// SetRadioSetShareToken generates and stores a share token for a radio set.
func (d *DB) SetRadioSetShareToken(id, userID, token string) error {
	_, err := d.db.Exec(
		`UPDATE radio_sets SET share_token = $1 WHERE id = $2 AND user_id = $3`,
		token, id, userID)
	return err
}

// ClearRadioSetShareToken removes the share token from a radio set.
func (d *DB) ClearRadioSetShareToken(id, userID string) error {
	_, err := d.db.Exec(
		`UPDATE radio_sets SET share_token = NULL WHERE id = $1 AND user_id = $2`,
		id, userID)
	return err
}

// GetRadioSetByShareToken looks up a radio set by its public share token.
// Returns nil (no error) when the token is not found.
func (d *DB) GetRadioSetByShareToken(token string) (*RadioSet, error) {
	row := d.db.QueryRow(
		`SELECT id, user_id, name, selection_mode, talkgroups, talkgroup_groups, share_token, ptt_talkgroup, created_at, updated_at
		 FROM radio_sets WHERE share_token = $1`, token)
	rs, err := scanRadioSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rs, nil
}
