package trunk

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/check"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/daemon"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/decode/trunkrecorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hub"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/radioreference"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
	"github.com/spf13/cobra"
)

type trunkOptions struct {
	configPath string
	hubURL     string
	sourceKey  string
}

func NewCommand(hubURL, sourceKey string) *cobra.Command {
	opts := &trunkOptions{
		configPath: defaultTrunkConfigPath(),
		hubURL:     hubURL,
		sourceKey:  sourceKey,
	}
	cmd := &cobra.Command{
		Use:     "trunk",
		Aliases: []string{"trk", "tr", "t"},
		Short:   "SDR trunk recorder for OKWIN and GMRS",
		Run: func(cmd *cobra.Command, _ []string) {
			printBanner(cmd.OutOrStdout(), "Trunk Recorder")
			printLine(cmd.OutOrStdout(), "info", "quick", "sf trk setup")
			_ = cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", opts.configPath, "trunk recorder config path")
	cmd.PersistentFlags().StringVar(&opts.hubURL, "hub-url", opts.hubURL, "SignalForge Hub base URL")
	cmd.PersistentFlags().StringVar(&opts.sourceKey, "source-key", opts.sourceKey, "Hub source upload key")

	// Short primary names — long forms remain as aliases.
	cmd.AddCommand(
		newTrunkSetup(opts),
		newTrunkStart(opts),
		newTrunkCheck(opts),
		newTrunkDevices(opts),
		newTrunkStatus(opts),
		newTrunkInstallDeps(opts),
		newTrunkImportRR(opts),
		newTrunkRenderConfig(opts),
		newTrunkInit(opts),
		newTrunkSyncRR(opts),
	)
	return cmd
}

func newTrunkStart(opts *trunkOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "start",
		Aliases: []string{"run", "daemon"},
		Short:   "Start the trunk recorder daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadTrunkConfig(opts)
			if err != nil {
				return err
			}
			d, err := daemon.New(cfg, opts.configPath)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			printBanner(cmd.OutOrStdout(), "Trunk Recorder")
			printLine(cmd.OutOrStdout(), "info", "config", opts.configPath)
			printLine(cmd.OutOrStdout(), "info", "sdr", "discovering RTL-SDR devices...")
			if err := d.Run(ctx); err != nil && ctx.Err() == nil {
				return err
			}
			printLine(cmd.OutOrStdout(), "warn", "trunk", "stopped")
			return nil
		},
	}
}

func newTrunkDevices(opts *trunkOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "dev",
		Aliases: []string{"devices", "sdr"},
		Short:   "List discovered SDRs and role assignments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool := sdr.NewPool()
			devices, err := pool.Discover()
			if err != nil {
				return err
			}
			plan := pool.Rebalance()
			printBanner(cmd.OutOrStdout(), "SDR Devices")
			printLine(cmd.OutOrStdout(), "info", "count", fmt.Sprintf("%d", len(devices)))
			printLine(cmd.OutOrStdout(), "info", "roles", fmt.Sprintf("control=%d voice=%d gmrs=%d backup=%d",
				plan.ControlHunt, plan.Voice, plan.GMRS, plan.HuntBackup))
			fmt.Fprint(cmd.OutOrStdout(), pool.Summary())
			return nil
		},
	}
}

func newTrunkCheck(opts *trunkOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "chk",
		Aliases: []string{"check", "preflight", "doctor"},
		Short:   "Preflight checks before starting",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadTrunkConfig(opts)
			if err != nil {
				return err
			}
			hubClient, _ := hub.NewClient(cfg.Hub.BaseURL, cfg.Hub.SourceKey, 10*time.Second)
			report, _, err := check.Run(cfg, opts.configPath, hubClient)
			if err != nil {
				return err
			}
			printBanner(cmd.OutOrStdout(), "Preflight")
			for _, item := range report.Items {
				printLine(cmd.OutOrStdout(), item.Status, item.Label, item.Message)
			}
			if !report.OK() {
				return fmt.Errorf("preflight failed")
			}
			return nil
		},
	}
}

func newTrunkRenderConfig(opts *trunkOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "render",
		Aliases: []string{"render-config", "gen-config"},
		Short:   "Generate trunk-recorder.json from trunk.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadTrunkConfig(opts)
			if err != nil {
				return err
			}
			path, err := trunkrecorder.RenderOrDiscover(cfg, opts.configPath, nil)
			if err != nil {
				return err
			}
			printBanner(cmd.OutOrStdout(), "Trunk Recorder Config")
			printLine(cmd.OutOrStdout(), "ok", "written", path)
			return nil
		},
	}
}

func newTrunkStatus(opts *trunkOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "st",
		Aliases: []string{"status", "stat"},
		Short:   "Show trunk recorder status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadTrunkConfig(opts)
			if err != nil {
				return err
			}
			hubClient, _ := hub.NewClient(cfg.Hub.BaseURL, cfg.Hub.SourceKey, 10*time.Second)
			report, _, err := check.Run(cfg, opts.configPath, hubClient)
			if err != nil {
				return err
			}
			printBanner(cmd.OutOrStdout(), "Trunk Status")
			printLine(cmd.OutOrStdout(), "info", "engine", cfg.Engine())
			for _, item := range report.Items {
				printLine(cmd.OutOrStdout(), item.Status, item.Label, item.Message)
			}
			if !report.OK() {
				printLine(cmd.OutOrStdout(), "warn", "ready", "not ready to start")
			} else {
				printLine(cmd.OutOrStdout(), "ok", "ready", "ready to start")
			}
			return nil
		},
	}
}

