package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const (
	// Canonical stream parameters. Every clip and every silence gap is encoded
	// to this so players can concatenate the bytes as one MP3.
	streamMP3SampleRate  = 22050
	streamMP3BitrateKbps = 64
	streamMP3Channels    = 1

	// icyMetaInterval is the SHOUTcast metadata period in audio bytes.
	icyMetaInterval = 8192

	streamBytesPerSecond = streamMP3BitrateKbps * 1000 / 8
	streamClientQueue    = 8
	streamClientMaxDrop  = 8
	streamClipQueue      = 16
	streamIdleTitle      = "Scanning"

	publicStreamRecheck = 15 * time.Second
	streamWriteBudget   = 20 * time.Second
)

// streamPieceDuration is how long the pacer waits after sending a piece.
// It follows the MP3 duration so a listener stays at 64 kbps instead of
// receiving a whole call in one burst. A broker can override it in tests.
var streamPieceDuration = mp3Duration

// transcodeStreamAudio encodes one call clip to the canonical MP3.
var transcodeStreamAudio = transcodeStreamAudioFFmpeg

// loadStreamSilence returns a cached second of canonical MP3 silence.
var loadStreamSilence = loadStreamSilenceFFmpeg

var (
	streamSilenceMu    sync.Mutex
	streamSilenceBytes []byte
	streamMP3Cache     = newStreamAudioCache(32)
)

// parsePublicStreamFile splits "share_<id>.mp3" into the share token and the
// response kind (mp3, m3u, pls). A name with no extension is an MP3 stream.
func parsePublicStreamFile(name string) (token string, kind string, ok bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return "", "", false
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".mp3"):
		token, kind = name[:len(name)-4], "mp3"
	case strings.HasSuffix(lower, ".m3u"):
		token, kind = name[:len(name)-4], "m3u"
	case strings.HasSuffix(lower, ".pls"):
		token, kind = name[:len(name)-4], "pls"
	default:
		token, kind = name, "mp3"
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t?#") {
		return "", "", false
	}
	return token, kind, true
}

