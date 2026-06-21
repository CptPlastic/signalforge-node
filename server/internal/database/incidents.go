package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func scanHubIdentityIncidentFields(row scanner) (*HubIdentity, error) {
	var identity HubIdentity
	var watchAreasJSON []byte
	if err := row.Scan(
		&identity.HubID,
		&identity.Name,
		&identity.PublicURL,
		&identity.Region,
		&identity.Contact,
		&identity.PublicKey,
		&identity.PrivateKey,
		&identity.FederationEnabled,
		&identity.DirectoryValidationStatus,
		&identity.TrustLevel,
		&identity.TrustIssuerHubID,
		&identity.TrustCertificate,
		&identity.TrustExpiresAt,
		&identity.TrustVerifiedAt,
		&identity.IncidentManagementEnabled,
		&identity.IncidentHandlerHubID,
		&identity.IncidentAutoSuggest,
		&identity.IncidentAutoOpen,
		&watchAreasJSON,
		&identity.IncidentWatchPointLat,
		&identity.IncidentWatchPointLon,
		&identity.CreatedAt,
		&identity.UpdatedAt,
	); err != nil {
		return nil, err
	}
	identity.IncidentWatchAreas = decodeStringJSONArray(watchAreasJSON)
	return &identity, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func decodeStringJSONArray(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return values
}

func encodeStringJSONArray(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]", err
	}
	return string(raw), nil
}

// UpdateHubIncidentSettings persists incident-management toggles on hub identity.
func (d *DB) UpdateHubIncidentSettings(settings HubIdentity) (*HubIdentity, error) {
	watchAreasJSON, err := encodeStringJSONArray(settings.IncidentWatchAreas)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	row := d.db.QueryRow(`
		UPDATE hub_identity SET
			incident_management_enabled = $1,
			incident_handler_hub_id = $2,
			incident_auto_suggest = $3,
			incident_auto_open = $4,
			incident_watch_areas = $5::jsonb,
			incident_watch_point_lat = $6,
			incident_watch_point_lon = $7,
			updated_at = $8
		WHERE id = 'local'
		RETURNING hub_id, name, public_url, region, contact, public_key, private_key,
		          federation_enabled, directory_validation_status, trust_level,
		          trust_issuer_hub_id, trust_certificate, trust_expires_at, trust_verified_at,
		          incident_management_enabled, incident_handler_hub_id, incident_auto_suggest,
		          incident_auto_open, incident_watch_areas, incident_watch_point_lat,
		          incident_watch_point_lon, created_at, updated_at`,
		settings.IncidentManagementEnabled,
		strings.TrimSpace(settings.IncidentHandlerHubID),
		settings.IncidentAutoSuggest,
		settings.IncidentAutoOpen,
		watchAreasJSON,
		settings.IncidentWatchPointLat,
		settings.IncidentWatchPointLon,
		now,
	)
	return scanHubIdentityIncidentFields(row)
}

func normalizeIncidentStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft", "active", "monitoring", "closed", "archived":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "draft"
	}
}

func normalizeIncidentExposure(exposure string) string {
	switch strings.ToLower(strings.TrimSpace(exposure)) {
	case "internal", "members", "community":
		return strings.ToLower(strings.TrimSpace(exposure))
	default:
		return "members"
	}
}

func normalizeIncidentPriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "low", "normal", "high", "urgent":
		return strings.ToLower(strings.TrimSpace(priority))
	default:
		return "normal"
	}
}

func scanIncident(row scanner) (Incident, error) {
	var incident Incident
	var metadata []byte
	if err := row.Scan(
		&incident.ID,
		&incident.Title,
		&incident.IncidentType,
		&incident.Status,
		&incident.Priority,
		&incident.Exposure,
		&incident.RadioSetID,
		&incident.TemplateID,
		&incident.OpenedByUserID,
		&incident.HandlerIncidentID,
		&incident.Notes,
		&metadata,
		&incident.OpenedAt,
		&incident.ClosedAt,
		&incident.ArchivedAt,
		&incident.CreatedAt,
		&incident.UpdatedAt,
	); err != nil {
		return Incident{}, err
	}
	if len(metadata) > 0 {
		incident.Metadata = metadata
	}
	return incident, nil
}

