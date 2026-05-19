package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

func randomToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand.Read failed: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// NewShareToken generates a new cryptographically random share token.
func NewShareToken() string {
	return randomToken("share_")
}

// NewHubID generates a stable public identifier for this hub.
func NewHubID() string {
	return randomToken("hub_")
}

// NewHubInviteID generates a stable identifier for a hub invite record.
func NewHubInviteID() string {
	return randomToken("invite_")
}

// NewHubInviteToken generates the secret token shared with a peer hub.
func NewHubInviteToken() string {
	return randomToken("hub_invite_")
}

// NewHubPeerID generates a stable identifier for a known hub peer.
func NewHubPeerID() string {
	return randomToken("peer_")
}
