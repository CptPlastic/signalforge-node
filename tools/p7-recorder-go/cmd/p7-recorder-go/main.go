package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

const appName = "P7 Recorder Go"

var version = "dev"

type Config struct {
	P7           P7Config           `toml:"p7"`
	Audio        AudioConfig        `toml:"audio"`
	Metadata     MetadataConfig     `toml:"metadata"`
	Queue        QueueConfig        `toml:"queue"`
	FolderIngest FolderIngestConfig `toml:"folder_ingest"`
	Canary       CanaryConfig       `toml:"canary"`
}

type P7Config struct {
	BaseURL    string  `toml:"base_url"`
	SourceKey  string  `toml:"source_key"`
	TimeoutSec float64 `toml:"timeout_sec"`
}

type AudioConfig struct {
	Device         string `toml:"device"`
	SampleRate     int    `toml:"sample_rate"`
	Channels       int    `toml:"channels"`
	BlockMS        int    `toml:"block_ms"`
	Threshold      int    `toml:"threshold"`
	SilenceMS      int    `toml:"silence_ms"`
	MinDurationMS  int    `toml:"min_duration_ms"`
	MaxDurationSec int    `toml:"max_duration_sec"`
	PreRollMS      int    `toml:"pre_roll_ms"`
}

type MetadataConfig struct {
	System         int    `toml:"system"`
	SystemLabel    string `toml:"system_label"`
	Talkgroup      int    `toml:"talkgroup"`
	TalkgroupLabel string `toml:"talkgroup_label"`
	TalkgroupGroup string `toml:"talkgroup_group"`
	TalkgroupTag   string `toml:"talkgroup_tag"`
	Frequency      int    `toml:"frequency"`
}

type QueueConfig struct {
	Directory string `toml:"directory"`
}

type FolderIngestConfig struct {
	Enabled            bool   `toml:"enabled"`
	Directory          string `toml:"directory"`
	ProcessedDirectory string `toml:"processed_directory"`
	ReprocessProcessed bool   `toml:"reprocess_processed"`
	PollMS             int    `toml:"poll_ms"`
	StableMS           int    `toml:"stable_ms"`
}

type CanaryConfig struct {
	Enabled        bool   `toml:"enabled"`
	IntervalSec    int    `toml:"interval_sec"`
	Talkgroup      int    `toml:"talkgroup"`
	TalkgroupLabel string `toml:"talkgroup_label"`
}

type RuntimeConfig struct {
	Config
	ConfigPath         string
	QueueDir           string
	FolderIngestDir    string
	FolderProcessedDir string
	UploadURL          string
}

type queuedFields map[string]string

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "config.toml", "path to recorder config TOML")
	initConfig := flag.Bool("init-config", false, "create a recorder config interactively and exit")
	forceConfig := flag.Bool("force", false, "overwrite an existing config when used with --init-config")
	listDevices := flag.Bool("list-devices", false, "list capture devices and exit")
	checkHub := flag.Bool("check-hub", false, "check hub health and source key connectivity, then exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", appName, version)
		return 0
	}

	if *checkHub {
		runtimeConfig, err := loadConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config error: %v\n", err)
			return 1
		}
		if err := checkHubConnectivity(runtimeConfig, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "hub check failed: %v\n", err)
			return 1
		}
		return 0
	}

	ctx, err := newAudioContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "audio init failed: %v\n", err)
		return 1
	}
	defer ctx.Close()

	if *initConfig {
		if err := writeInitialConfig(*configPath, *forceConfig, ctx, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "config init failed: %v\n", err)
			return 1
		}
		return 0
	}

	if *listDevices {
		if err := ctx.ListDevices(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "device list failed: %v\n", err)
			return 1
		}
		return 0
	}

	runtimeConfig, err := loadConfig(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "config error: %s does not exist. Run with --init-config to create it.\n", *configPath)
			return 1
		}
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	recorder := NewRecorder(runtimeConfig, ctx)
	if err := recorder.Run(rootCtx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "recorder error: %v\n", err)
		return 1
	}
	return 0
}

