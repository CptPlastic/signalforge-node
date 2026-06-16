package trunkrecorder_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/decode/trunkrecorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
)

func TestGenerateOKWIN(t *testing.T) {
	cfg := config.Default()
	cfg.Trunking.Systems[0].Sites = []config.Site{{
		Include:     true,
		Frequencies: []string{"857.9375c", "858.9375c"},
	}}
	devices := []sdr.Device{
		{Index: 0, Role: sdr.RoleControlHunt},
		{Index: 1, Role: sdr.RoleVoice},
		{Index: 2, Role: sdr.RoleGMRS},
	}
	dir := t.TempDir()
	tr, err := trunkrecorder.Generate(cfg, filepath.Join(dir, "trunk.yaml"), devices)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Sources) != 3 {
		t.Fatalf("sources=%d", len(tr.Sources))
	}
	if tr.Sources[2].Center != 462650000 {
		t.Fatalf("gmrs center=%v", tr.Sources[2].Center)
	}
	if len(tr.Systems) < 2 {
		t.Fatalf("systems=%d", len(tr.Systems))
	}
	if tr.Systems[0].Type != "p25" || len(tr.Systems[0].ControlChannels) != 2 {
		t.Fatalf("p25 system=%+v", tr.Systems[0])
	}
}

func TestShortName(t *testing.T) {
	if got := trunkrecorder.ShortName("OKWIN"); got != "OKWIN" {
		t.Fatalf("short=%q", got)
	}
}

func TestWatcherParseCall(t *testing.T) {
	dir := t.TempDir()
	meta := map[string]any{
		"freq":          858937500,
		"start_time":    1700000000,
		"stop_time":     1700000010,
		"call_length":   10,
		"talkgroup":     1001,
		"talkgroup_tag": "TEST",
		"short_name":    "OKWIN",
	}
	data, _ := json.Marshal(meta)
	jsonPath := filepath.Join(dir, "call.json")
	wavPath := filepath.Join(dir, "call.wav")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wavPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	call, ok := trunkrecorder.ParseCallFile(jsonPath)
	if !ok {
		t.Fatal("expected parsed call")
	}
	if call.Meta.Talkgroup != 1001 || call.AudioPath != wavPath {
		t.Fatalf("call=%+v", call)
	}
}
