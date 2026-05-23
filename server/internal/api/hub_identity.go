package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const (
	hubInviteTTL                 = 7 * 24 * time.Hour
	hubDirectorySyncInitialDelay = 15 * time.Minute
	hubDirectorySyncInterval     = 24 * time.Hour
	errLoadHubIdentity           = "load hub identity"
	errInvalidJSON               = "invalid json"
	errFederationOff             = "federation disabled"
)

type updateHubIdentityRequest struct {
	Name              string `json:"name"`
	PublicURL         string `json:"publicUrl"`
	Region            string `json:"region"`
	Contact           string `json:"contact"`
	FederationEnabled bool   `json:"federationEnabled"`
}

type connectHubPeerRequest struct {
	RemoteURL   string `json:"remoteUrl"`
	InviteToken string `json:"inviteToken"`
}

type acceptHubInviteRequest struct {
	Token string               `json:"token"`
	Peer  database.HubIdentity `json:"peer"`
}

type acceptHubInviteResponse struct {
	Identity database.HubIdentity `json:"identity"`
	Peer     database.HubPeer     `json:"peer"`
}

type hubDirectoryFeed struct {
	Version   int                 `json:"version"`
	UpdatedAt int64               `json:"updatedAt"`
	Issuer    string              `json:"issuer"`
	Hubs      []hubDirectoryEntry `json:"hubs"`
}

type hubDirectoryEntry struct {
	HubID            string `json:"hubId"`
	Name             string `json:"name"`
	PublicURL        string `json:"publicUrl"`
	Region           string `json:"region"`
	PublicKey        string `json:"publicKey"`
	DirectoryStatus  string `json:"directoryStatus"`
	TrustLevel       string `json:"trustLevel"`
	TrustIssuerHubID string `json:"trustIssuerHubId"`
	TrustCertificate string `json:"trustCertificate"`
	TrustExpiresAt   int64  `json:"trustExpiresAt"`
	TrustVerifiedAt  int64  `json:"trustVerifiedAt"`
	LastSeenAt       int64  `json:"lastSeenAt"`
}

func (h *handler) handleGetHubIdentity(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, identity)
}

func (h *handler) handleUpdateHubIdentity(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	existing, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}

	var req updateHubIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, errInvalidJSON, http.StatusBadRequest)
		return
	}

	updated := *existing
	updated.Name = strings.TrimSpace(req.Name)
	updated.PublicURL = strings.TrimSpace(req.PublicURL)
	updated.Region = strings.TrimSpace(req.Region)
	updated.Contact = strings.TrimSpace(req.Contact)
	updated.FederationEnabled = req.FederationEnabled
	if updated.Name == "" {
		updated.Name = "SignalForge Hub"
	}

	saved, err := h.db.UpsertHubIdentity(updated)
	if err != nil {
		h.logger.Error("save hub identity failed", "error", err)
		http.Error(w, "save hub identity", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.identity_updated", "hub", saved.HubID, map[string]any{
		"name":              saved.Name,
		"publicUrl":         saved.PublicURL,
		"region":            saved.Region,
		"federationEnabled": saved.FederationEnabled,
	})
	writeJSON(w, http.StatusOK, saved)
}

func (h *handler) handleRefreshHubDirectory(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	saved, found, err := h.refreshHubDirectory(r.Context())
	if err != nil {
		h.logger.Error("refresh hub directory failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.directory_refreshed", "hub", saved.HubID, map[string]any{
		"found":           found,
		"directoryStatus": saved.DirectoryValidationStatus,
		"trustLevel":      saved.TrustLevel,
	})
	writeJSON(w, http.StatusOK, saved)
}

func (h *handler) refreshHubDirectory(ctx context.Context) (*database.HubIdentity, bool, error) {
	identity, err := h.ensureHubIdentity()
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", errLoadHubIdentity, err)
	}

	entry, found, err := h.fetchHubDirectoryEntry(ctx, identity.HubID)
	if err != nil {
		return nil, false, err
	}

	updated := *identity
	if !found {
		updated.DirectoryValidationStatus = "unlisted"
		updated.TrustLevel = "community"
		updated.TrustIssuerHubID = ""
		updated.TrustCertificate = ""
		updated.TrustExpiresAt = 0
		updated.TrustVerifiedAt = 0
	} else {
		updated.DirectoryValidationStatus = normalizeDirectoryStatus(entry.DirectoryStatus)
		updated.TrustLevel = normalizeDirectoryTrustLevel(entry.TrustLevel)
		updated.TrustIssuerHubID = strings.TrimSpace(entry.TrustIssuerHubID)
		updated.TrustCertificate = strings.TrimSpace(entry.TrustCertificate)
		updated.TrustExpiresAt = entry.TrustExpiresAt
		updated.TrustVerifiedAt = entry.TrustVerifiedAt
	}

	saved, err := h.db.UpsertHubIdentity(updated)
	if err != nil {
		return nil, found, fmt.Errorf("save hub identity: %w", err)
	}
	return saved, found, nil
}

