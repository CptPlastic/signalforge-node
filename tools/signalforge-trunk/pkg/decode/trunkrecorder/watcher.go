package trunkrecorder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CallMeta is Trunk Recorder's per-call JSON metadata.
type CallMeta struct {
	Freq                 float64 `json:"freq"`
	StartTime            int64   `json:"start_time"`
	StopTime             int64   `json:"stop_time"`
	CallLength           int     `json:"call_length"`
	Talkgroup            int     `json:"talkgroup"`
	TalkgroupTag         string  `json:"talkgroup_tag"`
	TalkgroupDescription string  `json:"talkgroup_description"`
	TalkgroupGroup       string  `json:"talkgroup_group"`
	TalkgroupGroupTag    string  `json:"talkgroup_group_tag"`
	ShortName            string  `json:"short_name"`
	Encrypted            int     `json:"encrypted"`
	AudioType            string  `json:"audio_type"`
}

// CompletedCall is a decoded TR call ready for Hub upload.
type CompletedCall struct {
	Meta      CallMeta
	AudioPath string
	JSONPath  string
}

// Watcher scans captureDir for new JSON+audio pairs.
type Watcher struct {
	dir      string
	stable   time.Duration
	onCall   func(CompletedCall)
	seen     map[string]struct{}
}

func NewWatcher(dir string, stable time.Duration, onCall func(CompletedCall)) *Watcher {
	if stable <= 0 {
		stable = 2500 * time.Millisecond
	}
	return &Watcher{dir: dir, stable: stable, onCall: onCall, seen: make(map[string]struct{})}
}

func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

func (w *Watcher) scan() {
	_ = filepath.Walk(w.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".json" {
			return nil
		}
		if _, ok := w.seen[path]; ok {
			return nil
		}
		if time.Since(info.ModTime()) < w.stable {
			return nil
		}
		call, ok := w.parseCall(path)
		if !ok {
			return nil
		}
		w.seen[path] = struct{}{}
		if w.onCall != nil {
			w.onCall(call)
		}
		return nil
	})
}

func (w *Watcher) parseCall(jsonPath string) (CompletedCall, bool) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return CompletedCall{}, false
	}
	var meta CallMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return CompletedCall{}, false
	}
	if meta.Talkgroup == 0 && meta.Freq == 0 {
		return CompletedCall{}, false
	}
	base := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath))
	for _, ext := range []string{".wav", ".m4a", ".mp3"} {
		audio := base + ext
		if _, err := os.Stat(audio); err == nil {
			return CompletedCall{Meta: meta, AudioPath: audio, JSONPath: jsonPath}, true
		}
	}
	return CompletedCall{}, false
}

// ParseCallFile reads a Trunk Recorder JSON sidecar and pairs it with audio.
func ParseCallFile(jsonPath string) (CompletedCall, bool) {
	w := &Watcher{}
	return w.parseCall(jsonPath)
}

func (c CompletedCall) Duration() time.Duration {
	if c.Meta.CallLength > 0 {
		return time.Duration(c.Meta.CallLength) * time.Second
	}
	if c.Meta.StopTime > c.Meta.StartTime {
		return time.Duration(c.Meta.StopTime-c.Meta.StartTime) * time.Second
	}
	return time.Second
}

func (c CompletedCall) StartedAt() time.Time {
	if c.Meta.StartTime > 0 {
		return time.Unix(c.Meta.StartTime, 0)
	}
	info, err := os.Stat(c.AudioPath)
	if err != nil {
		return time.Now()
	}
	return info.ModTime()
}

func (c CompletedCall) FrequencyHz() int {
	if c.Meta.Freq >= 1e6 {
		return int(c.Meta.Freq)
	}
	return int(c.Meta.Freq)
}
