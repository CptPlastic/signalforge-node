package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func TestParsePublicStreamFile(t *testing.T) {
	cases := []struct {
		name  string
		token string
		kind  string
		ok    bool
	}{
		{name: "share_abc.mp3", token: "share_abc", kind: "mp3", ok: true},
		{name: "share_abc.MP3", token: "share_abc", kind: "mp3", ok: true},
		{name: "share_abc.m3u", token: "share_abc", kind: "m3u", ok: true},
		{name: "share_abc.pls", token: "share_abc", kind: "pls", ok: true},
		{name: "share_abc", token: "share_abc", kind: "mp3", ok: true},
		{name: ".mp3", ok: false},
		{name: "", ok: false},
		{name: "..", ok: false},
		{name: "a/b.mp3", ok: false},
		{name: "has space.mp3", ok: false},
	}
	for _, tc := range cases {
		token, kind, ok := parsePublicStreamFile(tc.name)
		if ok != tc.ok || token != tc.token || kind != tc.kind {
			t.Fatalf("parse %q = (%q, %q, %v), want (%q, %q, %v)", tc.name, token, kind, ok, tc.token, tc.kind, tc.ok)
		}
	}
}

func TestPublicHTTPStreamRejectsBadName(t *testing.T) {
	h := &handler{logger: slog.Default()}
	for _, name := range []string{"", ".mp3", "..", "a/b.mp3", "bad name.mp3"} {
		req := httptest.NewRequest(http.MethodGet, "/public/stream/ignored", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", name)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.handlePublicHTTPStream(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%q status = %d, want 404", name, rec.Code)
		}
		if strings.Contains(rec.Header().Get("Content-Type"), "audio/mpeg") {
			t.Fatalf("%q was served as audio", name)
		}
	}
}

