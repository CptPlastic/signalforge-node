package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBeaconMetadataDefaultsLabel(t *testing.T) {
	settings := DefaultSettings()
	settings.Beacon.Enabled = true
	meta := settings.BeaconMetadata()
	if meta.TalkgroupLabel != "BEACON" {
		t.Fatalf("expected BEACON label, got %q", meta.TalkgroupLabel)
	}
}

func TestBeaconUploadName(t *testing.T) {
	name := BeaconUploadName("/tmp/announcement.mp3", time.Unix(1710000000, 0))
	if name != "announcement-1710000000.mp3" {
		t.Fatalf("unexpected upload name: %s", name)
	}
}

func TestBeaconFileMatches(t *testing.T) {
	dir := t.TempDir()
	beacon := filepath.Join(dir, "beacon.wav")
	other := filepath.Join(dir, "call.wav")
	if err := os.WriteFile(beacon, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings := Settings{Beacon: BeaconSettings{FilePath: beacon}}
	if !settings.BeaconFileMatches(beacon) {
		t.Fatal("expected beacon path to match")
	}
	if settings.BeaconFileMatches(other) {
		t.Fatal("expected other path not to match")
	}
}