func (h *handler) handlePublicHTTPStream(w http.ResponseWriter, r *http.Request) {
	token, kind, ok := parsePublicStreamFile(chi.URLParam(r, "name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	rs, err := h.db.GetRadioSetByShareToken(token)
	if err != nil || rs == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch kind {
	case "m3u":
		h.writePublicPlaylist(w, r, []publicStreamEntry{publicStreamEntryFromSet(h.publicStreamBase(r), *rs)}, "m3u")
	case "pls":
		h.writePublicPlaylist(w, r, []publicStreamEntry{publicStreamEntryFromSet(h.publicStreamBase(r), *rs)}, "pls")
	default:
		h.servePublicMP3(w, r, rs, token)
	}
}

// handleRadioSetPlaylist serves every public share the caller can already see
// in the radio-set list: their own sets, or every set for an admin.
func (h *handler) handleRadioSetPlaylist(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	var sets []database.RadioSet
	var err error
	if isAdmin(user) {
		sets, err = h.db.ListAllRadioSets()
	} else {
		sets, err = h.db.ListRadioSets(user.ID)
	}
	if err != nil {
		h.logger.Error("list radio set playlist failed", "error", err)
		http.Error(w, "query radio sets", http.StatusInternalServerError)
		return
	}
	base := h.publicStreamBase(r)
	entries := make([]publicStreamEntry, 0, len(sets))
	for _, rs := range sets {
		if rs.ShareToken == nil || strings.TrimSpace(*rs.ShareToken) == "" {
			continue
		}
		entries = append(entries, publicStreamEntryFromSet(base, rs))
	}
	kind := "m3u"
	if strings.HasSuffix(strings.ToLower(r.URL.Path), ".pls") {
		kind = "pls"
	}
	h.writePublicPlaylist(w, r, entries, kind)
}

func (h *handler) servePublicMP3(w http.ResponseWriter, r *http.Request, rs *database.RadioSet, token string) {
	sourceIDs, err := h.db.ListReadableSourceIDsForUser(rs.UserID)
	if err != nil {
		h.logger.Error("list public mp3 readable sources failed", "error", err, "radio_set_id", rs.ID)
		http.Error(w, "query sources", http.StatusInternalServerError)
		return
	}
	icy := icyMetadataRequested(r)
	if r.Method == http.MethodHead {
		applyPublicMP3Headers(w.Header(), rs.Name, icy)
		w.WriteHeader(http.StatusOK)
		return
	}

	client, leave := h.mp3Broker.join(rs, sourceIDs)
	defer leave()

	h.logger.Info("public mp3 stream connected",
		"radio_set_id", rs.ID,
		"radio_set_name", rs.Name,
		"icy", icy,
	)
	defer h.logger.Info("public mp3 stream disconnected", "radio_set_id", rs.ID)

	applyPublicMP3Headers(w.Header(), rs.Name, icy)
	w.WriteHeader(http.StatusOK)
	writer := newIcyWriter(w, icy)
	writer.SetTitle(streamIdleTitle)
	if sil, silErr := loadStreamSilence(); silErr != nil {
		h.logger.Warn("public mp3 silence unavailable", "error", silErr, "radio_set_id", rs.ID)
	} else {
		// Two seconds of audio crosses the first ICY metadata boundary (8192 bytes
		// at 64 kbps) and gives players a buffer before the next transmission.
		for range 2 {
			if err := writer.WriteAudio(sil); err != nil {
				return
			}
		}
	}

	recheck := time.NewTicker(publicStreamRecheck)
	defer recheck.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-recheck.C:
			current, lookupErr := h.db.GetRadioSetByShareToken(token)
			if lookupErr != nil {
				h.logger.Warn("public mp3 share recheck failed", "error", lookupErr, "radio_set_id", rs.ID)
				continue
			}
			if current == nil || current.ID != rs.ID {
				return
			}
		case piece, open := <-client.ch:
			if !open {
				return
			}
			if piece.title != "" {
				writer.SetTitle(piece.title)
			}
			if err := writer.WriteAudio(piece.audio); err != nil {
				return
			}
		}
	}
}

func icyMetadataRequested(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("Icy-MetaData")) == "1"
}

func applyPublicMP3Headers(h http.Header, station string, icy bool) {
	station = sanitizeHeaderValue(station)
	h.Set("Content-Type", "audio/mpeg")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	h.Set("Connection", "close")
	h.Set("Accept-Ranges", "none")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("icy-name", station)
	h.Set("icy-description", "SignalForge Hub")
	h.Set("icy-genre", "Scanner")
	h.Set("icy-br", fmt.Sprintf("%d", streamMP3BitrateKbps))
	h.Set("icy-sr", fmt.Sprintf("%d", streamMP3SampleRate))
	h.Set("icy-pub", "0")
	h.Set("ice-audio-info", fmt.Sprintf("ice-samplerate=%d;ice-bitrate=%d;ice-channels=%d",
		streamMP3SampleRate, streamMP3BitrateKbps, streamMP3Channels))
	if icy {
		h.Set("icy-metaint", fmt.Sprintf("%d", icyMetaInterval))
	}
}

type icyWriter struct {
	w       http.ResponseWriter
	icy     bool
	sent    int
	title   string
	pending string
	queued  string
}

func newIcyWriter(w http.ResponseWriter, icy bool) *icyWriter {
	return &icyWriter{w: w, icy: icy}
}

func (iw *icyWriter) SetTitle(title string) {
	title = sanitizeStreamTitle(title)
	if title == "" || !iw.icy {
		return
	}
	// A short P25 burst often ends before the next 8192-byte metadata slot.
	// Replacing the unsent talkgroup title with "Scanning" dropped it on the
	// floor, so players kept showing Scanning and the call looked like silence.
	if iw.pending == "" {
		if title == iw.title {
			return
		}
		iw.pending = title
		iw.title = title
		return
	}
	if title == iw.pending && iw.queued == "" {
		return
	}
	iw.queued = title
	iw.title = title
}

