package api

import (
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
