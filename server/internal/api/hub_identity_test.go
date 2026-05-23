package api

import (
	"testing"

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
