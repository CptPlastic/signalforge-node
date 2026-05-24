package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectInputCountsSupportedFolderAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one.wav", "two.mp3", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	status, err := InspectInput(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != "folder" || status.SupportedCount != 2 || status.SkippedCount != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestValidateFileInputRejectsUnsupportedExtensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ValidateFileInput(path); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}

func TestReadyFilesOnlyReturnsStableSupportedFiles(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready.wav")
	fresh := filepath.Join(dir, "fresh.mp3")
	ignored := filepath.Join(dir, "ignored.txt")
	for _, path := range []string{ready, fresh, ignored} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(ready, now.Add(-10*time.Second), now.Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh, now, now); err != nil {
		t.Fatal(err)
	}

	files, err := ReadyFiles(Settings{Input: dir, Stable: 5 * time.Second}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "ready.wav" {
		t.Fatalf("unexpected files: %+v", files)
	}
}

func TestMoveToProcessedUsesUniqueDestination(t *testing.T) {
	dir := t.TempDir()
	processed := filepath.Join(dir, "processed")
	path := filepath.Join(dir, "call.wav")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(processed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processed, "call.wav"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	destination, err := MoveToProcessed(Settings{Input: dir}, path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(destination) != "call-1.wav" {
		t.Fatalf("expected unique destination, got %s", destination)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected source to be moved, stat err=%v", err)
	}
}
