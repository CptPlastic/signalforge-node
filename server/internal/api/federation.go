package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const federationPollInterval = 15 * time.Second

type federationCallsResponse struct {
	Hub     database.HubIdentity       `json:"hub"`
	Sources []database.IngestionSource `json:"sources"`
	Calls   []database.FederatedCall   `json:"calls"`
}

type federationStatusResponse struct {
	Hub                 database.HubIdentity       `json:"hub"`
	Peers               []database.HubPeer         `json:"peers"`
	SharedSources       []database.IngestionSource `json:"sharedSources"`
	ExportableCallCount int64                      `json:"exportableCallCount"`
	ImportedSourceCount int64                      `json:"importedSourceCount"`
	ImportedCallCount   int64                      `json:"importedCallCount"`
	FederationImportCap int                        `json:"federationImportCap"`
	PullPeerCount       int                        `json:"pullPeerCount"`
	PeerStatuses        []federationPeerStatus     `json:"peerStatuses"`
	Warnings            []string                   `json:"warnings"`
}

type federationPeerStatus struct {
	PeerID              string `json:"peerId"`
	HubID               string `json:"hubId"`
	Name                string `json:"name"`
	PublicURL           string `json:"publicUrl"`
	CanPull             bool   `json:"canPull"`
	RemoteSharedSources int    `json:"remoteSharedSources"`
	RemoteSampleCalls   int    `json:"remoteSampleCalls"`
	ImportedCallCount   int64  `json:"importedCallCount"`
	ImportCap           int    `json:"importCap"`
	ImportCapReached    bool   `json:"importCapReached"`
	Error               string `json:"error,omitempty"`
}

func (h *handler) handleFederationStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity for federation status failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return
	}
	peers, err := h.db.ListHubPeers()
	if err != nil {
		h.logger.Error("list peers for federation status failed", "error", err)
		http.Error(w, "list hub peers", http.StatusInternalServerError)
		return
	}
	sharedSources, err := h.db.ListSharedIngestionSources()
	if err != nil {
		h.logger.Error("list shared sources for federation status failed", "error", err)
		http.Error(w, "list shared sources", http.StatusInternalServerError)
		return
	}
	for i := range sharedSources {
		sharedSources[i].UserID = ""
	}
	exportableCallCount, err := h.db.CountFederatedCalls()
	if err != nil {
		h.logger.Error("count federated calls failed", "error", err)
		http.Error(w, "count federated calls", http.StatusInternalServerError)
		return
	}
	importedSourceCount, err := h.db.CountImportedFederatedSources()
	if err != nil {
		h.logger.Error("count imported federated sources failed", "error", err)
		http.Error(w, "count imported federated sources", http.StatusInternalServerError)
		return
	}
	importedCallCount, err := h.db.CountImportedFederatedCalls()
	if err != nil {
		h.logger.Error("count imported federated calls failed", "error", err)
		http.Error(w, "count imported federated calls", http.StatusInternalServerError)
		return
	}

	importCap := federationImportCapForDirectoryStatus(identity.DirectoryValidationStatus)
	pullPeerCount := 0
	peerStatuses := make([]federationPeerStatus, 0, len(peers))
	for _, peer := range peers {
		if canPullFederatedPeer(peer) {
			pullPeerCount++
		}
		peerStatuses = append(peerStatuses, h.checkFederationPeerStatus(identity.HubID, peer, importCap))
	}
	warnings := make([]string, 0)
	if !identity.FederationEnabled {
		warnings = append(warnings, "federation is disabled")
	}
	if pullPeerCount == 0 {
		warnings = append(warnings, "this hub has no connected outbound peer to pull from")
	}
	if len(sharedSources) == 0 {
		warnings = append(warnings, "no enabled sources are marked shared")
	}
	if exportableCallCount == 0 {
		warnings = append(warnings, "no calls are currently eligible for federation export")
	}
	for _, peerStatus := range peerStatuses {
		if !peerStatus.CanPull {
			continue
		}
		if peerStatus.Error != "" {
			warnings = append(warnings, fmt.Sprintf("peer %s federation check failed: %s", peerStatus.Name, peerStatus.Error))
		} else if peerStatus.RemoteSharedSources == 0 {
			warnings = append(warnings, fmt.Sprintf("peer %s has no enabled shared sources", peerStatus.Name))
		} else if peerStatus.RemoteSampleCalls == 0 {
			warnings = append(warnings, fmt.Sprintf("peer %s has shared sources but no exportable calls yet", peerStatus.Name))
		}
		if peerStatus.ImportCapReached {
			warnings = append(warnings, fmt.Sprintf(
				"peer %s import cap reached (%d/%d calls — listed hubs may pull %d, unlisted %d)",
				peerStatus.Name,
				peerStatus.ImportedCallCount,
				peerStatus.ImportCap,
				federationImportCapListed,
				federationImportCapUnlisted,
			))
		}
	}

	writeJSON(w, http.StatusOK, federationStatusResponse{
		Hub:                 *identity,
		Peers:               peers,
		SharedSources:       sharedSources,
		ExportableCallCount: exportableCallCount,
		ImportedSourceCount: importedSourceCount,
		ImportedCallCount:   importedCallCount,
		FederationImportCap: importCap,
		PullPeerCount:       pullPeerCount,
		PeerStatuses:        peerStatuses,
		Warnings:            warnings,
	})
}

