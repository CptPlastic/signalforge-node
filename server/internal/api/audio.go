package api

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func serveAudioBytes(w http.ResponseWriter, r *http.Request, audio []byte, audioType, audioName string, download bool, cacheControl string) {
	if audioType == "" {
		audioType = "audio/mpeg"
	}
	if strings.TrimSpace(audioName) == "" {
		audioName = "call-audio.mp3"
	}
	if !download && !browserPlayableAudio(audioType, audioName) {
		if mp3, err := transcodeAudioToMP3(r.Context(), audio); err == nil {
			audio = mp3
			audioType = "audio/mpeg"
			audioName = audioNameWithExt(audioName, ".mp3")
		}
	}

	headers := w.Header()
	headers.Set("Content-Type", audioType)
	if cacheControl != "" {
		headers.Set("Cache-Control", cacheControl)
	}
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	headers.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": audioName}))

	http.ServeContent(w, r, audioName, time.Time{}, bytes.NewReader(audio))
}

func defaultCallAudioName(id int64, audioName string) string {
	if strings.TrimSpace(audioName) != "" {
		return audioName
	}
	return fmt.Sprintf("call-%d.mp3", id)
}

func browserPlayableAudio(audioType, audioName string) bool {
	t := strings.ToLower(strings.TrimSpace(strings.Split(audioType, ";")[0]))
	switch t {
	case "audio/mpeg", "audio/mp3", "audio/mp4", "audio/aac", "audio/wav", "audio/wave", "audio/x-wav", "audio/ogg", "audio/webm", "audio/flac":
		return true
	case "audio/aiff", "audio/x-aiff":
		return false
	}

	switch strings.ToLower(filepath.Ext(strings.TrimSpace(audioName))) {
	case ".mp3", ".m4a", ".aac", ".wav", ".ogg", ".oga", ".webm", ".flac":
		return true
	case ".aif", ".aiff":
		return false
	}

	return strings.HasPrefix(t, "audio/")
}

func transcodeAudioToMP3(ctx context.Context, audio []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-vn", "-f", "mp3", "-codec:a", "libmp3lame", "-b:a", "64k", "pipe:1")
	cmd.Stdin = bytes.NewReader(audio)
	return cmd.Output()
}

func audioNameWithExt(audioName, ext string) string {
	trimmed := strings.TrimSpace(audioName)
	if trimmed == "" {
		return "call-audio" + ext
	}
	currentExt := filepath.Ext(trimmed)
	if currentExt == "" {
		return trimmed + ext
	}
	return strings.TrimSuffix(trimmed, currentExt) + ext
}