func (h *handler) startHubDirectorySyncLoop() {
	go func() {
		timer := time.NewTimer(hubDirectorySyncInitialDelay)
		defer timer.Stop()
		for {
			<-timer.C
			identity, found, err := h.refreshHubDirectory(context.Background())
			if err != nil {
				h.logger.Error("scheduled hub directory refresh failed", "error", err)
			} else {
				h.logger.Info("scheduled hub directory refresh complete", "hub_id", identity.HubID, "found", found, "trust_level", identity.TrustLevel)
			}
			timer.Reset(hubDirectorySyncInterval)
		}
	}()
}

func (h *handler) handleGenerateHubKeyPair(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity before key generation failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}

	if strings.TrimSpace(identity.PublicKey) != "" && strings.TrimSpace(identity.PrivateKey) != "" {
		writeJSON(w, http.StatusOK, identity)
		return
	}

	publicKey, privateKey, err := generateHubKeyPair()
	if err != nil {
		h.logger.Error("generate hub keypair failed", "error", err)
		http.Error(w, "generate hub keypair", http.StatusInternalServerError)
		return
	}

	saved, err := h.db.SetHubIdentityKeyPair(publicKey, privateKey)
	if err != nil {
		h.logger.Error("save hub keypair failed", "error", err)
		http.Error(w, "save hub keypair", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.keypair_generated", "hub", saved.HubID, map[string]any{
		"publicKey": saved.PublicKey,
	})
	writeJSON(w, http.StatusOK, saved)
}

func (h *handler) handleListHubInvites(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	invites, err := h.db.ListHubInvites()
	if err != nil {
		h.logger.Error("list hub invites failed", "error", err)
		http.Error(w, "list hub invites", http.StatusInternalServerError)
		return
	}
	if invites == nil {
		invites = make([]database.HubInvite, 0)
	}

	writeJSON(w, http.StatusOK, invites)
}

