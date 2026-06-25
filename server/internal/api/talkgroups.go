package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const talkgroupResolutionInterval = 60 * time.Second

func (h *handler) handleListTalkgroupSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	settings, err := h.db.ListTalkgroupSettings()
	if err != nil {
		h.logger.Error("list talkgroup settings failed", "error", err)
		http.Error(w, "query talkgroup settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

type updateTalkgroupSettingsRequest struct {
	Favorite   bool `json:"favorite"`
	Muted      bool `json:"muted"`
	Transcribe bool `json:"transcribe"`
}

func (h *handler) handleUpsertTalkgroupSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuthenticated(w, r); !ok {
		return
	}

	talkgroup, err := strconv.Atoi(chi.URLParam(r, "talkgroup"))
	if err != nil || talkgroup <= 0 {
		http.Error(w, "invalid talkgroup", http.StatusBadRequest)
		return
	}

	var req updateTalkgroupSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s := database.TalkgroupSetting{
		Talkgroup:  talkgroup,
		Favorite:   req.Favorite,
		Muted:      req.Muted,
		Transcribe: req.Transcribe,
		UpdatedAt:  time.Now().Unix(),
	}
	if err := h.db.UpsertTalkgroupSetting(s); err != nil {
		h.logger.Error("upsert talkgroup settings failed", "error", err)
		http.Error(w, "save talkgroup settings", http.StatusInternalServerError)
		return
	}
	if h.cfg.TranscriptionWorkerToken != "" {
		if err := h.db.SkipUnselectedPendingTranscriptionJobs(); err != nil {
			h.logger.Error("apply talkgroup transcription policy failed", "error", err)
			http.Error(w, "apply transcription policy", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *handler) handleDeleteTalkgroup(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	talkgroup, err := strconv.Atoi(chi.URLParam(r, "talkgroup"))
	if err != nil || talkgroup <= 0 {
		http.Error(w, "invalid talkgroup", http.StatusBadRequest)
		return
	}

	deleted, err := h.db.DeleteTalkgroup(talkgroup)
	if err != nil {
		h.logger.Error("delete talkgroup failed", "error", err)
		http.Error(w, "delete talkgroup", http.StatusInternalServerError)
		return
	}

	_ = h.db.AppendAuditLog(admin.ID, "admin.talkgroup_deleted", "talkgroup", strconv.Itoa(talkgroup), map[string]any{"callsDeleted": deleted})
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "talkgroup": talkgroup, "callsDeleted": deleted})
}

func (h *handler) startTalkgroupResolutionLoop() {
	go func() {
		timer := time.NewTimer(talkgroupResolutionInterval)
		defer timer.Stop()
		for {
			<-timer.C
			h.resolveRadioSetTalkgroups()
			timer.Reset(talkgroupResolutionInterval)
		}
	}()
}

func (h *handler) resolveRadioSetTalkgroups() {
	identity, found, err := h.db.GetHubIdentity()
	if err != nil {
		h.logger.Error("load hub identity for talkgroup resolution failed", "error", err)
		return
	}
	var systemLabels []string
	if found {
		systemLabels = identity.IncidentSystemLabels
	}

	sets, err := h.db.ListAllRadioSets()
	if err != nil {
		h.logger.Error("list radio sets for talkgroup resolution failed", "error", err)
		return
	}
	for _, rs := range sets {
		if !rs.IsGroupsMode() || len(rs.TalkgroupGroups) == 0 {
			continue
		}
		tgIDs, resolveErr := h.db.ListDistinctTalkgroupsForGroups(rs.TalkgroupGroups, systemLabels)
		if resolveErr != nil {
			h.logger.Warn("talkgroup resolution failed for radio set",
				slog.String("radioSetId", rs.ID),
				slog.Any("groups", rs.TalkgroupGroups),
				slog.Any("error", resolveErr))
			continue
		}
		if len(tgIDs) == 0 {
			continue
		}
		if intSliceEqual(tgIDs, rs.Talkgroups) {
			continue
		}
		if updateErr := h.db.SetRadioSetTalkgroups(rs.ID, tgIDs); updateErr != nil {
			h.logger.Warn("update radio set talkgroups failed",
				slog.String("radioSetId", rs.ID),
				slog.Any("error", updateErr))
		} else {
			h.logger.Info("auto-resolved talkgroups for radio set",
				slog.String("radioSetId", rs.ID),
				slog.Int("count", len(tgIDs)))
		}
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (h *handler) handleListDistinctTalkgroups(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	tgs, err := h.db.ListDistinctTalkgroups(user.ID, isAdmin(user))
	if err != nil {
		h.logger.Error("list distinct talkgroups failed", "error", err)
		http.Error(w, "query talkgroups", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tgs)
}
