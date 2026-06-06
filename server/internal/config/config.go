package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	ListenAddr               string
	DatabaseURL              string
	AppEnv                   string
	StackID                  string
	DeployTag                string
	PublicBaseURL            string
	MailFromEmail            string
	MailFromName             string
	MailjetAPIKey            string
	MailjetSecretKey         string
	HubName                  string
	HubPublicURL             string
	HubRegion                string
	HubContact               string
	HubFederation            bool
	HubTrustLevel            string
	HubTrustIssuer           string
	HubTrustCert             string
	HubTrustExpires          int64
	HubDirectoryURL          string
	UpdateCheckURL           string
	TranscriptionWorkerToken string
	TranscriptionMinDuration float64
	AuthBootstrapEmail       string
	AuthBootstrapPassword    string
	AuthAutoApproveUsers     bool
	AuthPasswordLoginEnabled bool
	LogLevel                 slog.Level
}

func Load() (Config, error) {
	publicBaseURL := strings.TrimSpace(getEnv("APP_BASE_URL", ""))
	cfg := Config{
		ListenAddr:               getEnv("LISTEN_ADDR", ":8080"),
		DatabaseURL:              getEnv("DATABASE_URL", "postgres://p7scanner@localhost:5432/p7scanner?sslmode=disable"),
		AppEnv:                   getEnv("APP_ENV", "development"),
		StackID:                  getFirstEnv([]string{"STACK_ID", "PORTAINER_STACK_NAME", "COMPOSE_PROJECT_NAME"}, "local"),
		DeployTag:                getFirstEnv([]string{"DEPLOY_TAG", "IMAGE_TAG"}, "unknown"),
		PublicBaseURL:            publicBaseURL,
		MailFromEmail:            strings.TrimSpace(getEnv("MAIL_FROM_EMAIL", "")),
		MailFromName:             strings.TrimSpace(getEnv("MAIL_FROM_NAME", "SignalForge Hub")),
		MailjetAPIKey:            strings.TrimSpace(getEnv("MAILJET_API_KEY", "")),
		MailjetSecretKey:         strings.TrimSpace(getEnv("MAILJET_SECRET_KEY", "")),
		HubName:                  strings.TrimSpace(getEnv("HUB_NAME", "SignalForge Hub")),
		HubPublicURL:             strings.TrimSpace(getEnv("HUB_PUBLIC_URL", publicBaseURL)),
		HubRegion:                strings.TrimSpace(getEnv("HUB_REGION", "")),
		HubContact:               strings.TrimSpace(getEnv("HUB_CONTACT", "")),
		HubFederation:            getBoolEnv("HUB_FEDERATION_ENABLED", false),
		HubTrustLevel:            normalizeHubTrustLevel(getEnv("HUB_TRUST_LEVEL", "community")),
		HubTrustIssuer:           strings.TrimSpace(getEnv("HUB_TRUST_ISSUER", "")),
		HubTrustCert:             strings.TrimSpace(getEnv("HUB_TRUST_CERTIFICATE", "")),
		HubTrustExpires:          getInt64Env("HUB_TRUST_EXPIRES_AT", 0),
		HubDirectoryURL:          strings.TrimSpace(getEnv("HUB_DIRECTORY_URL", "https://signalforge.org/directory/hubs.json")),
		UpdateCheckURL:           strings.TrimSpace(getEnv("UPDATE_CHECK_URL", "https://signalforge.org/p7-scanner-update.json")),
		TranscriptionWorkerToken: strings.TrimSpace(getEnv("TRANSCRIPTION_WORKER_TOKEN", "")),
		TranscriptionMinDuration: getFloat64Env("TRANSCRIPTION_MIN_DURATION_SECONDS", 0.75),
		AuthBootstrapEmail:       strings.ToLower(strings.TrimSpace(getEnv("AUTH_BOOTSTRAP_EMAIL", ""))),
		AuthBootstrapPassword:    getEnv("AUTH_BOOTSTRAP_PASSWORD", ""),
		AuthAutoApproveUsers:     getBoolEnv("AUTH_AUTO_APPROVE_USERS", false),
		AuthPasswordLoginEnabled: getBoolEnv("AUTH_PASSWORD_LOGIN_ENABLED", false),
	}

	logLevel := getEnv("LOG_LEVEL", "info")
	if err := cfg.LogLevel.UnmarshalText([]byte(logLevel)); err != nil {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: %w", logLevel, err)
	}

	switch cfg.AppEnv {
	case "development", "staging", "production", "test":
	default:
		return Config{}, fmt.Errorf("invalid APP_ENV %q", cfg.AppEnv)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getFirstEnv(keys []string, fallback string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func getInt64Env(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int64
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func getFloat64Env(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func normalizeHubTrustLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "listed", "verified", "trusted", "official", "suspended":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "community"
	}
}