func (h *handler) handleCreateHubInvite(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity before invite failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}
	if !identity.FederationEnabled {
		http.Error(w, errFederationOff, http.StatusBadRequest)
		return
	}

	expiresAt := time.Now().Add(hubInviteTTL).Unix()
	invite, err := h.db.CreateHubInvite(admin.ID, expiresAt)
	if err != nil {
		h.logger.Error("create hub invite failed", "error", err)
		http.Error(w, "create hub invite", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.invite_created", "hub_invite", invite.ID, map[string]any{"expiresAt": invite.ExpiresAt})
	writeJSON(w, http.StatusCreated, invite)
}

func (h *handler) handleRevokeHubInvite(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	inviteID := strings.TrimSpace(r.PathValue("id"))
	if inviteID == "" {
		http.Error(w, "missing invite id", http.StatusBadRequest)
		return
	}

	invite, found, err := h.db.RevokeHubInvite(inviteID)
	if err != nil {
		h.logger.Error("revoke hub invite failed", "error", err)
		http.Error(w, "revoke hub invite", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "invite not found", http.StatusNotFound)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.invite_revoked", "hub_invite", invite.ID, map[string]any{"revokedAt": invite.RevokedAt})
	writeJSON(w, http.StatusOK, invite)
}

func (h *handler) handleAcceptHubInvite(w http.ResponseWriter, r *http.Request) {
	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity before accepting peer failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}
	if !identity.FederationEnabled {
		http.Error(w, errFederationOff, http.StatusBadRequest)
		return
	}

	var req acceptHubInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, errInvalidJSON, http.StatusBadRequest)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.Peer.HubID = strings.TrimSpace(req.Peer.HubID)
	req.Peer.Name = strings.TrimSpace(req.Peer.Name)
	req.Peer.PublicURL = strings.TrimSpace(req.Peer.PublicURL)
	req.Peer.Region = strings.TrimSpace(req.Peer.Region)
	req.Peer.Contact = strings.TrimSpace(req.Peer.Contact)
	if req.Token == "" || req.Peer.HubID == "" {
		http.Error(w, "missing invite token or peer hub id", http.StatusBadRequest)
		return
	}
	if req.Peer.HubID == identity.HubID {
		http.Error(w, "cannot peer with self", http.StatusBadRequest)
		return
	}

	invite, ok, reason, err := h.db.RedeemHubInvite(req.Token)
	if err != nil {
		h.logger.Error("redeem hub invite failed", "error", err)
		http.Error(w, "redeem hub invite", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, reason, http.StatusBadRequest)
		return
	}

	peer, err := h.db.UpsertHubPeer(database.HubPeer{
		HubID:     req.Peer.HubID,
		Name:      req.Peer.Name,
		PublicURL: req.Peer.PublicURL,
		Region:    req.Peer.Region,
		Contact:   req.Peer.Contact,
		Status:    "connected",
		Direction: "bidirectional",
	})
	if err != nil {
		h.logger.Error("save inbound hub peer failed", "error", err)
		http.Error(w, "save hub peer", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog("", "hub.peer_accepted", "hub_peer", peer.ID, map[string]any{
		"hubId":    peer.HubID,
		"inviteId": invite.ID,
	})
	writeJSON(w, http.StatusOK, acceptHubInviteResponse{Identity: *identity, Peer: *peer})
}

func (h *handler) handleListHubPeers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	peers, err := h.db.ListHubPeers()
	if err != nil {
		h.logger.Error("list hub peers failed", "error", err)
		http.Error(w, "list hub peers", http.StatusInternalServerError)
		return
	}
	if peers == nil {
		peers = make([]database.HubPeer, 0)
	}

	writeJSON(w, http.StatusOK, peers)
}

func (h *handler) handleConnectHubPeer(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity before connecting peer failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}
	if !identity.FederationEnabled {
		http.Error(w, errFederationOff, http.StatusBadRequest)
		return
	}

	var req connectHubPeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, errInvalidJSON, http.StatusBadRequest)
		return
	}
	remoteURL, err := normalizeHubURL(req.RemoteURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.InviteToken = strings.TrimSpace(req.InviteToken)
	if req.InviteToken == "" {
		http.Error(w, "missing invite token", http.StatusBadRequest)
		return
	}

	remoteIdentity, err := h.acceptRemoteHubInvite(r, remoteURL, req.InviteToken, *identity)
	if err != nil {
		h.logger.Error("connect hub peer failed", "remote_url", remoteURL, "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if remoteIdentity.HubID == identity.HubID {
		http.Error(w, "cannot peer with self", http.StatusBadRequest)
		return
	}
	if remoteIdentity.PublicURL == "" {
		remoteIdentity.PublicURL = remoteURL
	}

	peer, err := h.db.UpsertHubPeer(database.HubPeer{
		HubID:     remoteIdentity.HubID,
		Name:      remoteIdentity.Name,
		PublicURL: remoteIdentity.PublicURL,
		Region:    remoteIdentity.Region,
		Contact:   remoteIdentity.Contact,
		Status:    "connected",
		Direction: "bidirectional",
	})
	if err != nil {
		h.logger.Error("save outbound hub peer failed", "error", err)
		http.Error(w, "save hub peer", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.peer_connected", "hub_peer", peer.ID, map[string]any{
		"hubId":     peer.HubID,
		"remoteUrl": remoteURL,
	})
	writeJSON(w, http.StatusCreated, peer)
}

func (h *handler) handleDeleteHubPeer(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	peerID := strings.TrimSpace(r.PathValue("id"))
	if peerID == "" {
		http.Error(w, "missing peer id", http.StatusBadRequest)
		return
	}

	peer, found, err := h.db.DeleteHubPeer(peerID)
	if err != nil {
		h.logger.Error("delete hub peer failed", "error", err)
		http.Error(w, "delete hub peer", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "peer not found", http.StatusNotFound)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.peer_deleted", "hub_peer", peer.ID, map[string]any{"hubId": peer.HubID})
	go h.cleanupDeletedHubPeer(peer.HubID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) cleanupDeletedHubPeer(peerHubID string) {
	h.logger.Info("delete federated peer imports started", "peer_hub_id", peerHubID)
	stats, err := h.db.DeleteFederatedPeerImports(peerHubID)
	if err != nil {
		h.logger.Error("delete federated peer imports failed", "peer_hub_id", peerHubID, "error", err)
		return
	}
	h.logger.Info(
		"deleted federated peer imports",
		"peer_hub_id", peerHubID,
		"calls_deleted", stats.CallsDeleted,
		"sources_deleted", stats.SourcesDeleted,
		"imports_deleted", stats.ImportsDeleted,
	)
}

func (h *handler) handleEnableHubPeer(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	peerID := strings.TrimSpace(r.PathValue("id"))
	if peerID == "" {
		http.Error(w, "missing peer id", http.StatusBadRequest)
		return
	}

	peer, found, err := h.db.EnableHubPeer(peerID)
	if err != nil {
		h.logger.Error("enable hub peer failed", "error", err)
		http.Error(w, "enable hub peer", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "peer not found", http.StatusNotFound)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "hub.peer_enabled", "hub_peer", peer.ID, map[string]any{"hubId": peer.HubID})
	writeJSON(w, http.StatusOK, peer)
}

func (h *handler) ensureHubIdentity() (*database.HubIdentity, error) {
	identity, found, err := h.db.GetHubIdentity()
	if err != nil {
		return nil, err
	}
	if found {
		return h.ensureConfiguredHubIdentity(identity)
	}

	return h.db.UpsertHubIdentity(database.HubIdentity{
		HubID:                     database.NewHubID(),
		Name:                      h.cfg.HubName,
		PublicURL:                 h.cfg.HubPublicURL,
		Region:                    h.cfg.HubRegion,
		Contact:                   h.cfg.HubContact,
		FederationEnabled:         h.cfg.HubFederation,
		DirectoryValidationStatus: "unverified",
		TrustLevel:                h.cfg.HubTrustLevel,
		TrustIssuerHubID:          h.cfg.HubTrustIssuer,
		TrustCertificate:          h.cfg.HubTrustCert,
		TrustExpiresAt:            h.cfg.HubTrustExpires,
	})
}

func (h *handler) ensureConfiguredHubIdentity(identity *database.HubIdentity) (*database.HubIdentity, error) {
	updated := h.configuredHubIdentity(identity)
	if updated.Name == identity.Name &&
		updated.PublicURL == identity.PublicURL &&
		updated.Region == identity.Region &&
		updated.Contact == identity.Contact &&
		updated.FederationEnabled == identity.FederationEnabled &&
		updated.TrustLevel == identity.TrustLevel &&
		updated.TrustIssuerHubID == identity.TrustIssuerHubID &&
		updated.TrustCertificate == identity.TrustCertificate &&
		updated.TrustExpiresAt == identity.TrustExpiresAt {
		return identity, nil
	}

	return h.db.UpsertHubIdentity(updated)
}

func (h *handler) configuredHubIdentity(identity *database.HubIdentity) database.HubIdentity {
	updated := *identity
	if h.cfg.HubName != "" && updated.Name != h.cfg.HubName {
		updated.Name = h.cfg.HubName
	}
	if h.cfg.HubPublicURL != "" && updated.PublicURL != h.cfg.HubPublicURL {
		updated.PublicURL = h.cfg.HubPublicURL
	}
	if h.cfg.HubRegion != "" && updated.Region != h.cfg.HubRegion {
		updated.Region = h.cfg.HubRegion
	}
	if h.cfg.HubContact != "" && updated.Contact != h.cfg.HubContact {
		updated.Contact = h.cfg.HubContact
	}
	if h.cfg.HubFederation && !updated.FederationEnabled {
		updated.FederationEnabled = true
	}

	configuredLevel := strings.TrimSpace(h.cfg.HubTrustLevel)
	if configuredLevel != "" && configuredLevel != "community" {
		updated.TrustLevel = configuredLevel
		updated.TrustIssuerHubID = h.cfg.HubTrustIssuer
		updated.TrustCertificate = h.cfg.HubTrustCert
		updated.TrustExpiresAt = h.cfg.HubTrustExpires
	}
	return updated
}

func normalizeHubURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("missing remote hub url")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid remote hub url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("remote hub url must use http or https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("remote hub url must not include credentials")
	}
	if err := validateRemoteHubHost(parsed.Hostname()); err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeDirectoryURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("missing hub directory url")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid hub directory url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("hub directory url must use http or https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("hub directory url must not include credentials")
	}
	if err := validateRemoteHubHost(parsed.Hostname()); err != nil {
		return "", err
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func normalizeDirectoryStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "listed", "verified", "suspended", "unlisted":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unverified"
	}
}

func normalizeDirectoryTrustLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "listed", "verified", "trusted", "official", "suspended":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "community"
	}
}