func checkHubConnectivity(cfg RuntimeConfig, out io.Writer) error {
	base, err := url.Parse(cfg.P7.BaseURL)
	if err != nil {
		return fmt.Errorf("p7.base_url: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/v1/health"
	base.RawQuery = ""
	base.Fragment = ""

	timeout := time.Duration(cfg.P7.TimeoutSec * float64(time.Second))
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(base.String())
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	fmt.Fprintf(out, "hub health ok: %s\n", base.String())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("key", cfg.P7.SourceKey); err != nil {
		return err
	}
	if err := writer.WriteField("test", "1"); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.UploadURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "P7 Recorder Go")
	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("source key probe: %w", err)
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	_ = resp.Body.Close()
	message := strings.TrimSpace(string(respBody))
	if resp.StatusCode == http.StatusExpectationFailed && strings.Contains(message, "incomplete call data") {
		fmt.Fprintln(out, "source key ok")
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("source key rejected")
	}
	if resp.StatusCode == http.StatusForbidden {
		return errors.New("source is disabled")
	}
	return fmt.Errorf("source key probe returned %s: %s", resp.Status, message)
}

func loadConfig(path string) (RuntimeConfig, error) {
	cfg := Config{
		P7:       P7Config{BaseURL: "https://p7hub.projectseven.us/", TimeoutSec: 20},
		Audio:    AudioConfig{SampleRate: 16000, Channels: 1, BlockMS: 100, Threshold: 500, SilenceMS: 1200, MinDurationMS: 400, MaxDurationSec: 120, PreRollMS: 300},
		Metadata: MetadataConfig{System: 1, SystemLabel: "GMRS", Talkgroup: 18, TalkgroupLabel: "GMRS Channel 18", TalkgroupGroup: "GMRS", TalkgroupTag: "voice", Frequency: 462625000},
		Queue:    QueueConfig{Directory: "queue"},
		FolderIngest: FolderIngestConfig{
			Directory:          "ingest",
			ProcessedDirectory: "processed",
			PollMS:             1000,
			StableMS:           2500,
		},
		Canary: CanaryConfig{IntervalSec: 300},
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return RuntimeConfig{}, err
	}

	cfg.P7.BaseURL = strings.TrimSpace(cfg.P7.BaseURL)
	cfg.P7.SourceKey = strings.TrimSpace(cfg.P7.SourceKey)
	if cfg.P7.BaseURL == "" {
		return RuntimeConfig{}, errors.New("p7.base_url is required")
	}
	if cfg.P7.SourceKey == "" || strings.HasPrefix(cfg.P7.SourceKey, "sk_live_REPLACE") {
		return RuntimeConfig{}, errors.New("p7.source_key must be set to a generated source API key")
	}
	if cfg.Audio.SampleRate <= 0 || cfg.Audio.Channels <= 0 {
		return RuntimeConfig{}, errors.New("audio.sample_rate and audio.channels must be positive")
	}
	if cfg.Audio.Channels > 2 {
		return RuntimeConfig{}, errors.New("audio.channels must be 1 or 2")
	}
	if cfg.Audio.BlockMS <= 0 {
		cfg.Audio.BlockMS = 100
	}
	if cfg.P7.TimeoutSec <= 0 {
		cfg.P7.TimeoutSec = 20
	}

	base, err := url.Parse(cfg.P7.BaseURL)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("p7.base_url: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/call-upload"

	queueDir := strings.TrimSpace(cfg.Queue.Directory)
	if queueDir == "" {
		queueDir = "queue"
	}
	if !filepath.IsAbs(queueDir) {
		queueDir = filepath.Join(filepath.Dir(path), queueDir)
	}
	ingestDir := strings.TrimSpace(cfg.FolderIngest.Directory)
	if ingestDir == "" {
		ingestDir = "ingest"
	}
	if !filepath.IsAbs(ingestDir) {
		ingestDir = filepath.Join(filepath.Dir(path), ingestDir)
	}
	processedDir := strings.TrimSpace(cfg.FolderIngest.ProcessedDirectory)
	if processedDir == "" {
		processedDir = "processed"
	}
	if !filepath.IsAbs(processedDir) {
		processedDir = filepath.Join(ingestDir, processedDir)
	}
	if cfg.FolderIngest.PollMS <= 0 {
		cfg.FolderIngest.PollMS = 1000
	}
	if cfg.FolderIngest.StableMS <= 0 {
		cfg.FolderIngest.StableMS = 2500
	}
	if cfg.Canary.Enabled && cfg.Canary.IntervalSec <= 0 {
		cfg.Canary.IntervalSec = 300
	}

	return RuntimeConfig{Config: cfg, ConfigPath: path, QueueDir: queueDir, FolderIngestDir: ingestDir, FolderProcessedDir: processedDir, UploadURL: base.String()}, nil
}

func writeInitialConfig(path string, force bool, audio *AudioContext, in io.Reader, out io.Writer) error {
	if path == "" {
		return errors.New("config path is required")
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to overwrite it", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}

	reader := bufio.NewReader(in)
	fmt.Fprintln(out, "P7 Recorder Go config setup")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Available capture devices:")
	if err := audio.ListDevices(out); err != nil {
		fmt.Fprintf(out, "device list unavailable: %v\n", err)
	}
	fmt.Fprintln(out, "")

	baseURL := promptString(reader, out, "SignalForge Hub URL", "https://p7hub.projectseven.us/")
	sourceKey := promptString(reader, out, "Source API key", "sk_live_REPLACE_WITH_GENERATED_SOURCE_KEY")
	device := promptString(reader, out, "Audio device index or name", "")
	threshold := promptInt(reader, out, "VOX threshold", 500)
	system := promptInt(reader, out, "System ID", 1)
	systemLabel := promptString(reader, out, "System label", "GMRS")
	talkgroup := promptInt(reader, out, "Talkgroup", 18)
	talkgroupLabel := promptString(reader, out, "Talkgroup label", "GMRS Channel 18")
	talkgroupGroup := promptString(reader, out, "Talkgroup group", "GMRS")
	frequency := promptInt(reader, out, "Frequency Hz", 462625000)

	contents := buildInitialConfig(Config{
		P7:       P7Config{BaseURL: baseURL, SourceKey: sourceKey, TimeoutSec: 20},
		Audio:    AudioConfig{Device: device, SampleRate: 16000, Channels: 1, BlockMS: 100, Threshold: threshold, SilenceMS: 1200, MinDurationMS: 400, MaxDurationSec: 120, PreRollMS: 300},
		Metadata: MetadataConfig{System: system, SystemLabel: systemLabel, Talkgroup: talkgroup, TalkgroupLabel: talkgroupLabel, TalkgroupGroup: talkgroupGroup, TalkgroupTag: "voice", Frequency: frequency},
		Queue:    QueueConfig{Directory: "queue"},
		FolderIngest: FolderIngestConfig{
			Directory:          "ingest",
			ProcessedDirectory: "processed",
			PollMS:             1000,
			StableMS:           2500,
		},
	})
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nWrote %s\n", path)
	fmt.Fprintf(out, "Next: run with --config %s --list-devices to verify audio, then run with --config %s.\n", path, path)
	return nil
}

func promptString(reader *bufio.Reader, out io.Writer, label, fallback string) string {
	if fallback == "" {
		fmt.Fprintf(out, "%s: ", label)
	} else {
		fmt.Fprintf(out, "%s [%s]: ", label, fallback)
	}
	value, _ := reader.ReadString('\n')
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func promptInt(reader *bufio.Reader, out io.Writer, label string, fallback int) int {
	for {
		value := promptString(reader, out, label, strconv.Itoa(fallback))
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(out, "%s must be a number.\n", label)
	}
}

func buildInitialConfig(cfg Config) string {
	return fmt.Sprintf(`[p7]
base_url = %s
source_key = %s
timeout_sec = %.0f

[audio]
# Use --list-devices to find input device names. Leave blank for the default input.
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
enabled = false
directory = "ingest"
processed_directory = "processed"
reprocess_processed = false
poll_ms = 1000
stable_ms = 2500

[canary]
# Upload a silent heartbeat clip at a regular interval to verify the pipeline is alive.
# interval_sec accepts any positive value (e.g. 60 = every minute, 3600 = every hour).
enabled = false
interval_sec = 300
`, strconv.Quote(cfg.P7.BaseURL), strconv.Quote(cfg.P7.SourceKey), cfg.P7.TimeoutSec, strconv.Quote(cfg.Audio.Device), cfg.Audio.SampleRate, cfg.Audio.Channels, cfg.Audio.BlockMS, cfg.Audio.Threshold, cfg.Audio.SilenceMS, cfg.Audio.MinDurationMS, cfg.Audio.MaxDurationSec, cfg.Audio.PreRollMS, cfg.Metadata.System, strconv.Quote(cfg.Metadata.SystemLabel), cfg.Metadata.Talkgroup, strconv.Quote(cfg.Metadata.TalkgroupLabel), strconv.Quote(cfg.Metadata.TalkgroupGroup), strconv.Quote(cfg.Metadata.TalkgroupTag), cfg.Metadata.Frequency, strconv.Quote(cfg.Queue.Directory))
}

type Recorder struct {
	cfg   RuntimeConfig
	audio *AudioContext
	in    chan []byte
}

func NewRecorder(cfg RuntimeConfig, audio *AudioContext) *Recorder {
	return &Recorder{cfg: cfg, audio: audio, in: make(chan []byte, 200)}
}

func (r *Recorder) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.QueueDir, 0o755); err != nil {
		return err
	}
	if err := r.FlushQueue(); err != nil {
		fmt.Fprintf(os.Stderr, "queue flush skipped: %v\n", err)
	}
	if r.cfg.Canary.Enabled {
		go r.runCanary(ctx)
	}
	if r.cfg.FolderIngest.Enabled {
		return r.runFolderIngest(ctx)
	}

	stopAudio, err := r.audio.StartCapture(r.cfg.Audio, r.in)
	if err != nil {
		return err
	}
	defer stopAudio()

	fmt.Println("P7 recorder running. Press Ctrl+C to stop.")
	return r.loop(ctx)
}

func (r *Recorder) loop(ctx context.Context) error {
	audio := r.cfg.Audio
	silenceBlocksRequired := max(1, audio.SilenceMS/audio.BlockMS)
	minBlocks := max(1, audio.MinDurationMS/audio.BlockMS)
	maxBlocks := max(1, audio.MaxDurationSec*1000/audio.BlockMS)
	preRollBlocks := max(0, audio.PreRollMS/audio.BlockMS)
	preRoll := newRing(preRollBlocks)
	activeBlocks := make([][]byte, 0, maxBlocks)
	active := false
	silentBlocks := 0
	startedAt := int64(0)

	for {
		select {
		case <-ctx.Done():
			if active && len(activeBlocks) >= minBlocks {
				if _, err := r.QueueCall(activeBlocks, startedAtOrNow(startedAt)); err != nil {
					return err
				}
			}
			return ctx.Err()
		case block := <-r.in:
			level := rmsInt16(block)
			voice := level >= audio.Threshold
			if !active {
				preRoll.Add(block)
				if voice {
					active = true
					silentBlocks = 0
					startedAt = time.Now().Unix()
					activeBlocks = preRoll.Items()
					fmt.Printf("voice start rms=%d\n", level)
				}
				continue
			}

			activeBlocks = append(activeBlocks, block)
			if voice {
				silentBlocks = 0
			} else {
				silentBlocks++
			}

			tooQuiet := silentBlocks >= silenceBlocksRequired
			tooLong := len(activeBlocks) >= maxBlocks
			if tooQuiet || tooLong {
				if len(activeBlocks) >= minBlocks {
					path, err := r.QueueCall(activeBlocks, startedAt)
					if err != nil {
						return err
					}
					fmt.Printf("queued %s\n", filepath.Base(path))
					if err := r.FlushQueue(); err != nil {
						fmt.Fprintf(os.Stderr, "upload unavailable: %v\n", err)
					}
				} else {
					fmt.Println("discarded short burst")
				}
				active = false
				activeBlocks = activeBlocks[:0]
				preRoll.Clear()
				silentBlocks = 0
			}
		}
	}
}

func rmsInt16(block []byte) int {
	if len(block) < 2 {
		return 0
	}
	var total int64
	samples := len(block) / 2
	for i := 0; i+1 < len(block); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(block[i:]))
		total += int64(sample) * int64(sample)
	}
	return int(math.Sqrt(float64(total) / float64(samples)))
}