func (h *handler) checkFederationPeerStatus(localHubID string, peer database.HubPeer, importCap int) federationPeerStatus {
	status := federationPeerStatus{
		PeerID:    peer.ID,
		HubID:     peer.HubID,
		Name:      peer.Name,
		PublicURL: peer.PublicURL,
		CanPull:   canPullFederatedPeer(peer),
		ImportCap: importCap,
	}
	if imported, err := h.db.CountImportedFederatedCallsFromPeer(peer.HubID); err == nil {
		status.ImportedCallCount = imported
		status.ImportCapReached = imported >= int64(importCap)
	}
	if status.Name == "" {
		status.Name = peer.HubID
	}
	if !status.CanPull {
		return status
	}

	remoteURL, err := normalizeHubURL(peer.PublicURL)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	var sourcesPayload struct {
		Sources []database.IngestionSource `json:"sources"`
	}
	if err := h.getFederationJSON(localHubID, remoteURL+"/api/v1/federation/sources", &sourcesPayload); err != nil {
		status.Error = err.Error()
		return status
	}
	status.RemoteSharedSources = len(sourcesPayload.Sources)
	if status.RemoteSharedSources == 0 {
		return status
	}

	var callsPayload federationCallsResponse
	if err := h.getFederationJSON(localHubID, remoteURL+"/api/v1/federation/calls?since=0&limit=1", &callsPayload); err != nil {
		status.Error = err.Error()
		return status
	}
	status.RemoteSampleCalls = len(callsPayload.Calls)
	return status
}