const incidentSelect = `
	SELECT id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
	       opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
	       archived_at, created_at, updated_at
	FROM incidents`

// CreateIncident inserts a new incident row.
func (d *DB) CreateIncident(incident Incident) (Incident, error) {
	now := time.Now().Unix()
	id := incident.ID
	if id == "" {
		id = randomToken("inc_")
	}
	metadata := incident.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	row := d.db.QueryRow(`
		INSERT INTO incidents (
			id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
			opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
			archived_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13,$14,$15,$16,$16)
		RETURNING id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
		          opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
		          archived_at, created_at, updated_at`,
		id,
		strings.TrimSpace(incident.Title),
		strings.TrimSpace(incident.IncidentType),
		normalizeIncidentStatus(incident.Status),
		normalizeIncidentPriority(incident.Priority),
		normalizeIncidentExposure(incident.Exposure),
		incident.RadioSetID,
		incident.TemplateID,
		incident.OpenedByUserID,
		strings.TrimSpace(incident.HandlerIncidentID),
		incident.Notes,
		metadata,
		incident.OpenedAt,
		incident.ClosedAt,
		incident.ArchivedAt,
		now,
	)
	return scanIncident(row)
}

// GetIncident returns one incident by id.
func (d *DB) GetIncident(id string) (Incident, bool, error) {
	row := d.db.QueryRow(incidentSelect+` WHERE id = $1`, id)
	incident, err := scanIncident(row)
	if err == sql.ErrNoRows {
		return Incident{}, false, nil
	}
	if err != nil {
		return Incident{}, false, err
	}
	return incident, true, nil
}

