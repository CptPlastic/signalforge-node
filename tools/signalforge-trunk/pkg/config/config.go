package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the trunk recorder operator configuration.
type Config struct {
	Hub        HubConfig        `yaml:"hub"`
	Decode     DecodeConfig     `yaml:"decode"`
	SDR        SDRConfig        `yaml:"sdr"`
	Trunking   TrunkingConfig   `yaml:"trunking"`
	Scanner    ScannerConfig    `yaml:"scanner"`
	Recordings RecordingsConfig `yaml:"recordings"`
	Upload     UploadConfig     `yaml:"upload"`
}

// DecodeConfig selects the RF decode engine (swappable foundation).
type DecodeConfig struct {
	Engine       string              `yaml:"engine"`
	TrunkRecorder TrunkRecorderConfig `yaml:"trunk_recorder"`
	GopherTrunk  GopherTrunkConfig   `yaml:"gophertrunk"`
}

type TrunkRecorderConfig struct {
	Binary     string  `yaml:"binary"`
	ConfigPath string  `yaml:"config_path"`
	PPM        float64 `yaml:"ppm"`
	Gain       float64 `yaml:"gain"`
	SampleRate int     `yaml:"sample_rate"`
}

type GopherTrunkConfig struct {
	Binary string `yaml:"binary"`
}

const (
	EngineTrunkRecorder = "trunk-recorder"
	EngineGopherTrunk   = "gophertrunk"
	EngineExternal      = "external"
)

type HubConfig struct {
	BaseURL   string `yaml:"base_url"`
	SourceKey string `yaml:"source_key"`
}

type SDRConfig struct {
	AutoDiscover bool         `yaml:"auto_discover"`
	Devices      []DeviceSpec `yaml:"devices"`
}

type DeviceSpec struct {
	ID       string `yaml:"id"`
	Serial   string `yaml:"serial"`
	Index    int    `yaml:"index"`
	Disabled bool   `yaml:"disabled"`
}

type TrunkingConfig struct {
	Systems []System `yaml:"systems"`
}

type System struct {
	Name               string   `yaml:"name"`
	RadioReferenceSID  int      `yaml:"radioreference_sid"`
	SysID              string   `yaml:"sysid"`
	WACN               string   `yaml:"wacn"`
	NAC                string   `yaml:"nac"`
	Protocol           string   `yaml:"protocol"`
	P25Phase1DemodMode string   `yaml:"p25_phase1_demod_mode"`
	P25Phase2Enabled   bool     `yaml:"p25_phase2_enabled"`
	Sites              []Site   `yaml:"sites"`
	TalkgroupCSV       string   `yaml:"talkgroup_csv"`
	ControlChannels    []string `yaml:"control_channels"`
}

type Site struct {
	RFSS       int      `yaml:"rfss"`
	SiteID     int      `yaml:"site_id"`
	Name       string   `yaml:"name"`
	County     string   `yaml:"county"`
	Include    bool     `yaml:"include"`
	Frequencies []string `yaml:"frequencies"`
}

type ScannerConfig struct {
	Roles []RoleSpec `yaml:"roles"`
}

type RoleSpec struct {
	Type     string   `yaml:"type"`
	Name     string   `yaml:"name"`
	SDRs     string   `yaml:"sdrs"`
	Channels []string `yaml:"channels"`
}

type RecordingsConfig struct {
	Directory string `yaml:"directory"`
}

type UploadConfig struct {
	QueueDirectory string `yaml:"queue_directory"`
	CanaryInterval string `yaml:"canary_interval"`
}

