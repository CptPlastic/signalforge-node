package service

import (
	"strings"
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/profile"
)

func TestWatchArgsFolderAndCanary(t *testing.T) {
	prof := profile.Default()
	prof.Folder.Enabled = true
	prof.Folder.Directory = "/tmp/ingest"
	prof.Canary.Enabled = true

	args, err := WatchArgs(prof)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"rec watch", "-i /tmp/ingest", "--canary"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in %q", want, joined)
		}
	}
}

func TestWatchArgsBeaconOnly(t *testing.T) {
	prof := profile.Default()
	prof.Folder.Enabled = false
	prof.Beacon.Enabled = true
	prof.Beacon.FilePath = "/tmp/beacon.wav"

	args, err := WatchArgs(prof)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"rec watch", "--beacon", "--beacon-file /tmp/beacon.wav"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in %q", want, joined)
		}
	}
}

func TestWatchArgsRequiresMode(t *testing.T) {
	prof := profile.Default()
	prof.Folder.Enabled = false
	prof.Canary.Enabled = false
	if _, err := WatchArgs(prof); err == nil {
		t.Fatal("expected error when no watch mode enabled")
	}
}