func (iw *icyWriter) WriteAudio(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	bumpWriteDeadline(iw.w)
	if !iw.icy {
		if _, err := iw.w.Write(p); err != nil {
			return err
		}
		flushStream(iw.w)
		return nil
	}
	for len(p) > 0 {
		space := icyMetaInterval - iw.sent
		if space == 0 {
			if err := iw.writeMeta(); err != nil {
				return err
			}
			iw.sent = 0
			space = icyMetaInterval
		}
		n := len(p)
		if n > space {
			n = space
		}
		if _, err := iw.w.Write(p[:n]); err != nil {
			return err
		}
		iw.sent += n
		p = p[n:]
	}
	flushStream(iw.w)
	return nil
}

func (iw *icyWriter) writeMeta() error {
	if iw.pending == "" {
		_, err := iw.w.Write([]byte{0})
		return err
	}
	block := icyMetadataBlock(iw.pending)
	iw.pending = iw.queued
	iw.queued = ""
	_, err := iw.w.Write(block)
	return err
}

func icyMetadataBlock(title string) []byte {
	payload := "StreamTitle='" + title + "';"
	if rem := len(payload) % 16; rem != 0 {
		payload += strings.Repeat("\x00", 16-rem)
	}
	if len(payload) > 16*255 {
		payload = payload[:16*255]
	}
	out := make([]byte, 1+len(payload))
	out[0] = byte(len(payload) / 16)
	copy(out[1:], payload)
	return out
}

func bumpWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteBudget))
}

func flushStream(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

type publicStreamEntry struct {
	Name string
	URL  string
}

func publicStreamEntryFromSet(base string, rs database.RadioSet) publicStreamEntry {
	token := ""
	if rs.ShareToken != nil {
		token = *rs.ShareToken
	}
	return publicStreamEntry{
		Name: sanitizePlaylistTitle(rs.Name),
		URL:  publicStreamFileURL(base, token, ".mp3"),
	}
}

func publicStreamFileURL(base, token, ext string) string {
	path := "/public/stream/" + url.PathEscape(token) + ext
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return path
	}
	return base + path
}

func (h *handler) publicStreamBase(r *http.Request) string {
	if base := requestPublicBase(r); base != "" {
		return base
	}
	for _, candidate := range []string{h.cfg.HubPublicURL, h.cfg.PublicBaseURL} {
		candidate = strings.TrimRight(strings.TrimSpace(candidate), "/")
		if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			return candidate
		}
	}
	return ""
}

func requestPublicBase(r *http.Request) string {
	proto := headerFirstToken(r.Header.Get("X-Forwarded-Proto"))
	if proto != "https" && proto != "http" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := headerFirstToken(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = headerFirstToken(r.Host)
	}
	if !validHTTPHost(host) {
		return ""
	}
	return proto + "://" + host
}