func (h *handler) getFederationJSON(localHubID, endpoint string, target any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-SignalHub-Peer-ID", localHubID)

	client := newRemoteHubHTTPClient(5 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (h *handler) handleFederationSources(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.requireFederationEnabled(w, r)
	if !ok {
		return
	}

	sources, err := h.db.ListSharedIngestionSources()
	if err != nil {
		h.logger.Error("list federated sources failed", "error", err)
		http.Error(w, "query federated sources", http.StatusInternalServerError)
		return
	}
	for i := range sources {
		sources[i].UserID = ""
	}

	writeJSON(w, http.StatusOK, map[string]any{"hub": identity, "sources": sources})
}

func (h *handler) handleFederationCalls(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.requireFederationEnabled(w, r)
	if !ok {
		return
	}

	sinceID := queryInt64(r, "since", 0)
	limit := boundedQueryInt(r, "limit", 100, 1, 250)
	sources, err := h.db.ListSharedIngestionSources()
	if err != nil {
		h.logger.Error("list federated sources failed", "error", err)
		http.Error(w, "query federated sources", http.StatusInternalServerError)
		return
	}
	for i := range sources {
		sources[i].UserID = ""
	}

	calls, err := h.db.ListFederatedCalls(sinceID, limit)
	if err != nil {
		h.logger.Error("list federated calls failed", "error", err)
		http.Error(w, "query federated calls", http.StatusInternalServerError)
		return
	}
	for i := range calls {
		calls[i].Call.UserID = ""
		calls[i].AudioBase = base64.StdEncoding.EncodeToString(calls[i].Audio)
		calls[i].Audio = nil
	}

	writeJSON(w, http.StatusOK, federationCallsResponse{Hub: *identity, Sources: sources, Calls: calls})
}

func (h *handler) requireFederationEnabled(w http.ResponseWriter, r *http.Request) (*database.HubIdentity, bool) {
	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity for federation failed", "error", err)
		http.Error(w, errLoadHubIdentity, http.StatusInternalServerError)
		return nil, false
	}
	if !identity.FederationEnabled {
		http.Error(w, errFederationOff, http.StatusForbidden)
		return nil, false
	}
	if !h.isAuthorizedFederationPeer(r) {
		http.Error(w, "unauthorized peer", http.StatusUnauthorized)
		return nil, false
	}
	return identity, true
}

func (h *handler) isAuthorizedFederationPeer(r *http.Request) bool {
	peerHubID := strings.TrimSpace(r.Header.Get("X-SignalHub-Peer-ID"))
	if peerHubID == "" {
		return false
	}
	peers, err := h.db.ListHubPeers()
	if err != nil {
		h.logger.Error("list peers for federation authorization failed", "error", err)
		return false
	}
	for _, peer := range peers {
		if peer.HubID == peerHubID && peer.Status == "connected" {
			return true
		}
	}
	return false
}

func queryInt64(r *http.Request, key string, fallback int64) int64 {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func (h *handler) startFederationSyncLoop() {
	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			<-timer.C
			h.syncFederatedPeers()
			timer.Reset(federationPollInterval)
		}
	}()
}

func (h *handler) syncFederatedPeers() {
	identity, err := h.ensureHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity before federation sync failed", "error", err)
		return
	}
	if !identity.FederationEnabled {
		return
	}

	peers, err := h.db.ListHubPeers()
	if err != nil {
		h.logger.Error("list peers for federation sync failed", "error", err)
		return
	}
	for _, peer := range peers {
		if !canPullFederatedPeer(peer) {
			continue
		}
		if err := h.syncFederatedPeer(identity, peer); err != nil {
			h.logger.Error("sync federated peer failed", "peer_hub_id", peer.HubID, "peer_url", peer.PublicURL, "error", err)
		}
	}
}

func canPullFederatedPeer(peer database.HubPeer) bool {
	return peer.Status == "connected" && strings.TrimSpace(peer.PublicURL) != ""
}

func (h *handler) syncFederatedPeer(identity *database.HubIdentity, peer database.HubPeer) error {
	importCap := federationImportCapForDirectoryStatus(identity.DirectoryValidationStatus)
	importedCount, err := h.db.CountImportedFederatedCallsFromPeer(peer.HubID)
	if err != nil {
		return fmt.Errorf("count imported calls from peer: %w", err)
	}
	if importedCount >= int64(importCap) {
		return nil
	}
	remaining := int(int64(importCap) - importedCount)
	batchLimit := federationImportBatchLimit(remaining)
	if batchLimit == 0 {
		return nil
	}

	sinceID, err := h.db.MaxImportedRemoteCallID(peer.HubID)
	if err != nil {
		return fmt.Errorf("load last imported call id: %w", err)
	}

	remoteURL, err := normalizeHubURL(peer.PublicURL)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/v1/federation/calls?since=%d&limit=%d", remoteURL, sinceID, batchLimit)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-SignalHub-Peer-ID", identity.HubID)

	client := newRemoteHubHTTPClient(20 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("remote federation endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload federationCallsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.Hub.HubID) != "" && payload.Hub.HubID != peer.HubID {
		return fmt.Errorf("remote hub id mismatch: expected %s got %s", peer.HubID, payload.Hub.HubID)
	}
	return h.importFederatedCalls(peer, payload, importCap, importedCount)
}

func (h *handler) importFederatedCalls(peer database.HubPeer, payload federationCallsResponse, importCap int, alreadyImported int64) error {
	sourceLabels, err := h.upsertFederatedSources(peer, payload.Sources)
	if err != nil {
		return err
	}

	importedThisBatch := int64(0)
	for _, item := range payload.Calls {
		if alreadyImported+importedThisBatch >= int64(importCap) {
			break
		}
		inserted, err := h.importFederatedCall(peer, sourceLabels, item)
		if err != nil {
			return err
		}
		if inserted {
			importedThisBatch++
		}
	}
	return nil
}