// ListIncidents returns incidents optionally including archived rows.
func (d *DB) ListIncidents(includeArchived bool) ([]Incident, error) {
	query := incidentSelect
	if !includeArchived {
		query += ` WHERE status NOT IN ('archived')`
	}
	query += ` ORDER BY CASE status
		WHEN 'active' THEN 1 WHEN 'monitoring' THEN 2 WHEN 'draft' THEN 3
		WHEN 'closed' THEN 4 ELSE 5 END, updated_at DESC`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	incidents := make([]Incident, 0)
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

// UpdateIncidentRadioSet links a radio set to an incident.
func (d *DB) UpdateIncidentRadioSet(id, radioSetID string) (Incident, error) {
	now := time.Now().Unix()
	row := d.db.QueryRow(`
		UPDATE incidents SET radio_set_id = $2, updated_at = $3 WHERE id = $1
		RETURNING id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
		          opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
		          archived_at, created_at, updated_at`,
		id, radioSetID, now,
	)
	return scanIncident(row)
}

// ActivateIncident marks a draft as active and sets opened_at.
func (d *DB) ActivateIncident(id string) (Incident, error) {
	now := time.Now().Unix()
	row := d.db.QueryRow(`
		UPDATE incidents SET status = 'active', opened_at = CASE WHEN opened_at = 0 THEN $2 ELSE opened_at END, updated_at = $2
		WHERE id = $1
		RETURNING id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
		          opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
		          archived_at, created_at, updated_at`,
		id, now,
	)
	return scanIncident(row)
}

// CloseIncident closes an open incident.
func (d *DB) CloseIncident(id string) (Incident, error) {
	now := time.Now().Unix()
	row := d.db.QueryRow(`
		UPDATE incidents SET status = 'closed', closed_at = $2, updated_at = $2 WHERE id = $1
		RETURNING id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
		          opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
		          archived_at, created_at, updated_at`,
		id, now,
	)
	return scanIncident(row)
}

// ArchiveIncident moves a closed incident to archived storage.
func (d *DB) ArchiveIncident(id string) (Incident, error) {
	now := time.Now().Unix()
	row := d.db.QueryRow(`
		UPDATE incidents SET status = 'archived', archived_at = $2, updated_at = $2 WHERE id = $1
		RETURNING id, title, incident_type, status, priority, exposure, radio_set_id, template_id,
		          opened_by_user_id, handler_incident_id, notes, metadata, opened_at, closed_at,
		          archived_at, created_at, updated_at`,
		id, now,
	)
	return scanIncident(row)
}

func scanIncidentSignal(row scanner) (IncidentSignal, error) {
	var signal IncidentSignal
	var raw []byte
	if err := row.Scan(
		&signal.ID,
		&signal.Source,
		&signal.ExternalID,
		&signal.EventType,
		&signal.Severity,
		&signal.Title,
		&signal.Detail,
		&signal.TemplateID,
		&signal.IncidentID,
		&raw,
		&signal.ReceivedAt,
	); err != nil {
		return IncidentSignal{}, err
	}
	if len(raw) > 0 {
		signal.Raw = raw
	}
	return signal, nil
}

// UpsertIncidentSignal stores an external signal idempotently.
func (d *DB) UpsertIncidentSignal(signal IncidentSignal) (IncidentSignal, bool, error) {
	id := signal.ID
	if id == "" {
		id = randomToken("sig_")
	}
	raw := signal.Raw
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	now := time.Now().Unix()
	if signal.ReceivedAt == 0 {
		signal.ReceivedAt = now
	}

	var existingID string
	err := d.db.QueryRow(`SELECT id FROM incident_signals WHERE source = $1 AND external_id = $2`,
		signal.Source, signal.ExternalID).Scan(&existingID)
	if err == nil {
		row := d.db.QueryRow(`
			SELECT id, source, external_id, event_type, severity, title, detail, template_id,
			       incident_id, raw, received_at FROM incident_signals WHERE id = $1`, existingID)
		existing, scanErr := scanIncidentSignal(row)
		return existing, false, scanErr
	}
	if err != sql.ErrNoRows {
		return IncidentSignal{}, false, err
	}

	row := d.db.QueryRow(`
		INSERT INTO incident_signals (
			id, source, external_id, event_type, severity, title, detail, template_id,
			incident_id, raw, received_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, source, external_id, event_type, severity, title, detail, template_id,
		          incident_id, raw, received_at`,
		id,
		signal.Source,
		signal.ExternalID,
		signal.EventType,
		signal.Severity,
		signal.Title,
		signal.Detail,
		signal.TemplateID,
		signal.IncidentID,
		raw,
		signal.ReceivedAt,
	)
	inserted, err := scanIncidentSignal(row)
	if err != nil {
		return IncidentSignal{}, false, err
	}
	return inserted, true, nil
}

// LinkIncidentSignal associates a signal with an incident.
func (d *DB) LinkIncidentSignal(signalID, incidentID string) error {
	_, err := d.db.Exec(`UPDATE incident_signals SET incident_id = $2 WHERE id = $1`, signalID, incidentID)
	return err
}

// ListPendingIncidentSignals returns unlinked recent signals.
func (d *DB) ListPendingIncidentSignals(limit int) ([]IncidentSignal, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`
		SELECT id, source, external_id, event_type, severity, title, detail, template_id,
		       incident_id, raw, received_at
		FROM incident_signals
		WHERE incident_id = '' OR incident_id IS NULL
		ORDER BY received_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signals := make([]IncidentSignal, 0)
	for rows.Next() {
		signal, err := scanIncidentSignal(rows)
		if err != nil {
			return nil, err
		}
		signals = append(signals, signal)
	}
	return signals, rows.Err()
}

// SeedDefaultIncidentTemplates inserts built-in templates if missing.
func (d *DB) SeedDefaultIncidentTemplates() error {
	now := time.Now().Unix()
	templates := []IncidentTemplate{
		{
			ID:               "weather-severe",
			Name:             "Severe Weather",
			IncidentType:     "weather",
			SelectionMode:    "groups",
			TalkgroupGroups:  []string{"Fire", "EMS", "Public Works", "Skywarn"},
			DefaultExposure:  "community",
			DefaultPriority:  "high",
			NWSEventPatterns: []string{"Tornado Warning", "Severe Thunderstorm Warning", "Tornado Watch", "Severe Thunderstorm Watch", "Flash Flood Warning"},
		},
		{
			ID:              "fire-structural",
			Name:            "Structure Fire",
			IncidentType:    "fire",
			SelectionMode:   "groups",
			TalkgroupGroups: []string{"Fire", "EMS", "Command"},
			DefaultExposure: "members",
			DefaultPriority: "high",
		},
		{
			ID:              "ems-general",
			Name:            "EMS / Medical",
			IncidentType:    "ems",
			SelectionMode:   "groups",
			TalkgroupGroups: []string{"EMS", "Fire", "Hospital"},
			DefaultExposure: "members",
			DefaultPriority: "normal",
		},
	}
	for _, tmpl := range templates {
		groupsJSON, _ := json.Marshal(tmpl.TalkgroupGroups)
		tgsJSON, _ := json.Marshal(tmpl.Talkgroups)
		patternsJSON, _ := json.Marshal(tmpl.NWSEventPatterns)
		_, err := d.db.Exec(`
			INSERT INTO incident_templates (
				id, name, incident_type, selection_mode, talkgroups, talkgroup_groups,
				default_exposure, default_priority, nws_event_patterns, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
			ON CONFLICT (id) DO NOTHING`,
			tmpl.ID, tmpl.Name, tmpl.IncidentType, tmpl.SelectionMode,
			string(tgsJSON), string(groupsJSON),
			tmpl.DefaultExposure, tmpl.DefaultPriority, string(patternsJSON), now,
		)
		if err != nil {
			return fmt.Errorf("seed template %s: %w", tmpl.ID, err)
		}
	}
	return nil
}

func scanIncidentTemplate(row scanner) (IncidentTemplate, error) {
	var tmpl IncidentTemplate
	var tgsJSON, groupsJSON, patternsJSON []byte
	if err := row.Scan(
		&tmpl.ID, &tmpl.Name, &tmpl.IncidentType, &tmpl.SelectionMode,
		&tgsJSON, &groupsJSON, &tmpl.DefaultExposure, &tmpl.DefaultPriority,
		&patternsJSON, &tmpl.CreatedAt, &tmpl.UpdatedAt,
	); err != nil {
		return IncidentTemplate{}, err
	}
	_ = json.Unmarshal(tgsJSON, &tmpl.Talkgroups)
	_ = json.Unmarshal(groupsJSON, &tmpl.TalkgroupGroups)
	_ = json.Unmarshal(patternsJSON, &tmpl.NWSEventPatterns)
	return tmpl, nil
}

// ListIncidentTemplates returns all templates.
func (d *DB) ListIncidentTemplates() ([]IncidentTemplate, error) {
	rows, err := d.db.Query(`
		SELECT id, name, incident_type, selection_mode, talkgroups, talkgroup_groups,
		       default_exposure, default_priority, nws_event_patterns, created_at, updated_at
		FROM incident_templates ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]IncidentTemplate, 0)
	for rows.Next() {
		tmpl, err := scanIncidentTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}
	return templates, rows.Err()
}