func newTrunkImportRR(opts *trunkOptions) *cobra.Command {
	var sid int
	var pdfPath, csvPath, name, sysid string
	var force bool
	cmd := &cobra.Command{
		Use:     "imp",
		Aliases: []string{"import-rr", "import", "rr"},
		Short:   "Import RadioReference export",
		Long: `Import OKWIN (sid 6949) or other systems from RadioReference PDF/CSV exports.
Download from https://www.radioreference.com/db/sid/<sid>/download (Premium required).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadTrunkConfig(opts)
			if err != nil {
				if os.IsNotExist(err) {
					cfg = config.Default()
				} else {
					return err
				}
			}
			if sid == 6949 && name == "" {
				name = "OKWIN"
			}
			if sid == 6949 && sysid == "" {
				sysid = "92C"
			}
			for i := range cfg.Trunking.Systems {
				if cfg.Trunking.Systems[i].Name == "OKWIN" || cfg.Trunking.Systems[i].RadioReferenceSID == sid {
					cfg.Trunking.Systems[i].RadioReferenceSID = sid
				}
			}
			csvDir := filepath.Dir(opts.configPath)
			if csvDir == "" || csvDir == "." {
				csvDir, _ = os.Getwd()
			}
			if err := radioreference.ImportFromPaths(&cfg, pdfPath, csvPath, name, sysid, csvDir, force); err != nil {
				return err
			}
			if err := config.Save(opts.configPath, cfg); err != nil {
				return err
			}
			printBanner(cmd.OutOrStdout(), "RR Import")
			printLine(cmd.OutOrStdout(), "ok", "saved", opts.configPath)
			for _, sys := range cfg.Trunking.Systems {
				printLine(cmd.OutOrStdout(), "info", "system", fmt.Sprintf("%s sid=%d sysid=%s sites=%d ccs=%d",
					sys.Name, sys.RadioReferenceSID, sys.SysID, len(sys.Sites), len(sys.AllControlFrequenciesMHz())))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&sid, "sid", 6949, "RadioReference system id")
	cmd.Flags().StringVar(&pdfPath, "pdf", "", "path to RR PDF export")
	cmd.Flags().StringVar(&csvPath, "csv", "", "path to RR CSV export or bundle")
	cmd.Flags().StringVar(&name, "name", "", "system name override")
	cmd.Flags().StringVar(&sysid, "sysid", "", "system id override")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing system entry")
	return cmd
}

func newTrunkSyncRR(opts *trunkOptions) *cobra.Command {
	var sid int
	var apiKey, username, password string
	cmd := &cobra.Command{
		Use:     "sync",
		Aliases: []string{"sync-rr"},
		Short:   "Sync system metadata from RadioReference SOAP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if apiKey == "" || username == "" || password == "" {
				return fmt.Errorf("set --rr-api-key, --rr-username, and --rr-password (requires RR premium + developer API key)")
			}
			client := radioreference.NewSOAPClient(apiKey, username, password)
			resp, err := radioreference.SyncOKWIN(client, sid)
			if err != nil {
				return err
			}
			cfg, err := loadTrunkConfig(opts)
			if err != nil {
				cfg = config.Default()
			}
			for i, sys := range cfg.Trunking.Systems {
				if sys.RadioReferenceSID == sid || (sid == 6949 && sys.Name == "OKWIN") {
					cfg.Trunking.Systems[i].Name = resp.Name
					cfg.Trunking.Systems[i].SysID = resp.SysID
					cfg.Trunking.Systems[i].WACN = resp.WACN
					cfg.Trunking.Systems[i].NAC = resp.WACN
					cfg.Trunking.Systems[i].Protocol = resp.Protocol
					break
				}
			}
			if err := config.Save(opts.configPath, cfg); err != nil {
				return err
			}
			printBanner(cmd.OutOrStdout(), "RR SOAP Sync")
			printLine(cmd.OutOrStdout(), "ok", "synced", resp.Name)
			return nil
		},
	}
	cmd.Flags().IntVar(&sid, "sid", 6949, "RadioReference system id")
	cmd.Flags().StringVar(&apiKey, "rr-api-key", os.Getenv("RADIOREFERENCE_API_KEY"), "RR developer API key")
	cmd.Flags().StringVar(&username, "rr-username", os.Getenv("RADIOREFERENCE_USERNAME"), "RR premium username")
	cmd.Flags().StringVar(&password, "rr-password", os.Getenv("RADIOREFERENCE_PASSWORD"), "RR premium password")
	return cmd
}

func newTrunkInit(opts *trunkOptions) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write default OKWIN + GMRS trunk config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := os.Stat(opts.configPath); err == nil && !force {
				return fmt.Errorf("%s exists; use --force to overwrite", opts.configPath)
			}
			cfg := config.Default()
			cfg.Hub.BaseURL = opts.hubURL
			cfg.Hub.SourceKey = opts.sourceKey
			if err := config.Save(opts.configPath, cfg); err != nil {
				return err
			}
			printLine(cmd.OutOrStdout(), "ok", "created", opts.configPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func loadTrunkConfig(opts *trunkOptions) (config.Config, error) {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return config.Config{}, err
	}
	if opts.hubURL != "" {
		cfg.Hub.BaseURL = opts.hubURL
	}
	if opts.sourceKey != "" {
		cfg.Hub.SourceKey = opts.sourceKey
	}
	return cfg, nil
}

func printBanner(out io.Writer, title string) {
	fmt.Fprintf(out, "\nSignalForge Console\n// %s\n", title)
}

func printLine(out io.Writer, tone, label, value string) {
	tag := "[..]"
	switch tone {
	case "ok":
		tag = "[OK]"
	case "warn":
		tag = "[!!]"
	case "error", "missing":
		tag = "[XX]"
	}
	fmt.Fprintf(out, "%s %s: %s\n", tag, label, value)
}
