package recorder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MinBeaconInterval = 30 * time.Second

type BeaconSettings struct {
	Enabled        bool
	FilePath       string
	Interval       time.Duration
	Talkgroup      int
	TalkgroupLabel string
}

func (s Settings) BeaconMetadata() Metadata {
	meta := s.Metadata
	if s.Beacon.Talkgroup > 0 {
		meta.Talkgroup = s.Beacon.Talkgroup
	}
	if strings.TrimSpace(s.Beacon.TalkgroupLabel) != "" {
		meta.TalkgroupLabel = s.Beacon.TalkgroupLabel
	} else {
		meta.TalkgroupLabel = "BEACON"
	}
	return meta
}

func NormalizeBeaconInterval(interval time.Duration) time.Duration {
	if interval < MinBeaconInterval {
		return MinBeaconInterval
	}
	return interval
}

func ValidateBeaconFile(path string) (InputStatus, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return InputStatus{}, fmt.Errorf("beacon audio file path is required")
	}
	return ValidateFileInput(path)
}

func BeaconUploadName(path string, now time.Time) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "beacon"
	}
	return fmt.Sprintf("%s-%d%s", stem, now.Unix(), ext)
}

func (s Settings) BeaconSourcePath() string {
	return strings.TrimSpace(s.Beacon.FilePath)
}

func (s Settings) BeaconFileMatches(path string) bool {
	beaconPath := s.BeaconSourcePath()
	if beaconPath == "" {
		return false
	}
	absBeacon, err := filepath.Abs(beaconPath)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return absBeacon == absPath
}

func FileModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}
