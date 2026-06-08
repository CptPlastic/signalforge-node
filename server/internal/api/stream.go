package api

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

// streamCallMeta is the JSON payload pushed to SSE and embedded in the player.
type streamCallMeta struct {
	ID             int64   `json:"id"`
	Talkgroup      int     `json:"talkgroup"`
	TalkgroupLabel string  `json:"talkgroupLabel"`
	TalkgroupGroup string  `json:"talkgroupGroup"`
	DateTime       int64   `json:"dateTime"`
	Duration       float64 `json:"duration"`
	Frequency      int     `json:"frequency"`
	SystemLabel    string  `json:"systemLabel"`
	AudioType      string  `json:"audioType"`
	TranscriptText string  `json:"transcriptText,omitempty"`
	Origin         string  `json:"origin,omitempty"`
	SenderUserID   string  `json:"senderUserId,omitempty"`
	SenderEmail    string  `json:"senderEmail,omitempty"`
}

type streamChunk struct {
	audio []byte
	meta  streamCallMeta
}

type streamListener struct {
	ownerUserID     string
	talkgroups      map[int]struct{}
	talkgroupGroups map[string]struct{}
	sourceIDs       map[string]struct{}
	ch              chan streamChunk
}

// streamHub fans new calls out to all active HTTP stream and SSE listeners.
type streamHub struct {
	mu        sync.Mutex
	listeners []*streamListener
	logger    *slog.Logger
}

func newStreamHub(logger *slog.Logger) *streamHub {
	return &streamHub{logger: logger}
}

// push sends a new call to all matching listeners.
// It is called from the call upload handler with the call and raw audio bytes.
func (sh *streamHub) push(call *database.Call, audio []byte) {
	meta := streamCallMeta{
		ID:             call.ID,
		Talkgroup:      call.Talkgroup,
		TalkgroupLabel: call.TalkgroupLabel,
		TalkgroupGroup: call.TalkgroupGroup,
		DateTime:       call.DateTime,
		Duration:       call.Duration,
		Frequency:      call.Frequency,
		SystemLabel:    call.SystemLabel,
		AudioType:      call.AudioType,
		TranscriptText: call.TranscriptText,
		Origin:         call.Origin,
		SenderUserID:   call.SenderUserID,
		SenderEmail:    call.SenderEmail,
	}
	chunk := streamChunk{audio: audio, meta: meta}

	sh.mu.Lock()
	defer sh.mu.Unlock()
	delivered := 0
	for _, l := range sh.listeners {
		if !l.matchesTalkgroup(call) {
			continue
		}
		if !l.matchesCall(call) {
			continue
		}
		select {
		case l.ch <- chunk:
			delivered++
		default:
			sh.logger.Debug("stream listener channel full, dropping chunk",
				"talkgroup", call.Talkgroup,
			)
		}
	}
	sh.logger.Debug("public stream fanout evaluated",
		"call_id", call.ID,
		"source_id", call.SourceID,
		"call_user_id", call.UserID,
		"talkgroup", call.Talkgroup,
		"listeners", len(sh.listeners),
		"delivered", delivered,
	)
}

func (l *streamListener) matchesTalkgroup(call *database.Call) bool {
	if _, ok := l.talkgroups[call.Talkgroup]; ok {
		return true
	}
	if len(l.talkgroupGroups) == 0 {
		return false
	}
	group := strings.TrimSpace(call.TalkgroupGroup)
	if group == "" {
		return false
	}
	_, ok := l.talkgroupGroups[group]
	return ok
}

func (l *streamListener) matchesCall(call *database.Call) bool {
	if l.ownerUserID != "" && l.ownerUserID == call.UserID {
		return true
	}
	if call.SourceID != "" {
		_, ok := l.sourceIDs[call.SourceID]
		return ok
	}
	return false
}