func TestRadioSetPlaylistRequiresAuth(t *testing.T) {
	h := &handler{logger: slog.Default()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/radio-sets/playlist.m3u", nil)
	rec := httptest.NewRecorder()
	h.handleRadioSetPlaylist(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPublicPlaylists(t *testing.T) {
	name := sanitizePlaylistTitle("City\r\n#EXTINF:0,http://evil")
	if strings.ContainsAny(name, "\r\n") {
		t.Fatalf("title still has a newline: %q", name)
	}
	entries := []publicStreamEntry{
		{Name: name, URL: "https://p7hub.projectseven.us/public/stream/share_abc.mp3"},
		{Name: "EMS", URL: "https://p7hub.projectseven.us/public/stream/share_def.mp3"},
	}
	m3u := string(buildM3U(entries))
	lines := strings.Split(strings.TrimSuffix(m3u, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("m3u lines = %d\n%s", len(lines), m3u)
	}
	if lines[0] != "#EXTM3U" {
		t.Fatalf("m3u header = %q", lines[0])
	}
	if !strings.Contains(lines[1], name) || strings.Contains(lines[1], "\n") {
		t.Fatalf("extinf = %q", lines[1])
	}
	if lines[2] != entries[0].URL || lines[4] != entries[1].URL {
		t.Fatalf("m3u urls:\n%s", m3u)
	}

	pls := string(buildPLS(entries))
	if !strings.Contains(pls, "NumberOfEntries=2") || !strings.Contains(pls, "File1="+entries[0].URL) || !strings.Contains(pls, "Title2=EMS") {
		t.Fatalf("pls:\n%s", pls)
	}
	if strings.Contains(pls, "\r") {
		t.Fatal("pls contains CR")
	}
}

func TestRequestPublicBase(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/public/stream/share_abc.m3u", nil)
	req.Host = "internal:8080"
	req.Header.Set("X-Forwarded-Host", "p7hub.projectseven.us")
	req.Header.Set("X-Forwarded-Proto", "https,http")
	if got := requestPublicBase(req); got != "https://p7hub.projectseven.us" {
		t.Fatalf("base = %q", got)
	}

	req.Host = "bad host"
	req.Header.Del("X-Forwarded-Host")
	if got := requestPublicBase(req); got != "" {
		t.Fatalf("bad host base = %q", got)
	}

	plain := &handler{cfg: config.Config{HubPublicURL: "https://p7hub.projectseven.us/"}}
	if got := plain.publicStreamBase(req); got != "https://p7hub.projectseven.us" {
		t.Fatalf("fallback base = %q", got)
	}
	token := "share_abc"
	rs := database.RadioSet{Name: "City FD", ShareToken: &token}
	entry := publicStreamEntryFromSet(plain.publicStreamBase(req), rs)
	if entry.URL != "https://p7hub.projectseven.us/public/stream/share_abc.mp3" {
		t.Fatalf("entry url = %q", entry.URL)
	}

	stream := &handler{cfg: config.Config{StreamPublicURL: "https://stream.p7hub.projectseven.us"}}
	req.Host = "p7hub.projectseven.us"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "p7hub.projectseven.us")
	if got := stream.publicStreamBase(req); got != "https://stream.p7hub.projectseven.us" {
		t.Fatalf("stream base = %q", got)
	}
	entry = publicStreamEntryFromSet(stream.publicStreamBase(req), rs)
	if entry.URL != "https://stream.p7hub.projectseven.us/public/stream/share_abc.mp3" {
		t.Fatalf("playlist stayed on the hub host: %q", entry.URL)
	}
}

func TestPublicMP3Headers(t *testing.T) {
	h := http.Header{}
	applyPublicMP3Headers(h, "City\nFD", false)
	if h.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("content type = %q", h.Get("Content-Type"))
	}
	if h.Get("icy-metaint") != "" {
		t.Fatalf("metaint set without icy request: %q", h.Get("icy-metaint"))
	}
	if h.Get("Icy-Br") != "64" || h.Get("Icy-Sr") != "22050" {
		t.Fatalf("icy bitrate/rate = %q %q", h.Get("Icy-Br"), h.Get("Icy-Sr"))
	}
	if strings.Contains(h.Get("Icy-Name"), "\n") {
		t.Fatalf("station name not sanitized: %q", h.Get("Icy-Name"))
	}
	if h.Get("X-Accel-Buffering") != "no" {
		t.Fatal("expected X-Accel-Buffering: no")
	}

	icy := http.Header{}
	applyPublicMP3Headers(icy, "City", true)
	if icy.Get("Icy-Metaint") != "8192" {
		t.Fatalf("metaint = %q", icy.Get("Icy-Metaint"))
	}
}

func TestIcyWriterKeepsShortCallTitle(t *testing.T) {
	// A P25 burst is often shorter than the 8192-byte ICY slot. The next
	// silence piece used to replace the pending talkgroup title before it
	// was written, so the wire only showed StreamTitle='Scanning'.
	rec := httptest.NewRecorder()
	writer := newIcyWriter(rec, true)
	writer.SetTitle("Scanning")
	if err := writer.WriteAudio(bytes.Repeat([]byte{0x11}, icyMetaInterval+100)); err != nil {
		t.Fatal(err)
	}
	writer.SetTitle("OHP AM-Comm")
	if err := writer.WriteAudio(bytes.Repeat([]byte{0x22}, 6000)); err != nil {
		t.Fatal(err)
	}
	writer.SetTitle("Scanning")
	if err := writer.WriteAudio(bytes.Repeat([]byte{0x11}, icyMetaInterval)); err != nil {
		t.Fatal(err)
	}
	_, titles := decodeICY(t, rec.Body.Bytes())
	joined := strings.Join(titles, "\n")
	if !strings.Contains(joined, "StreamTitle='OHP AM-Comm';") {
		t.Fatalf("talkgroup title dropped, got %q", titles)
	}
}

func TestIcyWriterRoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := newIcyWriter(rec, true)
	writer.SetTitle("Fire Dispatch")
	audio := bytes.Repeat([]byte{0x11}, 20000)
	if err := writer.WriteAudio(audio); err != nil {
		t.Fatal(err)
	}
	decoded, titles := decodeICY(t, rec.Body.Bytes())
	if !bytes.Equal(decoded, audio) {
		t.Fatalf("decoded %d bytes, want %d", len(decoded), len(audio))
	}
	if len(titles) == 0 || !strings.Contains(titles[0], "StreamTitle='Fire Dispatch';") {
		t.Fatalf("titles = %#v", titles)
	}

	plain := httptest.NewRecorder()
	raw := newIcyWriter(plain, false)
	raw.SetTitle("ignored")
	if err := raw.WriteAudio(audio[:100]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.Body.Bytes(), audio[:100]) {
		t.Fatal("non-icy writer changed bytes")
	}
}

func decodeICY(t *testing.T, got []byte) ([]byte, []string) {
	t.Helper()
	var decoded []byte
	var titles []string
	sent := 0
	i := 0
	for i < len(got) {
		if sent == icyMetaInterval {
			if i >= len(got) {
				t.Fatal("truncated metadata length")
			}
			blocks := int(got[i])
			i++
			if blocks > 0 {
				n := blocks * 16
				if i+n > len(got) {
					t.Fatal("truncated metadata block")
				}
				titles = append(titles, string(bytes.TrimRight(got[i:i+n], "\x00")))
				i += n
			}
			sent = 0
			continue
		}
		n := icyMetaInterval - sent
		if n > len(got)-i {
			n = len(got) - i
		}
		decoded = append(decoded, got[i:i+n]...)
		i += n
		sent += n
	}
	return decoded, titles
}

func TestMP3FrameHeader(t *testing.T) {
	header := []byte{0xFF, 0xF3, 0x80, 0xC0}
	size := mp3FrameSize(header)
	if size != 208 {
		t.Fatalf("frame size = %d, want 208", size)
	}
	rate, bitrate, channels, ok := mp3FrameParams(header)
	if !ok || rate != 22050 || bitrate != 64 || channels != 1 {
		t.Fatalf("params = %d Hz %d kbps %d ch ok=%v", rate, bitrate, channels, ok)
	}
	if mp3FrameSamples(header) != 576 {
		t.Fatal("expected MPEG2 layer3 samples")
	}

	frames := bytes.Repeat(append(header, make([]byte, size-4)...), 40)
	first, _, isCall := popStreamAudio(&[]queuedClip{{audio: frames, title: "Fire"}}, new([]byte), new(string), new(bool))
	if !isCall || len(first) == 0 || len(first)%size != 0 || len(first) > streamBytesPerSecond {
		t.Fatalf("first slice = %d bytes", len(first))
	}
	dur := mp3Duration(first)
	if dur < 900*time.Millisecond || dur > 1100*time.Millisecond {
		t.Fatalf("duration = %s", dur)
	}

	id3 := append([]byte("ID3"), make([]byte, 7)...)
	id3[6], id3[7], id3[8], id3[9] = 0, 0, 0, 0
	trimmed := trimStreamMP3(append(id3, frames[:size]...))
	if !bytes.Equal(trimmed[:4], header) {
		t.Fatalf("trim did not reach the frame: %x", trimmed[:4])
	}
}

func TestCanonicalStreamMP3CachesByCallID(t *testing.T) {
	var calls atomic.Int32
	broker := newPublicMP3Broker(nil, slog.Default())
	broker.cache = newStreamAudioCache(2)
	broker.transcodeFn = func(context.Context, []byte) ([]byte, error) {
		calls.Add(1)
		return []byte("MP3"), nil
	}

	ctx := context.Background()
	first, err := broker.canonical(ctx, 99, []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := broker.canonical(ctx, 99, []byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !bytes.Equal(first, second) {
		t.Fatalf("calls = %d", calls.Load())
	}
	for _, id := range []int64{1, 2, 3} {
		if _, err := broker.canonical(ctx, id, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := broker.cache.get(99); ok {
		t.Fatal("expected call 99 to be evicted")
	}
	if _, ok := broker.cache.get(1); ok {
		t.Fatal("expected call 1 to be evicted")
	}
}

func TestStreamRoutesPreferStaticPaths(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/api/v1/radio-sets/playlist.m3u", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/api/v1/radio-sets/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	r.Get("/public/stream/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Name", chi.URLParam(r, "name"))
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/radio-sets/playlist.m3u", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("playlist status = %d, want the static route", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/public/stream/share_abc.mp3", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Header().Get("X-Name") != "share_abc.mp3" {
		t.Fatalf("name param = %q", rec.Header().Get("X-Name"))
	}
}

func TestPublicMP3FeedFansOutAndStops(t *testing.T) {
	hub := newStreamHub(slog.Default())
	broker := newPublicMP3Broker(hub, slog.Default())
	configureFastBroker(broker)
	rs := &database.RadioSet{ID: "rs_test", UserID: "owner-a", Name: "City", Talkgroups: []int{101}}
	first, leaveFirst := broker.join(rs, []string{"src"})
	second, leaveSecond := broker.join(rs, []string{"src"})
	defer leaveFirst()
	defer leaveSecond()
	waitStreamListeners(t, hub, 1)

	hub.push(&database.Call{ID: 7, UserID: "owner-a", SourceID: "src", Talkgroup: 999, TalkgroupLabel: "Nope"}, []byte("nope"))
	hub.push(&database.Call{ID: 42, UserID: "owner-a", SourceID: "src", Talkgroup: 101, TalkgroupLabel: "Fire"}, []byte("clip-a"))

	gotFirst := readUntilAudio(t, first.ch, []byte("clip-a"))
	gotSecond := readUntilAudio(t, second.ch, []byte("clip-a"))
	if gotFirst.title != "Fire" || gotSecond.title != "Fire" {
		t.Fatalf("titles = %q %q", gotFirst.title, gotSecond.title)
	}
	if gotFirst.silence || gotSecond.silence {
		t.Fatal("call piece marked silence")
	}
	select {
	case piece, ok := <-first.ch:
		if !ok || !piece.silence || piece.title != streamIdleTitle {
			t.Fatalf("expected scanning silence after the call, got ok=%v title=%q silence=%v", ok, piece.title, piece.silence)
		}
	case <-time.After(time.Second):
		t.Fatal("no silence after the call")
	}

	leaveFirst()
	leaveSecond()
	waitStreamListeners(t, hub, 0)
}

func TestPublicMP3FeedDropsSlowListener(t *testing.T) {
	hub := newStreamHub(slog.Default())
	broker := newPublicMP3Broker(hub, slog.Default())
	configureFastBroker(broker)
	rs := &database.RadioSet{ID: "rs_slow", UserID: "owner-a", Name: "City", Talkgroups: []int{101}}
	fast, leaveFast := broker.join(rs, []string{"src"})
	slow, leaveSlow := broker.join(rs, nil)
	defer leaveFast()
	defer leaveSlow()
	waitStreamListeners(t, hub, 1)

	fastCalls := make(chan struct{}, 32)
	go func() {
		for piece := range fast.ch {
			if bytes.Equal(piece.audio, []byte("call")) {
				fastCalls <- struct{}{}
			}
		}
	}()

	for i := 0; i < 24; i++ {
		hub.push(&database.Call{
			ID:             int64(1000 + i),
			UserID:         "owner-a",
			SourceID:       "src",
			Talkgroup:      101,
			TalkgroupLabel: "Fire",
		}, []byte("call"))
	}

	deadline := time.After(2 * time.Second)
	calls := 0
	for calls < 10 {
		select {
		case <-fastCalls:
			calls++
		case <-deadline:
			t.Fatalf("fast listener got %d calls", calls)
		}
	}

	// Stay unread so the client buffer stays full and drop counting can disconnect it.
	time.Sleep(100 * time.Millisecond)
	deadline = time.After(time.Second)
	for {
		select {
		case _, ok := <-slow.ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("slow listener was not disconnected")
		}
	}
}

func TestPlayerPageIncludesHTTPStreamLinks(t *testing.T) {
	cfg, err := json.Marshal(playerPageConfig{Token: "share_abc", Name: "City"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := playerTmpl.Execute(&buf, playerPageData{Name: "City", Config: template.JS(cfg)}); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, needle := range []string{"m3u-link", "pls-link", "/public/stream/", "httpStreamURL", "cfg.streamBase", "copyStream"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("player page missing %q", needle)
		}
	}
}

func TestStreamMP3EncoderProducesCanonicalFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	silence, err := encodeStreamSilence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	silence = trimStreamMP3(silence)
	assertCanonicalMP3(t, silence)

	wav := tinyWAV(streamMP3SampleRate, 0.3)
	encoded, err := transcodeStreamAudioFFmpeg(ctx, wav)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalMP3(t, encoded)
}

func assertCanonicalMP3(t *testing.T, audio []byte) {
	t.Helper()
	if bytes.HasPrefix(audio, []byte("ID3")) {
		t.Fatal("mp3 starts with ID3")
	}
	frames := 0
	for len(audio) >= 4 {
		size := mp3FrameSize(audio)
		if size < 4 {
			if frames > 0 && len(audio) < 4 {
				break
			}
			t.Fatalf("bad frame after %d frames, remain %d", frames, len(audio))
		}
		if size > len(audio) {
			if frames > 0 && len(audio) < size {
				break
			}
			t.Fatalf("short frame after %d frames", frames)
		}
		if frames == 0 && frameHasXing(audio) {
			t.Fatal("xing header present")
		}
		rate, bitrate, channels, ok := mp3FrameParams(audio)
		if !ok || rate != streamMP3SampleRate || bitrate != streamMP3BitrateKbps || channels != streamMP3Channels {
			t.Fatalf("frame %d = %d Hz %d kbps %d ch", frames, rate, bitrate, channels)
		}
		audio = audio[size:]
		frames++
	}
	if frames < 5 {
		t.Fatalf("frames = %d", frames)
	}
}

func tinyWAV(sampleRate int, seconds float64) []byte {
	n := int(float64(sampleRate) * seconds)
	if n < 1 {
		n = 1
	}
	dataSize := n * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	return buf
}

func configureFastBroker(b *publicMP3Broker) {
	b.transcodeFn = func(_ context.Context, audio []byte) ([]byte, error) {
		return append([]byte(nil), audio...), nil
	}
	b.silenceFn = func() ([]byte, error) { return []byte("SIL"), nil }
	b.paceFn = func([]byte) time.Duration { return 2 * time.Millisecond }
	b.cache = newStreamAudioCache(32)
}

func waitStreamListeners(t *testing.T, hub *streamHub, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		got := len(hub.listeners)
		hub.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	hub.mu.Lock()
	got := len(hub.listeners)
	hub.mu.Unlock()
	t.Fatalf("listeners = %d, want %d", got, want)
}

func readUntilAudio(t *testing.T, ch <-chan mp3Piece, want []byte) mp3Piece {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case piece, ok := <-ch:
			if !ok {
				t.Fatal("stream closed while waiting for audio")
			}
			if bytes.Equal(piece.audio, want) {
				return piece
			}
			if bytes.Equal(piece.audio, []byte("nope")) {
				t.Fatal("received audio for a talkgroup that is not on the share")
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}
