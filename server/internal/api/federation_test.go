package api

import (
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func TestCanPullFederatedPeerUsesConnectedReachablePeers(t *testing.T) {
	tests := []struct {
		name string
		peer database.HubPeer
		want bool
	}{
		{
			name: "bidirectional peer",
			peer: database.HubPeer{Status: "connected", Direction: "bidirectional", PublicURL: "https://peer.example.com"},
			want: true,
		},
		{
			name: "legacy outbound peer",
			peer: database.HubPeer{Status: "connected", Direction: "outbound", PublicURL: "https://peer.example.com"},
			want: true,
		},
		{
			name: "legacy inbound peer",
			peer: database.HubPeer{Status: "connected", Direction: "inbound", PublicURL: "https://peer.example.com"},
			want: true,
		},
		{
			name: "disabled peer",
			peer: database.HubPeer{Status: "disabled", Direction: "bidirectional", PublicURL: "https://peer.example.com"},
			want: false,
		},
		{
			name: "missing url",
			peer: database.HubPeer{Status: "connected", Direction: "bidirectional"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canPullFederatedPeer(tt.peer); got != tt.want {
				t.Fatalf("canPullFederatedPeer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFederatedSourcesAreReadableButNotSharedForRelay(t *testing.T) {
	peer := database.HubPeer{HubID: "hub-a", Name: "Hub A"}
	remoteID := federatedSourceID(peer.HubID, "dispatch")

	h := &handler{}
	allowed := h.canReadCall(
		authUser{ID: "hub-b-user"},
		database.Call{SourceID: remoteID},
		map[string]database.IngestionSource{
			remoteID: {ID: remoteID, Enabled: true, IsShared: false},
		},
		map[string]bool{},
	)
	if !allowed {
		t.Fatalf("remote federated source should be readable on the receiving hub")
	}

	if !database.IsFederatedSourceID(remoteID) {
		t.Fatalf("expected %q to be recognized as federated", remoteID)
	}
}

func TestFederationImportCapForDirectoryStatus(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{status: "unlisted", want: 500},
		{status: "unverified", want: 500},
		{status: "listed", want: 1000},
		{status: "verified", want: 1000},
		{status: "suspended", want: 500},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := federationImportCapForDirectoryStatus(tt.status); got != tt.want {
				t.Fatalf("federationImportCapForDirectoryStatus(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

func TestFederationImportBatchLimit(t *testing.T) {
	if got := federationImportBatchLimit(250); got != 100 {
		t.Fatalf("federationImportBatchLimit(250) = %d, want 100", got)
	}
	if got := federationImportBatchLimit(30); got != 30 {
		t.Fatalf("federationImportBatchLimit(30) = %d, want 30", got)
	}
	if got := federationImportBatchLimit(0); got != 0 {
		t.Fatalf("federationImportBatchLimit(0) = %d, want 0", got)
	}
}

func TestLocalUnsharedSourceStillRequiresOwnershipOrShare(t *testing.T) {
	h := &handler{}
	allowed := h.canReadCall(
		authUser{ID: "other-user"},
		database.Call{SourceID: "local-source"},
		map[string]database.IngestionSource{
			"local-source": {ID: "local-source", UserID: "owner-user", Enabled: true, IsShared: false},
		},
		map[string]bool{},
	)
	if allowed {
		t.Fatalf("local unshared source should not be readable by unrelated users")
	}
}
