package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/check"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/decode/trunkrecorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hub"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/radioreference"
)

// Options controls the non-interactive setup pipeline.
type Options struct {
	ConfigPath      string
	HubURL          string
	SourceKey       string
	RRCSV           string
	RRPDF           string
	SkipDeps        bool
	AutoInstallDeps bool
	ForceConfig     bool
	AutoImportRR    bool
	AutoRender      bool
}

// Result summarizes setup outcome.
type Result struct {
	ConfigPath   string
	Deps         Report
	Check        check.Report
	TRConfigPath string
	Blockers     []string
	Warnings     []string
}

func (r Result) OK() bool {
	return len(r.Blockers) == 0
}

// Run executes the full trunk setup pipeline.
func Run(ctx context.Context, out io.Writer, opts Options) (Result, error) {
	if out == nil {
		out = os.Stdout
	}
	result := Result{}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return result, err
		}
	}
	result.ConfigPath = configPath

	deps := Check()
	result.Deps = deps
	if err := printDeps(out, deps); err != nil {
		return result, err
	}

	if !opts.SkipDeps && deps.NeedsInstall() && opts.AutoInstallDeps {
		fmt.Fprintln(out, "[..] install: installing trunk-recorder (may take several minutes)...")
		installCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
		err := Install(installCtx, out)
		cancel()
		if err != nil {
			return result, err
		}
		deps = Check()
		result.Deps = deps
	}

	hubURL := strings.TrimRight(strings.TrimSpace(opts.HubURL), "/")
	if hubURL == "" {
		hubURL = config.Default().Hub.BaseURL
	}
	sourceKey := strings.TrimSpace(opts.SourceKey)
	if sourceKey == "" {
		result.Blockers = append(result.Blockers, "hub source key required — pass --source-key or run sf onboard first")
	} else {
		client, err := hub.NewClient(hubURL, sourceKey, 20*time.Second)
		if err != nil {
			return result, err
		}
		if err := client.ProbeSourceKey(); err != nil {
			result.Blockers = append(result.Blockers, "hub source key: "+err.Error())
		}
	}

	cfg, created, err := loadOrCreateConfig(configPath, hubURL, sourceKey, opts.ForceConfig)
	if err != nil {
		return result, err
	}
	if created {
		fmt.Fprintf(out, "[OK] config: created %s\n", configPath)
	} else {
		fmt.Fprintf(out, "[..] config: using %s\n", configPath)
	}

	if opts.AutoImportRR || NeedsRRImport(cfg) {
		csvPath := opts.RRCSV
		if csvPath == "" {
			csvPath, err = ResolveSampleCSV("")
			if err != nil {
				result.Blockers = append(result.Blockers, "no RR data — pass --csv or add control channels via sf trunk import-rr")
			}
		}
		if csvPath != "" {
			csvDir := filepath.Dir(configPath)
			if err := radioreference.ImportFromPaths(&cfg, opts.RRPDF, csvPath, "OKWIN", "92C", csvDir, true); err != nil {
				return result, err
			}
			if err := config.Save(configPath, cfg); err != nil {
				return result, err
			}
			for _, sys := range cfg.Trunking.Systems {
				fmt.Fprintf(out, "[OK] import: %s sites=%d control_channels=%d\n",
					sys.Name, len(sys.Sites), len(sys.AllControlFrequenciesMHz()))
			}
		}
	}

	hubClient, _ := hub.NewClient(cfg.Hub.BaseURL, cfg.Hub.SourceKey, 15*time.Second)
	report, _, err := check.Run(cfg, configPath, hubClient)
	if err != nil {
		return result, err
	}
	result.Check = report

	blockers, warnings := Analyze(deps, report)
	result.Blockers = append(result.Blockers, blockers...)
	result.Warnings = append(result.Warnings, warnings...)

	hasSDR := false
	for _, item := range report.Items {
		if item.Label == "sdr.count" && item.Status == "ok" {
			hasSDR = true
		}
	}
	if opts.AutoRender && hasSDR && deps.Ready() {
		path, err := trunkrecorder.RenderOrDiscover(cfg, configPath, nil)
		if err != nil {
			result.Warnings = append(result.Warnings, "trunk-recorder.json: "+err.Error())
		} else {
			result.TRConfigPath = path
			fmt.Fprintf(out, "[OK] trunk-recorder.json: %s\n", path)
		}
	}

	return result, nil
}