// subscribe registers a listener. Returns a receive-only channel and an
// unsubscribe function that must be called (typically via defer).
func (sh *streamHub) subscribe(ownerUserID string, talkgroups []int, talkgroupGroups []string, sourceIDs []string) (<-chan streamChunk, func()) {
	tgSet := make(map[int]struct{}, len(talkgroups))
	for _, tg := range talkgroups {
		tgSet[tg] = struct{}{}
	}
	groupSet := make(map[string]struct{}, len(talkgroupGroups))
	for _, group := range talkgroupGroups {
		group = strings.TrimSpace(group)
		if group != "" {
			groupSet[group] = struct{}{}
		}
	}
	sourceIDSet := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID != "" {
			sourceIDSet[sourceID] = struct{}{}
		}
	}
	ch := make(chan streamChunk, 32)
	l := &streamListener{
		ownerUserID:     ownerUserID,
		talkgroups:      tgSet,
		talkgroupGroups: groupSet,
		sourceIDs:       sourceIDSet,
		ch:              ch,
	}

	sh.mu.Lock()
	sh.listeners = append(sh.listeners, l)
	sh.mu.Unlock()

	return ch, func() {
		sh.mu.Lock()
		defer sh.mu.Unlock()
		for i, existing := range sh.listeners {
			if existing == l {
				sh.listeners = append(sh.listeners[:i], sh.listeners[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

// playerWSCallMsg is sent over the public player WebSocket for each call.
// The Audio field is JSON-marshaled as a base64 string, so client-side
// atob() + Blob + createObjectURL gives an immediately playable URL.
type playerWSCallMsg struct {
	Cmd            string  `json:"cmd"`
	ID             int64   `json:"id"`
	Talkgroup      int     `json:"talkgroup"`
	TalkgroupLabel string  `json:"talkgroupLabel"`
	TalkgroupGroup string  `json:"talkgroupGroup"`
	DateTime       int64   `json:"dateTime"`
	Duration       float64 `json:"duration"`
	Frequency      int     `json:"frequency"`
	SystemLabel    string  `json:"systemLabel"`
	AudioType      string  `json:"audioType"`
	TranscriptText string  `json:"transcriptText,omitempty"`
	Origin         string  `json:"origin,omitempty"`
	SenderUserID   string  `json:"senderUserId,omitempty"`
	SenderEmail    string  `json:"senderEmail,omitempty"`
	Audio          []byte  `json:"audio"` // base64-encoded in JSON output
}

func (h *handler) seedPublicStreamCalls(rs *database.RadioSet, _ []int) ([]database.Call, error) {
	return h.db.GetRecentCallsForRadioSet(rs.UserID, *rs, 5)
}

// handlePublicWS serves the public player via a single WebSocket connection.
// Each message carries both call metadata and audio bytes (base64-encoded in JSON),
// making audio and display atomically paired — the drift and race conditions that
// plagued the old separate audio-stream + SSE design are impossible here.
func (h *handler) handlePublicWS(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rs, err := h.db.GetRadioSetByShareToken(token)
	if err != nil || rs == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	sourceIDs, err := h.db.ListReadableSourceIDsForUser(rs.UserID)
	if err != nil {
		h.logger.Error("list public stream readable sources failed", "error", err)
		http.Error(w, "query sources", http.StatusInternalServerError)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error("public ws accept failed", "error", err)
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	h.logger.Info("public stream connected",
		"radio_set_id", rs.ID,
		"radio_set_name", rs.Name,
		"owner_user_id", rs.UserID,
		"selection_mode", rs.SelectionMode,
		"talkgroups", len(rs.Talkgroups),
		"talkgroup_groups", len(rs.TalkgroupGroups),
		"readable_sources", len(sourceIDs),
	)

	// Public-share subscribers also see PTT calls on the set's virtual PTT talkgroup.
	subscribedTalkgroups := make([]int, 0, len(rs.Talkgroups)+1)
	subscribedGroups := make([]string, 0, len(rs.TalkgroupGroups))
	if rs.IsGroupsMode() {
		subscribedGroups = append(subscribedGroups, rs.TalkgroupGroups...)
	} else {
		subscribedTalkgroups = append(subscribedTalkgroups, rs.Talkgroups...)
	}
	if rs.PTTTalkgroup != nil {
		subscribedTalkgroups = append(subscribedTalkgroups, *rs.PTTTalkgroup)
	}

	// Subscribe before seeding so no live calls are missed during the seed phase.
	ch, unsubscribe := h.streamHub.subscribe(rs.UserID, subscribedTalkgroups, subscribedGroups, sourceIDs)
	defer unsubscribe()

	wantMP3 := strings.EqualFold(r.URL.Query().Get("format"), "mp3")
	if wantMP3 {
		h.logger.Info("public stream mp3 format enabled", "radio_set_id", rs.ID)
	}

	sendCall := func(meta streamCallMeta, audio []byte) error {
		audioType := meta.AudioType
		if wantMP3 {
			var err error
			audio, audioType, err = preparePublicStreamAudio(ctx, audio, audioType, true)
			if err != nil {
				h.logger.Warn("public stream mp3 transcode failed",
					"radio_set_id", rs.ID,
					"call_id", meta.ID,
					"audio_type", meta.AudioType,
					"error", err,
				)
			}
		}
		msg := playerWSCallMsg{
			Cmd:            "call",
			ID:             meta.ID,
			Talkgroup:      meta.Talkgroup,
			TalkgroupLabel: meta.TalkgroupLabel,
			TalkgroupGroup: meta.TalkgroupGroup,
			DateTime:       meta.DateTime,
			Duration:       meta.Duration,
			Frequency:      meta.Frequency,
			SystemLabel:    meta.SystemLabel,
			AudioType:      audioType,
			TranscriptText: meta.TranscriptText,
			Origin:         meta.Origin,
			SenderUserID:   meta.SenderUserID,
			SenderEmail:    meta.SenderEmail,
			Audio:          audio,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)
	}

	// Seed with recent calls so the client has audio to play immediately on connect.
	// Embedded clients (field handheld) pass ?seed=0 to avoid multi-megabyte bursts on connect.
	skipSeed := r.URL.Query().Get("seed") == "0"
	var seedCalls []database.Call
	var seedErr error
	if skipSeed {
		h.logger.Info("public stream seed skipped", "radio_set_id", rs.ID)
	} else {
		seedCalls, seedErr = h.seedPublicStreamCalls(rs, subscribedTalkgroups)
	}
	if seedErr != nil {
		h.logger.Error("public stream seed query failed", "radio_set_id", rs.ID, "error", seedErr)
	} else if len(seedCalls) > 0 {
		h.logger.Info("public stream seed query completed", "radio_set_id", rs.ID, "calls", len(seedCalls))
		for _, c := range seedCalls {
			audio, _, _, _, _, dbErr := h.db.GetCallAudio(c.ID)
			if dbErr != nil {
				h.logger.Error("public stream seed audio load failed", "radio_set_id", rs.ID, "call_id", c.ID, "error", dbErr)
				continue
			}
			meta := streamCallMeta{
				ID:             c.ID,
				Talkgroup:      c.Talkgroup,
				TalkgroupLabel: c.TalkgroupLabel,
				TalkgroupGroup: c.TalkgroupGroup,
				DateTime:       c.DateTime,
				Duration:       c.Duration,
				Frequency:      c.Frequency,
				SystemLabel:    c.SystemLabel,
				AudioType:      c.AudioType,
				TranscriptText: c.TranscriptText,
				Origin:         c.Origin,
				SenderUserID:   c.SenderUserID,
				SenderEmail:    c.SenderEmail,
			}
			if err := sendCall(meta, audio); err != nil {
				return
			}
		}
	}

	// The WebSocket read pump must run to detect disconnections and to prevent
	// internal send-buffer stalls; the client does not send meaningful messages.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	// Fan out live calls to this connection.
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if err := sendCall(chunk.meta, chunk.audio); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// playerPageConfig is JSON-encoded into the player page so it can be consumed by JS.
// URLs are constructed client-side using window.location.origin to avoid server-side
// scheme detection issues with reverse proxies.
type playerPageConfig struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type playerPageData struct {
	Name   string
	Config template.JS // JSON-encoded; safe for direct script embedding.
}

var playerTmpl = template.Must(template.New("player").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">
<meta name="theme-color" content="#0a0a0a">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="apple-mobile-web-app-title" content="{{.Name}}">
<title>{{.Name}} // SignalForge Hub</title>
<script>
(function(){try{var m=localStorage.getItem('sf-display-mode');if(m==='dark'||m==='nite'||m==='nvg'||m==='light')document.documentElement.dataset.sfDisplayMode=m;}catch(e){}})();
</script>
<style>
:root,[data-sf-display-mode="dark"]{--sf-bg:#0a0a0a;--sf-surface:#111111;--sf-border:#1f1f1f;--sf-text:#c9c9c9;--sf-muted:#555555;--sf-accent:#ffaa00;--sf-accent-bright:#ffc700;--sf-error:#ff4444;--sf-accent-glow:255,170,0}
[data-sf-display-mode="nite"]{--sf-bg:#080000;--sf-surface:#120404;--sf-border:#2a1010;--sf-text:#8a3030;--sf-muted:#4a2020;--sf-accent:#aa2020;--sf-accent-bright:#cc3333;--sf-error:#ff4444;--sf-accent-glow:170,32,32}
[data-sf-display-mode="nvg"]{--sf-bg:#06080a;--sf-surface:#0a1014;--sf-border:#141c22;--sf-text:#5a6a72;--sf-muted:#3a4548;--sf-accent:#4a6878;--sf-accent-bright:#5a7888;--sf-error:#8a4a4a;--sf-accent-glow:74,104,120}
[data-sf-display-mode="light"]{--sf-bg:#ece8dc;--sf-surface:#f8f6f0;--sf-border:#c8c0b0;--sf-text:#1a1814;--sf-muted:#6a6458;--sf-accent:#a07800;--sf-accent-bright:#c89600;--sf-error:#c02828;--sf-accent-glow:160,120,0}
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
html,body{height:100dvh;background:var(--sf-bg);overflow:hidden;transition:background .2s ease,color .2s ease}
body{color:var(--sf-text);font-family:'JetBrains Mono','Courier New',Courier,monospace;height:100dvh;display:flex;flex-direction:column;align-items:center;justify-content:center;-webkit-tap-highlight-color:transparent}
.panel{width:min(520px,96vw,96vh);aspect-ratio:1/1;flex:0 0 auto;border:1px solid var(--sf-border);background:var(--sf-surface);display:flex;flex-direction:column;overflow:hidden;transition:background .2s ease,border-color .2s ease}
.hdr{display:flex;justify-content:space-between;align-items:center;padding:.45rem .75rem;border-bottom:1px solid var(--sf-border);background:var(--sf-bg);gap:.5rem}
.hdr-logo{font-size:9px;color:var(--sf-muted);letter-spacing:.18em;white-space:nowrap;opacity:.65}
.hdr-name{font-size:10px;color:var(--sf-accent-bright);letter-spacing:.06em;font-weight:700;text-overflow:ellipsis;overflow:hidden;white-space:nowrap;flex:1;text-align:center}
.hdr-status{display:flex;align-items:center;gap:.3rem;flex-shrink:0}
.dot{width:7px;height:7px;border-radius:50%;background:var(--sf-border);flex-shrink:0}
.dot.connecting{background:var(--sf-accent-bright);animation:blink-accent 1s step-start infinite}
.dot.live{background:var(--sf-accent-bright);box-shadow:0 0 6px rgba(var(--sf-accent-glow),.55);animation:pulse-live 1.4s ease-in-out infinite}
.dot.err{background:var(--sf-error)}
@keyframes blink-accent{0%,100%{opacity:1}50%{opacity:.2}}
@keyframes pulse-live{0%,100%{box-shadow:0 0 3px rgba(var(--sf-accent-glow),.45)}50%{box-shadow:0 0 9px rgba(var(--sf-accent-glow),.65)}}
.status-lbl{font-size:9px;color:var(--sf-muted);text-transform:uppercase;letter-spacing:.12em;min-width:58px;text-align:right}
.display{padding:.7rem .75rem .55rem;border-bottom:1px solid var(--sf-border);min-height:80px;display:flex;flex-direction:column;justify-content:center}
.disp-meta{font-size:9px;color:var(--sf-muted);text-transform:uppercase;letter-spacing:.14em;margin-bottom:.35rem;height:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.disp-label-row{display:flex;align-items:center;min-height:26px}
.disp-label{font-size:19px;color:var(--sf-accent-bright);letter-spacing:.03em;line-height:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%}
.cursor{display:inline-block;width:11px;height:17px;background:var(--sf-accent-bright);margin-left:2px;vertical-align:middle;animation:blink-cur .75s step-start infinite;flex-shrink:0}
@keyframes blink-cur{0%,49%{opacity:1}50%,100%{opacity:0}}
.disp-freq{font-size:9px;color:var(--sf-muted);margin-top:.3rem;height:13px;letter-spacing:.08em}
.disp-idle{font-size:10px;color:var(--sf-muted);display:flex;align-items:center;min-height:26px}
.log-table{border-bottom:1px solid var(--sf-border);flex:1;min-height:0;display:flex;flex-direction:column}
.log-hdr{display:grid;grid-template-columns:52px 80px 1fr;padding:.28rem .75rem;background:var(--sf-bg);border-bottom:1px solid var(--sf-border)}
.log-hdr span,.log-cell{font-size:9px;text-transform:uppercase;letter-spacing:.1em;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.log-hdr span{color:var(--sf-muted)}
.log-row:last-child{border-bottom:none}
.log-row.flash{background:rgba(var(--sf-accent-glow),.1)}
#log-body{flex:1;min-height:0;overflow-y:auto;scrollbar-width:none;-ms-overflow-style:none}
#log-body::-webkit-scrollbar{display:none}
.log-cell{font-size:10px}
.log-time{color:var(--sf-muted)}
.log-sys{color:var(--sf-text);opacity:.75}
.log-tg{color:var(--sf-text)}
.mode-bar{display:flex;gap:.3rem;padding:.38rem .75rem;border-bottom:1px solid var(--sf-border);background:var(--sf-bg);justify-content:center}
.mode-btn{background:none;border:1px solid var(--sf-border);color:var(--sf-muted);padding:.22rem .42rem;font-family:inherit;font-size:10px;cursor:pointer;border-radius:2px;-webkit-appearance:none;transition:border-color .1s,color .1s,background .1s;line-height:1}
.mode-btn:hover{border-color:var(--sf-accent);color:var(--sf-accent)}
.mode-btn.on{border-color:var(--sf-accent);color:var(--sf-accent);background:rgba(var(--sf-accent-glow),.1)}
.controls{display:flex;gap:.45rem;padding:.6rem .75rem;border-bottom:1px solid var(--sf-border);background:var(--sf-bg);flex-wrap:wrap}
.btn{background:none;border:1px solid var(--sf-border);color:var(--sf-muted);padding:.32rem .75rem;font-family:inherit;font-size:9px;text-transform:uppercase;letter-spacing:.12em;cursor:pointer;border-radius:2px;-webkit-appearance:none;transition:border-color .1s,color .1s,background .1s;white-space:nowrap}
.btn:hover{border-color:var(--sf-accent-bright);color:var(--sf-accent-bright)}
.btn:active{background:rgba(var(--sf-accent-glow),.08)}
.btn.on{border-color:var(--sf-accent-bright);color:var(--sf-accent-bright)}
.btn:disabled{opacity:.3;cursor:default}
.btn:disabled:hover{border-color:var(--sf-border);color:var(--sf-muted)}
.vol{display:flex;align-items:center;gap:.4rem;min-width:142px;flex:1;border:1px solid var(--sf-border);padding:.28rem .45rem;border-radius:2px;color:var(--sf-muted)}
.vol-lbl,.vol-val{font-size:9px;text-transform:uppercase;letter-spacing:.12em;line-height:1;white-space:nowrap}
.vol-val{width:34px;text-align:right;color:var(--sf-accent-bright);font-variant-numeric:tabular-nums}
.vol input{min-width:0;flex:1;accent-color:var(--sf-accent-bright);background:transparent}
.stream-bar{display:flex;align-items:center;gap:.5rem;padding:.5rem .75rem;background:var(--sf-bg)}
.stream-url{font-size:9px;color:var(--sf-muted);flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;letter-spacing:.04em;opacity:.7}
.copy-btn{background:none;border:1px solid var(--sf-border);color:var(--sf-muted);font-family:inherit;font-size:9px;text-transform:uppercase;letter-spacing:.1em;cursor:pointer;padding:.2rem .5rem;border-radius:2px;flex-shrink:0;transition:border-color .1s,color .1s}
.copy-btn:hover{border-color:var(--sf-accent);color:var(--sf-accent)}
.copy-btn.ok{border-color:var(--sf-accent-bright);color:var(--sf-accent-bright)}
.disp-transcript{font-size:9px;color:var(--sf-muted);margin-top:.35rem;letter-spacing:.03em;overflow:hidden;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;line-height:1.5;word-break:break-word}
.log-row{display:grid;grid-template-columns:52px 80px 1fr;padding:.28rem .75rem;border-bottom:1px solid var(--sf-border);align-items:start}
.log-tg-cell{overflow:hidden;min-width:0}
.log-tg{font-size:10px;color:var(--sf-text);text-transform:uppercase;letter-spacing:.1em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;display:block}
.log-transcript{font-size:8px;color:var(--sf-muted);white-space:normal;word-break:break-word;letter-spacing:.02em;line-height:1.35;margin-top:2px;display:block}
.ptt-badge{display:inline-block;font-size:8px;color:var(--sf-accent-bright);border:1px solid rgba(var(--sf-accent-glow),.45);padding:0 4px;margin-left:6px;letter-spacing:.16em;text-transform:uppercase;vertical-align:1px}
.disp-meta .ptt-badge{font-size:9px;margin-left:8px}
.log-row.ptt{box-shadow:inset 2px 0 0 var(--sf-accent-bright)}
</style>
</head>
<body>
<div class="panel">
  <div class="hdr">
	<span class="hdr-logo">SIGNALFORGE // HUB</span>
    <span class="hdr-name">{{.Name}}</span>
    <div class="hdr-status">
      <span class="dot" id="dot"></span>
      <span class="status-lbl" id="status-lbl">STANDBY</span>
    </div>
  </div>
  <div class="display">
    <div class="disp-idle" id="idle-state"><span id="idle-txt">WAITING FOR CALLS</span><span id="idle-dots"></span></div>
    <div id="call-state" style="display:none">
      <div class="disp-meta" id="disp-meta"></div>
      <div class="disp-label-row">
        <span class="disp-label" id="disp-label"></span><span class="cursor" id="cur"></span>
      </div>
      <div class="disp-freq" id="disp-freq"></div>
      <div class="disp-transcript" id="disp-transcript"></div>
    </div>
  </div>
  <div class="log-table">
    <div class="log-hdr"><span>TIME</span><span>SYSTEM</span><span>TALKGROUP</span></div>
    <div id="log-body"></div>
  </div>
  <div class="mode-bar" aria-label="Display mode">
    <button type="button" class="mode-btn" data-mode="dark" title="DARK — tap again to cycle" aria-pressed="false">◑</button>
    <button type="button" class="mode-btn" data-mode="nite" title="NITE" aria-pressed="false">▌</button>
    <button type="button" class="mode-btn" data-mode="nvg" title="NVG" aria-pressed="false">◈</button>
    <button type="button" class="mode-btn" data-mode="light" title="LIGHT — daylight" aria-pressed="false">☀</button>
  </div>
  <div class="controls">
    <button class="btn on" id="live-btn" onclick="toggleLive()">LIVE ON</button>
    <button class="btn" id="replay-btn" onclick="replayLast()">REPLAY LAST</button>
		<label class="vol" title="Player volume">
			<span class="vol-lbl">VOL</span>
			<input id="volume-range" type="range" min="0" max="100" step="1" value="100" oninput="setPlayerVolume(this.value)">
			<span class="vol-val" id="volume-val">100%</span>
		</label>
  </div>
  <div class="stream-bar">
    <span class="stream-url" id="stream-url-txt"></span>
    <button class="copy-btn" id="copy-btn" onclick="copyStream()">COPY</button>
  </div>
</div>
<audio id="audio" preload="none" playsinline></audio>
<audio id="audio-keepalive" preload="auto" loop playsinline></audio>
<script>
(function(){
  var cfg = {{.Config}};
  var DISPLAY_MODE_KEY = 'sf-display-mode';
  var TACTICAL_MODES = ['dark', 'nite', 'nvg'];
  var THEME_COLORS = { dark: '#0a0a0a', nite: '#080000', nvg: '#06080a', light: '#ece8dc' };
  var LONG_PRESS_MS = 800;

  function validDisplayMode(m) {
    return m === 'dark' || m === 'nite' || m === 'nvg' || m === 'light';
  }

  function getDisplayMode() {
    try {
      var stored = localStorage.getItem(DISPLAY_MODE_KEY);
      return validDisplayMode(stored) ? stored : 'dark';
    } catch (_) {
      return 'dark';
    }
  }

  function applyDisplayMode(mode) {
    document.documentElement.dataset.sfDisplayMode = mode;
    try { localStorage.setItem(DISPLAY_MODE_KEY, mode); } catch (_) {}
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta && THEME_COLORS[mode]) meta.content = THEME_COLORS[mode];
    document.querySelectorAll('.mode-btn').forEach(function(btn) {
      var active = btn.getAttribute('data-mode') === mode;
      btn.classList.toggle('on', active);
      btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
  }

  function nextTacticalMode(current) {
    var index = TACTICAL_MODES.indexOf(current);
    if (index < 0) return 'dark';
    return TACTICAL_MODES[(index + 1) % TACTICAL_MODES.length];
  }

  function wireDisplayModes() {
    document.querySelectorAll('.mode-btn').forEach(function(btn) {
      var target = btn.getAttribute('data-mode');
      if (!validDisplayMode(target)) return;
      var pressTimer = null;
      var longPressFired = false;
      function clearPress() {
        if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; }
      }
      btn.addEventListener('click', function() {
        if (longPressFired) { longPressFired = false; return; }
        var current = getDisplayMode();
        if (target === 'light') {
          applyDisplayMode(current === 'light' ? 'dark' : 'light');
          return;
        }
        if (current === target) applyDisplayMode(nextTacticalMode(current));
        else applyDisplayMode(target);
      });
      if (target === 'dark') {
        function startLongPress() {
          clearPress();
          longPressFired = false;
          pressTimer = setTimeout(function() {
            longPressFired = true;
            applyDisplayMode('light');
          }, LONG_PRESS_MS);
        }
        btn.addEventListener('mousedown', startLongPress);
        btn.addEventListener('mouseup', clearPress);
        btn.addEventListener('mouseleave', clearPress);
        btn.addEventListener('touchstart', startLongPress, { passive: true });
        btn.addEventListener('touchend', clearPress);
        btn.addEventListener('touchcancel', clearPress);
      }
    });
  }

  applyDisplayMode(getDisplayMode());
  wireDisplayModes();

  var base = window.location.origin;
  var wsURL = base.replace(/^http/, 'ws') + '/public/ws/' + cfg.token;
  document.getElementById('stream-url-txt').textContent = wsURL;

  var audio = document.getElementById('audio');
  var keepAlive = document.getElementById('audio-keepalive');
  var keepAliveStarted = false;
  var ws = null;

  // buildSilentWavBlob makes a half-second silent 16-bit mono WAV that we
  // loop forever on a second <audio> element. Mobile browsers (especially
  // iOS Safari) release the audio session when the only playing audio
  // element pauses — which happens every time we swap audio.src between
  // calls. Keeping a continuously-playing silent element alongside makes
  // the OS think audio is always playing, so the session survives screen
  // sleep and the gaps between live calls.
  function buildSilentWavBlob() {
    var sampleRate = 8000;
    var seconds = 0.5;
    var numSamples = sampleRate * seconds;
    var dataSize = numSamples * 2;
    var buf = new ArrayBuffer(44 + dataSize);
    var view = new DataView(buf);
    function writeAscii(off, s) {
      for (var i = 0; i < s.length; i++) view.setUint8(off + i, s.charCodeAt(i));
    }
    writeAscii(0, 'RIFF');
    view.setUint32(4, 36 + dataSize, true);
    writeAscii(8, 'WAVE');
    writeAscii(12, 'fmt ');
    view.setUint32(16, 16, true);
    view.setUint16(20, 1, true);
    view.setUint16(22, 1, true);
    view.setUint32(24, sampleRate, true);
    view.setUint32(28, sampleRate * 2, true);
    view.setUint16(32, 2, true);
    view.setUint16(34, 16, true);
    writeAscii(36, 'data');
    view.setUint32(40, dataSize, true);
    return new Blob([buf], { type: 'audio/wav' });
  }
  function startKeepAlive() {
    if (keepAliveStarted || !keepAlive) return;
    keepAliveStarted = true;
    keepAlive.src = URL.createObjectURL(buildSilentWavBlob());
    keepAlive.loop = true;
    // Some browsers skip processing of fully-muted streams, so use a
    // floor value that's well below audible.
    keepAlive.volume = 0.001;
    keepAlive.play().catch(function() {
      // If the gesture was somehow lost, allow a future toggleLive to retry.
      keepAliveStarted = false;
    });
  }
  function setMediaPlaybackState(state) {
    if ('mediaSession' in navigator) {
      try { navigator.mediaSession.playbackState = state; } catch(_) {}
    }
  }
  var queue = [];       // [{meta, blobURL}]
  var playing = false;
	var liveActive = true;
	var playbackBlocked = false;
  var seenIDs = {};
  var curBlobURL = null;
  var prevBlobURL = null;
  var typeTimer = null;
  var idleDotsTimer = null;
  var idleDotN = 0;
  var callLog = [];
  var reconnectDelay = 1000;
	var reconnectAttempts = 0;
	var reconnectTimer = null;
	var MAX_RECONNECT_ATTEMPTS = 5;
  var MAX_QUEUE = 20;
  var volumeStorageKey = 'signalforge_public_player_volume';
  var playerVolume = getStoredVolume();

  function getStoredVolume() {
    var saved = Number(localStorage.getItem(volumeStorageKey));
    if (!Number.isFinite(saved)) return 100;
    return Math.min(100, Math.max(0, Math.round(saved)));
  }

  function applyVolume(target) {
    target.volume = playerVolume / 100;
  }

  window.setPlayerVolume = function(value) {
    playerVolume = Math.min(100, Math.max(0, Math.round(Number(value))));
    localStorage.setItem(volumeStorageKey, String(playerVolume));
    document.getElementById('volume-range').value = String(playerVolume);
    document.getElementById('volume-val').textContent = playerVolume + '%';
    applyVolume(audio);
  };

  window.setPlayerVolume(playerVolume);

  function esc(s) {
    return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function setStatus(cls, text) {
    var d = document.getElementById('dot');
    d.className = 'dot' + (cls ? ' ' + cls : '');
    document.getElementById('status-lbl').textContent = text;
  }

  (function tickIdle() {
    idleDotN = (idleDotN + 1) % 4;
    var el = document.getElementById('idle-dots');
    if (el) el.textContent = '.'.repeat(idleDotN);
    idleDotsTimer = setTimeout(tickIdle, 500);
  })();

  function typeText(text) {
    clearTimeout(typeTimer);
    var el = document.getElementById('disp-label');
    el.textContent = '';
    var i = 0;
    (function step() {
      if (i < text.length) {
        el.textContent += text.charAt(i++);
        typeTimer = setTimeout(step, 20);
      }
    })();
  }

  function renderLog() {
    var rows = callLog.map(function(c) {
      var classes = 'log-row' + (c.flash ? ' flash' : '') + (c.ptt ? ' ptt' : '');
      var pttBadge = '';
      if (c.ptt) {
        var senderLabel = c.senderEmail ? (' · ' + esc(c.senderEmail.split('@')[0])) : '';
        pttBadge = '<span class="ptt-badge" title="' + esc(c.senderEmail || '') + '">PTT' + senderLabel + '</span>';
      }
      return '<div class="' + classes + '">' +
        '<span class="log-cell log-time">' + esc(c.time) + '</span>' +
        '<span class="log-cell log-sys">' + esc(c.sys) + '</span>' +
        '<span class="log-tg-cell">' +
          '<span class="log-tg">' + esc(c.tg) + pttBadge + '</span>' +
          (c.transcript ? '<span class="log-transcript">' + esc(c.transcript) + '</span>' : '') +
        '</span>' +
        '</div>';
    });
    document.getElementById('log-body').innerHTML = rows.join('');
  }

  function showCall(meta) {
    document.getElementById('replay-btn').disabled = false;
    clearTimeout(idleDotsTimer);
    document.getElementById('idle-state').style.display = 'none';
    document.getElementById('call-state').style.display = '';

    var parts = [];
    if (meta.talkgroupGroup) parts.push(meta.talkgroupGroup);
    if (meta.systemLabel) parts.push(meta.systemLabel);
    var dispMeta = document.getElementById('disp-meta');
    dispMeta.textContent = parts.join(' \u00b7 ');
    if (meta.origin === 'ptt') {
      var badge = document.createElement('span');
      badge.className = 'ptt-badge';
      var senderLocalPart = meta.senderEmail ? meta.senderEmail.split('@')[0] : '';
      badge.textContent = senderLocalPart ? ('PTT · ' + senderLocalPart) : 'PTT';
      if (meta.senderEmail) badge.title = meta.senderEmail;
      dispMeta.appendChild(badge);
    }

    var freq = [];
    if (meta.frequency) freq.push((meta.frequency / 1e6).toFixed(4) + ' MHz');
    if (meta.duration) freq.push(meta.duration.toFixed(1) + 's');
    document.getElementById('disp-freq').textContent = freq.join('  \u00b7  ');
    document.getElementById('disp-transcript').textContent = meta.transcriptText || '';

    typeText((meta.talkgroupLabel || ('#' + meta.talkgroup)).toUpperCase());

    var ts = new Date(meta.dateTime * 1000);
    var timeStr = String(ts.getHours()).padStart(2,'0') + ':' +
      String(ts.getMinutes()).padStart(2,'0') + ':' +
      String(ts.getSeconds()).padStart(2,'0');
    callLog.unshift({ time: timeStr, sys: meta.systemLabel || '-', tg: meta.talkgroupLabel || ('#' + meta.talkgroup), transcript: meta.transcriptText || '', flash: true, ptt: meta.origin === 'ptt', senderEmail: meta.senderEmail || '' });
    if (callLog.length > 50) callLog.pop();
    renderLog();
    setTimeout(function() { if (callLog.length) { callLog[0].flash = false; renderLog(); } }, 1800);

    if ('mediaSession' in navigator) {
      navigator.mediaSession.metadata = new MediaMetadata({
        title: (meta.talkgroupLabel || ('#' + meta.talkgroup)).toUpperCase(),
		artist: meta.talkgroupGroup || meta.systemLabel || 'SignalForge Hub',
        album: cfg.name || ''
      });
    }
  }

  var chirpCtx = null;
  function getChirpCtx() {
    if (chirpCtx) return chirpCtx;
    var Ctor = window.AudioContext || window.webkitAudioContext;
    if (!Ctor) return null;
    try { chirpCtx = new Ctor(); } catch(_) { chirpCtx = null; }
    return chirpCtx;
  }
  function playChirp(volume) {
    var ctx = getChirpCtx();
    if (!ctx || !(volume > 0)) return Promise.resolve();
    if (ctx.state === 'suspended') { try { ctx.resume(); } catch(_){} }
    var now = ctx.currentTime;
    var peak = Math.max(0, Math.min(1, volume));
    function beep(freq, startOffset, duration) {
      var osc = ctx.createOscillator();
      var gain = ctx.createGain();
      osc.type = 'sine';
      osc.frequency.value = freq;
      gain.gain.setValueAtTime(0, now + startOffset);
      gain.gain.linearRampToValueAtTime(peak, now + startOffset + 0.025);
      gain.gain.setValueAtTime(peak, now + startOffset + duration - 0.04);
      gain.gain.linearRampToValueAtTime(0, now + startOffset + duration);
      osc.connect(gain).connect(ctx.destination);
      osc.start(now + startOffset);
      osc.stop(now + startOffset + duration + 0.02);
    }
    beep(900, 0, 0.10);
    beep(1150, 0.11, 0.12);
    return new Promise(function(resolve){ setTimeout(resolve, 250); });
  }

  function playNext() {
    if (queue.length === 0) {
      playing = false;
      setStatus('live', 'SCANNING');
      return;
    }
    var item = queue.shift();
    // Slide the blob-URL window: revoke 2-calls-ago, keep last two for replay.
    if (prevBlobURL) URL.revokeObjectURL(prevBlobURL);
    prevBlobURL = curBlobURL;
    curBlobURL = item.blobURL;
		playing = true;
		playbackBlocked = false;
		document.getElementById('live-btn').textContent = 'LIVE ON';
		document.getElementById('live-btn').className = 'btn on';
    setStatus('live', 'LIVE');
    showCall(item.meta);
    audio.src = curBlobURL;
		applyVolume(audio);
    var chirpReady = item.meta.origin === 'ptt' ? playChirp((audio.volume || 1) * 0.35) : Promise.resolve();
    chirpReady.then(function(){
      return audio.play();
    }).catch(function() {
			playbackBlocked = true;
			document.getElementById('live-btn').textContent = 'PLAY';
			document.getElementById('live-btn').className = 'btn';
			setStatus('err', 'TAP PLAY');
      playing = false;
    });
  }

  audio.onended = function() { playNext(); };
  audio.onerror = function() { playing = false; playNext(); };

  function base64ToArrayBuffer(b64) {
    var bin = atob(b64);
    var buf = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
    return buf.buffer;
  }

  function enqueueCall(msg) {
    if (seenIDs[msg.id]) return;
    seenIDs[msg.id] = true;
    if (queue.length >= MAX_QUEUE) {
      var dropped = queue.shift();
      URL.revokeObjectURL(dropped.blobURL);
		}
		var blob = new Blob([base64ToArrayBuffer(msg.audio)], {type: msg.audioType || 'audio/mpeg'});
    queue.push({meta: msg, blobURL: URL.createObjectURL(blob)});
    if (!playing) playNext();
  }

  function connect() {
		if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
    if (ws) { try { ws.close(); } catch(_){} ws = null; }
    setStatus('connecting', 'CONNECTING');
    ws = new WebSocket(wsURL);

    ws.onopen = function() {
      reconnectDelay = 1000;
			reconnectAttempts = 0;
      setStatus('live', 'SCANNING');
    };

    ws.onmessage = function(e) {
      try {
        var msg = JSON.parse(e.data);
        if (msg.cmd === 'call' && msg.audio) enqueueCall(msg);
      } catch(_) {}
    };

		ws.onerror = function() {
			setStatus('err', 'NO SIGNAL');
		};

    ws.onclose = function() {
      ws = null;
      if (!liveActive) return;
      setStatus('err', 'NO SIGNAL');
			reconnectAttempts += 1;
			if (reconnectAttempts > MAX_RECONNECT_ATTEMPTS) {
				liveActive = false;
				document.getElementById('live-btn').textContent = 'RETRY';
				document.getElementById('live-btn').className = 'btn';
				return;
			}
			reconnectTimer = setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 30000);
    };
  }

  window.toggleLive = function() {
		var btn = document.getElementById('live-btn');
		// Any branch of this handler runs inside a user gesture, so this is
		// the right moment to spin up the silent keep-alive stream.
		startKeepAlive();
		if (liveActive && playbackBlocked) {
			playbackBlocked = false;
			playing = true;
			audio.play().then(function() {
				btn.textContent = 'LIVE ON';
				btn.className = 'btn on';
				setStatus('live', 'LIVE');
				setMediaPlaybackState('playing');
			}).catch(function() {
				playbackBlocked = true;
				playing = false;
				btn.textContent = 'PLAY';
				btn.className = 'btn';
				setStatus('err', 'TAP PLAY');
				setMediaPlaybackState('paused');
			});
			return;
		}
    if (liveActive) {
      liveActive = false;
		if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
      if (ws) { try { ws.close(); } catch(_){} ws = null; }
      audio.pause();
      audio.src = '';
      playing = false;
		playbackBlocked = false;
      queue.forEach(function(item) { URL.revokeObjectURL(item.blobURL); });
      queue = [];
      btn.textContent = 'LIVE OFF';
      btn.className = 'btn';
      setStatus('err', 'OFF');
      setMediaPlaybackState('paused');
    } else {
      liveActive = true;
      btn.textContent = 'LIVE ON';
      btn.className = 'btn on';
      reconnectDelay = 1000;
			reconnectAttempts = 0;
      connect();
      setMediaPlaybackState('playing');
    }
  };

  window.replayLast = function() {
    var url = curBlobURL || prevBlobURL;
    if (!url) return;
    var a = new Audio(url);
		applyVolume(a);
    a.play().catch(function(){});
  };

  if ('mediaSession' in navigator) {
    navigator.mediaSession.setActionHandler('play', function() { if (!liveActive) window.toggleLive(); });
    navigator.mediaSession.setActionHandler('pause', function() { if (liveActive) window.toggleLive(); });
  }

  window.copyStream = function() {
    var btn = document.getElementById('copy-btn');
    navigator.clipboard.writeText(wsURL).then(function() {
      btn.textContent = 'COPIED'; btn.className = 'copy-btn ok';
      setTimeout(function() { btn.textContent = 'COPY'; btn.className = 'copy-btn'; }, 1500);
    }).catch(function(){});
  };

  renderLog();
  connect();
})();
</script>
</body>
</html>
`))

// handlePublicPlayer serves the embeddable web player page.
func (h *handler) handlePublicPlayer(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rs, err := h.db.GetRadioSetByShareToken(token)
	if err != nil || rs == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	cfgBytes, err := json.Marshal(playerPageConfig{
		Token: token,
		Name:  rs.Name,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := playerPageData{
		Name:   rs.Name,
		Config: template.JS(cfgBytes), // json.Marshal output is safe for JS embedding
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := playerTmpl.Execute(w, data); err != nil {
		h.logger.Error("player template render failed", "error", err)
	}
}

// handlePublicLastCall returns the audio of the most recent call for a radio set.
func (h *handler) handlePublicLastCall(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rs, err := h.db.GetRadioSetByShareToken(token)
	if err != nil || rs == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	calls, err := h.db.GetRecentCallsForRadioSet(rs.UserID, *rs, 1)
	if err != nil || len(calls) == 0 {
		http.Error(w, "no calls", http.StatusNotFound)
		return
	}
	audio, audioType, audioName, _, _, err := h.db.GetCallAudio(calls[len(calls)-1].ID)
	if err != nil {
		http.Error(w, "audio not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	serveAudioBytes(w, r, audio, audioType, defaultCallAudioName(calls[len(calls)-1].ID, audioName), false, "no-store")
}
