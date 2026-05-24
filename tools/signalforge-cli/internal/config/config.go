package config

import (
	"os"
	"strings"
	"time"
)

const DefaultHubURL = "https://p7hub.projectseven.us"

type Config struct {
	HubURL    string
	SourceKey string
	Timeout   time.Duration
}

func FromEnv() Config {
	return Config{
		HubURL:    firstNonEmpty(os.Getenv("SIGNALFORGE_HUB_URL"), DefaultHubURL),
		SourceKey: strings.TrimSpace(os.Getenv("SIGNALFORGE_SOURCE_KEY")),
		Timeout:   20 * time.Second,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
