package database

import (
	"database/sql"
	"strings"
	"time"
)

const federatedImportDeleteBatchSize = 1000

// FederatedPeerImportDeleteStats reports cleanup work completed for a deleted peer.
type FederatedPeerImportDeleteStats struct {
	CallsDeleted   int64
	SourcesDeleted int64
	ImportsDeleted int64
}

// GetHubIdentity returns the local hub identity, if it has been initialized.
func (d *DB) GetHubIdentity() (*HubIdentity, bool, error) {
	row := d.db.QueryRow(`
		SELECT hub_id, name, public_url, region, contact, public_key, private_key,
		       federation_enabled, directory_validation_status, trust_level,
		       trust_issuer_hub_id, trust_certificate, trust_expires_at, trust_verified_at,
		       created_at, updated_at
		FROM hub_identity
		WHERE id = 'local'`)

	var identity HubIdentity
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
		&identity.CreatedAt,
		&identity.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &identity, true, nil
}

// UpsertHubIdentity stores the single local hub identity row.
func (d *DB) UpsertHubIdentity(identity HubIdentity) (*HubIdentity, error) {
	now := time.Now().Unix()
	if identity.HubID == "" {
		identity.HubID = NewHubID()
	}
	if identity.DirectoryValidationStatus == "" {
		identity.DirectoryValidationStatus = "unverified"
	}
	if identity.TrustLevel == "" {
		identity.TrustLevel = "community"
	}
	if identity.TrustVerifiedAt == 0 && (identity.TrustLevel == "verified" || identity.TrustLevel == "official") {
		identity.TrustVerifiedAt = now
	}

	row := d.db.QueryRow(`
		INSERT INTO hub_identity
			(id, hub_id, name, public_url, region, contact, public_key, private_key,
			 federation_enabled, directory_validation_status, trust_level, trust_issuer_hub_id,
			 trust_certificate, trust_expires_at, trust_verified_at, created_at, updated_at)
		VALUES ('local', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name,
			public_url = excluded.public_url,
			region = excluded.region,
			contact = excluded.contact,
			public_key = excluded.public_key,
			private_key = excluded.private_key,
			federation_enabled = excluded.federation_enabled,
			directory_validation_status = excluded.directory_validation_status,
			trust_level = excluded.trust_level,
			trust_issuer_hub_id = excluded.trust_issuer_hub_id,
			trust_certificate = excluded.trust_certificate,
			trust_expires_at = excluded.trust_expires_at,
			trust_verified_at = excluded.trust_verified_at,
			updated_at = excluded.updated_at
		RETURNING hub_id, name, public_url, region, contact, public_key, private_key,
		          federation_enabled, directory_validation_status, trust_level,
		          trust_issuer_hub_id, trust_certificate, trust_expires_at, trust_verified_at,
		          created_at, updated_at`,
		identity.HubID,
		identity.Name,
		identity.PublicURL,
		identity.Region,
		identity.Contact,
		identity.PublicKey,
		identity.PrivateKey,
		identity.FederationEnabled,
		identity.DirectoryValidationStatus,
		identity.TrustLevel,
		identity.TrustIssuerHubID,
		identity.TrustCertificate,
		identity.TrustExpiresAt,
		identity.TrustVerifiedAt,
		now,
	)

	var saved HubIdentity
	if err := row.Scan(
		&saved.HubID,
		&saved.Name,
		&saved.PublicURL,
		&saved.Region,
		&saved.Contact,
		&saved.PublicKey,
		&saved.PrivateKey,
		&saved.FederationEnabled,
		&saved.DirectoryValidationStatus,
		&saved.TrustLevel,
		&saved.TrustIssuerHubID,
		&saved.TrustCertificate,
		&saved.TrustExpiresAt,
		&saved.TrustVerifiedAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &saved, nil
}

// SetHubIdentityKeyPair stores the local hub signing keypair and returns the public identity.
func (d *DB) SetHubIdentityKeyPair(publicKey, privateKey string) (*HubIdentity, error) {
	now := time.Now().Unix()
	row := d.db.QueryRow(`
		UPDATE hub_identity
		SET public_key = $1, private_key = $2, updated_at = $3
		WHERE id = 'local'
		RETURNING hub_id, name, public_url, region, contact, public_key, private_key,
		          federation_enabled, directory_validation_status, trust_level,
		          trust_issuer_hub_id, trust_certificate, trust_expires_at, trust_verified_at,
		          created_at, updated_at`, publicKey, privateKey, now)

	var saved HubIdentity
	if err := row.Scan(
		&saved.HubID,
		&saved.Name,
		&saved.PublicURL,
		&saved.Region,
		&saved.Contact,
		&saved.PublicKey,
		&saved.PrivateKey,
		&saved.FederationEnabled,
		&saved.DirectoryValidationStatus,
		&saved.TrustLevel,
		&saved.TrustIssuerHubID,
		&saved.TrustCertificate,
		&saved.TrustExpiresAt,
		&saved.TrustVerifiedAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &saved, nil
}

// CreateHubInvite stores a new peer invite token.
func (d *DB) CreateHubInvite(createdByUserID string, expiresAt int64) (*HubInvite, error) {
	invite := HubInvite{
		ID:              NewHubInviteID(),
		Token:           NewHubInviteToken(),
		CreatedByUserID: createdByUserID,
		ExpiresAt:       expiresAt,
		CreatedAt:       time.Now().Unix(),
	}

	row := d.db.QueryRow(`
		INSERT INTO hub_invites (id, token, created_by_user_id, expires_at, used_at, revoked_at, created_at)
		VALUES ($1, $2, $3, $4, 0, 0, $5)
		RETURNING id, token, COALESCE(created_by_user_id, ''), expires_at, used_at, revoked_at, created_at`,
		invite.ID,
		invite.Token,
		nullableString(invite.CreatedByUserID),
		invite.ExpiresAt,
		invite.CreatedAt,
	)

	var saved HubInvite
	if err := row.Scan(
		&saved.ID,
		&saved.Token,
		&saved.CreatedByUserID,
		&saved.ExpiresAt,
		&saved.UsedAt,
		&saved.RevokedAt,
		&saved.CreatedAt,
	); err != nil {
		return nil, err
	}

	return &saved, nil
}

// ListHubInvites returns recent peer invite tokens for admin management.
func (d *DB) ListHubInvites() ([]HubInvite, error) {
	rows, err := d.db.Query(`
		SELECT id, token, COALESCE(created_by_user_id, ''), expires_at, used_at, revoked_at, created_at
		FROM hub_invites
		ORDER BY created_at DESC
		LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invites := make([]HubInvite, 0)
	for rows.Next() {
		var invite HubInvite
		if err := rows.Scan(
			&invite.ID,
			&invite.Token,
			&invite.CreatedByUserID,
			&invite.ExpiresAt,
			&invite.UsedAt,
			&invite.RevokedAt,
			&invite.CreatedAt,
		); err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	return invites, rows.Err()
}

// RevokeHubInvite marks an invite as no longer usable.
func (d *DB) RevokeHubInvite(id string) (*HubInvite, bool, error) {
	row := d.db.QueryRow(`
		UPDATE hub_invites
		SET revoked_at = $1
		WHERE id = $2 AND revoked_at = 0
		RETURNING id, token, COALESCE(created_by_user_id, ''), expires_at, used_at, revoked_at, created_at`,
		time.Now().Unix(),
		id,
	)

	var invite HubInvite
	if err := row.Scan(
		&invite.ID,
		&invite.Token,
		&invite.CreatedByUserID,
		&invite.ExpiresAt,
		&invite.UsedAt,
		&invite.RevokedAt,
		&invite.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &invite, true, nil
}

// RedeemHubInvite marks a valid invite token as used.
func (d *DB) RedeemHubInvite(token string) (*HubInvite, bool, string, error) {
	row := d.db.QueryRow(`
		SELECT id, token, COALESCE(created_by_user_id, ''), expires_at, used_at, revoked_at, created_at
		FROM hub_invites
		WHERE token = $1`, token)

	var invite HubInvite
	if err := row.Scan(
		&invite.ID,
		&invite.Token,
		&invite.CreatedByUserID,
		&invite.ExpiresAt,
		&invite.UsedAt,
		&invite.RevokedAt,
		&invite.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, "invalid invite", nil
		}
		return nil, false, "", err
	}

	now := time.Now().Unix()
	if invite.RevokedAt > 0 {
		return &invite, false, "invite revoked", nil
	}
	if invite.UsedAt > 0 {
		return &invite, false, "invite already used", nil
	}
	if invite.ExpiresAt <= now {
		return &invite, false, "invite expired", nil
	}

	row = d.db.QueryRow(`
		UPDATE hub_invites
		SET used_at = $1
		WHERE token = $2 AND used_at = 0 AND revoked_at = 0 AND expires_at > $1
		RETURNING id, token, COALESCE(created_by_user_id, ''), expires_at, used_at, revoked_at, created_at`, now, token)
	if err := row.Scan(
		&invite.ID,
		&invite.Token,
		&invite.CreatedByUserID,
		&invite.ExpiresAt,
		&invite.UsedAt,
		&invite.RevokedAt,
		&invite.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, "invite already used", nil
		}
		return nil, false, "", err
	}

	return &invite, true, "", nil
}

// UpsertHubPeer creates or updates a known hub peer by remote hub ID.
func (d *DB) UpsertHubPeer(peer HubPeer) (*HubPeer, error) {
	now := time.Now().Unix()
	if peer.ID == "" {
		peer.ID = NewHubPeerID()
	}
	if peer.Status == "" {
		peer.Status = "connected"
	}
	if peer.Direction == "" {
		peer.Direction = "outbound"
	}
	if peer.AcceptedAt == 0 {
		peer.AcceptedAt = now
	}
	peer.LastSeenAt = now

	row := d.db.QueryRow(`
		INSERT INTO hub_peers
			(id, hub_id, name, public_url, region, contact, status, direction, accepted_at, last_seen_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $10)
		ON CONFLICT (hub_id) DO UPDATE SET
			name = excluded.name,
			public_url = excluded.public_url,
			region = excluded.region,
			contact = excluded.contact,
			status = excluded.status,
			direction = excluded.direction,
			accepted_at = CASE WHEN hub_peers.accepted_at = 0 THEN excluded.accepted_at ELSE hub_peers.accepted_at END,
			last_seen_at = excluded.last_seen_at,
			updated_at = excluded.updated_at
		RETURNING id, hub_id, name, public_url, region, contact, status, direction, accepted_at, last_seen_at, created_at, updated_at`,
		peer.ID,
		peer.HubID,
		peer.Name,
		peer.PublicURL,
		peer.Region,
		peer.Contact,
		peer.Status,
		peer.Direction,
		peer.AcceptedAt,
		peer.LastSeenAt,
	)

	var saved HubPeer
	if err := row.Scan(
		&saved.ID,
		&saved.HubID,
		&saved.Name,
		&saved.PublicURL,
		&saved.Region,
		&saved.Contact,
		&saved.Status,
		&saved.Direction,
		&saved.AcceptedAt,
		&saved.LastSeenAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &saved, nil
}

// ListHubPeers returns known peer hubs.
func (d *DB) ListHubPeers() ([]HubPeer, error) {
	rows, err := d.db.Query(`
		SELECT id, hub_id, name, public_url, region, contact, status, direction, accepted_at, last_seen_at, created_at, updated_at
		FROM hub_peers
		ORDER BY updated_at DESC
		LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	peers := make([]HubPeer, 0)
	for rows.Next() {
		var peer HubPeer
		if err := rows.Scan(
			&peer.ID,
			&peer.HubID,
			&peer.Name,
			&peer.PublicURL,
			&peer.Region,
			&peer.Contact,
			&peer.Status,
			&peer.Direction,
			&peer.AcceptedAt,
			&peer.LastSeenAt,
			&peer.CreatedAt,
			&peer.UpdatedAt,
		); err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}
	return peers, rows.Err()
}

// DisableHubPeer marks a peer as disabled without deleting history.
func (d *DB) DisableHubPeer(id string) (*HubPeer, bool, error) {
	row := d.db.QueryRow(`
		UPDATE hub_peers
		SET status = 'disabled', updated_at = $1
		WHERE id = $2
		RETURNING id, hub_id, name, public_url, region, contact, status, direction, accepted_at, last_seen_at, created_at, updated_at`,
		time.Now().Unix(),
		id,
	)

	var peer HubPeer
	if err := row.Scan(
		&peer.ID,
		&peer.HubID,
		&peer.Name,
		&peer.PublicURL,
		&peer.Region,
		&peer.Contact,
		&peer.Status,
		&peer.Direction,
		&peer.AcceptedAt,
		&peer.LastSeenAt,
		&peer.CreatedAt,
		&peer.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &peer, true, nil
}

// DeleteHubPeer removes the peer relationship quickly. Imported call cleanup can be slow,
// so callers should run DeleteFederatedPeerImports outside the request path.
func (d *DB) DeleteHubPeer(id string) (*HubPeer, bool, error) {
	row := d.db.QueryRow(`
		DELETE FROM hub_peers
		WHERE id = $1
		RETURNING id, hub_id, name, public_url, region, contact, status, direction, accepted_at, last_seen_at, created_at, updated_at`,
		id,
	)

	var peer HubPeer
	if err := row.Scan(
		&peer.ID,
		&peer.HubID,
		&peer.Name,
		&peer.PublicURL,
		&peer.Region,
		&peer.Contact,
		&peer.Status,
		&peer.Direction,
		&peer.AcceptedAt,
		&peer.LastSeenAt,
		&peer.CreatedAt,
		&peer.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &peer, true, nil
}

// DeleteFederatedPeerImports removes imported calls, remote sources, and cursor rows for a deleted peer.
func (d *DB) DeleteFederatedPeerImports(peerHubID string) (FederatedPeerImportDeleteStats, error) {
	stats := FederatedPeerImportDeleteStats{}

	importsResult, err := d.db.Exec(`DELETE FROM federation_call_imports WHERE peer_hub_id = $1`, peerHubID)
	if err != nil {
		return stats, err
	}
	stats.ImportsDeleted, _ = importsResult.RowsAffected()

	remoteSourcePattern := federatedPeerSourceLikePattern(peerHubID)
	for {
		callsResult, err := d.db.Exec(`
			WITH doomed AS (
				SELECT id
				FROM calls
				WHERE source_id LIKE $1 ESCAPE '\'
				LIMIT $2
			)
			DELETE FROM calls
			WHERE id IN (SELECT id FROM doomed)`,
			remoteSourcePattern,
			federatedImportDeleteBatchSize,
		)
		if err != nil {
			return stats, err
		}

		deleted, _ := callsResult.RowsAffected()
		stats.CallsDeleted += deleted
		if deleted == 0 {
			break
		}
	}

	sourcesResult, err := d.db.Exec(`DELETE FROM ingestion_sources WHERE id LIKE $1 ESCAPE '\'`, remoteSourcePattern)
	if err != nil {
		return stats, err
	}
	stats.SourcesDeleted, _ = sourcesResult.RowsAffected()
	return stats, nil
}

func federatedPeerSourceLikePattern(peerHubID string) string {
	cleanPeer := strings.NewReplacer(":", "_", "/", "_", " ", "_").Replace(strings.TrimSpace(peerHubID))
	return "remote_" + escapeSQLLike(cleanPeer) + "_%"
}

func escapeSQLLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// EnableHubPeer marks a disabled peer as connected again without requiring a new invite.
func (d *DB) EnableHubPeer(id string) (*HubPeer, bool, error) {
	row := d.db.QueryRow(`
		UPDATE hub_peers
		SET status = 'connected', updated_at = $1
		WHERE id = $2
		RETURNING id, hub_id, name, public_url, region, contact, status, direction, accepted_at, last_seen_at, created_at, updated_at`,
		time.Now().Unix(),
		id,
	)

	var peer HubPeer
	if err := row.Scan(
		&peer.ID,
		&peer.HubID,
		&peer.Name,
		&peer.PublicURL,
		&peer.Region,
		&peer.Contact,
		&peer.Status,
		&peer.Direction,
		&peer.AcceptedAt,
		&peer.LastSeenAt,
		&peer.CreatedAt,
		&peer.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &peer, true, nil
}
