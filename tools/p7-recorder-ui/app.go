package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var version = "dev"

type App struct {
	ctx       context.Context
	mu        sync.Mutex
	recorder  *exec.Cmd
	cancelRun context.CancelFunc
}

type RecorderConfig struct {
	BaseURL                        string `json:"baseUrl"`
	SourceKey                      string `json:"sourceKey"`
	TimeoutSec                     int    `json:"timeoutSec"`
	Device                         string `json:"device"`
	SampleRate                     int    `json:"sampleRate"`
	Channels                       int    `json:"channels"`
	BlockMS                        int    `json:"blockMs"`
	Threshold                      int    `json:"threshold"`
	SilenceMS                      int    `json:"silenceMs"`
	MinDurationMS                  int    `json:"minDurationMs"`
	MaxDurationSec                 int    `json:"maxDurationSec"`
	PreRollMS                      int    `json:"preRollMs"`
	System                         int    `json:"system"`
	SystemLabel                    string `json:"systemLabel"`
	Talkgroup                      int    `json:"talkgroup"`
	TalkgroupLabel                 string `json:"talkgroupLabel"`
	TalkgroupGroup                 string `json:"talkgroupGroup"`
	TalkgroupTag                   string `json:"talkgroupTag"`
	Frequency                      int    `json:"frequency"`
	QueueDirectory                 string `json:"queueDirectory"`
	FolderIngestEnabled            bool   `json:"folderIngestEnabled"`
	FolderIngestDirectory          string `json:"folderIngestDirectory"`
	FolderIngestProcessedDirectory string `json:"folderIngestProcessedDirectory"`
	FolderIngestReprocessProcessed bool   `json:"folderIngestReprocessProcessed"`
	FolderIngestPollMS             int    `json:"folderIngestPollMs"`
	FolderIngestStableMS           int    `json:"folderIngestStableMs"`
	CanaryEnabled                  bool   `json:"canaryEnabled"`
	CanaryIntervalSec              int    `json:"canaryIntervalSec"`
	CanaryTalkgroup                int    `json:"canaryTalkgroup"`
	CanaryTalkgroupLabel           string `json:"canaryTalkgroupLabel"`
	ConfigPath                     string `json:"configPath"`
}

