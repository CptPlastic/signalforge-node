package hub_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hub"
)

func TestQueueEnqueueDrain(t *testing.T) {
	dir := t.TempDir()
	q, err := hub.NewQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	audio := filepath.Join(dir, "test.wav")
	if err := os.WriteFile(audio, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	fields := hub.UploadFields{
		Metadata: hub.Metadata{SystemLabel: "OKWIN", Talkgroup: 2711},
		AudioName: "test.wav",
		StartedAt: time.Now(),
	}
	if err := q.Enqueue(audio, fields); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	jsonCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 1 {
		t.Fatalf("queue files=%d", jsonCount)
	}
}
