package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

// maxPTTSize caps the size of a single PTT upload. PTT is short-form
// audio (≤30s); anything bigger is rejected to keep abuse small.
const maxPTTSize = 10 << 20 // 10 MB

type pttUploadResponse struct {
	CallID    int64 `json:"callId"`
	Talkgroup int   `json:"talkgroup"`
}

// handlePTTUpload accepts a multipart PTT recording from an authenticated user
// and delivers it as a synthetic Call on the radio set's virtual PTT talkgroup.
//
// Multipart fields:
//
//	audio      required, the recorded clip (m4a/mp4/aac preferred, mp3 ok)
//	duration   optional float seconds; estimated from bytes if missing
//	clientId   required idempotency key — a retry with the same clientId is
//	           treated as a duplicate and returns the original callId
//
// Auth: session cookie. Caller must have tx_enabled=true. Public share tokens
// cannot reach this endpoint.
func (h *handler) handlePTTUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	if !user.TxEnabled {
		http.Error(w, "tx not enabled for this user", http.StatusForbidden)
		return
	}
	if isGuest(user) {
		http.Error(w, "guests cannot transmit", http.StatusForbidden)
		return
	}

	radioSetID := chi.URLParam(r, "id")
	rs, found, err := h.db.GetRadioSetForPTT(radioSetID)
	if err != nil {
		h.logger.Error("ptt: lookup radio set failed", "error", err, "radio_set_id", radioSetID)
		http.Error(w, "lookup radio set", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "radio set not found", http.StatusNotFound)
		return
	}
	if rs.PTTTalkgroup == nil {
		http.Error(w, "radio set has no ptt talkgroup", http.StatusBadRequest)
		return
	}

	if err := r.ParseMultipartForm(maxPTTSize); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	clientID := strings.TrimSpace(r.FormValue("clientId"))
	if clientID == "" {
		http.Error(w, "missing clientId", http.StatusBadRequest)
		return
	}
	if existingCallID, ok, err := h.db.GetPTTUploadCallID(clientID); err != nil {
		h.logger.Error("ptt: idempotency lookup failed", "error", err)
		http.Error(w, "idempotency lookup", http.StatusInternalServerError)
		return
	} else if ok {
		pttWriteJSON(w, http.StatusOK, pttUploadResponse{CallID: existingCallID, Talkgroup: *rs.PTTTalkgroup})
		return
	}

	f, audioHeader, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing audio", http.StatusBadRequest)
		return
	}
	defer f.Close()
	audio, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read audio", http.StatusInternalServerError)
		return
	}
	if len(audio) <= 44 {
		http.Error(w, "audio too small", http.StatusBadRequest)
		return
	}

	audioType := resolveAudioType("", audioHeader.Filename, audioHeader.Header.Get("Content-Type"))
	duration := formFloat(r, "duration")
	if duration == 0 {
		duration = estimateAudioDuration(audio, audioType)
	}

	now := time.Now().Unix()
	call := &database.Call{
		UserID:         rs.UserID, // owner of the set — keeps stream-hub fan-out + recent-calls seeding consistent
		DateTime:       now,
		Talkgroup:      *rs.PTTTalkgroup,
		TalkgroupLabel: ptTalkgroupLabel(rs, user),
		TalkgroupGroup: "PTT",
		Duration:       duration,
		AudioName:      audioHeader.Filename,
		AudioType:      audioType,
		Origin:         "ptt",
		SenderUserID:   user.ID,
		SenderEmail:    user.Email,
		CreatedAt:      now,
	}

	id, err := h.db.InsertCall(call, audio)
	if err != nil {
		h.logger.Error("ptt: insert call failed", "error", err)
		http.Error(w, "store call", http.StatusInternalServerError)
		return
	}
	call.ID = id

	if err := h.db.RecordPTTUpload(clientID, id, user.ID); err != nil {
		// Duplicate clientId raced past the earlier check — return the existing call ID.
		var existing int64
		if existingID, found, lookupErr := h.db.GetPTTUploadCallID(clientID); lookupErr == nil && found {
			existing = existingID
		}
		if existing != 0 && existing != id {
			h.logger.Warn("ptt: idempotency race resolved", "client_id", clientID, "winning_call_id", existing, "losing_call_id", id)
			pttWriteJSON(w, http.StatusOK, pttUploadResponse{CallID: existing, Talkgroup: *rs.PTTTalkgroup})
			return
		}
		h.logger.Error("ptt: record upload failed", "error", err)
	}

	h.prepareInsertedCallTranscriptStatus(call)
	h.streamHub.push(call, audio)
	h.broadcastCall(call, "")

	h.logger.Info("ptt call delivered",
		"call_id", id,
		"radio_set_id", radioSetID,
		"talkgroup", *rs.PTTTalkgroup,
		"sender_user_id", user.ID,
		"duration_s", duration,
	)

	pttWriteJSON(w, http.StatusOK, pttUploadResponse{CallID: id, Talkgroup: *rs.PTTTalkgroup})
}