func (r *Recorder) QueueCall(blocks [][]byte, startedAt int64) (string, error) {
	if err := os.MkdirAll(r.cfg.QueueDir, 0o755); err != nil {
		return "", err
	}
	audio := joinBlocks(blocks)
	durationSec := float64(len(audio)) / float64(r.cfg.Audio.SampleRate*r.cfg.Audio.Channels*2)
	stem := fmt.Sprintf("call-%d-%d-%s", startedAt, time.Now().UnixMilli(), randomSuffix())
	wavPath := filepath.Join(r.cfg.QueueDir, stem+".wav")
	jsonPath := filepath.Join(r.cfg.QueueDir, stem+".json")
	if err := writeWAV(wavPath, audio, r.cfg.Audio.SampleRate, r.cfg.Audio.Channels); err != nil {
		return "", err
	}
	fields := r.metadataFields(startedAt, durationSec, filepath.Base(wavPath))
	payload, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(jsonPath, payload, 0o600); err != nil {
		return "", err
	}
	return wavPath, nil
}

func (r *Recorder) QueueExternalFile(path string, reprocess bool) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", errors.New("folder ingest path is a directory")
	}
	if err := os.MkdirAll(r.cfg.QueueDir, 0o755); err != nil {
		return "", "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	stem := fmt.Sprintf("ingest-%d-%d-%s", info.ModTime().Unix(), time.Now().UnixMilli(), randomSuffix())
	audioPath := filepath.Join(r.cfg.QueueDir, stem+ext)
	jsonPath := filepath.Join(r.cfg.QueueDir, stem+".json")
	processedPath := uniquePath(filepath.Join(r.cfg.FolderProcessedDir, filepath.Base(path)))
	if reprocess {
		if err := copyFile(path, audioPath); err != nil {
			return "", "", err
		}
	} else if err := os.Rename(path, audioPath); err != nil {
		if err := copyFile(path, audioPath); err != nil {
			return "", "", err
		}
		if err := os.Remove(path); err != nil {
			return "", "", err
		}
	}
	fields := r.metadataFields(info.ModTime().Unix(), 0, filepath.Base(path))
	fields["audioType"] = audioTypeForExtension(ext)
	fields["_processedPath"] = processedPath
	if reprocess {
		fields["_reprocess"] = "true"
	}
	payload, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, payload, 0o600); err != nil {
		return "", "", err
	}
	return audioPath, jsonPath, nil
}

