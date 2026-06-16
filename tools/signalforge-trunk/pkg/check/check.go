package check

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hub"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
)

type Item struct {
	Label   string
	Status  string // ok, warn, error
	Message string
}

type Report struct {
	Items []Item
}

func (r Report) OK() bool {
	for _, item := range r.Items {
		if item.Status == "error" {
			return false
		}
	}
	return true
}

func Run(cfg config.Config, configPath string, hubClient *hub.Client) (report Report, devices []sdr.Device, err error) {
	var items []Item

	engine := cfg.Engine()
	items = append(items, Item{"decode.engine", "ok", engine})

	switch engine {
	case config.EngineTrunkRecorder:
		bin := normalizeBinary(cfg.Decode.TrunkRecorder.Binary)
		if path, ok := binaryOnPath(bin); ok {
			items = append(items, Item{"trunk-recorder", "ok", path})
		} else {
			items = append(items, Item{"trunk-recorder", "error", fmt.Sprintf("%q not on PATH — install from https://github.com/TrunkRecorder/trunk-recorder", bin)})
		}
	case config.EngineGopherTrunk:
		bin := cfg.Decode.GopherTrunk.Binary
		if bin == "" {
			bin = "gophertrunk"
		}
		if path, ok := binaryOnPath(bin); ok {
			items = append(items, Item{"gophertrunk", "ok", path})
		} else {
			items = append(items, Item{"gophertrunk", "warn", fmt.Sprintf("%q not on PATH", bin)})
		}
	}

	pool := sdr.NewPool()
	devices, derr := pool.Discover()
	if derr != nil {
		items = append(items, Item{"sdr.discover", "error", derr.Error()})
	} else if len(devices) == 0 {
		items = append(items, Item{"sdr.count", "error", "no RTL-SDR devices found"})
	} else {
		plan := pool.Rebalance()
		items = append(items, Item{"sdr.count", "ok", fmt.Sprintf("%d device(s)", len(devices))})
		items = append(items, Item{"sdr.roles", "ok", fmt.Sprintf("control=%d voice=%d gmrs=%d", plan.ControlHunt, plan.Voice, plan.GMRS)})
	}

	for _, sys := range cfg.Trunking.Systems {
		ccs := len(sys.AllControlFrequenciesMHz())
		if ccs == 0 {
			items = append(items, Item{fmt.Sprintf("rr.%s", sys.Name), "error", "no control channels — run sf trunk import-rr"})
			continue
		}
		items = append(items, Item{fmt.Sprintf("rr.%s", sys.Name), "ok", fmt.Sprintf("%d control channels, %d sites", ccs, len(sys.Sites))})
		if sys.TalkgroupCSV != "" {
			tgPath := cfg.Resolve(sys.TalkgroupCSV, configPath)
			if _, err := os.Stat(tgPath); err != nil {
				items = append(items, Item{"talkgroups.csv", "warn", fmt.Sprintf("%s missing (import RR talkgroup CSV)", tgPath)})
			} else {
				items = append(items, Item{"talkgroups.csv", "ok", tgPath})
			}
		}
	}

	if hubClient != nil {
		if cfg.Hub.SourceKey == "" {
			items = append(items, Item{"hub.source_key", "warn", "not set — uploads will fail"})
		} else if err := hubClient.ProbeSourceKey(); err != nil {
			items = append(items, Item{"hub.source_key", "error", err.Error()})
		} else {
			items = append(items, Item{"hub.source_key", "ok", "validated"})
		}
	}

	trPath := cfg.Resolve(cfg.Decode.TrunkRecorder.ConfigPath, configPath)
	if engine == config.EngineTrunkRecorder {
		if _, err := os.Stat(trPath); err != nil {
			items = append(items, Item{"trunk-recorder.json", "warn", fmt.Sprintf("not generated yet — run sf trunk render-config (expected %s)", trPath)})
		} else {
			items = append(items, Item{"trunk-recorder.json", "ok", trPath})
		}
	}

	capture := cfg.Resolve(cfg.Recordings.Directory, configPath)
	if err := os.MkdirAll(capture, 0o755); err != nil {
		items = append(items, Item{"recordings.dir", "error", err.Error()})
	} else {
		items = append(items, Item{"recordings.dir", "ok", capture})
	}

	if strings.TrimSpace(configPath) != "" {
		if _, err := os.Stat(configPath); err == nil {
			items = append(items, Item{"trunk.yaml", "ok", configPath})
		}
	}

	report = Report{Items: items}
	return report, devices, nil
}

func normalizeBinary(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "trunk-recorder"
	}
	return name
}

func binaryOnPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}
