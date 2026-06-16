package trunkrecorder

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
)

// Config is Trunk Recorder's config.json shape (ver 2).
type Config struct {
	Ver           int              `json:"ver"`
	Sources       []Source         `json:"sources"`
	Systems       []System         `json:"systems"`
	CaptureDir    string           `json:"captureDir"`
	CallTimeout   int              `json:"callTimeout"`
	ControlWarnRate int            `json:"controlWarnRate"`
}

type Source struct {
	Center           float64 `json:"center"`
	Rate             float64 `json:"rate"`
	PPM              float64 `json:"ppm,omitempty"`
	Gain             float64 `json:"gain"`
	DigitalRecorders int     `json:"digitalRecorders"`
	Driver           string  `json:"driver"`
	Device           string  `json:"device"`
	Squelch          float64 `json:"squelch,omitempty"`
}

type System struct {
	Type            string    `json:"type"`
	ShortName       string    `json:"shortName"`
	ControlChannels []int     `json:"control_channels,omitempty"`
	Channels        []int     `json:"channels,omitempty"`
	Modulation      string    `json:"modulation,omitempty"`
	TalkgroupsFile  string    `json:"talkgroupsFile,omitempty"`
	Squelch         float64   `json:"squelch,omitempty"`
	HideEncrypted   bool      `json:"hideEncrypted,omitempty"`
	AudioArchive    bool      `json:"audioArchive,omitempty"`
}

// Generate builds trunk-recorder.json from SignalForge trunk.yaml and discovered SDRs.
func Generate(cfg config.Config, configPath string, devices []sdr.Device) (Config, error) {
	if len(devices) == 0 {
		return Config{}, fmt.Errorf("no RTL-SDR devices discovered")
	}
	plan := sdr.PlanRolesForN(len(devices))
	for i := range devices {
		if devices[i].Role == sdr.RoleUnassigned || devices[i].Role == "" {
			devices[i].Role = roleForIndex(plan, i)
		}
	}
	base := cfg.BaseDir(configPath)
	captureDir := cfg.Resolve(cfg.Recordings.Directory, configPath)
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		return Config{}, err
	}

	tr := Config{
		Ver:             2,
		CaptureDir:      captureDir,
		CallTimeout:     3,
		ControlWarnRate: 10,
	}

	digitalRecorders := 2
	if plan.Voice > 1 {
		digitalRecorders = plan.Voice
	}
	p25Center := 858000000.0

	for i, dev := range devices {
		device := fmt.Sprintf("rtl=%d,buflen=65536", i)
		if dev.Serial != "" && !strings.HasPrefix(dev.Serial, "bus") {
			device = fmt.Sprintf("rtl=%s,buflen=65536", dev.Serial)
		}
		src := Source{
			Center:           p25Center,
			Rate:             float64(cfg.Decode.TrunkRecorder.SampleRate),
			PPM:              cfg.Decode.TrunkRecorder.PPM,
			Gain:             cfg.Decode.TrunkRecorder.Gain,
			DigitalRecorders: digitalRecorders,
			Driver:           "osmosdr",
			Device:           device,
		}
		if dev.Role == sdr.RoleGMRS {
			src.Center = 462650000
			src.Squelch = -50
			src.DigitalRecorders = 1
		}
		tr.Sources = append(tr.Sources, src)
	}

	for _, sys := range cfg.Trunking.Systems {
		if strings.ToLower(sys.Protocol) != "p25" {
			continue
		}
		ccs := controlChannelsHz(sys)
		if len(ccs) == 0 {
			return Config{}, fmt.Errorf("system %q has no control channels; run sf trunk import-rr", sys.Name)
		}
		tgFile := cfg.Resolve(sys.TalkgroupCSV, configPath)
		if base != "" {
			tgFile = filepath.Base(tgFile)
		}
		modulation := "qpsk"
		if strings.EqualFold(sys.P25Phase1DemodMode, "c4fm") {
			modulation = "fsk4"
		}
		shortName := ShortName(sys.Name)
		tr.Systems = append(tr.Systems, System{
			Type:            "p25",
			ShortName:       shortName,
			ControlChannels: ccs,
			Modulation:      modulation,
			TalkgroupsFile:  tgFile,
			HideEncrypted:   true,
			AudioArchive:    true,
		})
		p25Center = centerFromChannels(ccs)
		for i := range tr.Sources {
			if devices[i].Role != sdr.RoleGMRS {
				tr.Sources[i].Center = p25Center
			}
		}
	}

	for _, role := range cfg.Scanner.Roles {
		if role.Type != "conventional" || len(role.Channels) == 0 {
			continue
		}
		var channels []int
		for _, ch := range role.Channels {
			if mhz, ok := config.ParseFrequencyMHz(ch); ok {
				channels = append(channels, int(mhz*1e6))
			}
		}
		if len(channels) == 0 {
			continue
		}
		tr.Systems = append(tr.Systems, System{
			Type:      "conventional",
			ShortName: ShortName(role.Name),
			Channels:  channels,
			Squelch:   -50,
			AudioArchive: true,
		})
	}

	if len(tr.Systems) == 0 {
		return Config{}, fmt.Errorf("no trunk systems configured")
	}
	return tr, nil
}

func Write(path string, tr Config) error {
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" {
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

func controlChannelsHz(sys config.System) []int {
	var out []int
	for _, mhz := range sys.AllControlFrequenciesMHz() {
		out = append(out, int(math.Round(mhz*1e6)))
	}
	return out
}

func centerFromChannels(hz []int) float64 {
	if len(hz) == 0 {
		return 858000000
	}
	min, max := hz[0], hz[0]
	for _, f := range hz[1:] {
		if f < min {
			min = f
		}
		if f > max {
			max = f
		}
	}
	center := float64(min+max) / 2
	rate := 2400000.0
	if float64(max-min) > rate {
		center = float64(min) + rate/2
	}
	return center
}

// ShortName maps a system name to Trunk Recorder's shortName field.
func ShortName(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 6 {
		return s[:6]
	}
	if s == "" {
		return "SYS"
	}
	return s
}

func roleForIndex(plan sdr.RolePlan, idx int) sdr.Role {
	order := []struct {
		role  sdr.Role
		count int
	}{
		{sdr.RoleControlHunt, plan.ControlHunt},
		{sdr.RoleVoice, plan.Voice},
		{sdr.RoleGMRS, plan.GMRS},
		{sdr.RoleHuntBackup, plan.HuntBackup},
	}
	pos := 0
	for _, item := range order {
		for i := 0; i < item.count; i++ {
			if pos == idx {
				return item.role
			}
			pos++
		}
	}
	return sdr.RoleUnassigned
}
