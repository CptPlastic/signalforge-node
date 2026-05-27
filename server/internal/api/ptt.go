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