func (h *handler) upsertFederatedSources(peer database.HubPeer, sources []database.IngestionSource) (map[string]string, error) {
	sourceLabels := make(map[string]string, len(sources))
	for _, source := range sources {
		label := federatedSourceLabel(source)
		sourceLabels[source.ID] = label
		if err := h.db.UpsertIngestionSource(database.IngestionSource{
			ID:            federatedSourceID(peer.HubID, source.ID),
			Label:         fmt.Sprintf("%s / %s", peer.Name, label),
			Enabled:       true,
			IsShared:      false,
			SystemID:      source.SystemID,
			SystemLabel:   source.SystemLabel,
			LastSeenUnix:  source.LastSeenUnix,
			ErrorCount:    source.ErrorCount,
			CallsReceived: source.CallsReceived,
		}); err != nil {
			return nil, fmt.Errorf("upsert remote source %s: %w", source.ID, err)
		}
	}
	return sourceLabels, nil
}

func (h *handler) importFederatedCall(peer database.HubPeer, sourceLabels map[string]string, item database.FederatedCall) (bool, error) {
	remoteCallID := item.Call.ID
	if remoteCallID <= 0 {
		return false, nil
	}
	remoteSourceID := federatedCallSourceID(item)
	localSourceID := federatedSourceID(peer.HubID, remoteSourceID)
	if _, ok := sourceLabels[remoteSourceID]; !ok {
		if err := h.upsertFallbackFederatedSource(peer, remoteSourceID, localSourceID); err != nil {
			return false, err
		}
	}

	audio, err := base64.StdEncoding.DecodeString(item.AudioBase)
	if err != nil {
		return false, fmt.Errorf("decode remote call %d audio: %w", remoteCallID, err)
	}
	call := item.Call
	call.ID = 0
	call.UserID = ""
	call.SourceID = localSourceID
	localCallID, err := h.db.InsertCall(&call, audio)
	if err != nil {
		return false, fmt.Errorf("insert remote call %d: %w", remoteCallID, err)
	}
	call.ID = localCallID
	h.finalizeCallTranscription(&call)
	inserted, err := h.db.RecordFederatedCallImport(peer.HubID, remoteCallID, localCallID)
	if err != nil {
		return false, fmt.Errorf("record remote call %d import: %w", remoteCallID, err)
	}
	if inserted {
		_ = h.db.IncrementSourceMetrics(localSourceID, true)
		h.broadcastCall(&call, localSourceID)
		h.streamHub.push(&call, audio)
	}
	return inserted, nil
}

func (h *handler) upsertFallbackFederatedSource(peer database.HubPeer, remoteSourceID, localSourceID string) error {
	if err := h.db.UpsertIngestionSource(database.IngestionSource{
		ID:       localSourceID,
		Label:    fmt.Sprintf("%s / remote source", peer.Name),
		Enabled:  true,
		IsShared: false,
	}); err != nil {
		return fmt.Errorf("upsert fallback remote source %s: %w", remoteSourceID, err)
	}
	return nil
}

func federatedSourceLabel(source database.IngestionSource) string {
	label := strings.TrimSpace(source.Label)
	if label == "" {
		label = source.ID
	}
	return label
}

func federatedCallSourceID(item database.FederatedCall) string {
	remoteSourceID := strings.TrimSpace(item.Source)
	if remoteSourceID == "" {
		remoteSourceID = strings.TrimSpace(item.Call.SourceID)
	}
	return remoteSourceID
}

func federatedSourceID(peerHubID, sourceID string) string {
	cleanPeer := strings.NewReplacer(":", "_", "/", "_", " ", "_").Replace(strings.TrimSpace(peerHubID))
	cleanSource := strings.NewReplacer(":", "_", "/", "_", " ", "_").Replace(strings.TrimSpace(sourceID))
	if cleanSource == "" {
		cleanSource = "unknown"
	}
	return "remote_" + cleanPeer + "_" + cleanSource
}
