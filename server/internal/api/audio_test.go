package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestStreamAudioIsMP3(t *testing.T) {
	if !streamAudioIsMP3("audio/mpeg", []byte{0xFF, 0xFB, 0x90, 0x00}) {
		t.Fatal("expected mp3 sync frame to be mp3")
	}
	m4a := []byte{0x00, 0x00, 0x00, 0x1c, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}
	if streamAudioIsMP3("audio/mpeg", m4a) {
		t.Fatal("expected mislabeled m4a not to be mp3")
	}
	if !streamAudioIsMP3("audio/mp4", []byte("ID3")) {
		t.Fatal("expected ID3 tag to be treated as mp3")
	}
	if streamAudioIsMP3("audio/mp4", m4a) {
		t.Fatal("expected m4a ftyp to not be mp3")
	}
	wav := append(append([]byte("RIFF"), make([]byte, 4)...), []byte("WAVE")...)
	if streamAudioIsMP3("audio/mpeg", wav) {
		t.Fatal("expected wav not to be mp3")
	}
}

func TestPreparePublicStreamAudioSkipsWhenNotRequested(t *testing.T) {
	in := []byte("wav-bytes")
	out, outType, err := preparePublicStreamAudio(t.Context(), in, "audio/wav", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(in) || outType != "audio/wav" {
		t.Fatalf("expected passthrough, got type=%q len=%d", outType, len(out))
	}
}

func TestPreparePublicStreamAudioPassesThroughMP3(t *testing.T) {
	in := []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02}
	out, outType, err := preparePublicStreamAudio(t.Context(), in, "audio/mp4", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(in) || outType != "audio/mpeg" {
		t.Fatalf("expected mp3 passthrough with audio/mpeg type, got type=%q", outType)
	}
}

func TestPTTAudioNeedsNormalize(t *testing.T) {
	if pttAudioNeedsNormalize("audio/mp4") {
		t.Fatal("expected mp4 to skip normalize")
	}
	if pttAudioNeedsNormalize("audio/mpeg") {
		t.Fatal("expected mp3 to skip normalize")
	}
	if !pttAudioNeedsNormalize("audio/webm") {
		t.Fatal("expected webm to require normalize")
	}
	if !pttAudioNeedsNormalize("audio/webm;codecs=opus") {
		t.Fatal("expected webm opus to require normalize")
	}
	if !pttAudioNeedsNormalize("audio/mp4;codecs=opus") {
		t.Fatal("expected mp4 opus to require normalize")
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

func TestNormalizePTTAudioStoresWebMWhenTranscodeFails(t *testing.T) {
	// Not valid webm — ffmpeg should fail; upload must still succeed with original bytes.
	in := []byte(strings.Repeat("x", 128))
	out, outType, outName, err := normalizePTTAudio(t.Context(), in, "audio/webm", "ptt.webm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(in) {
		t.Fatal("expected original bytes when transcode fails")
	}
	if outType != "audio/webm" {
		t.Fatalf("expected audio/webm, got %q", outType)
	}
	if outName != "ptt.webm" {
		t.Fatalf("expected ptt.webm, got %q", outName)
	}
}
