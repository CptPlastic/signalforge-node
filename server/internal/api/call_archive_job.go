package api

import (
	"fmt"
	"net/http"
	"sync"
	"strings"
	"time"
)

var (
	archiveJobMu   sync.Mutex
	archiveSweepMu sync.Mutex
	archiveJob     *archiveJobStatus
)

type archiveJobStatus struct {
	Running          bool   `json:"running"`
	Phase            string `json:"phase"`
	StatusLine       string `json:"statusLine"`
	Error            string `json:"error,omitempty"`
	StartedAt        int64  `json:"startedAt,omitempty"`
	UpdatedAt        int64  `json:"updatedAt,omitempty"`
	LastBatchSize    int    `json:"lastBatchSize"`
	InitialRemaining int64  `json:"initialRemaining"`
	archiveCallsResult
}

func archiveAsync(req archiveCallsRequest) bool {
	if req.DryRun {
		return false
	}
	if req.Async == nil {
		return true
	}
	return *req.Async
}

func (h *handler) handleArchiveJobStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.snapshotArchiveJob())
}

func (h *handler) snapshotArchiveJob() archiveJobStatus {
	archiveJobMu.Lock()
	defer archiveJobMu.Unlock()
	if archiveJob == nil {
		return archiveJobStatus{Phase: "idle", StatusLine: "No archive job has run yet."}
	}
	return *archiveJob
}

func (h *handler) startArchiveJob(adminID string, req archiveCallsRequest) (archiveJobStatus, int, error) {
	untilEmpty := true
	req.UntilEmpty = &untilEmpty
	if req.Limit <= 0 {
		req.Limit = 100
	}

	archiveJobMu.Lock()
	if archiveJob != nil && archiveJob.Running {
		status := *archiveJob
		archiveJobMu.Unlock()
		return status, http.StatusOK, nil
	}

	now := time.Now().Unix()
	archiveJob = &archiveJobStatus{
		Running:    true,
		Phase:      "starting",
		StatusLine: "Starting archive sweep…",
		StartedAt:  now,
		UpdatedAt:  now,
		archiveCallsResult: archiveCallsResult{
			Note: "Archive runs in the background. Poll GET /api/v1/admin/calls/archive/status for progress.",
		},
	}
	archiveJobMu.Unlock()

	go h.runArchiveJob(adminID, req)
	return h.snapshotArchiveJob(), http.StatusAccepted, nil
}

func (h *handler) updateArchiveJob(mutate func(job *archiveJobStatus)) {
	archiveJobMu.Lock()
	defer archiveJobMu.Unlock()
	if archiveJob == nil {
		return
	}
	mutate(archiveJob)
	archiveJob.UpdatedAt = time.Now().Unix()
}

func (h *handler) runArchiveJob(adminID string, req archiveCallsRequest) {
	if !archiveSweepMu.TryLock() {
		h.updateArchiveJob(func(job *archiveJobStatus) {
			job.Running = false
			job.Phase = "error"
			job.Error = "another archive sweep is already running"
			job.StatusLine = job.Error
		})
		return
	}
	defer archiveSweepMu.Unlock()

	result, err := h.archiveCallsWithProgress(req, func(batch int, batchResult archiveCallsBatchResult, total *archiveCallsResult) {
		h.updateArchiveJob(func(job *archiveJobStatus) {
			if job.InitialRemaining == 0 && batchResult.Archived > 0 {
				job.InitialRemaining = batchResult.RemainingOld + int64(batchResult.Archived)
			}
			job.Phase = "running"
			job.LastBatchSize = batchResult.Archived
			job.archiveCallsResult = *total
			if batchResult.Archived > 0 {
				job.StatusLine = fmt.Sprintf(
					"Batch %d: exported %d calls — %d remaining…",
					batch,
					batchResult.Archived,
					batchResult.RemainingOld,
				)
			} else {
				job.StatusLine = fmt.Sprintf("Batch %d in progress…", batch)
			}
		})
	})

	h.updateArchiveJob(func(job *archiveJobStatus) {
		job.Running = false
		job.archiveCallsResult = result
		job.LastBatchSize = 0
		if err != nil {
			job.Phase = "error"
			job.Error = err.Error()
			job.StatusLine = "Archive failed: " + err.Error()
			return
		}
		switch {
		case result.StoppedEarly:
			job.Phase = "paused"
			job.StatusLine = fmt.Sprintf(
				"Paused — %d archived, %d remaining. Run again to continue.",
				result.Archived,
				result.RemainingOld,
			)
		case result.Archived == 0:
			job.Phase = "done"
			job.StatusLine = "Nothing to archive."
		default:
			job.Phase = "done"
			job.StatusLine = fmt.Sprintf(
				"Complete — %d calls archived, %s freed.",
				result.Archived,
				formatArchiveBytes(result.FreedBytes),
			)
		}
	})

	if err == nil && result.Archived > 0 {
		_ = h.db.AppendAuditLog(adminID, "admin.calls_archived", "calls", "batch", map[string]any{
			"archived":      result.Archived,
			"deleted":       result.Deleted,
			"freedBytes":    result.FreedBytes,
			"cutoffUnix":    result.CutoffUnix,
			"dryRun":        false,
			"olderThanDays": req.OlderThanDays,
			"batches":       result.Batches,
			"async":         true,
			"stoppedEarly":  result.StoppedEarly,
			"remainingOld":  result.RemainingOld,
		})
	}
}

func formatArchiveBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

type archiveProgressFn func(batch int, batchResult archiveCallsBatchResult, total *archiveCallsResult)

func (h *handler) archiveCallsWithProgress(req archiveCallsRequest, onProgress archiveProgressFn) (archiveCallsResult, error) {
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
	limit := req.Limit
	if limit <= 0 || limit > callArchiveBatchLimit {
		limit = callArchiveBatchLimit
	}

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	result := archiveCallsResult{
		CutoffUnix: cutoff,
		ArchiveDir: archiveDir,
		S3URI:      strings.TrimSpace(h.cfg.CallArchiveS3URI),
		BatchLimit: limit,
	}

	untilEmpty := archiveUntilEmpty(req)
	deadline := time.Now().Add(callArchiveMaxSweepDuration)
	for {
		result.Batches++
		if onProgress != nil {
			onProgress(result.Batches, archiveCallsBatchResult{}, &result)
		}

		batch, err := h.archiveCallsBatch(archiveDir, cutoff, limit)
		if err != nil {
			return result, err
		}
		result.Archived += batch.Archived
		result.Deleted += batch.Deleted
		result.FreedBytes += batch.FreedBytes
		result.S3DirsSynced += batch.S3DirsSynced
		result.LocalRemoved = result.LocalRemoved || batch.LocalRemoved
		result.RemainingOld = batch.RemainingOld

		if onProgress != nil {
			onProgress(result.Batches, batch, &result)
		}

		if batch.Archived == 0 || batch.RemainingOld == 0 || !untilEmpty {
			break
		}
		if time.Now().After(deadline) {
			result.StoppedEarly = true
			result.Note = fmt.Sprintf(
				"Sweep paused after %s (safety limit). %d calls remain — run again or wait for the 6-hour scheduler.",
				callArchiveMaxSweepDuration,
				batch.RemainingOld,
			)
			break
		}
	}

	if result.Deleted > 0 {
		result.VacuumQueued = h.scheduleCallsVacuum(result.Deleted, result.RemainingOld)
	}
	return result, nil
}