func headerFirstToken(value string) string {
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func validHTTPHost(host string) bool {
	if host == "" || len(host) > 255 {
		return false
	}
	return !strings.ContainsAny(host, " \t\r\n/\\")
}

func (h *handler) writePublicPlaylist(w http.ResponseWriter, _ *http.Request, entries []publicStreamEntry, kind string) {
	var body []byte
	var contentType, filename string
	switch kind {
	case "pls":
		body = buildPLS(entries)
		contentType = "audio/x-scpls; charset=utf-8"
		filename = "signalforge.pls"
	default:
		body = buildM3U(entries)
		contentType = "audio/x-mpegurl; charset=utf-8"
		filename = "signalforge.m3u"
	}
	if len(entries) == 1 && entries[0].Name != "" {
		filename = playlistFilename(entries[0].Name, kind)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func playlistFilename(name, kind string) string {
	ext := ".m3u"
	if kind == "pls" {
		ext = ".pls"
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\?%*:|"<>`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Join(strings.Fields(name), "-")
	if name == "" {
		name = "signalforge"
	}
	return trimRunes(name, 80) + ext
}

func buildM3U(entries []publicStreamEntry) []byte {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, entry := range entries {
		fmt.Fprintf(&b, "#EXTINF:-1,%s\n%s\n", entry.Name, entry.URL)
	}
	return []byte(b.String())
}

func buildPLS(entries []publicStreamEntry) []byte {
	var b strings.Builder
	b.WriteString("[playlist]\n")
	fmt.Fprintf(&b, "NumberOfEntries=%d\n", len(entries))
	for i, entry := range entries {
		n := i + 1
		fmt.Fprintf(&b, "File%d=%s\nTitle%d=%s\nLength%d=-1\n", n, entry.URL, n, entry.Name, n)
	}
	b.WriteString("Version=2\n")
	return []byte(b.String())
}

func sanitizePlaylistTitle(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "SignalForge"
	}
	return trimRunes(name, 120)
}

func sanitizeStreamTitle(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '\'' || r == '\\' || r == ';' {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return streamIdleTitle
	}
	return trimRunes(name, 120)
}

func sanitizeHeaderValue(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "SignalForge Hub"
	}
	return trimRunes(name, 128)
}

func trimRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for count := 0; count < n && i < len(s); count++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

func httpStreamTitle(meta streamCallMeta) string {
	label := strings.TrimSpace(meta.TalkgroupLabel)
	if label == "" {
		label = fmt.Sprintf("TG %d", meta.Talkgroup)
	}
	if group := strings.TrimSpace(meta.TalkgroupGroup); group != "" {
		label += " — " + group
	}
	return sanitizeStreamTitle(label)
}

// publicMP3Broker runs one paced MP3 feed per radio set and fans the same
// bytes out to every HTTP listener. Call clips are transcoded once.
type publicMP3Broker struct {
	mu          sync.Mutex
	feeds       map[string]*publicMP3Feed
	hub         *streamHub
	logger      *slog.Logger
	cache       *streamAudioCache
	transcodeFn func(context.Context, []byte) ([]byte, error)
	silenceFn   func() ([]byte, error)
	paceFn      func([]byte) time.Duration
}

type publicMP3Feed struct {
	broker      *publicMP3Broker
	radioSetID  string
	ownerUserID string
	talkgroups  []int
	groups      []string
	sourceIDs   []string
	listeners   map[*mp3Client]struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
}

type mp3Client struct {
	ch      chan mp3Piece
	dropped int
	closed  bool
}

type mp3Piece struct {
	audio   []byte
	title   string
	silence bool
}

type queuedClip struct {
	audio []byte
	title string
}

func newPublicMP3Broker(hub *streamHub, logger *slog.Logger) *publicMP3Broker {
	return &publicMP3Broker{
		feeds:  map[string]*publicMP3Feed{},
		hub:    hub,
		logger: logger,
		cache:  streamMP3Cache,
	}
}

func (b *publicMP3Broker) logInfo(msg string, args ...any) {
	if b != nil && b.logger != nil {
		b.logger.Info(msg, args...)
	}
}

func (b *publicMP3Broker) logWarn(msg string, args ...any) {
	if b != nil && b.logger != nil {
		b.logger.Warn(msg, args...)
	}
}

func (b *publicMP3Broker) join(rs *database.RadioSet, sourceIDs []string) (*mp3Client, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	feed := b.feeds[rs.ID]
	if feed == nil {
		ctx, cancel := context.WithCancel(context.Background())
		talkgroups, groups := publicStreamSubscription(rs)
		feed = &publicMP3Feed{
			broker:      b,
			radioSetID:  rs.ID,
			ownerUserID: rs.UserID,
			talkgroups:  append([]int(nil), talkgroups...),
			groups:      append([]string(nil), groups...),
			sourceIDs:   append([]string(nil), sourceIDs...),
			listeners:   map[*mp3Client]struct{}{},
			ctx:         ctx,
			cancel:      cancel,
		}
		b.feeds[rs.ID] = feed
		go feed.run()
	}
	client := &mp3Client{ch: make(chan mp3Piece, streamClientQueue)}
	feed.mu.Lock()
	feed.listeners[client] = struct{}{}
	feed.mu.Unlock()
	return client, func() { b.leave(rs.ID, client) }
}

func (b *publicMP3Broker) leave(id string, client *mp3Client) {
	b.mu.Lock()
	defer b.mu.Unlock()
	feed := b.feeds[id]
	if feed == nil {
		return
	}
	feed.mu.Lock()
	if _, ok := feed.listeners[client]; ok {
		delete(feed.listeners, client)
		if !client.closed {
			client.closed = true
			close(client.ch)
		}
	}
	empty := len(feed.listeners) == 0
	feed.mu.Unlock()
	if empty {
		delete(b.feeds, id)
		feed.cancel()
	}
}

func (f *publicMP3Feed) run() {
	subCh, unsub := f.broker.hub.subscribe(f.ownerUserID, f.radioSetID, f.talkgroups, f.groups, f.sourceIDs)
	defer unsub()
	defer f.closeListeners()

	encoded := make(chan queuedClip, streamClipQueue)
	go f.encodeLoop(subCh, encoded)

	var queue []queuedClip
	var rest []byte
	var restTitle string
	var restFresh bool
	scanning := true
	silenceLogged := false

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-f.ctx.Done():
			return
		case clip, open := <-encoded:
			if !open {
				return
			}
			if len(queue) >= streamClipQueue {
				queue = queue[1:]
				if f.broker.logger != nil {
					f.broker.logger.Debug("public mp3 clip queue full, dropping oldest", "radio_set_id", f.radioSetID)
				}
			}
			queue = append(queue, clip)
		case <-timer.C:
			audio, title, isCall := popStreamAudio(&queue, &rest, &restTitle, &restFresh)
			if !isCall {
				sil, err := f.broker.loadSilence()
				if err != nil {
					if !silenceLogged {
						silenceLogged = true
						f.broker.logWarn("public mp3 silence unavailable", "error", err, "radio_set_id", f.radioSetID)
					}
					timer.Reset(time.Second)
					continue
				}
				audio = sil
				if !scanning {
					title = streamIdleTitle
					scanning = true
				} else {
					title = ""
				}
			} else {
				scanning = false
			}
			if len(audio) > 0 {
				if isCall && title != "" {
					f.broker.logInfo("public mp3 call playing",
						"radio_set_id", f.radioSetID,
						"title", title,
						"mp3_bytes", len(audio),
					)
				}
				f.broadcast(mp3Piece{audio: audio, title: title, silence: !isCall})
			}
			dur := f.broker.pieceDuration(audio)
			if dur <= 0 {
				dur = time.Second
			}
			timer.Reset(dur)
		}
	}
}

