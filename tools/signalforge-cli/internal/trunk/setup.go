package trunk

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/check"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/setup"
	"github.com/spf13/cobra"
)

func defaultTrunkConfigPath() string {
	path, err := setup.DefaultConfigPath()
	if err != nil {
		return "trunk.yaml"
	}
	return path
}

func newTrunkSetup(opts *trunkOptions) *cobra.Command {
	var (
		nonInteractive bool
		skipDeps       bool
		rrCSV          string
		rrPDF          string
		forceConfig    bool
	)
	cmd := &cobra.Command{
		Use:     "setup",
		Aliases: []string{"install", "onboard"},
		Short:   "Easy one-flow setup: deps, config, OKWIN data, and preflight",
		Long: `The easiest way to get started:

  sf trunk setup --source-key sk_live_...

What it does:
  1. Installs trunk-recorder (Homebrew on Mac, build script on Linux)
  2. Saves trunk.yaml to your SignalForge config folder
  3. Imports built-in OKWIN starter data (or your --csv export)
  4. Validates Hub credentials and checks SDRs

Use --yes for fully automatic setup with saved profile defaults.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			printBanner(out, "Trunk Setup")
			if !nonInteractive {
				return runInteractiveSetup(cmd, opts, skipDeps, rrCSV, rrPDF, forceConfig)
			}
			return runAutomaticSetup(cmd, opts, skipDeps, rrCSV, rrPDF, forceConfig)
		},
	}
	cmd.Flags().BoolVar(&nonInteractive, "yes", false, "Fully automatic setup (install deps, import bundled OKWIN data)")
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "Skip trunk-recorder installation")
	cmd.Flags().StringVar(&rrCSV, "csv", "", "RadioReference CSV export (default: built-in OKWIN starter bundle)")
	cmd.Flags().StringVar(&rrPDF, "pdf", "", "RadioReference PDF export path")
	cmd.Flags().BoolVar(&forceConfig, "force", false, "Overwrite trunk.yaml when creating config")
	return cmd
}

func newTrunkInstallDeps(opts *trunkOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "install-deps",
		Aliases: []string{"deps", "install-trunk-recorder"},
		Short:   "Install trunk-recorder and dependencies",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			printBanner(out, "Install Dependencies")
			ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Minute)
			defer cancel()
			if err := setup.InstallDeps(ctx, out); err != nil {
				return err
			}
			deps := setup.Check()
			for _, item := range deps.Items {
				printLine(out, item.Status, item.Name, item.Detail)
			}
			if !deps.Ready() {
				return fmt.Errorf("trunk-recorder still not ready")
			}
			printLine(out, "ok", "done", "run sf trunk setup to finish configuration")
			return nil
		},
	}
}

func runAutomaticSetup(cmd *cobra.Command, opts *trunkOptions, skipDeps bool, rrCSV, rrPDF string, forceConfig bool) error {
	out := cmd.OutOrStdout()
	result, err := setup.Run(cmd.Context(), out, setup.Options{
		ConfigPath:      opts.configPath,
		HubURL:          opts.hubURL,
		SourceKey:       opts.sourceKey,
		RRCSV:           rrCSV,
		RRPDF:           rrPDF,
		SkipDeps:        skipDeps,
		AutoInstallDeps: !skipDeps,
		ForceConfig:     forceConfig,
		AutoImportRR:    true,
		AutoRender:      true,
	})
	if err != nil {
		return err
	}
	return finishSetup(out, result)
}

func runInteractiveSetup(cmd *cobra.Command, opts *trunkOptions, skipDeps bool, rrCSV, rrPDF string, forceConfig bool) error {
	out := cmd.OutOrStdout()
	prompt := newPrompter(cmd.InOrStdin(), out, false)

	deps := setup.Check()
	autoInstall := false
	if !skipDeps && deps.NeedsInstall() {
		install, err := prompt.askYesNo("Install trunk-recorder now", true)
		if err != nil {
			return err
		}
		autoInstall = install
	}

	hubURL := opts.hubURL
	if hubURL == "" {
		hubURL = "https://p7hub.projectseven.us"
	}
	hubURL, err := prompt.askRequired("Hub URL", hubURL)
	if err != nil {
		return err
	}

	sourceKey := opts.sourceKey
	if sourceKey == "" {
		printLine(out, "info", "tip", "get your key from Hub → Sources, or run sf onboard first")
	}
	sourceKey, err = prompt.askRequired("Source upload key", sourceKey)
	if err != nil {
		return err
	}

	doImport, err := prompt.askYesNo("Import OKWIN starter data (3 sites, sample talkgroups)", true)
	if err != nil {
		return err
	}
	csvPath := rrCSV
	if doImport && csvPath == "" {
		bundled, bundleErr := setup.BundledSamplePath()
		if bundleErr == nil {
			csvPath = bundled
			printLine(out, "info", "bundle", csvPath)
		} else {
			custom, err := prompt.ask("RR CSV path", "")
			if err != nil {
				return err
			}
			csvPath = expandPath(custom)
		}
	} else if !doImport {
		custom, err := prompt.ask("RR CSV path (optional)", "")
		if err != nil {
			return err
		}
		csvPath = expandPath(custom)
	}

	result, err := setup.Run(cmd.Context(), out, setup.Options{
		ConfigPath:      opts.configPath,
		HubURL:          hubURL,
		SourceKey:       sourceKey,
		RRCSV:           csvPath,
		RRPDF:           rrPDF,
		SkipDeps:        skipDeps || !autoInstall,
		AutoInstallDeps: autoInstall,
		ForceConfig:     forceConfig,
		AutoImportRR:    csvPath != "",
		AutoRender:      true,
	})
	if err != nil {
		return err
	}
	return finishSetup(out, result)
}

func finishSetup(out io.Writer, result setup.Result) error {
	printBanner(out, "Preflight")
	for _, item := range result.Check.Items {
		printLine(out, item.Status, item.Label, item.Message)
	}
	for _, warning := range result.Warnings {
		printLine(out, "warn", "note", warning)
	}
	for _, blocker := range result.Blockers {
		printLine(out, "error", "blocker", blocker)
	}

	ready := result.OK() && hasSDRs(result.Check)
	printSetupNextSteps(out, result.ConfigPath, ready)

	if !result.OK() {
		return fmt.Errorf("setup incomplete — fix blockers above and re-run sf trunk setup")
	}
	printLine(out, "ok", "setup", "complete")
	return nil
}

func hasSDRs(report check.Report) bool {
	for _, item := range report.Items {
		if item.Label == "sdr.count" && item.Status == "ok" {
			return true
		}
	}
	return false
}

func printSetupNextSteps(out io.Writer, configPath string, readyToStart bool) {
	fmt.Fprintln(out)
	printLine(out, "info", "config", configPath)
	if readyToStart {
		printLine(out, "info", "start", fmt.Sprintf("sf trunk start --config %q", configPath))
		return
	}
	printLine(out, "info", "next", "plug in RTL-SDR dongles")
	fmt.Fprintf(out, "  sf trunk devices\n")
	fmt.Fprintf(out, "  sf trunk start --config %q\n", configPath)
}
