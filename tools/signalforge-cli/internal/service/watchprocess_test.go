package service

import "testing"

func TestIsWatchCommand(t *testing.T) {
	cases := map[string]bool{
		"sf rec w -i /tmp/ingest":        true,
		"sf rec w --canary":              true,
		"signalforge recorder watch":     true,
		"node expo start":                false,
		"sf rec i -i /tmp/ingest":        false,
	}
	for command, want := range cases {
		if got := isWatchCommand(command); got != want {
			t.Fatalf("isWatchCommand(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestParsePgrepLine(t *testing.T) {
	process, ok := parsePgrepLine("50320 sf rec w -i /tmp/ingest")
	if !ok || process.PID != 50320 || process.Command != "sf rec w -i /tmp/ingest" {
		t.Fatalf("unexpected parse result: %+v ok=%v", process, ok)
	}
}
