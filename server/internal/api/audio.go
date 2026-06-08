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
	// WebM/Opus PTT from older browser uploads is not playable on iOS; serve M4A instead.
	if pttAudioNeedsNormalize(audioType) {
		if m4a, err := transcodeAudioToM4A(r.Context(), audio); err == nil {
			audio = m4a
			audioType = pttPreferredAudioType
			audioName = audioNameWithExt(audioName, ".m4a")
		}
	} else if !download && !browserPlayableAudio(audioType, audioName) {
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

func audioIsMP4Container(audio []byte) bool {
	return len(audio) >= 12 && bytes.Equal(audio[4:8], []byte("ftyp"))
}

func audioIsWAV(audio []byte) bool {
	return len(audio) >= 12 && bytes.Equal(audio[0:4], []byte("RIFF")) && bytes.Equal(audio[8:12], []byte("WAVE"))
}

// streamAudioIsMP3 reports whether bytes are already MP3 (not merely labeled audio/mpeg).
func streamAudioIsMP3(audioType string, audio []byte) bool {
	if audioIsMP4Container(audio) || audioIsWAV(audio) {
		return false
	}
	if len(audio) >= 3 && audio[0] == 'I' && audio[1] == 'D' && audio[2] == '3' {
		return true
	}
	if len(audio) >= 2 && audio[0] == 0xFF && (audio[1]&0xE0) == 0xE0 {
		return true
	}
	return readMP3Bitrate(audio) > 0
}

// preparePublicStreamAudio transcodes to MP3 when wantMP3 is set and the clip is not already MP3.
// On transcode failure the original bytes and type are returned with the error.
func preparePublicStreamAudio(ctx context.Context, audio []byte, audioType string, wantMP3 bool) ([]byte, string, error) {
	if !wantMP3 || len(audio) == 0 {
		if audioType == "" {
			return audio, "audio/mpeg", nil
		}
		return audio, audioType, nil
	}
	if streamAudioIsMP3(audioType, audio) {
		return audio, "audio/mpeg", nil
	}
	mp3, err := transcodeAudioToMP3(ctx, audio)
	if err != nil {
		return audio, audioType, err
	}
	return mp3, "audio/mpeg", nil
}

func transcodeAudioToMP3(ctx context.Context, audio []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-vn", "-f", "mp3", "-codec:a", "libmp3lame", "-b:a", "64k", "pipe:1")
	cmd.Stdin = bytes.NewReader(audio)
	return cmd.Output()
}

// pttPreferredAudioType is what mobile (iOS) and the web console store/play without conversion.
const pttPreferredAudioType = "audio/mp4"

// pttAudioNeedsNormalize reports whether uploaded PTT should be transcoded to M4A/AAC.
func pttAudioNeedsNormalize(audioType string) bool {
	t := strings.ToLower(strings.TrimSpace(audioType))
	if strings.Contains(t, "opus") {
		return true
	}
	base := strings.Split(t, ";")[0]
	switch base {
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/aac", "audio/mpeg", "audio/mp3":
		return false
	default:
		return true
	}
}

func transcodeAudioToM4A(ctx context.Context, audio []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-c:a", "aac",
		"-b:a", "64k",
		"-movflags", "+faststart",
		"-f", "mp4",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audio)
	return cmd.Output()
}

// normalizePTTAudio converts browser WebM/Opus clips to M4A so all clients share one format.
func normalizePTTAudio(ctx context.Context, audio []byte, audioType, audioName string) ([]byte, string, string, error) {
	if !pttAudioNeedsNormalize(audioType) {
		if audioType == "" {
			audioType = pttPreferredAudioType
		}
		if strings.TrimSpace(audioName) == "" {
			audioName = "ptt.m4a"
		}
		return audio, audioType, audioName, nil
	}
	out, err := transcodeAudioToM4A(ctx, audio)
	if err != nil {
		return nil, "", "", fmt.Errorf("transcode ptt to m4a: %w", err)
	}
	return out, pttPreferredAudioType, audioNameWithExt(audioName, ".m4a"), nil
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
