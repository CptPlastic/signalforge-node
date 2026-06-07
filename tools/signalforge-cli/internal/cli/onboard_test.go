package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardNonInteractive(t *testing.T) {
	t.Setenv("SIGNALFORGE_NO_UPDATE_CHECK", "1")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/call-upload":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			_ = r.ParseMultipartForm(1 << 20)
			if r.FormValue("key") != "test-key" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if r.FormValue("test") == "1" {
				w.WriteHeader(http.StatusExpectationFailed)
				_, _ = w.Write([]byte("incomplete call data"))
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ingest := filepath.Join(t.TempDir(), "ingest")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SetArgs([]string{
		"--hub-url", server.URL,
		"-k", "test-key",
		"onboard", "--yes",
		"--folder",
		"--input", ingest,
		"--canary",
		"--canary-interval", "2m",
	})
	if err := os.MkdirAll(ingest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "profile saved") {
		t.Fatalf("expected save confirmation:\n%s", out.String())
	}

	profilePath := filepath.Join(configHome, "signalforge", "profile.json")
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("profile not written: %v", err)
	}
}
