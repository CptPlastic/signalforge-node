package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

var callArchiveVacuumMu sync.Mutex

const (
	callArchiveBatchLimit       = 500
	callArchiveMaxSweepDuration = 4 * time.Hour
	callArchiveLoopInterval     = 6 * time.Hour
)

type callArchiveManifest struct {
	ID              int64   `json:"id"`
	UserID          string  `json:"userId,omitempty"`
	SourceID        string  `json:"sourceId,omitempty"`
	DateTime        int64   `json:"dateTime"`
	System          int     `json:"system"`
	SystemLabel     string  `json:"systemLabel"`
	Talkgroup       int     `json:"talkgroup"`
	TalkgroupLabel  string  `json:"talkgroupLabel"`
	TalkgroupGroup  string  `json:"talkgroupGroup"`
	TalkgroupTag    string  `json:"talkgroupTag"`
	Frequency       int     `json:"frequency"`
	Duration        float64 `json:"duration"`
	AudioName       string  `json:"audioName"`
	AudioType       string  `json:"audioType"`
	TranscriptText  string  `json:"transcriptText,omitempty"`
	TranscriptStatus string `json:"transcriptStatus,omitempty"`
	Origin          string  `json:"origin,omitempty"`
	SenderUserID    string  `json:"senderUserId,omitempty"`
	CreatedAt       int64   `json:"createdAt"`
	AudioFile       string  `json:"audioFile"`
	ArchivedAt      int64   `json:"archivedAt"`
}

type archiveCallsRequest struct {
	OlderThanDays int   `json:"olderThanDays"`
	DryRun        bool  `json:"dryRun"`
	Limit         int   `json:"limit"`
	UntilEmpty    *bool `json:"untilEmpty"`
	Async         *bool `json:"async"`
}

type archiveCallsResult struct {
	CutoffUnix    int64  `json:"cutoffUnix"`
	Archived      int    `json:"archived"`
	Deleted       int    `json:"deleted"`
	FreedBytes    int64  `json:"freedBytes"`
	ArchiveDir    string `json:"archiveDir"`
	S3URI         string `json:"s3Uri,omitempty"`
	S3DirsSynced  int    `json:"s3DirsSynced"`
	LocalRemoved  bool   `json:"localRemoved"`
	VacuumQueued  bool   `json:"vacuumQueued"`
	DryRun        bool   `json:"dryRun"`
	RemainingOld  int64  `json:"remainingOld"`
	Batches       int    `json:"batches"`
	BatchLimit    int    `json:"batchLimit"`
	StoppedEarly  bool   `json:"stoppedEarly"`
	Note          string `json:"note,omitempty"`
}

type archiveCallsBatchResult struct {
	Archived     int
	Deleted      int
	FreedBytes   int64
	S3DirsSynced int
	LocalRemoved bool
	RemainingOld int64
}

func (h *handler) handleCallStorageStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	stats, err := h.db.GetCallStorageStats()
	if err != nil {
		h.logger.Error("call storage stats failed", "error", err)
		http.Error(w, "query storage stats", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"callCount":                   stats.CallCount,
		"audioBytes":                  stats.AudioBytes,
		"oldestCallAt":                stats.OldestCallAt,
		"newestCallAt":                stats.NewestCallAt,
		"retentionDays":               h.cfg.CallRetentionDays,
		"retentionDaysFromEnv":        os.Getenv("CALL_RETENTION_DAYS") != "",
		"archiveDir":                  h.cfg.CallArchiveDir,
		"archiveDirFromEnv":           strings.TrimSpace(os.Getenv("CALL_ARCHIVE_DIR")) != "",
		"archiveS3Uri":                h.cfg.CallArchiveS3URI,
		"archiveS3Cfg":                h.cfg.CallArchiveS3Cfg,
		"archiveDeleteLocalAfterS3":   h.cfg.CallArchiveDeleteLocalAfterS3,
		"archiveLoopEnabled":          h.callArchiveLoopEnabled(),
	})
}