func (r *Recorder) metadataFields(startedAt int64, durationSec float64, audioName string) queuedFields {
	meta := r.cfg.Metadata
	return queuedFields{
		"key":            r.cfg.P7.SourceKey,
		"system":         strconv.Itoa(meta.System),
		"systemLabel":    meta.SystemLabel,
		"talkgroup":      strconv.Itoa(meta.Talkgroup),
		"talkgroupLabel": meta.TalkgroupLabel,
		"talkgroupGroup": meta.TalkgroupGroup,
		"talkgroupTag":   meta.TalkgroupTag,
		"frequency":      strconv.Itoa(meta.Frequency),
		"dateTime":       strconv.FormatInt(startedAt, 10),
		"duration":       fmt.Sprintf("%.3f", durationSec),
		"audioName":      audioName,
		"audioType":      "audio/wav",
	}
}

func (r *Recorder) FlushQueue() error {
	if err := os.MkdirAll(r.cfg.QueueDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(r.cfg.QueueDir)
	if err != nil {
		return err
	}
	jsonPaths := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			jsonPaths = append(jsonPaths, filepath.Join(r.cfg.QueueDir, entry.Name()))
		}
	}
	sort.Strings(jsonPaths)
	for _, jsonPath := range jsonPaths {
		ok, err := r.UploadOne(jsonPath)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	return nil
}

