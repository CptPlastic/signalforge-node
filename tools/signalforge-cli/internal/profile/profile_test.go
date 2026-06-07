package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	profile := Default()
	profile.HubURL = "https://hub.example.com"
	profile.SourceKey = "sk_test_key"
	profile.Folder.Enabled = true
	profile.Folder.Directory = filepath.Join(dir, "ingest")
	profile.Canary.Enabled = true
	profile.Canary.IntervalSec = 120

	path, err := Save(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("profile file missing: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HubURL != profile.HubURL {
		t.Fatalf("hub url mismatch: %q", loaded.HubURL)
	}
	if loaded.SourceKey != profile.SourceKey {
		t.Fatalf("source key mismatch")
	}
	if !loaded.Canary.Enabled || loaded.Canary.IntervalSec != 120 {
		t.Fatalf("canary config mismatch: %+v", loaded.Canary)
	}

	tomlPath, err := RecorderTOMLPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[canary]") || !strings.Contains(string(data), "enabled = true") {
		t.Fatalf("expected canary section in toml:\n%s", data)
	}
}
