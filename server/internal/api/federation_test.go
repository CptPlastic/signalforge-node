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
