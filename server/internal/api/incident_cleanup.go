package api

import (
	"sync"
	"time"
)

const (
	incidentCleanupBatchLimit   = 100
	incidentCleanupLoopInterval = 6 * time.Hour
	incidentCleanupInitialDelay = 15 * time.Minute
)

var incidentCleanupMu sync.Mutex

func (h *handler) incidentCleanupLoopEnabled() bool {
	return h.cfg.IncidentArchiveDays > 0
}

func (h *handler) startIncidentCleanupLoop() {
	if !h.incidentCleanupLoopEnabled() {
		h.logger.Info("incident archive cleanup disabled", "incidentArchiveDays", h.cfg.IncidentArchiveDays)
		return
	}
	h.logger.Info("incident archive cleanup enabled",
		"retentionDays", h.cfg.IncidentArchiveDays,
		"interval", incidentCleanupLoopInterval.String(),
	)
	go func() {
		timer := time.NewTimer(incidentCleanupInitialDelay)
		defer timer.Stop()
		for {
			<-timer.C
			purged, radioSets, err := h.purgeOldIncidents(h.cfg.IncidentArchiveDays, incidentCleanupBatchLimit)
			if err != nil {
				h.logger.Error("scheduled incident cleanup failed", "error", err)
			} else if purged > 0 {
				h.logger.Info("scheduled incident cleanup completed",
					"purged", purged,
					"radioSetsDeleted", radioSets,
					"retentionDays", h.cfg.IncidentArchiveDays,
				)
			}
			timer.Reset(incidentCleanupLoopInterval)
		}
	}()
}

func (h *handler) purgeOldIncidents(olderThanDays, limit int) (purged, radioSetsDeleted int, err error) {
	if olderThanDays <= 0 {
		return 0, 0, nil
	}
	if !incidentCleanupMu.TryLock() {
		h.logger.Info("incident cleanup skipped, already running")
		return 0, 0, nil
	}
	defer incidentCleanupMu.Unlock()

	if limit <= 0 {
		limit = incidentCleanupBatchLimit
	}
	cutoff := time.Now().AddDate(0, 0, -olderThanDays).Unix()

	for {
		incidents, listErr := h.db.ListIncidentsForPurge(cutoff, limit)
		if listErr != nil {
			return purged, radioSetsDeleted, listErr
		}
		if len(incidents) == 0 {
			return purged, radioSetsDeleted, nil
		}

		for _, inc := range incidents {
			radioSetID, delErr := h.db.DeleteIncident(inc.ID)
			if delErr != nil {
				h.logger.Warn("failed to purge incident", "incidentId", inc.ID, "error", delErr)
				continue
			}
			purged++
			_ = h.db.AppendAuditLog("", "incident.purged", "incident", inc.ID, map[string]any{
				"title":          inc.Title,
				"status":         inc.Status,
				"retentionDays":  olderThanDays,
				"radioSetId":     radioSetID,
			})
			if radioSetID == "" {
				continue
			}
			if rsErr := h.db.DeleteRadioSetByID(radioSetID); rsErr != nil {
				h.logger.Warn("failed to delete incident radio set", "incidentId", inc.ID, "radioSetId", radioSetID, "error", rsErr)
				continue
			}
			radioSetsDeleted++
		}

		if len(incidents) < limit {
			return purged, radioSetsDeleted, nil
		}
	}
}
