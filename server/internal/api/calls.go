package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func (h *handler) broadcastCall(call *database.Call, sourceID string) {
	type wsEvent struct {
		Type     string         `json:"type"`
		Call     *database.Call `json:"call"`
		SourceID string         `json:"sourceId,omitempty"`
	}

	sources, err := h.db.ListIngestionSources(false)
	if err != nil {
		h.logger.Error("list sources for live call access failed", "error", err)
	}
	sourceByID := make(map[string]database.IngestionSource, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}

	h.hub.broadcastForUsers(func(user authUser) any {
		visibleCall := *call
		sharedSourceIDs, shareErr := h.sharedSourceIDsForUser(user)
		if shareErr != nil {
			h.logger.Error("list source shares for live call access failed", "error", shareErr)
		}
		if !h.canReadCall(user, visibleCall, sourceByID, sharedSourceIDs) {
			redactCall(&visibleCall)
		}
		return wsEvent{Type: "call", Call: &visibleCall, SourceID: sourceID}
	})
}

// handleListCallGroups returns all distinct talkgroup_group values stored in the DB.
func (h *handler) handleListCallGroups(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	groups, err := h.db.ListTalkgroupGroups(user.ID, isAdmin(user))
	if err != nil {
		h.logger.Error("list groups failed", "error", err)
		http.Error(w, "query groups", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

// handleListCalls returns recent calls (no audio blobs).
func (h *handler) handleListCalls(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}

	calls, err := h.db.ListCalls(listCallsParamsFromRequest(r))
	if err != nil {
		h.logger.Error("list calls failed", "error", err)
		http.Error(w, "query calls", http.StatusInternalServerError)
		return
	}
	sourceByID, err := h.sourceAccessMap()
	if err != nil {
		h.logger.Error("list sources for call access failed", "error", err)
		http.Error(w, "query sources", http.StatusInternalServerError)
		return
	}
	sharedSourceIDs, err := h.sharedSourceIDsForUser(user)
	if err != nil {
		h.logger.Error("list source shares for call access failed", "error", err)
		http.Error(w, "query source shares", http.StatusInternalServerError)
		return
	}
	redactCallsForUser(h, user, calls, sourceByID, sharedSourceIDs)
	if h.cfg.TranscriptionWorkerToken == "" {
		clearTranscriptStatus(calls)
	}
	writeJSON(w, http.StatusOK, calls)
}

func listCallsParamsFromRequest(r *http.Request) database.ListCallsParams {
	return database.ListCallsParams{
		Limit:      boundedQueryInt(r, "limit", 100, 1, 1000),
		Offset:     boundedQueryInt(r, "offset", 0, 0, 1_000_000),
		SortBy:     r.URL.Query().Get("sort"),
		Order:      r.URL.Query().Get("order"),
		Search:     r.URL.Query().Get("q"),
		Group:      r.URL.Query().Get("group"),
		Talkgroups: parseTalkgroupsQuery(r.URL.Query().Get("talkgroups")),
	}
}

func boundedQueryInt(r *http.Request, key string, fallback, minValue, maxValue int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < minValue || n > maxValue {
		return fallback
	}
	return n
}

func parseTalkgroupsQuery(raw string) []int {
	talkgroups := make([]int, 0)
	for _, part := range strings.Split(raw, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			talkgroups = append(talkgroups, n)
		}
	}
	return talkgroups
}

func (h *handler) sourceAccessMap() (map[string]database.IngestionSource, error) {
	sources, err := h.db.ListIngestionSources(false)
	if err != nil {
		return nil, err
	}
	sourceByID := make(map[string]database.IngestionSource, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	return sourceByID, nil
}

func redactCallsForUser(h *handler, user authUser, calls []database.Call, sourceByID map[string]database.IngestionSource, sharedSourceIDs map[string]bool) {
	for i := range calls {
		if !h.canReadCall(user, calls[i], sourceByID, sharedSourceIDs) {
			redactCall(&calls[i])
		}
	}
}

func clearTranscriptStatus(calls []database.Call) {
	for i := range calls {
		calls[i].TranscriptText = ""
		calls[i].TranscriptStatus = ""
		calls[i].TranscriptProvider = ""
	}
}

// handleCallAudio serves the raw audio bytes for a single call.
func (h *handler) handleCallAudio(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	audio, audioType, audioName, ownerUserID, sourceID, err := h.db.GetCallAudio(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("get audio failed", "error", err)
		http.Error(w, "query", http.StatusInternalServerError)
		return
	}
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	sourceByID := map[string]database.IngestionSource{}
	if sourceID != "" {
		if source, found, sourceErr := h.db.GetIngestionSource(sourceID); sourceErr != nil {
			h.logger.Error("load source for call audio failed", "error", sourceErr)
			http.Error(w, "query source", http.StatusInternalServerError)
			return
		} else if found {
			sourceByID[source.ID] = source
		}
	}
	sharedSourceIDs, err := h.sharedSourceIDsForUser(user)
	if err != nil {
		h.logger.Error("list source shares for call audio failed", "error", err)
		http.Error(w, "query source shares", http.StatusInternalServerError)
		return
	}
	if !h.canReadCall(user, database.Call{UserID: ownerUserID, SourceID: sourceID}, sourceByID, sharedSourceIDs) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	serveAudioBytes(w, r, audio, audioType, defaultCallAudioName(id, audioName), r.URL.Query().Get("download") == "1", "private, max-age=3600")
}

func (h *handler) canReadCall(user authUser, call database.Call, sourceByID map[string]database.IngestionSource, sharedSourceIDs map[string]bool) bool {
	if isAdmin(user) {
		return true
	}
	if call.SourceID != "" {
		if source, ok := sourceByID[call.SourceID]; ok {
			return source.UserID == user.ID || source.IsShared || (database.IsFederatedSourceID(call.SourceID) && source.Enabled && source.DeletedAt == 0) || sharedSourceIDs[call.SourceID]
		}
	}
	return call.UserID != "" && call.UserID == user.ID
}

func (h *handler) sharedSourceIDsForUser(user authUser) (map[string]bool, error) {
	if isAdmin(user) {
		return map[string]bool{}, nil
	}
	return h.db.ListSharedSourceIDsForUser(user.ID)
}

func redactCall(call *database.Call) {
	call.UserID = ""
	call.System = 0
	call.SystemLabel = "REDACTED"
	call.Talkgroup = 0
	call.TalkgroupLabel = "REDACTED"
	call.TalkgroupGroup = "REDACTED"
	call.TalkgroupTag = ""
	call.Frequency = 0
	call.AudioName = ""
	call.AudioType = ""
	call.Redacted = true
}