// ptTalkgroupLabel returns a human-readable label for a PTT call.
// Uses the radio set name as the channel name; the sender email is recorded
// in SenderUserID for fine-grained display.
func ptTalkgroupLabel(rs database.RadioSet, sender authUser) string {
	name := strings.TrimSpace(rs.Name)
	if name == "" {
		name = "PTT"
	}
	return name + " · " + sender.Email
}

func pttWriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// pttBroadcastResult is the per-set outcome of a single broadcast call.
// Empty Error means the set was delivered to; CallID/Talkgroup are zero on failure.
type pttBroadcastResult struct {
	RadioSetID string `json:"radioSetId"`
	CallID     int64  `json:"callId,omitempty"`
	Talkgroup  int    `json:"talkgroup,omitempty"`
	Error      string `json:"error,omitempty"`
}

type pttBroadcastResponse struct {
	Results []pttBroadcastResult `json:"results"`
}

// handlePTTBroadcast fans a single PTT recording out to many radio sets at once.
// Used by dispatcher-mode clients to address multiple talkgroups with one keying.
//
// Multipart fields:
//
//	audio        required, the recorded clip
//	duration     optional float seconds
//	clientId     required idempotency base — per-set keys are derived as `<clientId>:<radioSetId>`
//	radioSetIds  required, comma-separated list of radio set IDs to deliver to
//
// Auth: session cookie/bearer. Caller must have both tx_enabled and dispatcher_enabled.
// Per-set failures are reported in the response Results rather than failing the whole call,
// so a partial multi-set delivery still surfaces what landed.
func (h *handler) handlePTTBroadcast(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAuthenticated(w, r)
	if !ok {
		return
	}
	if !user.TxEnabled || !user.DispatcherEnabled {
		http.Error(w, "dispatcher broadcast not enabled for this user", http.StatusForbidden)
		return
	}
	if isGuest(user) {
		http.Error(w, "guests cannot transmit", http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(maxPTTSize); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	clientID := strings.TrimSpace(r.FormValue("clientId"))
	if clientID == "" {
		http.Error(w, "missing clientId", http.StatusBadRequest)
		return
	}

	rawIDs := strings.TrimSpace(r.FormValue("radioSetIds"))
	if rawIDs == "" {
		http.Error(w, "missing radioSetIds", http.StatusBadRequest)
		return
	}
	radioSetIDs := splitAndTrim(rawIDs, ",")
	if len(radioSetIDs) == 0 {
		http.Error(w, "no radio sets specified", http.StatusBadRequest)
		return
	}

	f, audioHeader, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing audio", http.StatusBadRequest)
		return
	}
	defer f.Close()
	audio, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read audio", http.StatusInternalServerError)
		return
	}
	if len(audio) <= 44 {
		http.Error(w, "audio too small", http.StatusBadRequest)
		return
	}

	audioType := resolveAudioType("", audioHeader.Filename, audioHeader.Header.Get("Content-Type"))
	duration := formFloat(r, "duration")
	if duration == 0 {
		duration = estimateAudioDuration(audio, audioType)
	}

	results := make([]pttBroadcastResult, 0, len(radioSetIDs))
	for _, radioSetID := range radioSetIDs {
		results = append(results, h.deliverPTTToSet(user, radioSetID, clientID, audio, audioHeader.Filename, audioType, duration))
	}

	h.logger.Info("ptt broadcast delivered",
		"sender_user_id", user.ID,
		"radio_set_count", len(radioSetIDs),
		"audio_bytes", len(audio),
	)

	pttWriteJSON(w, http.StatusOK, pttBroadcastResponse{Results: results})
}

