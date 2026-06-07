package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorderWatchOnceUploadsAndMovesStableFile(t *testing.T) {
	t.Setenv("SIGNALFORGE_NO_UPDATE_CHECK", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/call-upload" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := r.FormValue("key"); got != "test-key" {
			http.Error(w, fmt.Sprintf("key %q", got), http.StatusUnauthorized)
			return
		}
		if got := r.FormValue("talkgroup"); got != "18" {
			http.Error(w, fmt.Sprintf("talkgroup %q", got), http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = file.Close()
		if header.Filename != "call.wav" {
			http.Error(w, fmt.Sprintf("filename %q", header.Filename), http.StatusBadRequest)
			return
		}
		uploads++
		fmt.Fprint(w, "Call imported successfully.\n")
	}))
	defer server.Close()

	dir := t.TempDir()
	input := filepath.Join(dir, "ingest")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(input, "call.wav")
	if err := os.WriteFile(audioPath, fakeWAV(), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(audioPath, old, old); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--hub-url", server.URL,
		"-k", "test-key",
		"recorder", "watch",
		"-i", input,
		"-s", "1ms",
		"-o",
		"--poll", "1s",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v\n%s", err, out.String())
	}
	if uploads != 1 {
		t.Fatalf("expected 1 upload, got %d", uploads)
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Fatalf("expected source file to move, stat err=%v", err)
	}
	processedPath := filepath.Join(input, "processed", "call.wav")
	if _, err := os.Stat(processedPath); err != nil {
		t.Fatalf("expected processed file: %v", err)
	}
	if !strings.Contains(out.String(), "uploaded:") {
		t.Fatalf("expected upload output, got %q", out.String())
	}
}

func TestUpdateCheckUsesConfiguredReleaseAPI(t *testing.T) {
	t.Setenv("SIGNALFORGE_NO_UPDATE_CHECK", "1")
	t.Setenv("LocalAppData", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"signalforge-cli-v99.0.0","html_url":"https://example.invalid/releases/99","assets":[{"name":"signalforge-windows-amd64.exe","browser_download_url":"https://example.invalid/signalforge.exe"}]}`)
	}))
	defer server.Close()
	t.Setenv("SIGNALFORGE_UPDATE_URL", server.URL+"/latest")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"update", "check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v\n%s", err, out.String())
	}
	for _, want := range []string{"latest: 99.0.0", "status: up to date", "signalforge-windows-amd64.exe"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in output:\n%s", want, out.String())
		}
	}
}

func TestVersionShortcuts(t *testing.T) {
	t.Setenv("SIGNALFORGE_NO_UPDATE_CHECK", "1")

	for _, args := range [][]string{{"version"}, {"ver"}, {"v"}, {"--version"}, {"-v"}, {"--v"}} {
		cmd := NewRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out.String())
		}
		if !strings.Contains(out.String(), "signalforge:") {
			t.Fatalf("expected version output for %v, got:\n%s", args, out.String())
		}
	}
}

func TestCommandAliases(t *testing.T) {
	t.Setenv("SIGNALFORGE_NO_UPDATE_CHECK", "1")

	dir := t.TempDir()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rec", "i", "-i", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "mode: folder") {
		t.Fatalf("expected recorder inspect output, got:\n%s", out.String())
	}

	out.Reset()
	cmd = NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"tab", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("completion alias failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "autocompletion") {
		t.Fatalf("expected completion help, got:\n%s", out.String())
	}

	out.Reset()
	cmd = NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("root help failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "sf rec chk") || !strings.Contains(out.String(), "signalforge also works") {
		t.Fatalf("expected sf alias in root help, got:\n%s", out.String())
	}
}

func fakeWAV() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 40, 0, 0, 0, 'W', 'A', 'V', 'E', 'f', 'm', 't', ' ',
		16, 0, 0, 0, 1, 0, 1, 0, 0x40, 0x1f, 0, 0, 0x80, 0x3e, 0, 0,
		2, 0, 16, 0, 'd', 'a', 't', 'a', 4, 0, 0, 0, 0, 0, 0, 0,
	}
}
