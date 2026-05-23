package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func TestConfiguredHubIdentityRefreshesDeploymentURL(t *testing.T) {
	handle := handler{cfg: config.Config{
		HubName:       "SignalForge Hub",
		HubPublicURL:  "https://p7hub.projectseven.us",
		HubRegion:     "Main",
		HubFederation: true,
	}}

	updated := handle.configuredHubIdentity(&database.HubIdentity{
		HubID:             "hub_123",
		Name:              "Old Hub",
		PublicURL:         "https://p7scan.projectseven.us",
		Region:            "Old",
		FederationEnabled: false,
		TrustLevel:        "community",
	})

	if updated.PublicURL != "https://p7hub.projectseven.us" {
		t.Fatalf("PublicURL = %q, want current deployment URL", updated.PublicURL)
	}
	if updated.Name != "SignalForge Hub" {
		t.Fatalf("Name = %q, want configured hub name", updated.Name)
	}
	if updated.Region != "Main" {
		t.Fatalf("Region = %q, want configured hub region", updated.Region)
	}
	if !updated.FederationEnabled {
		t.Fatal("FederationEnabled = false, want configured federation enabled")
	}
}

func TestNormalizeHubURLRejectsUnsafeHosts(t *testing.T) {
	tests := []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://10.0.0.5",
		"http://172.16.0.5",
		"http://192.168.1.10",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]:8080",
		"https://user:pass@example.com",
	}

	for _, raw := range tests {
		if got, err := normalizeHubURL(raw); err == nil {
			t.Fatalf("normalizeHubURL(%q) = %q, want error", raw, got)
		}
	}
}

func TestNormalizeHubURLAllowsPublicHubURL(t *testing.T) {
	got, err := normalizeHubURL("peer.projectseven.us/api/v1?ignored=1#fragment")
	if err != nil {
		t.Fatalf("normalizeHubURL returned error: %v", err)
	}
	if got != "https://peer.projectseven.us/api/v1" {
		t.Fatalf("normalizeHubURL = %q, want normalized public URL", got)
	}
}

func TestNormalizeHubURLRejectsUnresolvedHosts(t *testing.T) {
	_, err := normalizeHubURL("https://nonexistent.invalid")
	if err == nil || !strings.Contains(err.Error(), "could not be resolved") {
		t.Fatalf("normalizeHubURL unresolved host error = %v, want resolution error", err)
	}
}

func TestNormalizeDirectoryURLPreservesFeedPath(t *testing.T) {
	got, err := normalizeDirectoryURL("https://signalforge.org/directory/hubs.json?cache=1#ignored")
	if err != nil {
		t.Fatalf("normalizeDirectoryURL returned error: %v", err)
	}
	if got != "https://signalforge.org/directory/hubs.json?cache=1" {
		t.Fatalf("normalizeDirectoryURL = %q, want feed URL without fragment", got)
	}
}

func TestNormalizeDirectoryURLRejectsUnsafeHosts(t *testing.T) {
	tests := []string{
		"http://localhost/directory/hubs.json",
		"http://127.0.0.1/directory/hubs.json",
		"https://user:pass@signalforge.org/directory/hubs.json",
		"file:///tmp/hubs.json",
	}

	for _, raw := range tests {
		if got, err := normalizeDirectoryURL(raw); err == nil {
			t.Fatalf("normalizeDirectoryURL(%q) = %q, want error", raw, got)
		}
	}
}

func TestGenerateHubKeyPairReturnsEd25519Keys(t *testing.T) {
	publicKey, privateKey, err := generateHubKeyPair()
	if err != nil {
		t.Fatalf("generateHubKeyPair returned error: %v", err)
	}
	if !strings.HasPrefix(publicKey, "ed25519:") {
		t.Fatalf("public key = %q, want ed25519 prefix", publicKey)
	}
	if !strings.HasPrefix(privateKey, "ed25519:") {
		t.Fatalf("private key = %q, want ed25519 prefix", privateKey)
	}
	if publicKey == privateKey {
		t.Fatal("public and private keys matched, want distinct encoded keys")
	}
}

func TestHubIdentityJSONOmitsPrivateKey(t *testing.T) {
	payload, err := json.Marshal(database.HubIdentity{
		HubID:      "hub_test",
		PublicKey:  "ed25519:public",
		PrivateKey: "ed25519:private",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	if strings.Contains(encoded, "private") || strings.Contains(encoded, "PrivateKey") {
		t.Fatalf("hub identity JSON exposed private key: %s", encoded)
	}
}

func TestRemoteHubHTTPClientRejectsUnsafeRedirect(t *testing.T) {
	client := newRemoteHubHTTPClient(time.Second)
	redirectURL, err := url.Parse("http://127.0.0.1:8080/api/v1/hub/invites/accept")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: redirectURL}
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("CheckRedirect accepted localhost redirect, want error")
	}
}