func (r *Recorder) UploadOne(jsonPath string) (bool, error) {
	audioPath, err := queuedAudioPath(jsonPath)
	if errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(jsonPath)
		return true, nil
	} else if err != nil {
		return false, err
	}
	payload, err := os.ReadFile(jsonPath)
	if err != nil {
		return false, err
	}
	fields := queuedFields{}
	if err := json.Unmarshal(payload, &fields); err != nil {
		return false, err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if key == "audio" || strings.HasPrefix(key, "_") {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return false, err
		}
	}
	part, err := writer.CreateFormFile("audio", filepath.Base(audioPath))
	if err != nil {
		return false, err
	}
	wavFile, err := os.Open(audioPath)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(part, wavFile); err != nil {
		_ = wavFile.Close()
		return false, err
	}
	if err := wavFile.Close(); err != nil {
		return false, err
	}
	if err := writer.Close(); err != nil {
		return false, err
	}

	timeout := time.Duration(r.cfg.P7.TimeoutSec * float64(time.Second))
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodPost, r.cfg.UploadURL, body)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "P7 Recorder Go")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "upload failed: %d %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
		return false, nil
	}
	if processedPath := strings.TrimSpace(fields["_processedPath"]); processedPath != "" && fields["_reprocess"] != "true" {
		processedPath = uniquePath(processedPath)
		if err := os.MkdirAll(filepath.Dir(processedPath), 0o755); err != nil {
			return false, err
		}
		if err := os.Rename(audioPath, processedPath); err != nil {
			return false, err
		}
	} else {
		if err := os.Remove(audioPath); err != nil {
			return false, err
		}
	}
	if err := os.Remove(jsonPath); err != nil {
		return false, err
	}
	fmt.Printf("uploaded %s\n", filepath.Base(audioPath))
	return true, nil
}