func (h *handler) handleArchiveCalls(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	var req archiveCallsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if archiveAsync(req) {
		status, code, err := h.startArchiveJob(admin.ID, req)
		if err != nil {
			h.logger.Error("start archive job failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, code, status)
		return
	}

	result, err := h.archiveCalls(req)
	if err != nil {
		h.logger.Error("archive calls failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = h.db.AppendAuditLog(admin.ID, "admin.calls_archived", "calls", "batch", map[string]any{
		"archived":      result.Archived,
		"deleted":       result.Deleted,
		"freedBytes":    result.FreedBytes,
		"cutoffUnix":    result.CutoffUnix,
		"dryRun":        result.DryRun,
		"olderThanDays": req.OlderThanDays,
		"batches":       result.Batches,
		"untilEmpty":    archiveUntilEmpty(req),
		"stoppedEarly":  result.StoppedEarly,
		"remainingOld":  result.RemainingOld,
		"async":         false,
	})
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) callArchiveLoopEnabled() bool {
	return h.cfg.CallRetentionDays > 0 && strings.TrimSpace(h.cfg.CallArchiveDir) != ""
}

func (h *handler) startCallArchiveLoop() {
	if !h.callArchiveLoopEnabled() {
		return
	}
	go func() {
		timer := time.NewTimer(10 * time.Minute)
		defer timer.Stop()
		for {
			<-timer.C
			if !archiveSweepMu.TryLock() {
				h.logger.Info("scheduled call archive skipped, sweep already running")
				timer.Reset(callArchiveLoopInterval)
				continue
			}
			untilEmpty := true
			result, err := h.archiveCallsWithProgress(archiveCallsRequest{
				OlderThanDays: h.cfg.CallRetentionDays,
				Limit:         callArchiveBatchLimit,
				UntilEmpty:    &untilEmpty,
			}, nil)
			archiveSweepMu.Unlock()
			if err != nil {
				h.logger.Error("scheduled call archive failed", "error", err)
			} else if result.Archived > 0 {
				h.logger.Info("scheduled call archive completed",
					"archived", result.Archived,
					"deleted", result.Deleted,
					"freed_bytes", result.FreedBytes,
					"s3_dirs_synced", result.S3DirsSynced,
					"remaining_old", result.RemainingOld,
				)
			}
			timer.Reset(callArchiveLoopInterval)
		}
	}()
}

func archiveUntilEmpty(req archiveCallsRequest) bool {
	if req.DryRun {
		return false
	}
	if req.UntilEmpty == nil {
		return true
	}
	return *req.UntilEmpty
}

func (h *handler) archiveCalls(req archiveCallsRequest) (archiveCallsResult, error) {
	archiveDir := strings.TrimSpace(h.cfg.CallArchiveDir)
	if archiveDir == "" {
		return archiveCallsResult{}, fmt.Errorf("CALL_ARCHIVE_DIR is not configured")
	}
	days := req.OlderThanDays
	if days <= 0 {
		days = h.cfg.CallRetentionDays
	}
	if days <= 0 {
		return archiveCallsResult{}, fmt.Errorf("olderThanDays must be > 0 (or set CALL_RETENTION_DAYS)")
	}

	if req.DryRun {
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
		remaining, err := h.db.CountCallsOlderThan(cutoff)
		if err != nil {
			return archiveCallsResult{}, fmt.Errorf("count old calls: %w", err)
		}
		totalFreedBytes, err := h.db.SumAudioBytesOlderThan(cutoff)
		if err != nil {
			return archiveCallsResult{}, fmt.Errorf("sum audio bytes: %w", err)
		}
		return archiveCallsResult{
			CutoffUnix:   cutoff,
			ArchiveDir:   archiveDir,
			S3URI:        strings.TrimSpace(h.cfg.CallArchiveS3URI),
			DryRun:       true,
			RemainingOld: remaining,
			FreedBytes:   totalFreedBytes,
			Note:         "Archives run in batches until the backlog is cleared. VACUUM FULL runs automatically when remainingOld reaches 0.",
		}, nil
	}

	return h.archiveCallsWithProgress(req, nil)
}

func (h *handler) archiveCallsBatch(archiveDir string, cutoff int64, limit int) (archiveCallsBatchResult, error) {
	remaining, err := h.db.CountCallsOlderThan(cutoff)
	if err != nil {
		return archiveCallsBatchResult{}, fmt.Errorf("count old calls: %w", err)
	}
	out := archiveCallsBatchResult{RemainingOld: remaining}
	if remaining == 0 {
		return out, nil
	}

	records, err := h.db.ListCallsForArchive(cutoff, limit)
	if err != nil {
		return archiveCallsBatchResult{}, fmt.Errorf("list calls for archive: %w", err)
	}
	if len(records) == 0 {
		return out, nil
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return archiveCallsBatchResult{}, fmt.Errorf("create archive dir: %w", err)
	}

	ids := make([]int64, 0, len(records))
	dayDirs := make([]string, 0)
	seenDays := make(map[string]struct{})
	var batchFreed int64
	for _, rec := range records {
		day := time.Unix(rec.Call.DateTime, 0).UTC().Format("2006-01-02")
		if _, err := writeCallArchiveFile(archiveDir, rec); err != nil {
			return archiveCallsBatchResult{}, err
		}
		ids = append(ids, rec.Call.ID)
		batchFreed += int64(len(rec.Audio))
		out.Archived++
		if _, ok := seenDays[day]; !ok {
			seenDays[day] = struct{}{}
			dayDirs = append(dayDirs, day)
		}
	}

	s3URI := strings.TrimSpace(h.cfg.CallArchiveS3URI)
	if s3URI != "" {
		synced, err := syncArchiveDirsToS3(context.Background(), h.logger, s3URI, h.cfg.CallArchiveS3Cfg, archiveDir, dayDirs)
		if err != nil {
			return archiveCallsBatchResult{}, err
		}
		out.S3DirsSynced = synced
	}

	deleted, err := h.db.DeleteCallsByIDs(ids)
	if err != nil {
		return archiveCallsBatchResult{}, fmt.Errorf("delete archived calls: %w", err)
	}
	out.Deleted = int(deleted)
	out.FreedBytes = batchFreed
	out.RemainingOld = remaining - deleted

	if s3URI != "" && h.cfg.CallArchiveDeleteLocalAfterS3 && len(dayDirs) > 0 {
		if err := removeArchiveDayDirs(archiveDir, dayDirs); err != nil {
			return out, fmt.Errorf("remove local archive after s3 sync: %w", err)
		}
		out.LocalRemoved = true
	}
	return out, nil
}

// scheduleCallsVacuum runs after deletes. Intermediate batches get a light VACUUM;
// when remainingOld hits zero, VACUUM FULL shrinks the table file back to the OS.
func (h *handler) scheduleCallsVacuum(deleted int, remainingOld int64) bool {
	if deleted <= 0 {
		return false
	}
	full := h.cfg.CallArchiveVacuumFull && remainingOld == 0
	go func() {
		callArchiveVacuumMu.Lock()
		defer callArchiveVacuumMu.Unlock()
		start := time.Now()
		mode := "analyze"
		if full {
			mode = "full"
		}
		h.logger.Info("call archive vacuum starting", "mode", mode, "deleted", deleted, "remaining_old", remainingOld)
		if err := h.db.VacuumCallsTable(full); err != nil {
			h.logger.Error("call archive vacuum failed", "mode", mode, "error", err)
			return
		}
		h.logger.Info("call archive vacuum completed", "mode", mode, "duration_ms", time.Since(start).Milliseconds())
	}()
	return true
}

func writeCallArchiveFile(archiveDir string, rec database.CallArchiveRecord) (string, error) {
	dayDir := filepath.Join(archiveDir, time.Unix(rec.Call.DateTime, 0).UTC().Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", fmt.Errorf("create day archive dir: %w", err)
	}

	ext := archiveAudioExtension(rec.Call.AudioName, rec.Call.AudioType)
	audioFile := fmt.Sprintf("call-%d%s", rec.Call.ID, ext)
	audioPath := filepath.Join(dayDir, audioFile)
	if err := os.WriteFile(audioPath, rec.Audio, 0o644); err != nil {
		return "", fmt.Errorf("write audio for call %d: %w", rec.Call.ID, err)
	}

	manifest := callArchiveManifest{
		ID:               rec.Call.ID,
		UserID:           rec.Call.UserID,
		SourceID:         rec.Call.SourceID,
		DateTime:         rec.Call.DateTime,
		System:           rec.Call.System,
		SystemLabel:      rec.Call.SystemLabel,
		Talkgroup:        rec.Call.Talkgroup,
		TalkgroupLabel:   rec.Call.TalkgroupLabel,
		TalkgroupGroup:   rec.Call.TalkgroupGroup,
		TalkgroupTag:     rec.Call.TalkgroupTag,
		Frequency:        rec.Call.Frequency,
		Duration:         rec.Call.Duration,
		AudioName:        rec.Call.AudioName,
		AudioType:        rec.Call.AudioType,
		TranscriptText:   rec.TranscriptText,
		TranscriptStatus: rec.Call.TranscriptStatus,
		Origin:           rec.Call.Origin,
		SenderUserID:     rec.Call.SenderUserID,
		CreatedAt:        rec.Call.CreatedAt,
		AudioFile:        audioFile,
		ArchivedAt:       time.Now().Unix(),
	}
	metaPath := filepath.Join(dayDir, fmt.Sprintf("call-%d.json", rec.Call.ID))
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest for call %d: %w", rec.Call.ID, err)
	}
	if err := os.WriteFile(metaPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write manifest for call %d: %w", rec.Call.ID, err)
	}
	return audioFile, nil
}

func archiveAudioExtension(audioName, audioType string) string {
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(audioName))); ext != "" {
		return ext
	}
	t := strings.ToLower(strings.TrimSpace(strings.Split(audioType, ";")[0]))
	switch {
	case strings.Contains(t, "mpeg"), strings.Contains(t, "mp3"):
		return ".mp3"
	case strings.Contains(t, "mp4"), strings.Contains(t, "m4a"), strings.Contains(t, "aac"):
		return ".m4a"
	case strings.Contains(t, "wav"):
		return ".wav"
	case strings.Contains(t, "webm"):
		return ".webm"
	case strings.Contains(t, "ogg"):
		return ".ogg"
	default:
		return ".bin"
	}
}