// GetIncidentTemplate returns one template by id.
func (d *DB) GetIncidentTemplate(id string) (IncidentTemplate, bool, error) {
	row := d.db.QueryRow(`
		SELECT id, name, incident_type, selection_mode, talkgroups, talkgroup_groups,
		       default_exposure, default_priority, nws_event_patterns, created_at, updated_at
		FROM incident_templates WHERE id = $1`, id)
	tmpl, err := scanIncidentTemplate(row)
	if err == sql.ErrNoRows {
		return IncidentTemplate{}, false, nil
	}
	if err != nil {
		return IncidentTemplate{}, false, err
	}
	return tmpl, true, nil
}

// MatchIncidentTemplateByNWSEvent finds a template whose pattern matches the NWS event name.
func (d *DB) MatchIncidentTemplateByNWSEvent(event string) (IncidentTemplate, bool, error) {
	templates, err := d.ListIncidentTemplates()
	if err != nil {
		return IncidentTemplate{}, false, err
	}
	event = strings.TrimSpace(event)
	for _, tmpl := range templates {
		for _, pattern := range tmpl.NWSEventPatterns {
			if strings.EqualFold(strings.TrimSpace(pattern), event) {
				return tmpl, true, nil
			}
		}
	}
	return IncidentTemplate{}, false, nil
}