func (r *Recorder) runFolderIngest(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.FolderIngestDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(r.cfg.FolderProcessedDir, 0o755); err != nil {
		return err
	}
	mode := "watch"
	if r.cfg.FolderIngest.ReprocessProcessed {
		mode = "replay-processed"
	}
	fmt.Printf("folder ingest running mode=%s ingest=%s processed=%s\n", mode, r.cfg.FolderIngestDir, r.cfg.FolderProcessedDir)
	seen := map[string]bool{}
	if err := r.processFolderIngestBatch(seen); err != nil {
		fmt.Fprintf(os.Stderr, "folder ingest skipped: %v\n", err)
	}
	ticker := time.NewTicker(time.Duration(r.cfg.FolderIngest.PollMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processFolderIngestBatch(seen); err != nil {
				fmt.Fprintf(os.Stderr, "folder ingest skipped: %v\n", err)
			}
		}
	}
}

func (r *Recorder) runCanary(ctx context.Context) {
	interval := time.Duration(r.cfg.Canary.IntervalSec) * time.Second
	fmt.Printf("canary running interval=%s\n", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.uploadCanary(); err != nil {
				fmt.Fprintf(os.Stderr, "canary upload failed: %v\n", err)
			}
		}
	}
}

func (r *Recorder) uploadCanary() error {
	now := time.Now()
	meta := r.cfg.Metadata
	talkgroup := r.cfg.Canary.Talkgroup
	if talkgroup <= 0 {
		talkgroup = meta.Talkgroup
	}
	talkgroupLabel := r.cfg.Canary.TalkgroupLabel
	if talkgroupLabel == "" {
		talkgroupLabel = meta.TalkgroupLabel
	}
	sampleRate := r.cfg.Audio.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	channels := r.cfg.Audio.Channels
	if channels <= 0 {
		channels = 1
	}
	pcm := make([]byte, sampleRate*channels*2) // 1 second of silence
	audioName := fmt.Sprintf("canary-%d.wav", now.Unix())

	wavBuf := &bytes.Buffer{}
	if err := writeWAVWriter(wavBuf, pcm, sampleRate, channels); err != nil {
		return err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fields := map[string]string{
		"key":            r.cfg.P7.SourceKey,
		"system":         strconv.Itoa(meta.System),
		"systemLabel":    meta.SystemLabel,
		"talkgroup":      strconv.Itoa(talkgroup),
		"talkgroupLabel": talkgroupLabel,
		"talkgroupGroup": meta.TalkgroupGroup,
		"talkgroupTag":   meta.TalkgroupTag,
		"frequency":      strconv.Itoa(meta.Frequency),
		"dateTime":       strconv.FormatInt(now.Unix(), 10),
		"duration":       "1.000",
		"audioName":      audioName,
		"audioType":      "audio/wav",
	}
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("audio", audioName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, wavBuf); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	timeout := time.Duration(r.cfg.P7.TimeoutSec * float64(time.Second))
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodPost, r.cfg.UploadURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "P7 Recorder Go")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("canary upload: %d %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	fmt.Printf("canary uploaded %s\n", audioName)
	return nil
}

func (r *Recorder) processFolderIngestBatch(seen map[string]bool) error {
	reprocess := r.cfg.FolderIngest.ReprocessProcessed
	sourceDir := r.cfg.FolderIngestDir
	if reprocess {
		sourceDir = r.cfg.FolderProcessedDir
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	stableAge := time.Duration(r.cfg.FolderIngest.StableMS) * time.Millisecond
	for _, entry := range entries {
		if entry.IsDir() || !isSupportedIngestAudio(entry.Name()) {
			continue
		}
		path := filepath.Join(sourceDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < stableAge {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano())
		if reprocess && seen[key] {
			continue
		}
		audioPath, jsonPath, err := r.QueueExternalFile(path, reprocess)
		if err != nil {
			fmt.Fprintf(os.Stderr, "folder ingest queue failed %s: %v\n", filepath.Base(path), err)
			continue
		}
		if reprocess {
			seen[key] = true
		}
		fmt.Printf("folder ingest queued %s\n", filepath.Base(audioPath))
		if err := r.FlushQueue(); err != nil {
			fmt.Fprintf(os.Stderr, "upload unavailable: %v\n", err)
		}
		if _, err := os.Stat(jsonPath); errors.Is(err, os.ErrNotExist) && !reprocess {
			fmt.Printf("folder ingest processed %s\n", entry.Name())
		}
	}
	return nil
}

func writeWAV(path string, pcm []byte, sampleRate, channels int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return writeWAVWriter(file, pcm, sampleRate, channels)
}

func writeWAVWriter(w io.Writer, pcm []byte, sampleRate, channels int) error {
	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * channels * 2)
	blockAlign := uint16(channels * 2)
	if _, err := w.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(36)+dataSize); err != nil {
		return err
	}
	if _, err := w.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	for _, value := range []any{uint32(16), uint16(1), uint16(channels), uint32(sampleRate), byteRate, blockAlign, uint16(16)} {
		if err := binary.Write(w, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, dataSize); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}

func joinBlocks(blocks [][]byte) []byte {
	total := 0
	for _, block := range blocks {
		total += len(block)
	}
	out := make([]byte, 0, total)
	for _, block := range blocks {
		out = append(out, block...)
	}
	return out
}

func queuedAudioPath(jsonPath string) (string, error) {
	stem := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath))
	for _, ext := range []string{".wav", ".mp3", ".m4a", ".flac"} {
		candidate := stem + ext
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", os.ErrNotExist
}

func isSupportedIngestAudio(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".wav", ".mp3", ".m4a", ".flac":
		return true
	default:
		return false
	}
}

func audioTypeForExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
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

func uniquePath(path string) string {
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

type ring struct {
	items [][]byte
	cap   int
	start int
}

func newRing(capacity int) *ring {
	return &ring{cap: capacity}
}

func (r *ring) Add(value []byte) {
	if r.cap <= 0 {
		return
	}
	value = append([]byte(nil), value...)
	if len(r.items) < r.cap {
		r.items = append(r.items, value)
		return
	}
	r.items[r.start] = value
	r.start = (r.start + 1) % r.cap
}

func (r *ring) Items() [][]byte {
	if len(r.items) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(r.items))
	for i := 0; i < len(r.items); i++ {
		index := (r.start + i) % len(r.items)
		out = append(out, append([]byte(nil), r.items[index]...))
	}
	return out
}

func (r *ring) Clear() {
	r.items = nil
	r.start = 0
}

func startedAtOrNow(value int64) int64 {
	if value > 0 {
		return value
	}
	return time.Now().Unix()
}

func randomSuffix() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return fmt.Sprintf("%x", buf[:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
