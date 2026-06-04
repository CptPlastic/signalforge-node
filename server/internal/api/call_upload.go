package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const maxUploadSize = 50 << 20 // 50 MB
const uploadKeyRequired = true

const sourceLookupFailedMessage = "source lookup failed"

var (
	errMissingUploadKey = errors.New("missing upload key")
	errInvalidUploadKey = errors.New("invalid upload key")
)

// handleCallUpload accepts a Rdio Scanner-compatible multipart POST and stores the call.
//
// Expected multipart fields:
//
//	key            required API key
//	system         integer system ID
//	systemLabel    human-readable system name
//	talkgroup      integer talkgroup ID
//	talkgroupLabel human-readable talkgroup name
//	talkgroupGroup talkgroup group / category
//	talkgroupTag   talkgroup tag
//	dateTime       Unix timestamp (seconds)
//	frequency      frequency in Hz
//	duration       call duration in seconds (float)
//	audioName      filename for the audio file
//	audioType      MIME type (default: audio/mpeg)
//	audio          audio file (binary)
func (h *handler) handleCallUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	uploadKey := r.FormValue("key")
	src, metricsSourceID, err := h.resolveUploadSource(uploadKey)
	if err != nil {
		if errors.Is(err, errMissingUploadKey) {
			http.Error(w, "missing upload key", http.StatusUnauthorized)
			return
		}
		if errors.Is(err, errInvalidUploadKey) {
			http.Error(w, "invalid upload key", http.StatusUnauthorized)
			return
		}
		h.logger.Error(sourceLookupFailedMessage, "error", err)
		http.Error(w, sourceLookupFailedMessage, http.StatusInternalServerError)
		return
	}

	// Reject calls from disabled sources.
	if src != nil && !src.Enabled {
		http.Error(w, "source disabled", http.StatusForbidden)
		return
	}

	// SDRTrunk connectivity probes arrive with test=1 and no real call data.
	// Return the expected message so SDRTrunk transitions to CONNECTED without
	// recording an error against the source.
	if r.FormValue("test") == "1" {
		http.Error(w, "incomplete call data: no talkgroup", http.StatusExpectationFailed)
		return
	}

	call := &database.Call{
		UserID:         src.UserID,
		SourceID:       metricsSourceID,
		System:         formInt(r, "system"),
		SystemLabel:    r.FormValue("systemLabel"),
		Talkgroup:      formInt(r, "talkgroup"),
		TalkgroupLabel: r.FormValue("talkgroupLabel"),
		TalkgroupGroup: r.FormValue("talkgroupGroup"),
		TalkgroupTag:   r.FormValue("talkgroupTag"),
		Frequency:      formInt(r, "frequency"),
		Duration:       formFloat(r, "duration"),
		AudioName:      r.FormValue("audioName"),
		AudioType:      r.FormValue("audioType"),
		CreatedAt:      time.Now().Unix(),
	}
	if call.System == 0 {
		call.System = formInt(r, "systemId")
	}
	if call.System == 0 {
		call.System = formInt(r, "system_id")
	}
	if call.Talkgroup == 0 {
		call.Talkgroup = formInt(r, "talkgroupId")
	}
	if call.Talkgroup == 0 {
		call.Talkgroup = formInt(r, "talkgroup_id")
	}
	if call.Talkgroup == 0 {
		call.Talkgroup = formInt(r, "tgid")
	}
	if call.SystemLabel == "" {
		call.SystemLabel = r.FormValue("system_label")
	}
	if call.TalkgroupLabel == "" {
		call.TalkgroupLabel = r.FormValue("talkgroup_name")
	}
	if call.TalkgroupLabel == "" {
		call.TalkgroupLabel = r.FormValue("talkgroup_label")
	}
	if call.TalkgroupGroup == "" {
		call.TalkgroupGroup = r.FormValue("talkgroup_group")
	}
	if call.TalkgroupTag == "" {
		call.TalkgroupTag = r.FormValue("talkgroup_tag")
	}
	call.DateTime = parseDateTimeField(r.FormValue("dateTime"), call.CreatedAt)
	if call.DateTime == call.CreatedAt {
		if dt := parseDateTimeField(r.FormValue("start_time"), call.CreatedAt); dt != call.CreatedAt {
			call.DateTime = dt
		}
	}
	if call.DateTime == 0 {
		call.DateTime = call.CreatedAt
	}
	audio := []byte{}
	hasAudio := false
	audioPartType := ""
	f, audioHeader, err := r.FormFile("audio")
	if err != nil {
		f, audioHeader, err = r.FormFile("file")
	}
	if err != nil {
		f, audioHeader, err = r.FormFile("call")
	}
	if err == nil {
		hasAudio = true
		audioPartType = audioHeader.Header.Get("Content-Type")
		if call.AudioName == "" {
			call.AudioName = audioHeader.Filename
		}
		defer f.Close()
		audio, err = io.ReadAll(f)
		if err != nil {
			http.Error(w, "read audio", http.StatusInternalServerError)
			return
		}
	}
	call.AudioType = resolveAudioType(call.AudioType, call.AudioName, audioPartType)

	// SDRTrunk has no DURATION form field; estimate from audio payload size.
	if call.Duration == 0 && len(audio) > 0 {
		call.Duration = estimateAudioDuration(audio, call.AudioType)
	}

	if err := validateCallUpload(call, audio, hasAudio); err != nil {
		h.logger.Info("incomplete call upload",
			"source_id", metricsSourceID,
			"reason", err.Error(),
			"system", call.System,
			"talkgroup", call.Talkgroup,
		)
		_ = h.db.IncrementSourceMetrics(metricsSourceID, false)
		http.Error(w, fmt.Sprintf("incomplete call data: %s", err.Error()), http.StatusExpectationFailed)
		return
	}

	id, err := h.db.InsertCall(call, audio)
	if err != nil {
		h.logger.Error("insert call failed", "error", err)
		_ = h.db.IncrementSourceMetrics(metricsSourceID, false)
		http.Error(w, "store call", http.StatusInternalServerError)
		return
	}
	call.ID = id
	h.prepareInsertedCallTranscriptStatus(call)
	_ = h.db.IncrementSourceMetrics(metricsSourceID, true)

	h.logger.Info("call received",
		"id", id,
		"system", call.SystemLabel,
		"talkgroup", call.TalkgroupLabel,
		"freq_hz", call.Frequency,
		"duration_s", call.Duration,
	)

	h.broadcastCall(call, metricsSourceID)
	h.streamHub.push(call, audio)

	// SDRTrunk checks the response body for this exact string to confirm success.
	// Returning JSON causes TEMPORARY_BROADCAST_ERROR even on HTTP 200.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Call imported successfully.\n")
}

