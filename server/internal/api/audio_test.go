package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeAudioBytesSupportsRangeRequests(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1/audio", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()

	serveAudioBytes(rec, req, []byte("0123456789"), "audio/mpeg", "call.mp3", false, "no-store")

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Fatalf("expected range body %q, got %q", "2345", got)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("expected Content-Range %q, got %q", "bytes 2-5/10", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "inline; filename=call.mp3" {
		t.Fatalf("expected inline disposition, got %q", got)
	}
}

func TestServeAudioBytesSupportsDownloadDisposition(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1/audio?download=1", nil)
	rec := httptest.NewRecorder()

	serveAudioBytes(rec, req, []byte("audio"), "audio/mpeg", "call.mp3", true, "private, max-age=3600")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment; filename=call.mp3" {
		t.Fatalf("expected attachment disposition, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Fatalf("expected Cache-Control header, got %q", got)
	}
}

func TestBrowserPlayableAudioRejectsAIFF(t *testing.T) {
	if browserPlayableAudio("audio/aiff", "call.aiff") {
		t.Fatal("expected AIFF to require browser playback conversion")
	}
	if !browserPlayableAudio("audio/mpeg", "call.mp3") {
		t.Fatal("expected MP3 to be browser playable")
	}
}

func TestAudioNameWithExt(t *testing.T) {
	if got := audioNameWithExt("call.aiff", ".mp3"); got != "call.mp3" {
		t.Fatalf("expected call.mp3, got %q", got)
	}
	if got := audioNameWithExt("call", ".mp3"); got != "call.mp3" {
		t.Fatalf("expected call.mp3, got %q", got)
	}
}
