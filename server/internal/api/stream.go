package api

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
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
}

type streamChunk struct {
	audio []byte
	meta  streamCallMeta
}

type streamListener struct {
	ownerUserID string
	talkgroups  map[int]struct{}
	sourceIDs   map[string]struct{}
	ch          chan streamChunk
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
	}
	chunk := streamChunk{audio: audio, meta: meta}

	sh.mu.Lock()
	defer sh.mu.Unlock()
	delivered := 0
	for _, l := range sh.listeners {
		if _, ok := l.talkgroups[call.Talkgroup]; !ok {
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
func (sh *streamHub) subscribe(ownerUserID string, talkgroups []int, sourceIDs []string) (<-chan streamChunk, func()) {
	tgSet := make(map[int]struct{}, len(talkgroups))
	for _, tg := range talkgroups {
		tgSet[tg] = struct{}{}
	}
	sourceIDSet := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID != "" {
			sourceIDSet[sourceID] = struct{}{}
		}
	}
	ch := make(chan streamChunk, 32)
	l := &streamListener{ownerUserID: ownerUserID, talkgroups: tgSet, sourceIDs: sourceIDSet, ch: ch}

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
	Audio          []byte  `json:"audio"` // base64-encoded in JSON output
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

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error("public ws accept failed", "error", err)
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	sourceIDs, err := h.db.ListReadableSourceIDsForUser(rs.UserID)
	if err != nil {
		h.logger.Error("list public stream readable sources failed", "error", err)
		http.Error(w, "query sources", http.StatusInternalServerError)
		return
	}
	h.logger.Info("public stream connected",
		"radio_set_id", rs.ID,
		"radio_set_name", rs.Name,
		"owner_user_id", rs.UserID,
		"talkgroups", len(rs.Talkgroups),
		"readable_sources", len(sourceIDs),
	)

	// Subscribe before seeding so no live calls are missed during the seed phase.
	ch, unsubscribe := h.streamHub.subscribe(rs.UserID, rs.Talkgroups, sourceIDs)
	defer unsubscribe()

	sendCall := func(meta streamCallMeta, audio []byte) error {
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
			AudioType:      meta.AudioType,
			Audio:          audio,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)
	}

	// Seed with recent calls so the client has audio to play immediately on connect.
	if len(rs.Talkgroups) > 0 {
		seedCalls, seedErr := h.db.GetRecentCallsForTalkgroups(rs.UserID, rs.Talkgroups, 5)
		if seedErr != nil {
			h.logger.Error("public stream seed query failed", "radio_set_id", rs.ID, "error", seedErr)
		} else {
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
				}
				if err := sendCall(meta, audio); err != nil {
					return
				}
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
<title>{{.Name}} // P7 SCAN</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
html,body{height:100dvh;background:#0a0a0a;overflow:hidden}
body{color:#d4d4d4;font-family:'Courier New',Courier,monospace;height:100dvh;display:flex;flex-direction:column;align-items:center;justify-content:center;-webkit-tap-highlight-color:transparent}
.panel{width:min(520px,96vw,96vh);aspect-ratio:1/1;flex:0 0 auto;border:1px solid #1e1e1e;background:#0c0c0c;display:flex;flex-direction:column;overflow:hidden}
.hdr{display:flex;justify-content:space-between;align-items:center;padding:.45rem .75rem;border-bottom:1px solid #161616;background:#080808;gap:.5rem}
.hdr-logo{font-size:9px;color:#252525;letter-spacing:.18em;white-space:nowrap}
.hdr-name{font-size:10px;color:#7df7ae;letter-spacing:.06em;font-weight:700;text-overflow:ellipsis;overflow:hidden;white-space:nowrap;flex:1;text-align:center}
.hdr-status{display:flex;align-items:center;gap:.3rem;flex-shrink:0}
.dot{width:7px;height:7px;border-radius:50%;background:#1a1a1a;flex-shrink:0}
.dot.connecting{background:#fbbf24;animation:blink-amber 1s step-start infinite}
.dot.live{background:#7df7ae;box-shadow:0 0 6px #7df7ae;animation:pulse-green 1.4s ease-in-out infinite}
.dot.err{background:#f87171}
@keyframes blink-amber{0%,100%{opacity:1}50%{opacity:.2}}
@keyframes pulse-green{0%,100%{box-shadow:0 0 3px #7df7ae}50%{box-shadow:0 0 9px #7df7ae}}
.status-lbl{font-size:9px;color:#2a2a2a;text-transform:uppercase;letter-spacing:.12em;min-width:58px;text-align:right}
.display{padding:.7rem .75rem .55rem;border-bottom:1px solid #161616;min-height:80px;display:flex;flex-direction:column;justify-content:center}
.disp-meta{font-size:9px;color:#2e2e2e;text-transform:uppercase;letter-spacing:.14em;margin-bottom:.35rem;height:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.disp-label-row{display:flex;align-items:center;min-height:26px}
.disp-label{font-size:19px;color:#7df7ae;letter-spacing:.03em;line-height:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%}
.cursor{display:inline-block;width:11px;height:17px;background:#7df7ae;margin-left:2px;vertical-align:middle;animation:blink-cur .75s step-start infinite;flex-shrink:0}
@keyframes blink-cur{0%,49%{opacity:1}50%,100%{opacity:0}}
.disp-freq{font-size:9px;color:#2e2e2e;margin-top:.3rem;height:13px;letter-spacing:.08em}
.disp-idle{font-size:10px;color:#242424;display:flex;align-items:center;min-height:26px}
.log-table{border-bottom:1px solid #161616;flex:1;min-height:0;display:flex;flex-direction:column}
.log-hdr{display:grid;grid-template-columns:52px 80px 1fr;padding:.28rem .75rem;background:#080808;border-bottom:1px solid #111}
.log-hdr span,.log-cell{font-size:9px;text-transform:uppercase;letter-spacing:.1em;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.log-hdr span{color:#1e1e1e}
.log-row{display:grid;grid-template-columns:52px 80px 1fr;padding:.28rem .75rem;border-bottom:1px solid #0e0e0e}
.log-row:last-child{border-bottom:none}
.log-row.flash{background:#0b1a10}
#log-body{flex:1;min-height:0;overflow-y:auto;scrollbar-width:none;-ms-overflow-style:none}
#log-body::-webkit-scrollbar{display:none}
.log-cell{font-size:10px}
.log-time{color:#2a2a2a}
.log-sys{color:#383838}
.log-tg{color:#484848}
.controls{display:flex;gap:.45rem;padding:.6rem .75rem;border-bottom:1px solid #161616;background:#080808;flex-wrap:wrap}
.btn{background:none;border:1px solid #252525;color:#484848;padding:.32rem .75rem;font-family:inherit;font-size:9px;text-transform:uppercase;letter-spacing:.12em;cursor:pointer;border-radius:2px;-webkit-appearance:none;transition:border-color .1s,color .1s;white-space:nowrap}
.btn:hover{border-color:#7df7ae;color:#7df7ae}
.btn:active{background:rgba(125,247,174,.08)}
.btn.on{border-color:#7df7ae;color:#7df7ae}
.btn:disabled{opacity:.3;cursor:default}
.btn:disabled:hover{border-color:#252525;color:#484848}
.stream-bar{display:flex;align-items:center;gap:.5rem;padding:.5rem .75rem;background:#080808}
.stream-url{font-size:9px;color:#1e1e1e;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;letter-spacing:.04em}
.copy-btn{background:none;border:1px solid #1a1a1a;color:#2a2a2a;font-family:inherit;font-size:9px;text-transform:uppercase;letter-spacing:.1em;cursor:pointer;padding:.2rem .5rem;border-radius:2px;flex-shrink:0;transition:border-color .1s,color .1s}
.copy-btn:hover{border-color:#555;color:#555}
.copy-btn.ok{border-color:#7df7ae;color:#7df7ae}
</style>
</head>
<body>
<div class="panel">
  <div class="hdr">
    <span class="hdr-logo">P7 // SCAN</span>
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
    </div>
  </div>
  <div class="log-table">
    <div class="log-hdr"><span>TIME</span><span>SYSTEM</span><span>TALKGROUP</span></div>
    <div id="log-body"></div>
  </div>
  <div class="controls">
    <button class="btn on" id="live-btn" onclick="toggleLive()">LIVE ON</button>
    <button class="btn" id="replay-btn" onclick="replayLast()">REPLAY LAST</button>
  </div>
  <div class="stream-bar">
    <span class="stream-url" id="stream-url-txt"></span>
    <button class="copy-btn" id="copy-btn" onclick="copyStream()">COPY</button>
  </div>
</div>
<audio id="audio" preload="none"></audio>
<script>
(function(){
  var cfg = {{.Config}};
  var base = window.location.origin;
  var wsURL = base.replace(/^http/, 'ws') + '/public/ws/' + cfg.token;
  document.getElementById('stream-url-txt').textContent = wsURL;

  var audio = document.getElementById('audio');
  var ws = null;
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
  var MAX_QUEUE = 20;

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
      return '<div class="log-row' + (c.flash ? ' flash' : '') + '">' +
        '<span class="log-cell log-time">' + esc(c.time) + '</span>' +
        '<span class="log-cell log-sys">' + esc(c.sys) + '</span>' +
        '<span class="log-cell log-tg">' + esc(c.tg) + '</span>' +
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
    document.getElementById('disp-meta').textContent = parts.join(' \u00b7 ');

    var freq = [];
    if (meta.frequency) freq.push((meta.frequency / 1e6).toFixed(4) + ' MHz');
    if (meta.duration) freq.push(meta.duration.toFixed(1) + 's');
    document.getElementById('disp-freq').textContent = freq.join('  \u00b7  ');

    typeText((meta.talkgroupLabel || ('#' + meta.talkgroup)).toUpperCase());

    var ts = new Date(meta.dateTime * 1000);
    var timeStr = String(ts.getHours()).padStart(2,'0') + ':' +
      String(ts.getMinutes()).padStart(2,'0') + ':' +
      String(ts.getSeconds()).padStart(2,'0');
    callLog.unshift({ time: timeStr, sys: meta.systemLabel || '-', tg: meta.talkgroupLabel || ('#' + meta.talkgroup), flash: true });
    if (callLog.length > 50) callLog.pop();
    renderLog();
    setTimeout(function() { if (callLog.length) { callLog[0].flash = false; renderLog(); } }, 1800);

    if ('mediaSession' in navigator) {
      navigator.mediaSession.metadata = new MediaMetadata({
        title: (meta.talkgroupLabel || ('#' + meta.talkgroup)).toUpperCase(),
        artist: meta.talkgroupGroup || meta.systemLabel || 'P7 Scanner',
        album: cfg.name || ''
      });
    }
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
		audio.play().catch(function() {
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
    if (ws) { try { ws.close(); } catch(_){} ws = null; }
    setStatus('connecting', 'CONNECTING');
    ws = new WebSocket(wsURL);

    ws.onopen = function() {
      reconnectDelay = 1000;
      setStatus('live', 'SCANNING');
    };

    ws.onmessage = function(e) {
      try {
        var msg = JSON.parse(e.data);
        if (msg.cmd === 'call' && msg.audio) enqueueCall(msg);
      } catch(_) {}
    };

    ws.onclose = function() {
      ws = null;
      if (!liveActive) return;
      setStatus('err', 'NO SIGNAL');
      setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 30000);
    };
  }

  window.toggleLive = function() {
		var btn = document.getElementById('live-btn');
		if (liveActive && playbackBlocked) {
			playbackBlocked = false;
			playing = true;
			audio.play().then(function() {
				btn.textContent = 'LIVE ON';
				btn.className = 'btn on';
				setStatus('live', 'LIVE');
			}).catch(function() {
				playbackBlocked = true;
				playing = false;
				btn.textContent = 'PLAY';
				btn.className = 'btn';
				setStatus('err', 'TAP PLAY');
			});
			return;
		}
    if (liveActive) {
      liveActive = false;
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
    } else {
      liveActive = true;
      btn.textContent = 'LIVE ON';
      btn.className = 'btn on';
      reconnectDelay = 1000;
      connect();
    }
  };

  window.replayLast = function() {
    var url = curBlobURL || prevBlobURL;
    if (!url) return;
    var a = new Audio(url);
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
	if len(rs.Talkgroups) == 0 {
		http.Error(w, "no talkgroups", http.StatusNotFound)
		return
	}
	ids, err := h.db.GetRecentCallIDsForTalkgroups(rs.UserID, rs.Talkgroups, 1)
	if err != nil || len(ids) == 0 {
		http.Error(w, "no calls", http.StatusNotFound)
		return
	}
	audio, audioType, _, _, _, err := h.db.GetCallAudio(ids[0])
	if err != nil {
		http.Error(w, "audio not found", http.StatusNotFound)
		return
	}
	if audioType == "" {
		audioType = "audio/mpeg"
	}
	w.Header().Set("Content-Type", audioType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(audio) //nolint:errcheck
}
