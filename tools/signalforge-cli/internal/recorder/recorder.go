package recorder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Metadata struct {
	System         int
	SystemLabel    string
	Talkgroup      int
	TalkgroupLabel string
	TalkgroupGroup string
	TalkgroupTag   string
	Frequency      int
}

type Settings struct {
	Input      string
	Processed  string
	Poll       time.Duration
	Stable     time.Duration
	Reprocess  bool
	AutoUpload bool
	Metadata   Metadata
	Canary     CanarySettings
}

type InputStatus struct {
	Path           string
	Mode           string
	Supported      bool
	AudioType      string
	SizeBytes      int64
	ModifiedAt     time.Time
	SupportedCount int
	SkippedCount   int
	Message        string
}

type FileCandidate struct {
	Path       string
	Name       string
	AudioType  string
	SizeBytes  int64
	ModifiedAt time.Time
}

func DefaultSettings() Settings {
	return Settings{
		Poll:   time.Second,
		Stable: 2500 * time.Millisecond,
		Metadata: Metadata{
			System:         1,
			SystemLabel:    "GMRS",
			Talkgroup:      18,
			TalkgroupLabel: "GMRS Channel 18",
			TalkgroupGroup: "GMRS",
			TalkgroupTag:   "voice",
			Frequency:      462625000,
		},
	}
}

func InspectInput(path string) (InputStatus, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return InputStatus{Mode: "none", Message: "set --input to a file or folder"}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return InputStatus{Path: path, Mode: "missing"}, err
	}
	status := InputStatus{Path: path, SizeBytes: info.Size(), ModifiedAt: info.ModTime()}
	if !info.IsDir() {
		status.Mode = "file"
		status.AudioType = AudioTypeForPath(path)
		status.Supported = status.AudioType != ""
		if status.Supported {
			status.Message = "ready to upload"
		} else {
			status.Message = "unsupported audio extension"
		}
		return status, nil
	}

	status.Mode = "folder"
	entries, err := os.ReadDir(path)
	if err != nil {
		return status, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if AudioTypeForPath(entry.Name()) == "" {
			status.SkippedCount++
			continue
		}
		status.SupportedCount++
	}
	status.Supported = true
	status.Message = fmt.Sprintf("%d audio file(s) ready", status.SupportedCount)
	return status, nil
}

func AudioTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	default:
		return ""
	}
}

func ValidateFileInput(path string) (InputStatus, error) {
	status, err := InspectInput(path)
	if err != nil {
		return status, err
	}
	if status.Mode != "file" {
		return status, errors.New("input must be a single audio file")
	}
	if !status.Supported {
		return status, errors.New("input must be .wav, .mp3, .m4a, or .flac")
	}
	return status, nil
}

func ReadyFiles(settings Settings, now time.Time) ([]FileCandidate, error) {
	input := strings.TrimSpace(settings.Input)
	if input == "" {
		return nil, errors.New("input folder is required")
	}
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("input must be a folder")
	}
	stable := settings.Stable
	if stable <= 0 {
		stable = 2500 * time.Millisecond
	}
	entries, err := os.ReadDir(input)
	if err != nil {
		return nil, err
	}
	candidates := make([]FileCandidate, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		audioType := AudioTypeForPath(entry.Name())
		if audioType == "" {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(entryInfo.ModTime()) < stable {
			continue
		}
		path := filepath.Join(input, entry.Name())
		candidates = append(candidates, FileCandidate{Path: path, Name: entry.Name(), AudioType: audioType, SizeBytes: entryInfo.Size(), ModifiedAt: entryInfo.ModTime()})
	}
	return candidates, nil
}

func ProcessedPath(settings Settings, sourcePath string) string {
	processed := strings.TrimSpace(settings.Processed)
	if processed == "" {
		processed = "processed"
	}
	if !filepath.IsAbs(processed) {
		processed = filepath.Join(strings.TrimSpace(settings.Input), processed)
	}
	return UniquePath(filepath.Join(processed, filepath.Base(sourcePath)))
}

func MoveToProcessed(settings Settings, sourcePath string) (string, error) {
	destination := ProcessedPath(settings, sourcePath)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(sourcePath, destination); err == nil {
		return destination, nil
	} else if !errors.Is(err, os.ErrPermission) {
		if copyErr := copyFile(sourcePath, destination); copyErr != nil {
			return "", err
		}
		if removeErr := os.Remove(sourcePath); removeErr != nil {
			return "", removeErr
		}
		return destination, nil
	}
	if err := copyFile(sourcePath, destination); err != nil {
		return "", err
	}
	if err := os.Remove(sourcePath); err != nil {
		return "", err
	}
	return destination, nil
}

func UniquePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, index, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