func (h *handler) resolveUploadSource(uploadKey string) (*database.IngestionSource, string, error) {
	if uploadKey == "" {
		return nil, "", errMissingUploadKey
	}

	if apiKeySrc, found, err := h.db.GetSourceByAPIKey(uploadKey); err != nil {
		return nil, "", err
	} else if found {
		_ = h.db.UpdateKeyLastUsed(uploadKey)
		return &apiKeySrc, apiKeySrc.ID, nil
	}

	return nil, "", errInvalidUploadKey
}

func formInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.FormValue(key))
	return n
}

func formInt64(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(r.FormValue(key), 10, 64)
	return n
}

func formFloat(r *http.Request, key string) float64 {
	f, _ := strconv.ParseFloat(r.FormValue(key), 64)
	return f
}

func parseDateTimeField(value string, fallback int64) int64 {
	v := strings.TrimSpace(value)
	if v == "" {
		return fallback
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
		return n
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.Unix()
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.999", v, time.Local); err == nil {
		return t.UTC().Unix()
	}
	return fallback
}

func validateCallUpload(call *database.Call, audio []byte, hasAudio bool) error {
	var err error
	if !hasAudio || len(audio) <= 44 {
		err = errors.New("no audio")
	}
	if call.DateTime <= 0 {
		err = errors.New("no datetime")
	}
	if call.System < 1 {
		err = errors.New("no system")
	}
	if call.Talkgroup < 1 {
		err = errors.New("no talkgroup")
	}
	return err
}

func resolveAudioType(uploadType, audioName, audioPartType string) string {
	if t := normalizeAudioType(uploadType); t != "" {
		return t
	}
	if t := normalizeAudioType(audioPartType); t != "" {
		return t
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(audioName)))
	switch ext {
	case ".m4a":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".aac":
		return "audio/aac"
	case ".webm":
		return "audio/webm"
	case ".ogg", ".oga":
		return "audio/ogg"
	}

	if ext != "" {
		if t := normalizeAudioType(mime.TypeByExtension(ext)); t != "" {
			return t
		}
	}

	return "audio/mpeg"
}

func normalizeAudioType(value string) string {
	t := strings.ToLower(strings.TrimSpace(value))
	if t == "" || t == "application/octet-stream" {
		return ""
	}
	switch t {
	case "audio/x-m4a", "audio/m4a":
		return "audio/mp4"
	}
	if strings.HasPrefix(t, "audio/") {
		return t
	}
	return ""
}

// mpeg1Layer3Bitrates maps the 4-bit bitrate index to bps for MPEG-1 Layer 3 (MP3).
var mpeg1Layer3Bitrates = [16]int{0, 32000, 40000, 48000, 56000, 64000, 80000, 96000, 112000, 128000, 160000, 192000, 224000, 256000, 320000, 0}

// readMP3Bitrate scans data for the first valid MPEG-1 Layer-3 sync word and
// returns the encoded bitrate in bits-per-second, or 0 if not found.
func readMP3Bitrate(data []byte) int {
	for i := 0; i+3 < len(data); i++ {
		if data[i] != 0xFF || data[i+1]&0xE0 != 0xE0 {
			continue
		}
		idx := (data[i+2] >> 4) & 0x0F
		if idx == 0 || idx == 15 {
			continue
		}
		return mpeg1Layer3Bitrates[idx]
	}
	return 0
}

// estimateAudioDuration estimates playback duration from raw audio bytes.
// For MP3 it reads the first frame header to detect the actual bitrate.
// For all other types it falls back to a 64 kbps assumption.
func estimateAudioDuration(audio []byte, audioType string) float64 {
	if len(audio) == 0 {
		return 0
	}
	bitrateBps := 64000
	if strings.Contains(audioType, "mpeg") {
		if br := readMP3Bitrate(audio); br > 0 {
			bitrateBps = br
		}
	}
	return float64(len(audio)) * 8.0 / float64(bitrateBps)
}