func loadOrCreateConfig(path, hubURL, sourceKey string, force bool) (config.Config, bool, error) {
	cfg := config.Default()
	cfg.Hub.BaseURL = hubURL
	cfg.Hub.SourceKey = sourceKey
	if _, err := os.Stat(path); err == nil && !force {
		loaded, loadErr := config.Load(path)
		if loadErr != nil {
			return config.Config{}, false, loadErr
		}
		loaded.Hub.BaseURL = hubURL
		loaded.Hub.SourceKey = sourceKey
		if err := config.Save(path, loaded); err != nil {
			return config.Config{}, false, err
		}
		return loaded, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return config.Config{}, false, err
	}
	if err := config.Save(path, cfg); err != nil {
		return config.Config{}, false, err
	}
	return cfg, true, nil
}

// NeedsRRImport reports whether no control channels are configured yet.
func NeedsRRImport(cfg config.Config) bool {
	for _, sys := range cfg.Trunking.Systems {
		if len(sys.AllControlFrequenciesMHz()) > 0 {
			return false
		}
	}
	return true
}

// Analyze splits preflight results into hard blockers and soft warnings.
func Analyze(deps Report, report check.Report) (blockers, warnings []string) {
	if !deps.Ready() {
		blockers = append(blockers, "trunk-recorder not installed — run: sf trunk install-deps")
	}
	for _, item := range deps.Items {
		if item.Name == "homebrew" && item.Status == "missing" {
			blockers = append(blockers, "homebrew required on macOS — https://brew.sh")
		}
	}
	for _, item := range report.Items {
		switch item.Status {
		case "error":
			blockers = append(blockers, item.Label+": "+item.Message)
		case "warn":
			switch item.Label {
			case "sdr.count":
				warnings = append(warnings, "no RTL-SDR dongles detected yet — plug them in before sf trunk start")
			case "trunk-recorder.json":
				warnings = append(warnings, item.Message)
			case "hub.source_key":
				if strings.Contains(item.Message, "not set") {
					blockers = append(blockers, item.Message)
				} else {
					warnings = append(warnings, item.Message)
				}
			default:
				warnings = append(warnings, item.Label+": "+item.Message)
			}
		}
	}
	return blockers, warnings
}

func printDeps(out io.Writer, deps Report) error {
	fmt.Fprintf(out, "[..] platform: %s\n", deps.Platform)
	for _, item := range deps.Items {
		tag := "[..]"
		switch item.Status {
		case "ok":
			tag = "[OK]"
		case "warn":
			tag = "[!!]"
		case "missing", "error":
			tag = "[XX]"
		}
		if _, err := fmt.Fprintf(out, "%s %s: %s\n", tag, item.Name, item.Detail); err != nil {
			return err
		}
	}
	return nil
}

// InstallDeps installs trunk-recorder if missing.
func InstallDeps(ctx context.Context, out io.Writer) error {
	deps := Check()
	if !deps.NeedsInstall() {
		for _, item := range deps.Items {
			if item.Name == "trunk-recorder" && item.Status == "ok" {
				fmt.Fprintf(out, "[OK] trunk-recorder: %s\n", item.Detail)
				return nil
			}
		}
	}
	if !deps.NeedsInstall() {
		return nil
	}
	for _, item := range deps.Items {
		if item.Name == "homebrew" && item.Status == "missing" {
			return fmt.Errorf("install homebrew first: https://brew.sh")
		}
	}
	fmt.Fprintln(out, "[..] install: this may take several minutes...")
	return Install(ctx, out)
}
