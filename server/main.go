package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/api"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

var (
	V = "0.1.0"
	C = "unknown"
	D = "unknown"
)

func main() {
	api.BuildVersion = V
	api.BuildCommit = C
	api.BuildDate = D

	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	level := new(slog.LevelVar)
	level.Set(cfg.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	logger.Info("auth configuration",
		"passwordLoginEnabled", cfg.AuthPasswordLoginEnabled,
		"bootstrapEmailConfigured", cfg.AuthBootstrapEmail != "",
		"bootstrapPasswordConfigured", strings.TrimSpace(cfg.AuthBootstrapPassword) != "",
		"autoApproveUsers", cfg.AuthAutoApproveUsers,
	)

	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetAutoApproveUsers(cfg.AuthAutoApproveUsers)
	if err := bootstrapAuthAdmin(logger, db, cfg); err != nil {
		// Misconfigured bootstrap credentials must not take down an otherwise healthy hub.
		logger.Error("bootstrap auth admin failed; password login may be unavailable until fixed", "error", err)
	}
	if cfg.TranscriptionWorkerToken != "" {
		if err := db.EnsureTranscriptionQueueRows(); err != nil {
			logger.Error("failed to prepare transcription queue", "error", err)
			os.Exit(1)
		}
		if err := db.SkipUnselectedPendingTranscriptionJobs(); err != nil {
			logger.Error("failed to apply talkgroup transcription policy", "error", err)
			os.Exit(1)
		}
		if cfg.TranscriptionMinDuration > 0 {
			if err := db.SkipShortPendingTranscriptionJobs(cfg.TranscriptionMinDuration); err != nil {
				logger.Error("failed to skip short transcription jobs", "error", err)
				os.Exit(1)
			}
		}
	} else if err := db.CancelPendingTranscriptionJobs("transcription disabled"); err != nil {
		logger.Error("failed to clear disabled transcription queue", "error", err)
		os.Exit(1)
	}

	handler := api.NewRouter(logger, cfg, db)
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	go func() {
		logger.Info("server starting", "addr", cfg.ListenAddr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("server shutting down")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		_ = srv.Close()
	}
}

func bootstrapAuthAdmin(logger *slog.Logger, db *database.DB, cfg config.Config) error {
	email := strings.TrimSpace(cfg.AuthBootstrapEmail)
	password := cfg.AuthBootstrapPassword
	if email == "" || password == "" {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	if err := db.BootstrapAuthUser(email, string(hash)); err != nil {
		return err
	}
	logger.Info("bootstrap auth admin ensured", "email", email)
	return nil
}
