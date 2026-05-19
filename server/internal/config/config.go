package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	ListenAddr       string
	DatabaseURL      string
	AppEnv           string
	StackID          string
	DeployTag        string
	PublicBaseURL    string
	MailFromEmail    string
	MailFromName     string
	MailjetAPIKey    string
	MailjetSecretKey string
	HubName          string
	HubPublicURL     string
	HubRegion        string
	HubContact       string
	HubFederation    bool
	UpdateCheckURL   string
	LogLevel         slog.Level
}

func Load() (Config, error) {
	publicBaseURL := strings.TrimSpace(getEnv("APP_BASE_URL", ""))
	cfg := Config{
		ListenAddr:       getEnv("LISTEN_ADDR", ":8080"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://p7scanner@localhost:5432/p7scanner?sslmode=disable"),
		AppEnv:           getEnv("APP_ENV", "development"),
		StackID:          getFirstEnv([]string{"STACK_ID", "PORTAINER_STACK_NAME", "COMPOSE_PROJECT_NAME"}, "local"),
		DeployTag:        getFirstEnv([]string{"DEPLOY_TAG", "IMAGE_TAG"}, "unknown"),
		PublicBaseURL:    publicBaseURL,
		MailFromEmail:    strings.TrimSpace(getEnv("MAIL_FROM_EMAIL", "")),
		MailFromName:     strings.TrimSpace(getEnv("MAIL_FROM_NAME", "P7 Scanner")),
		MailjetAPIKey:    strings.TrimSpace(getEnv("MAILJET_API_KEY", "")),
		MailjetSecretKey: strings.TrimSpace(getEnv("MAILJET_SECRET_KEY", "")),
		HubName:          strings.TrimSpace(getEnv("HUB_NAME", "P7 Scanner Hub")),
		HubPublicURL:     strings.TrimSpace(getEnv("HUB_PUBLIC_URL", publicBaseURL)),
		HubRegion:        strings.TrimSpace(getEnv("HUB_REGION", "")),
		HubContact:       strings.TrimSpace(getEnv("HUB_CONTACT", "")),
		HubFederation:    getBoolEnv("HUB_FEDERATION_ENABLED", false),
		UpdateCheckURL:   strings.TrimSpace(getEnv("UPDATE_CHECK_URL", "https://signalforge.org/p7-scanner-update.json")),
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