func (f *publicMP3Feed) encodeLoop(subCh <-chan streamChunk, out chan<- queuedClip) {
	defer close(out)
	for {
		select {
		case <-f.ctx.Done():
			return
		case chunk, open := <-subCh:
			if !open {
				return
			}
			if len(chunk.audio) == 0 {
				continue
			}
			audio, err := f.broker.canonical(f.ctx, chunk.meta.ID, chunk.audio)
			if err != nil {
				f.broker.logWarn("public mp3 transcode failed",
					"error", err,
					"radio_set_id", f.radioSetID,
					"call_id", chunk.meta.ID,
					"talkgroup", chunk.meta.Talkgroup,
					"talkgroup_label", chunk.meta.TalkgroupLabel,
					"audio_type", chunk.meta.AudioType,
					"audio_bytes", len(chunk.audio),
				)
				continue
			}
			f.broker.logInfo("public mp3 call queued",
				"radio_set_id", f.radioSetID,
				"call_id", chunk.meta.ID,
				"talkgroup", chunk.meta.Talkgroup,
				"talkgroup_label", chunk.meta.TalkgroupLabel,
				"audio_type", chunk.meta.AudioType,
				"in_bytes", len(chunk.audio),
				"mp3_bytes", len(audio),
			)
			clip := queuedClip{audio: audio, title: httpStreamTitle(chunk.meta)}
			select {
			case <-f.ctx.Done():
				return
			case out <- clip:
			}
		}
	}
}

