package config

import (
	"os"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/profile"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/recorder"
)

const DefaultHubURL = "https://p7hub.projectseven.us"

type Config struct {
	HubURL    string
	SourceKey string
	Timeout   time.Duration
}

func FromEnv() Config {
	return mergeProfile(Config{
		HubURL:    firstNonEmpty(os.Getenv("SIGNALFORGE_HUB_URL"), DefaultHubURL),
		SourceKey: strings.TrimSpace(os.Getenv("SIGNALFORGE_SOURCE_KEY")),
		Timeout:   20 * time.Second,
	})
}

func mergeProfile(cfg Config) Config {
	prof, err := profile.Load()
	if err != nil {
		return cfg
	}
	if os.Getenv("SIGNALFORGE_HUB_URL") == "" && strings.TrimSpace(prof.HubURL) != "" {
		cfg.HubURL = strings.TrimSpace(prof.HubURL)
	}
	if os.Getenv("SIGNALFORGE_SOURCE_KEY") == "" && strings.TrimSpace(prof.SourceKey) != "" {
		cfg.SourceKey = strings.TrimSpace(prof.SourceKey)
	}
	if prof.TimeoutSec > 0 {
		cfg.Timeout = time.Duration(prof.TimeoutSec) * time.Second
	}
	return cfg
}

func LoadRecorderSettings() recorder.Settings {
	settings := recorder.DefaultSettings()
	prof, err := profile.Load()
	if err != nil {
		return settings
	}
	return prof.ToRecorderSettings()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