func generateHubKeyPair() (string, string, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return encodeHubKey(publicKey), encodeHubKey(privateKey), nil
}

func encodeHubKey(key []byte) string {
	return "ed25519:" + base64.RawStdEncoding.EncodeToString(key)
}

func (h *handler) fetchHubDirectoryEntry(ctx context.Context, hubID string) (hubDirectoryEntry, bool, error) {
	directoryURL, err := normalizeDirectoryURL(h.cfg.HubDirectoryURL)
	if err != nil {
		return hubDirectoryEntry{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, directoryURL, nil)
	if err != nil {
		return hubDirectoryEntry{}, false, err
	}
	req.Header.Set("Accept", "application/json")

	client := newRemoteHubHTTPClient(8 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return hubDirectoryEntry{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return hubDirectoryEntry{}, false, fmt.Errorf("hub directory returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var feed hubDirectoryFeed
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&feed); err != nil {
		return hubDirectoryEntry{}, false, err
	}
	for _, entry := range feed.Hubs {
		if strings.TrimSpace(entry.HubID) == hubID {
			if entry.TrustIssuerHubID == "" {
				entry.TrustIssuerHubID = feed.Issuer
			}
			return entry, true, nil
		}
	}
	return hubDirectoryEntry{}, false, nil
}

func validateRemoteHubHost(host string) error {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if host == "" {
		return fmt.Errorf("remote hub url must include a host")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("remote hub url host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedRemoteHubIP(ip) {
			return fmt.Errorf("remote hub url host is not allowed")
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("remote hub url host could not be resolved")
	}
	for _, ip := range ips {
		if isBlockedRemoteHubIP(ip) {
			return fmt.Errorf("remote hub url host resolves to a private or local address")
		}
	}
	return nil
}

func isBlockedRemoteHubIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func newRemoteHubHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("remote hub redirected too many times")
			}
			if req.URL == nil || req.URL.Scheme == "" || req.URL.Host == "" {
				return fmt.Errorf("remote hub redirect url is invalid")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("remote hub redirect must use http or https")
			}
			if req.URL.User != nil {
				return fmt.Errorf("remote hub redirect must not include credentials")
			}
			return validateRemoteHubHost(req.URL.Hostname())
		},
	}
}

func (h *handler) acceptRemoteHubInvite(r *http.Request, remoteURL, token string, identity database.HubIdentity) (*database.HubIdentity, error) {
	payload, err := json.Marshal(acceptHubInviteRequest{Token: token, Peer: identity})
	if err != nil {
		return nil, err
	}

	endpoint := remoteURL + "/api/v1/hub/invites/accept"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := newRemoteHubHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("remote hub rejected invite: %s", message)
	}

	var accepted acceptHubInviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		return nil, err
	}
	accepted.Identity.HubID = strings.TrimSpace(accepted.Identity.HubID)
	if accepted.Identity.HubID == "" {
		return nil, fmt.Errorf("remote hub did not return a hub id")
	}
	return &accepted.Identity, nil
}