func (f *publicMP3Feed) broadcast(piece mp3Piece) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for client := range f.listeners {
		if client.closed {
			continue
		}
		select {
		case client.ch <- piece:
			client.dropped = 0
		default:
			client.dropped++
			if client.dropped >= streamClientMaxDrop {
				client.closed = true
				close(client.ch)
				delete(f.listeners, client)
			}
		}
	}
}

func (f *publicMP3Feed) closeListeners() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for client := range f.listeners {
		if !client.closed {
			client.closed = true
			close(client.ch)
		}
	}
	f.listeners = map[*mp3Client]struct{}{}
}

// popStreamAudio returns the next ~1s of queued call audio. isCall is false
// when the queue is empty so the caller can substitute silence.
func popStreamAudio(queue *[]queuedClip, rest *[]byte, restTitle *string, restFresh *bool) (audio []byte, title string, isCall bool) {
	if len(*rest) == 0 && len(*queue) > 0 {
		clip := (*queue)[0]
		*queue = (*queue)[1:]
		*rest = clip.audio
		*restTitle = clip.title
		*restFresh = true
	}
	if len(*rest) == 0 {
		return nil, "", false
	}
	n := takeMP3Prefix(*rest, streamBytesPerSecond)
	audio = (*rest)[:n]
	*rest = (*rest)[n:]
	if *restFresh {
		title = *restTitle
		*restFresh = false
	}
	return audio, title, true
}

func takeMP3Prefix(b []byte, max int) int {
	if len(b) <= max || max <= 0 {
		return len(b)
	}
	n := 0
	for n+4 <= len(b) {
		size := mp3FrameSize(b[n:])
		if size < 4 || n+size > len(b) {
			break
		}
		if n > 0 && n+size > max {
			break
		}
		n += size
		if n >= max {
			break
		}
	}
	if n == 0 {
		return len(b)
	}
	return n
}

func (b *publicMP3Broker) loadSilence() ([]byte, error) {
	if b != nil && b.silenceFn != nil {
		return b.silenceFn()
	}
	return loadStreamSilence()
}

func (b *publicMP3Broker) pieceDuration(audio []byte) time.Duration {
	if b != nil && b.paceFn != nil {
		return b.paceFn(audio)
	}
	return streamPieceDuration(audio)
}

func (b *publicMP3Broker) canonical(ctx context.Context, callID int64, audio []byte) ([]byte, error) {
	cache := streamMP3Cache
	if b != nil && b.cache != nil {
		cache = b.cache
	}
	fn := transcodeStreamAudio
	if b != nil && b.transcodeFn != nil {
		fn = b.transcodeFn
	}
	if callID > 0 && cache != nil {
		if cached, ok := cache.get(callID); ok {
			return cached, nil
		}
	}
	out, err := fn(ctx, audio)
	if err != nil {
		return nil, err
	}
	if callID > 0 && cache != nil {
		cache.put(callID, out)
	}
	return out, nil
}

type streamAudioCache struct {
	mu    sync.Mutex
	max   int
	order []int64
	items map[int64][]byte
}

func newStreamAudioCache(max int) *streamAudioCache {
	return &streamAudioCache{max: max, items: map[int64][]byte{}}
}