// deliverPTTToSet inserts a synthetic PTT call onto one radio set's PTT talkgroup
// and pushes it through the stream hub + authenticated WS. Per-set idempotency
// is enforced via a derived clientId (`<base>:<setId>`) so retries are safe.
func (h *handler) deliverPTTToSet(
	user authUser,
	radioSetID, baseClientID string,
	audio []byte,
	audioName, audioType string,
	duration float64,
) pttBroadcastResult {
	clientID := baseClientID + ":" + radioSetID

	rs, found, err := h.db.GetRadioSetForPTT(radioSetID)
	if err != nil {
		h.logger.Error("ptt broadcast: lookup radio set failed", "error", err, "radio_set_id", radioSetID)
		return pttBroadcastResult{RadioSetID: radioSetID, Error: "lookup radio set"}
	}
	if !found {
		return pttBroadcastResult{RadioSetID: radioSetID, Error: "not found"}
	}
	if rs.PTTTalkgroup == nil {
		return pttBroadcastResult{RadioSetID: radioSetID, Error: "no ptt talkgroup"}
	}

	if existingCallID, ok, err := h.db.GetPTTUploadCallID(clientID); err != nil {
		h.logger.Error("ptt broadcast: idempotency lookup failed", "error", err, "radio_set_id", radioSetID)
		return pttBroadcastResult{RadioSetID: radioSetID, Error: "idempotency lookup"}
	} else if ok {
		return pttBroadcastResult{RadioSetID: radioSetID, CallID: existingCallID, Talkgroup: *rs.PTTTalkgroup}
	}

	now := time.Now().Unix()
	call := &database.Call{
		UserID:         rs.UserID,
		DateTime:       now,
		Talkgroup:      *rs.PTTTalkgroup,
		TalkgroupLabel: ptTalkgroupLabel(rs, user),
		TalkgroupGroup: "PTT",
		Duration:       duration,
		AudioName:      audioName,
		AudioType:      audioType,
		Origin:         "ptt",
		SenderUserID:   user.ID,
		SenderEmail:    user.Email,
		CreatedAt:      now,
	}
	id, err := h.db.InsertCall(call, audio)
	if err != nil {
		h.logger.Error("ptt broadcast: insert call failed", "error", err, "radio_set_id", radioSetID)
		return pttBroadcastResult{RadioSetID: radioSetID, Error: "store call"}
	}
	call.ID = id

	if err := h.db.RecordPTTUpload(clientID, id, user.ID); err != nil {
		if existingID, found, lookupErr := h.db.GetPTTUploadCallID(clientID); lookupErr == nil && found && existingID != id {
			h.logger.Warn("ptt broadcast: idempotency race resolved",
				"client_id", clientID, "winning_call_id", existingID, "losing_call_id", id,
			)
			return pttBroadcastResult{RadioSetID: radioSetID, CallID: existingID, Talkgroup: *rs.PTTTalkgroup}
		}
		h.logger.Error("ptt broadcast: record upload failed", "error", err)
	}

	h.prepareInsertedCallTranscriptStatus(call)
	h.streamHub.push(call, audio)
	h.broadcastCall(call, "")

	return pttBroadcastResult{RadioSetID: radioSetID, CallID: id, Talkgroup: *rs.PTTTalkgroup}
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
