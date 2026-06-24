package database

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type IncidentIntegrationConfig struct {
	IncidentTitle   string `json:"incidentTitle"`
	Exposure        string `json:"exposure"`
	PublicPlayerURL string `json:"publicPlayerUrl,omitempty"`
	StreamToken     string `json:"streamToken,omitempty"`
	VoiceChannelID  string `json:"voiceChannelId,omitempty"`
	TextChannelID   string `json:"textChannelId,omitempty"`
	CategoryID      string `json:"categoryId,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (d *DB) GetIncidentIntegration(incidentID, kind string) (IncidentIntegration, bool, error) {
	row := d.db.QueryRow(`
		SELECT id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at
		FROM incident_integrations WHERE incident_id = $1 AND kind = $2`,
		incidentID, kind,
	)
	return scanIncidentIntegration(row)
}

func (d *DB) GetIncidentIntegrationByID(id string) (IncidentIntegration, bool, error) {
	row := d.db.QueryRow(`
		SELECT id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at
		FROM incident_integrations WHERE id = $1`, id,
	)
	return scanIncidentIntegration(row)
}

func (d *DB) UpsertIncidentIntegration(integration IncidentIntegration) (IncidentIntegration, error) {
	now := time.Now().Unix()
	id := integration.ID
	if id == "" {
		id = randomToken("int_")
	}
	config := integration.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	botInstanceID := strings.TrimSpace(integration.BotInstanceID)
	row := d.db.QueryRow(`
		INSERT INTO incident_integrations (id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (incident_id, kind) DO UPDATE SET
			status = EXCLUDED.status,
			bot_instance_id = EXCLUDED.bot_instance_id,
			config = EXCLUDED.config,
			updated_at = EXCLUDED.updated_at
		RETURNING id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at`,
		id,
		integration.IncidentID,
		strings.TrimSpace(integration.Kind),
		strings.TrimSpace(integration.Status),
		botInstanceID,
		config,
		now,
	)
	return scanIncidentIntegrationRow(row)
}

func (d *DB) ListPendingDiscordIntegrationTasks(botInstanceID string, limit int) ([]IncidentIntegration, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.db.Query(`
		SELECT id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at
		FROM incident_integrations
		WHERE kind = 'discord' AND status IN ('pending', 'stopping')
		  AND ($1 = '' OR bot_instance_id = $1)
		ORDER BY updated_at ASC
		LIMIT $2`, botInstanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]IncidentIntegration, 0)
	for rows.Next() {
		item, _, err := scanIncidentIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) ListActiveDiscordIntegrations(botInstanceID string, limit int) ([]IncidentIntegration, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`
		SELECT id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at
		FROM incident_integrations
		WHERE kind = 'discord' AND status = 'active'
		  AND ($1 = '' OR bot_instance_id = $1)
		ORDER BY updated_at DESC
		LIMIT $2`, botInstanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]IncidentIntegration, 0)
	for rows.Next() {
		item, _, err := scanIncidentIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) UpdateIncidentIntegrationStatus(id, status string, config json.RawMessage) (IncidentIntegration, error) {
	now := time.Now().Unix()
	if len(config) == 0 {
		row := d.db.QueryRow(`
			UPDATE incident_integrations SET status = $2, updated_at = $3 WHERE id = $1
			RETURNING id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at`,
			id, status, now,
		)
		return scanIncidentIntegrationRow(row)
	}
	row := d.db.QueryRow(`
		UPDATE incident_integrations SET status = $2, config = $3, updated_at = $4 WHERE id = $1
		RETURNING id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at`,
		id, status, config, now,
	)
	return scanIncidentIntegrationRow(row)
}

func (d *DB) ListFailedDiscordIntegrations(limit int) ([]IncidentIntegration, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`
		SELECT id, incident_id, kind, status, bot_instance_id, config, created_at, updated_at
		FROM incident_integrations
		WHERE kind = 'discord' AND status = 'failed'
		ORDER BY updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]IncidentIntegration, 0)
	for rows.Next() {
		item, _, err := scanIncidentIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) CountDiscordIntegrationsByStatus(status string) (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM incident_integrations
		WHERE kind = 'discord' AND status = $1`, status).Scan(&count)
	return count, err
}

func (d *DB) ListActiveIncidentsMissingDiscord(limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`
		SELECT i.id, i.title, i.incident_type, i.status, i.priority, i.exposure,
			i.radio_set_id, i.template_id, i.opened_by_user_id, i.handler_incident_id,
			i.notes, i.metadata, i.opened_at, i.closed_at, i.archived_at, i.created_at, i.updated_at
		FROM incidents i
		LEFT JOIN incident_integrations ii
			ON ii.incident_id = i.id AND ii.kind = 'discord' AND ii.status IN ('pending', 'active', 'stopping')
		WHERE i.status IN ('active', 'monitoring')
			AND i.exposure <> 'internal'
			AND ii.id IS NULL
		ORDER BY i.updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Incident, 0)
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, incident)
	}
	return out, rows.Err()
}

func (d *DB) MarkDiscordIntegrationsStopping(incidentID string) error {
	now := time.Now().Unix()
	_, err := d.db.Exec(`
		UPDATE incident_integrations SET status = 'stopping', updated_at = $2
		WHERE incident_id = $1 AND kind = 'discord' AND status IN ('pending', 'active')`,
		incidentID, now,
	)
	return err
}

func scanIncidentIntegration(row scanner) (IncidentIntegration, bool, error) {
	item, err := scanIncidentIntegrationRow(row)
	if err == sql.ErrNoRows {
		return IncidentIntegration{}, false, nil
	}
	if err != nil {
		return IncidentIntegration{}, false, err
	}
	return item, true, nil
}

func scanIncidentIntegrationRow(row scanner) (IncidentIntegration, error) {
	var item IncidentIntegration
	var config []byte
	if err := row.Scan(&item.ID, &item.IncidentID, &item.Kind, &item.Status, &item.BotInstanceID, &config, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return IncidentIntegration{}, err
	}
	if len(config) > 0 {
		item.Config = json.RawMessage(config)
	}
	return item, nil
}
