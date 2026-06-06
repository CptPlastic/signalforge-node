package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func (h *handler) broadcastCall(call *database.Call, sourceID string) {
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
		h.sanitizeCallTranscription(&visibleCall)
		return wsCallEvent{Type: "call", Call: &visibleCall, SourceID: sourceID}
	})
}

// wsCallEvent is the WebSocket frame shape for a single call, used for both live
// broadcasts and ?since= replay so reconnecting clients see an identical format.
type wsCallEvent struct {
	Type     string         `json:"type"`
	Call     *database.Call `json:"call"`
	SourceID string         `json:"sourceId,omitempty"`
}

// wsReplayCompleteEvent is sent once after a reconnecting client's missed-call
// replay finishes. Its presence also advertises that the hub supports replay, so
// clients can skip their HTTP catch-up fallback.
type wsReplayCompleteEvent struct {
	Type  string `json:"type"`
	Since int64  `json:"since"`
	Count int    `json:"count"`
}

// replayMissedCalls streams calls newer than the client's ?since= cursor over the
// existing WebSocket, then sends a replay_complete sentinel. Calls are redacted
// per the connecting user exactly as live broadcasts are. It always sends the
// sentinel (even with no cursor or no calls) to advertise replay support.
func (h *handler) replayMissedCalls(hb *hub, client *hubClient, r *http.Request) {
	since := parseSinceParam(r)
	count := 0

	if since > 0 {
		count = h.replayCallsSince(hb, client, since)
	}

	data, err := json.Marshal(wsReplayCompleteEvent{Type: "replay_complete", Since: since, Count: count})
	if err != nil {
		h.logger.Error("ws replay_complete marshal failed", "error", err)
		return
	}
	hb.enqueueTo(client, data)
}

func (h *handler) replayCallsSince(hb *hub, client *hubClient, since int64) int {
	calls, err := h.db.ListCallsSince(since, 200)
	if err != nil {
		h.logger.Error("ws replay list calls failed", "error", err)
		return 0
	}
	if len(calls) == 0 {
		return 0
	}

	sourceByID, err := h.sourceAccessMap()
	if err != nil {
		h.logger.Error("ws replay list sources failed", "error", err)
		return 0
	}
	sharedSourceIDs, err := h.sharedSourceIDsForUser(client.user)
	if err != nil {
		h.logger.Error("ws replay list source shares failed", "error", err)
		sharedSourceIDs = map[string]bool{}
	}
	clearTranscripts := !h.transcriptionEnabled()

	sent := 0
	for i := range calls {
		call := calls[i]
		if !h.canReadCall(client.user, call, sourceByID, sharedSourceIDs) {
			redactCall(&call)
		}
		if clearTranscripts {
			call.TranscriptText = ""
			call.TranscriptStatus = ""
			call.TranscriptProvider = ""
		}
		data, marshalErr := json.Marshal(wsCallEvent{Type: "call", Call: &call})
		if marshalErr != nil {
			h.logger.Error("ws replay marshal call failed", "error", marshalErr)
			continue
		}
		hb.enqueueTo(client, data)
		sent++
	}
	return sent
}

// parseSinceParam reads the ?since=<callId> cursor from the WebSocket URL.
func parseSinceParam(r *http.Request) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get("since"))
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
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
	if !h.transcriptionEnabled() {
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
		Groups:     parseGroupsQuery(r.URL.Query().Get("groups")),
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

func parseGroupsQuery(raw string) []string {
	groups := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		group := strings.TrimSpace(part)
		if group != "" {
			groups = append(groups, group)
		}
	}
	return groups
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
		sanitizeCallTranscriptionFields(&calls[i])
	}
}

func sanitizeCallTranscriptionFields(call *database.Call) {
	call.TranscriptText = ""
	call.TranscriptStatus = ""
	call.TranscriptProvider = ""
}

func (h *handler) transcriptionEnabled() bool {
	return strings.TrimSpace(h.cfg.TranscriptionWorkerToken) != ""
}

func (h *handler) sanitizeCallTranscription(call *database.Call) {
	if h.transcriptionEnabled() {
		return
	}
	sanitizeCallTranscriptionFields(call)
}

func (h *handler) finalizeCallTranscription(call *database.Call) {
	call.TranscriptText = ""
	call.TranscriptProvider = ""
	if !h.transcriptionEnabled() {
		call.TranscriptStatus = ""
		return
	}
	allowed, err := h.db.ShouldTranscribeTalkgroup(call.Talkgroup)
	if err != nil {
		h.logger.Error("check talkgroup transcription policy failed", "talkgroup", call.Talkgroup, "error", err)
		if upsertErr := h.db.UpsertCallTranscriptStatus(call.ID, "pending", ""); upsertErr != nil {
			h.logger.Error("queue transcription job failed", "call_id", call.ID, "error", upsertErr)
		}
		call.TranscriptStatus = "pending"
		return
	}
	if !allowed {
		const message = "talkgroup not enabled for transcription"
		if err := h.db.UpsertCallTranscriptStatus(call.ID, "skipped", message); err != nil {
			h.logger.Error("skip unselected talkgroup transcription job failed", "call_id", call.ID, "talkgroup", call.Talkgroup, "error", err)
			call.TranscriptStatus = "pending"
			return
		}
		call.TranscriptStatus = "skipped"
		return
	}
	if h.cfg.TranscriptionMinDuration > 0 && call.Duration > 0 && call.Duration < h.cfg.TranscriptionMinDuration {
		message := fmt.Sprintf("audio duration %.1fs below %.1fs minimum", call.Duration, h.cfg.TranscriptionMinDuration)
		if err := h.db.UpsertCallTranscriptStatus(call.ID, "skipped", message); err != nil {
			h.logger.Error("skip short transcription job failed", "call_id", call.ID, "duration", call.Duration, "error", err)
			call.TranscriptStatus = "pending"
			return
		}
		call.TranscriptStatus = "skipped"
		return
	}
	if err := h.db.UpsertCallTranscriptStatus(call.ID, "pending", ""); err != nil {
		h.logger.Error("queue transcription job failed", "call_id", call.ID, "error", err)
	}
	call.TranscriptStatus = "pending"
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
