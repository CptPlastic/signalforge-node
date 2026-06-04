package database

import (
	"crypto/rand"
	"encoding/binary"
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

// randomNumericCode returns a cryptographically random numeric code of the
// given length, zero-padded (e.g. "048213" for length 6).
func randomNumericCode(digits int) string {
	if digits <= 0 {
		digits = 6
	}
	max := uint64(1)
	for i := 0; i < digits; i++ {
		max *= 10
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand.Read failed: %v", err))
	}
	n := binary.BigEndian.Uint64(b) % max
	return fmt.Sprintf("%0*d", digits, n)
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// IsFederatedSourceID reports whether a source was imported from a peer hub.
func IsFederatedSourceID(sourceID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sourceID), "remote_")
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
