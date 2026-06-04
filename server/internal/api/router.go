package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

const radioSetByIDRoute = "/api/v1/radio-sets/{id}"

func NewRouter(logger *slog.Logger, cfg config.Config, db *database.DB) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(structuredLogger(logger))

	h := newHub(logger)
	sh := newStreamHub(logger)
	handle := newHandler(cfg, db, h, sh, logger)
	handle.startFederationSyncLoop()
	handle.startHubDirectorySyncLoop()
	r.Use(handle.withUserContext)

	// Long-lived connections — no request timeout.
	r.Get("/ws", handle.handleWebSocket)
	r.Get("/public/ws/{token}", handle.handlePublicWS)

	// All other routes run under a 30-second timeout.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(30 * time.Second))
		r.Get("/public/player/{token}", handle.handlePublicPlayer)
		r.Get("/public/last-call/{token}", handle.handlePublicLastCall)
		r.Get("/api/v1/health", handle.handleHealth)
		r.Get("/api/v1/version", handle.handleVersion)
		r.Get("/api/v1/update-check", handle.handleUpdateCheck)
		r.Post("/api/v1/auth/magic-link", handle.handleRequestMagicLink)
		r.Get("/api/v1/auth/verify", handle.handleVerifyMagicLink)
		r.Post("/api/v1/auth/verify-code", handle.handleVerifyMagicCode)
		r.Post("/api/v1/auth/logout", handle.handleLogout)
		r.Post("/api/v1/auth/refresh", handle.handleRefreshSession)
		r.Get("/api/v1/auth/me", handle.handleMe)
		r.Get("/api/v1/hub/identity", handle.handleGetHubIdentity)
		r.Put("/api/v1/hub/identity", handle.handleUpdateHubIdentity)
		r.Post("/api/v1/hub/identity/keypair", handle.handleGenerateHubKeyPair)
		r.Post("/api/v1/hub/directory/refresh", handle.handleRefreshHubDirectory)
		r.Get("/api/v1/hub/invites", handle.handleListHubInvites)
		r.Post("/api/v1/hub/invites", handle.handleCreateHubInvite)
		r.Post("/api/v1/hub/invites/accept", handle.handleAcceptHubInvite)
		r.Delete("/api/v1/hub/invites/{id}", handle.handleRevokeHubInvite)
		r.Get("/api/v1/hub/peers", handle.handleListHubPeers)
		r.Post("/api/v1/hub/peers", handle.handleConnectHubPeer)
		r.Patch("/api/v1/hub/peers/{id}/enable", handle.handleEnableHubPeer)
		r.Delete("/api/v1/hub/peers/{id}", handle.handleDeleteHubPeer)
		r.Get("/api/v1/hub/federation/status", handle.handleFederationStatus)
		r.Get("/api/v1/federation/sources", handle.handleFederationSources)
		r.Get("/api/v1/federation/calls", handle.handleFederationCalls)
		r.Get("/api/v1/users", handle.handleListUsers)
		r.Patch("/api/v1/users/{id}", handle.handleUpdateUser)
		r.Delete("/api/v1/users/{id}", handle.handleDeleteUser)
		r.Get("/api/v1/audit-logs", handle.handleListAuditLogs)
		r.Get("/api/v1/calls/groups", handle.handleListCallGroups)
		r.Get("/api/v1/calls", handle.handleListCalls)
		r.Get("/api/v1/calls/{id}/audio", handle.handleCallAudio)
		r.Post("/internal/transcription/jobs/claim", handle.handleClaimTranscriptionJob)
		r.Get("/internal/transcription/jobs/{id}/audio", handle.handleTranscriptionJobAudio)
		r.Post("/internal/transcription/jobs/{id}/complete", handle.handleCompleteTranscriptionJob)
		r.Post("/internal/transcription/jobs/{id}/skip", handle.handleSkipTranscriptionJob)
		r.Post("/internal/transcription/jobs/{id}/fail", handle.handleFailTranscriptionJob)
		r.Get("/api/v1/talkgroups/distinct", handle.handleListDistinctTalkgroups)
		r.Get("/api/v1/talkgroups/settings", handle.handleListTalkgroupSettings)
		r.Put("/api/v1/talkgroups/{talkgroup}/settings", handle.handleUpsertTalkgroupSettings)
		r.Delete("/api/v1/talkgroups/{talkgroup}", handle.handleDeleteTalkgroup)
		r.Get("/api/v1/radio-sets", handle.handleListRadioSets)
		r.Post("/api/v1/radio-sets", handle.handleCreateRadioSet)
		r.Get(radioSetByIDRoute, handle.handleGetRadioSet)
		r.Put(radioSetByIDRoute, handle.handleUpdateRadioSet)
		r.Delete(radioSetByIDRoute, handle.handleDeleteRadioSet)
		r.Post("/api/v1/radio-sets/{id}/share", handle.handleGenerateShareToken)
		r.Delete("/api/v1/radio-sets/{id}/share", handle.handleRevokeShareToken)
		r.Post("/api/v1/radio-sets/{id}/ptt", handle.handlePTTUpload)
		r.Post("/api/v1/ptt/broadcast", handle.handlePTTBroadcast)
		r.Get("/api/v1/sources", handle.handleListIngestionSources)
		r.Put("/api/v1/sources", handle.handleUpsertIngestionSource)
		r.Delete("/api/v1/sources/{id}", handle.handleDeleteIngestionSource)
		r.Get("/api/v1/sources/{id}/shares", handle.handleListSourceShares)
		r.Put("/api/v1/sources/{id}/shares", handle.handleUpdateSourceShares)
		r.Post("/api/v1/sources/{id}/keys", handle.handleGenerateSourceKey)
		r.Get("/api/v1/sources/{id}/keys", handle.handleListSourceKeys)
		r.Delete("/api/v1/sources/{id}/keys/{keyId}", handle.handleRevokeSourceKey)
		r.Post("/api/call-upload", handle.handleCallUpload) // Rdio Scanner-compatible endpoint
	})

	return r
}

func structuredLogger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("request handled",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