func Default() Config {
	return Config{
		Hub: HubConfig{
			BaseURL: "https://p7hub.projectseven.us",
		},
		Decode: DecodeConfig{
			Engine: EngineTrunkRecorder,
			TrunkRecorder: TrunkRecorderConfig{
				Binary:     "trunk-recorder",
				ConfigPath: "trunk-recorder.json",
				Gain:       40,
				SampleRate: 2400000,
			},
			GopherTrunk: GopherTrunkConfig{
				Binary: "gophertrunk",
			},
		},
		SDR: SDRConfig{
			AutoDiscover: true,
		},
		Trunking: TrunkingConfig{
			Systems: []System{DefaultOKWIN()},
		},
		Scanner: ScannerConfig{
			Roles: []RoleSpec{
				{Type: "control_hunt", SDRs: "auto"},
				{Type: "voice", SDRs: "auto"},
				{Type: "conventional", Name: "GMRS", SDRs: "auto", Channels: DefaultGMRSChannels()},
			},
		},
		Recordings: RecordingsConfig{
			Directory: "recordings",
		},
		Upload: UploadConfig{
			QueueDirectory: "upload-queue",
			CanaryInterval: "5m",
		},
	}
}

func DefaultOKWIN() System {
	return System{
		Name:               "OKWIN",
		RadioReferenceSID:  6949,
		SysID:              "92C",
		WACN:               "BEE00",
		NAC:                "BEE00",
		Protocol:           "p25",
		P25Phase1DemodMode: "c4fm",
		P25Phase2Enabled:   true,
		TalkgroupCSV:       "talkgroups-okwin-92C.csv",
	}
}

func DefaultGMRSChannels() []string {
	return []string{
		"462.5625", "462.5875", "462.6125", "462.6375", "462.6625",
		"462.6875", "462.7125", "467.5625", "467.5875", "467.6125",
		"467.6375", "467.6625", "467.6875", "467.7125",
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Hub.BaseURL) == "" {
		return errors.New("hub.base_url is required")
	}
	if len(c.Trunking.Systems) == 0 {
		return errors.New("trunking.systems must include at least one system")
	}
	engine := strings.TrimSpace(c.Decode.Engine)
	if engine == "" {
		engine = EngineTrunkRecorder
	}
	switch engine {
	case EngineTrunkRecorder, EngineGopherTrunk, EngineExternal:
	default:
		return fmt.Errorf("decode.engine must be trunk-recorder, gophertrunk, or external (got %q)", engine)
	}
	for i, sys := range c.Trunking.Systems {
		if strings.TrimSpace(sys.Name) == "" {
			return fmt.Errorf("trunking.systems[%d].name is required", i)
		}
		if strings.TrimSpace(sys.Protocol) == "" {
			return fmt.Errorf("trunking.systems[%d].protocol is required", i)
		}
	}
	return nil
}

func (s System) AllControlFrequenciesMHz() []float64 {
	seen := make(map[float64]struct{})
	var out []float64
	for _, freq := range s.ControlChannels {
		if mhz, ok := ParseFrequencyMHz(freq); ok {
			if _, dup := seen[mhz]; !dup {
				seen[mhz] = struct{}{}
				out = append(out, mhz)
			}
		}
	}
	for _, site := range s.Sites {
		if !site.Include && len(s.Sites) > 0 {
			continue
		}
		for _, freq := range site.Frequencies {
			if !strings.HasSuffix(strings.ToLower(freq), "c") {
				continue
			}
			if mhz, ok := ParseFrequencyMHz(strings.TrimSuffix(strings.TrimSuffix(freq, "c"), "C")); ok {
				if _, dup := seen[mhz]; !dup {
					seen[mhz] = struct{}{}
					out = append(out, mhz)
				}
			}
		}
	}
	return out
}

func ParseFrequencyMHz(raw string) (float64, bool) {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(raw, "c"), "C"))
	if raw == "" {
		return 0, false
	}
	var mhz float64
	if _, err := fmt.Sscanf(raw, "%f", &mhz); err != nil {
		return 0, false
	}
	return mhz, mhz > 0
}

func (c Config) Engine() string {
	if strings.TrimSpace(c.Decode.Engine) == "" {
		return EngineTrunkRecorder
	}
	return strings.TrimSpace(c.Decode.Engine)
}

func (c Config) BaseDir(configPath string) string {
	dir := filepath.Dir(configPath)
	if dir == "" || dir == "." {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func (c Config) Resolve(path, configPath string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.BaseDir(configPath), path)
}
