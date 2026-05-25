package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

type claimTranscriptionJobRequest struct {
	WorkerID     string `json:"workerId"`
	LeaseSeconds int64  `json:"leaseSeconds"`
}

type completeTranscriptionJobRequest struct {
	Transcript string  `json:"transcript"`
	Provider   string  `json:"provider"`
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
}

type failTranscriptionJobRequest struct {
	Error string `json:"error"`
}

const invalidJSONMessage = "invalid json"

func (h *handler) requireTranscriptionWorker(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(h.cfg.TranscriptionWorkerToken)
	if expected == "" {
		http.Error(w, "transcription disabled", http.StatusNotFound)
		return false
	}

	actual := strings.TrimSpace(r.Header.Get("X-Transcription-Token"))
	if actual == "" {
		actual = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	}
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (h *handler) handleClaimTranscriptionJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireTranscriptionWorker(w, r) {
		return
	}

	var req claimTranscriptionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		workerID = "local-transcriber"
	}
	job, err := h.db.ClaimTranscriptionJob(workerID, req.LeaseSeconds)
	if database.IsNoTranscriptionJob(err) {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err != nil {
		h.logger.Error("claim transcription job failed", "error", err)
		http.Error(w, "claim job", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *handler) handleTranscriptionJobAudio(w http.ResponseWriter, r *http.Request) {
	if !h.requireTranscriptionWorker(w, r) {
		return
	}
	callID, ok := transcriptionJobID(w, r)
	if !ok {
		return
	}

	audio, audioType, audioName, _, _, err := h.db.GetCallAudio(callID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("get transcription audio failed", "call_id", callID, "error", err)
		http.Error(w, "query audio", http.StatusInternalServerError)
		return
	}
	serveAudioBytes(w, r, audio, audioType, defaultCallAudioName(callID, audioName), true, "no-store")
}

func (h *handler) handleCompleteTranscriptionJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireTranscriptionWorker(w, r) {
		return
	}
	callID, ok := transcriptionJobID(w, r)
	if !ok {
		return
	}

	var req completeTranscriptionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}
	transcript := strings.TrimSpace(req.Transcript)
	if transcript == "" {
		http.Error(w, "transcript required", http.StatusBadRequest)
		return
	}
	if err := h.db.CompleteTranscriptionJob(callID, transcript, strings.TrimSpace(req.Provider), strings.TrimSpace(req.Language), req.Confidence); err != nil {
		h.logger.Error("complete transcription job failed", "call_id", callID, "error", err)
		http.Error(w, "complete job", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) handleFailTranscriptionJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireTranscriptionWorker(w, r) {
		return
	}
	callID, ok := transcriptionJobID(w, r)
	if !ok {
		return
	}

	var req failTranscriptionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(req.Error)
	if message == "" {
		message = "transcription failed"
	}
	if err := h.db.FailTranscriptionJob(callID, message); err != nil {
		h.logger.Error("fail transcription job failed", "call_id", callID, "error", err)
		http.Error(w, "fail job", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) handleSkipTranscriptionJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireTranscriptionWorker(w, r) {
		return
	}
	callID, ok := transcriptionJobID(w, r)
	if !ok {
		return
	}

	var req failTranscriptionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidJSONMessage, http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(req.Error)
	if message == "" {
		message = "transcription skipped"
	}
	if err := h.db.SkipTranscriptionJob(callID, message); err != nil {
		h.logger.Error("skip transcription job failed", "call_id", callID, "error", err)
		http.Error(w, "skip job", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func transcriptionJobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}