func (c *streamAudioCache) get(id int64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	audio, ok := c.items[id]
	return audio, ok
}

func (c *streamAudioCache) put(id int64, audio []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[id]; ok {
		c.items[id] = audio
		return
	}
	c.items[id] = audio
	c.order = append(c.order, id)
	for len(c.order) > c.max {
		drop := c.order[0]
		c.order = c.order[1:]
		delete(c.items, drop)
	}
}

func loadStreamSilenceFFmpeg() ([]byte, error) {
	streamSilenceMu.Lock()
	defer streamSilenceMu.Unlock()
	if len(streamSilenceBytes) > 0 {
		return streamSilenceBytes, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := encodeStreamSilence(ctx)
	if err != nil {
		return nil, err
	}
	out = trimStreamMP3(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("silence mp3 empty")
	}
	streamSilenceBytes = out
	return streamSilenceBytes, nil
}

func encodeStreamSilence(ctx context.Context) ([]byte, error) {
	pcm := make([]byte, streamMP3SampleRate*2)
	return execFFmpegMP3(ctx, pcm, []string{
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", streamMP3SampleRate),
		"-ac", "1",
	})
}

func transcodeStreamAudioFFmpeg(ctx context.Context, audio []byte) ([]byte, error) {
	if len(audio) == 0 {
		return nil, fmt.Errorf("empty audio")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := execFFmpegMP3(ctx, audio, nil)
	if err == nil {
		if trimmed := trimStreamMP3(out); len(trimmed) > 0 {
			return trimmed, nil
		}
	}
	// m4a/mp4 often needs a seekable input because the moov atom is not at the start.
	out, err = execFFmpegMP3File(ctx, audio)
	if err != nil {
		return nil, err
	}
	trimmed := trimStreamMP3(out)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("ffmpeg produced no mp3")
	}
	return trimmed, nil
}

func streamMP3EncodeArgs(input []string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, input...)
	args = append(args,
		"-i", "pipe:0",
		"-vn",
		"-map_metadata", "-1",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", streamMP3SampleRate),
		"-c:a", "libmp3lame",
		"-b:a", fmt.Sprintf("%dk", streamMP3BitrateKbps),
		"-write_xing", "0",
		"-reservoir", "0",
		"-id3v2_version", "0",
		"-f", "mp3",
		"pipe:1",
	)
	return args
}

func execFFmpegMP3(ctx context.Context, audio []byte, input []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", streamMP3EncodeArgs(input)...)
	cmd.Stdin = bytes.NewReader(audio)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return out, nil
}