type tomlConfig struct {
	P7 struct {
		BaseURL    string `toml:"base_url"`
		SourceKey  string `toml:"source_key"`
		TimeoutSec int    `toml:"timeout_sec"`
	} `toml:"p7"`
	Audio struct {
		Device         string `toml:"device"`
		SampleRate     int    `toml:"sample_rate"`
		Channels       int    `toml:"channels"`
		BlockMS        int    `toml:"block_ms"`
		Threshold      int    `toml:"threshold"`
		SilenceMS      int    `toml:"silence_ms"`
		MinDurationMS  int    `toml:"min_duration_ms"`
		MaxDurationSec int    `toml:"max_duration_sec"`
		PreRollMS      int    `toml:"pre_roll_ms"`
	} `toml:"audio"`
	Metadata struct {
		System         int    `toml:"system"`
		SystemLabel    string `toml:"system_label"`
		Talkgroup      int    `toml:"talkgroup"`
		TalkgroupLabel string `toml:"talkgroup_label"`
		TalkgroupGroup string `toml:"talkgroup_group"`
		TalkgroupTag   string `toml:"talkgroup_tag"`
		Frequency      int    `toml:"frequency"`
	} `toml:"metadata"`
	Queue struct {
		Directory string `toml:"directory"`
	} `toml:"queue"`
	FolderIngest struct {
		Enabled            bool   `toml:"enabled"`
		Directory          string `toml:"directory"`
		ProcessedDirectory string `toml:"processed_directory"`
		ReprocessProcessed bool   `toml:"reprocess_processed"`
		PollMS             int    `toml:"poll_ms"`
		StableMS           int    `toml:"stable_ms"`
	} `toml:"folder_ingest"`
	Canary struct {
		Enabled        bool   `toml:"enabled"`
		IntervalSec    int    `toml:"interval_sec"`
		Talkgroup      int    `toml:"talkgroup"`
		TalkgroupLabel string `toml:"talkgroup_label"`
	} `toml:"canary"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.emitLog("UI READY")
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.StopRecorder()
}

func (a *App) LoadConfig(path string) (RecorderConfig, error) {
	path = normalizeConfigPath(path)
	cfg := defaultRecorderConfig(path)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	} else if err != nil {
		return cfg, err
	}

	parsed := tomlConfig{}
	if _, err := toml.DecodeFile(path, &parsed); err != nil {
		return cfg, err
	}
	cfg.BaseURL = firstNonEmpty(parsed.P7.BaseURL, cfg.BaseURL)
	cfg.SourceKey = parsed.P7.SourceKey
	cfg.TimeoutSec = firstPositive(parsed.P7.TimeoutSec, cfg.TimeoutSec)
	cfg.Device = parsed.Audio.Device
	cfg.SampleRate = firstPositive(parsed.Audio.SampleRate, cfg.SampleRate)
	cfg.Channels = firstPositive(parsed.Audio.Channels, cfg.Channels)
	cfg.BlockMS = firstPositive(parsed.Audio.BlockMS, cfg.BlockMS)
	cfg.Threshold = firstPositive(parsed.Audio.Threshold, cfg.Threshold)
	cfg.SilenceMS = firstPositive(parsed.Audio.SilenceMS, cfg.SilenceMS)
	cfg.MinDurationMS = firstPositive(parsed.Audio.MinDurationMS, cfg.MinDurationMS)
	cfg.MaxDurationSec = firstPositive(parsed.Audio.MaxDurationSec, cfg.MaxDurationSec)
	cfg.PreRollMS = firstPositive(parsed.Audio.PreRollMS, cfg.PreRollMS)
	cfg.System = firstPositive(parsed.Metadata.System, cfg.System)
	cfg.SystemLabel = firstNonEmpty(parsed.Metadata.SystemLabel, cfg.SystemLabel)
	cfg.Talkgroup = firstPositive(parsed.Metadata.Talkgroup, cfg.Talkgroup)
	cfg.TalkgroupLabel = firstNonEmpty(parsed.Metadata.TalkgroupLabel, cfg.TalkgroupLabel)
	cfg.TalkgroupGroup = firstNonEmpty(parsed.Metadata.TalkgroupGroup, cfg.TalkgroupGroup)
	cfg.TalkgroupTag = firstNonEmpty(parsed.Metadata.TalkgroupTag, cfg.TalkgroupTag)
	cfg.Frequency = firstPositive(parsed.Metadata.Frequency, cfg.Frequency)
	cfg.QueueDirectory = firstNonEmpty(parsed.Queue.Directory, cfg.QueueDirectory)
	cfg.FolderIngestEnabled = parsed.FolderIngest.Enabled
	cfg.FolderIngestDirectory = firstNonEmpty(parsed.FolderIngest.Directory, cfg.FolderIngestDirectory)
	cfg.FolderIngestProcessedDirectory = firstNonEmpty(parsed.FolderIngest.ProcessedDirectory, cfg.FolderIngestProcessedDirectory)
	cfg.FolderIngestReprocessProcessed = parsed.FolderIngest.ReprocessProcessed
	cfg.FolderIngestPollMS = firstPositive(parsed.FolderIngest.PollMS, cfg.FolderIngestPollMS)
	cfg.FolderIngestStableMS = firstPositive(parsed.FolderIngest.StableMS, cfg.FolderIngestStableMS)
	cfg.CanaryEnabled = parsed.Canary.Enabled
	cfg.CanaryIntervalSec = firstPositive(parsed.Canary.IntervalSec, cfg.CanaryIntervalSec)
	cfg.CanaryTalkgroup = parsed.Canary.Talkgroup
	cfg.CanaryTalkgroupLabel = parsed.Canary.TalkgroupLabel
	return cfg, nil
}

func (a *App) SaveConfig(cfg RecorderConfig) error {
	cfg.ConfigPath = normalizeConfigPath(cfg.ConfigPath)
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return errors.New("server URL is required")
	}
	if strings.TrimSpace(cfg.SourceKey) == "" || strings.HasPrefix(strings.TrimSpace(cfg.SourceKey), "sk_live_REPLACE") {
		return errors.New("source API key is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cfg.ConfigPath, []byte(renderConfig(cfg)), 0o600); err != nil {
		return err
	}
	a.emitLog("CONFIG SAVED " + cfg.ConfigPath)
	return nil
}

func (a *App) ListDevices() (string, error) {
	output, err := runRecorderCommand(context.Background(), "--list-devices")
	return strings.TrimSpace(output), err
}

func (a *App) StartRecorder(cfg RecorderConfig) error {
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.recorder != nil && a.recorder.Process != nil {
		return errors.New("recorder is already running")
	}

	runCtx, cancel := context.WithCancel(context.Background())
	cmd, err := recorderCommand(runCtx, "--config", normalizeConfigPath(cfg.ConfigPath))
	if err != nil {
		cancel()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	a.recorder = cmd
	a.cancelRun = cancel
	a.emitLog("RECORDER STARTED")
	go a.pipeLogs(stdout)
	go a.pipeLogs(stderr)
	go a.waitRecorder(cmd)
	return nil
}

func (a *App) StopRecorder() error {
	a.mu.Lock()
	cmd := a.recorder
	cancel := a.cancelRun
	a.recorder = nil
	a.cancelRun = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		a.emitLog("RECORDER STOPPED")
	}
	return nil
}

func (a *App) OpenURL(url string) error {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	wailsruntime.BrowserOpenURL(a.ctx, url)
	return nil
}

func (a *App) GetVersion() string {
	return version
}

func (a *App) pipeLogs(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		a.emitLog(scanner.Text())
	}
}

func (a *App) waitRecorder(cmd *exec.Cmd) {
	err := cmd.Wait()
	a.mu.Lock()
	if a.recorder == cmd {
		a.recorder = nil
		a.cancelRun = nil
	}
	a.mu.Unlock()
	if err != nil {
		a.emitLog("RECORDER EXIT " + err.Error())
		return
	}
	a.emitLog("RECORDER EXIT OK")
}

func (a *App) emitLog(message string) {
	line := time.Now().Format("15:04:05") + " " + strings.TrimSpace(message)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "recorder:log", line)
	}
}

func runRecorderCommand(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd, err := recorderCommand(ctx, args...)
	if err != nil {
		return "", err
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func recorderCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	if sidecar := findSidecarRecorder(); sidecar != "" {
		return exec.CommandContext(ctx, sidecar, args...), nil
	}
	devRoot, err := findDevRecorderRoot()
	if err != nil {
		return nil, err
	}
	goArgs := append([]string{"run", "./cmd/p7-recorder-go"}, args...)
	cmd := exec.CommandContext(ctx, "go", goArgs...)
	cmd.Dir = devRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	return cmd, nil
}

func findSidecarRecorder() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "p7-recorder-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidate := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func findDevRecorderRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Clean(filepath.Join(cwd, "..", "p7-recorder-go")),
		filepath.Clean(filepath.Join(cwd, "tools", "p7-recorder-go")),
		filepath.Clean(filepath.Join(cwd, "..", "..", "tools", "p7-recorder-go")),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "cmd", "p7-recorder-go", "main.go")); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("p7-recorder-go binary was not found beside the UI and the development recorder source was not found")
}

func defaultRecorderConfig(path string) RecorderConfig {
	return RecorderConfig{
		BaseURL:                        "https://p7scan.projectseven.us/",
		SourceKey:                      "sk_live_REPLACE_WITH_GENERATED_SOURCE_KEY",
		TimeoutSec:                     20,
		Device:                         "",
		SampleRate:                     16000,
		Channels:                       1,
		BlockMS:                        100,
		Threshold:                      500,
		SilenceMS:                      1200,
		MinDurationMS:                  400,
		MaxDurationSec:                 120,
		PreRollMS:                      300,
		System:                         1,
		SystemLabel:                    "GMRS",
		Talkgroup:                      18,
		TalkgroupLabel:                 "GMRS Channel 18",
		TalkgroupGroup:                 "GMRS",
		TalkgroupTag:                   "voice",
		Frequency:                      462625000,
		QueueDirectory:                 "queue",
		FolderIngestEnabled:            false,
		FolderIngestDirectory:          "ingest",
		FolderIngestProcessedDirectory: "processed",
		FolderIngestReprocessProcessed: false,
		FolderIngestPollMS:             1000,
		FolderIngestStableMS:           2500,
		CanaryEnabled:                  false,
		CanaryIntervalSec:              300,
		CanaryTalkgroup:                0,
		CanaryTalkgroupLabel:           "",
		ConfigPath:                     path,
	}
}

func normalizeConfigPath(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		return path
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "P7 Recorder", "config.toml")
}

func renderConfig(cfg RecorderConfig) string {
	return fmt.Sprintf(`[p7]
base_url = %s
source_key = %s
timeout_sec = %d

[audio]
device = %s
sample_rate = %d
channels = %d
block_ms = %d
threshold = %d
silence_ms = %d
min_duration_ms = %d
max_duration_sec = %d
pre_roll_ms = %d

[metadata]
system = %d
system_label = %s
talkgroup = %d
talkgroup_label = %s
talkgroup_group = %s
talkgroup_tag = %s
frequency = %d

[queue]
directory = %s

[folder_ingest]
enabled = %t
directory = %s
processed_directory = %s
reprocess_processed = %t
poll_ms = %d
stable_ms = %d

[canary]
enabled = %t
interval_sec = %d
talkgroup = %d
talkgroup_label = %s
`, strconv.Quote(strings.TrimSpace(cfg.BaseURL)), strconv.Quote(strings.TrimSpace(cfg.SourceKey)), firstPositive(cfg.TimeoutSec, 20), strconv.Quote(strings.TrimSpace(cfg.Device)), firstPositive(cfg.SampleRate, 16000), firstPositive(cfg.Channels, 1), firstPositive(cfg.BlockMS, 100), firstPositive(cfg.Threshold, 500), firstPositive(cfg.SilenceMS, 1200), firstPositive(cfg.MinDurationMS, 400), firstPositive(cfg.MaxDurationSec, 120), firstPositive(cfg.PreRollMS, 300), firstPositive(cfg.System, 1), strconv.Quote(strings.TrimSpace(cfg.SystemLabel)), firstPositive(cfg.Talkgroup, 18), strconv.Quote(strings.TrimSpace(cfg.TalkgroupLabel)), strconv.Quote(strings.TrimSpace(cfg.TalkgroupGroup)), strconv.Quote(firstNonEmpty(cfg.TalkgroupTag, "voice")), firstPositive(cfg.Frequency, 462625000), strconv.Quote(firstNonEmpty(cfg.QueueDirectory, "queue")), cfg.FolderIngestEnabled, strconv.Quote(firstNonEmpty(cfg.FolderIngestDirectory, "ingest")), strconv.Quote(firstNonEmpty(cfg.FolderIngestProcessedDirectory, "processed")), cfg.FolderIngestReprocessProcessed, firstPositive(cfg.FolderIngestPollMS, 1000), firstPositive(cfg.FolderIngestStableMS, 2500), cfg.CanaryEnabled, firstPositive(cfg.CanaryIntervalSec, 300), cfg.CanaryTalkgroup, strconv.Quote(cfg.CanaryTalkgroupLabel))
}

func firstPositive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
