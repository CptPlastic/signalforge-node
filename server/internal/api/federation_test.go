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