func execFFmpegMP3File(ctx context.Context, audio []byte) ([]byte, error) {
	file, err := os.CreateTemp("", "sf-stream-*")
	if err != nil {
		return nil, err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(audio); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-i", name,
		"-vn",
		"-map_metadata", "-1",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", streamMP3SampleRate),
		"-c:a", "libmp3lame",
		"-b:a", fmt.Sprintf("%dk", streamMP3BitrateKbps),
		"-write_xing", "0",
		"-reservoir", "0",
		"-id3v2_version", "0",
		"-f", "mp3",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return out, nil
}

func trimStreamMP3(b []byte) []byte {
	b = stripID3v2(b)
	if i := indexMP3Frame(b); i > 0 {
		b = b[i:]
	}
	if len(b) >= 4 && frameHasXing(b) {
		if size := mp3FrameSize(b); size > 0 && size <= len(b) {
			b = b[size:]
		}
	}
	return b
}

func stripID3v2(b []byte) []byte {
	if len(b) < 10 || !bytes.Equal(b[:3], []byte("ID3")) {
		return b
	}
	size := int(b[6]&0x7f)<<21 | int(b[7]&0x7f)<<14 | int(b[8]&0x7f)<<7 | int(b[9]&0x7f)
	total := 10 + size
	if b[5]&0x10 != 0 {
		total += 10
	}
	if total < 0 || total > len(b) {
		return b
	}
	return b[total:]
}

func indexMP3Frame(b []byte) int {
	limit := len(b) - 4
	if limit > 2048 {
		limit = 2048
	}
	for i := 0; i <= limit; i++ {
		if mp3FrameSize(b[i:]) > 0 {
			return i
		}
	}
	return -1
}

func frameHasXing(b []byte) bool {
	size := mp3FrameSize(b)
	if size < 8 || size > len(b) {
		return false
	}
	return bytes.Contains(b[:size], []byte("Xing")) || bytes.Contains(b[:size], []byte("Info"))
}

func mp3Duration(b []byte) time.Duration {
	samples := 0
	i := 0
	for i+4 <= len(b) {
		size := mp3FrameSize(b[i:])
		if size < 4 || i+size > len(b) {
			break
		}
		samples += mp3FrameSamples(b[i:])
		i += size
	}
	if samples == 0 {
		return 0
	}
	return time.Duration(samples) * time.Second / streamMP3SampleRate
}

func mp3FrameSamples(header []byte) int {
	versionID := (header[1] >> 3) & 0x3
	if versionID == 3 {
		return 1152
	}
	return 576
}

// mp3FrameSize returns the size of the frame starting at header, or 0 when
// the bytes are not a Layer III frame header.
func mp3FrameSize(header []byte) int {
	if len(header) < 4 || header[0] != 0xFF || header[1]&0xE0 != 0xE0 {
		return 0
	}
	versionID := (header[1] >> 3) & 0x3
	layer := (header[1] >> 1) & 0x3
	bitrateIdx := (header[2] >> 4) & 0xF
	sampleIdx := (header[2] >> 2) & 0x3
	padding := int((header[2] >> 1) & 0x1)
	if layer != 1 || bitrateIdx == 0 || bitrateIdx == 15 || sampleIdx == 3 || versionID == 1 {
		return 0
	}
	bitrate := streamMPEG2BitrateKbps[bitrateIdx]
	sampleRates := streamMPEG2SampleRates
	scale := 72
	if versionID == 3 {
		bitrate = streamMPEG1BitrateKbps[bitrateIdx]
		sampleRates = streamMPEG1SampleRates
		scale = 144
	} else if versionID == 0 {
		sampleRates = streamMPEG25SampleRates
	}
	if bitrate == 0 {
		return 0
	}
	return scale*bitrate*1000/sampleRates[sampleIdx] + padding
}

func mp3FrameParams(header []byte) (sampleRate, bitrateKbps, channels int, ok bool) {
	if mp3FrameSize(header) == 0 {
		return 0, 0, 0, false
	}
	versionID := (header[1] >> 3) & 0x3
	bitrateIdx := (header[2] >> 4) & 0xF
	sampleIdx := (header[2] >> 2) & 0x3
	switch versionID {
	case 3:
		sampleRate = streamMPEG1SampleRates[sampleIdx]
		bitrateKbps = streamMPEG1BitrateKbps[bitrateIdx]
	case 2:
		sampleRate = streamMPEG2SampleRates[sampleIdx]
		bitrateKbps = streamMPEG2BitrateKbps[bitrateIdx]
	default:
		sampleRate = streamMPEG25SampleRates[sampleIdx]
		bitrateKbps = streamMPEG2BitrateKbps[bitrateIdx]
	}
	if (header[3]>>6)&0x3 == 3 {
		channels = 1
	} else {
		channels = 2
	}
	return sampleRate, bitrateKbps, channels, true
}

var (
	streamMPEG1BitrateKbps  = [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	streamMPEG2BitrateKbps  = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	streamMPEG1SampleRates  = [...]int{44100, 48000, 32000}
	streamMPEG2SampleRates  = [...]int{22050, 24000, 16000}
	streamMPEG25SampleRates = [...]int{11025, 12000, 8000}
)
