package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/api"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/buildinfo"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/recorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/tui"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/updater"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type options struct {
	hubURL    string
	sourceKey string
	timeout   time.Duration
	recorder  recorder.Settings
}

const (
	ansiReset  = "\x1b[0m"
	ansiCyan   = "\x1b[36;1m"
	ansiGreen  = "\x1b[32;1m"
	ansiYellow = "\x1b[33;1m"
	ansiRed    = "\x1b[31;1m"
	ansiDim    = "\x1b[90m"
)

func NewRootCommand() *cobra.Command {
	cfg := config.FromEnv()
	opts := &options{hubURL: cfg.HubURL, sourceKey: cfg.SourceKey, timeout: cfg.Timeout, recorder: recorder.DefaultSettings()}

	cmd := &cobra.Command{
		Use:   "signalforge",
		Short: "SignalForge operator CLI",
		Long:  "SignalForge is a cross-platform operator CLI for checking hubs, recorder keys, and federation-ready nodes.",
		Run: func(cmd *cobra.Command, _ []string) {
			_ = cmd.Help()
		},
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			runAutoUpdateCheck(cmd)
		},
	}
	cmd.PersistentFlags().StringVar(&opts.hubURL, "hub-url", opts.hubURL, "SignalForge Hub base URL")
	cmd.PersistentFlags().StringVar(&opts.sourceKey, "source-key", opts.sourceKey, "source upload API key")
	cmd.PersistentFlags().DurationVar(&opts.timeout, "timeout", opts.timeout, "HTTP timeout")
	cmd.AddCommand(newHubCommand(opts), newRecorderCommand(opts), newTUICommand(opts), newUpdateCommand(), newVersionCommand())
	configureHelp(cmd)
	return cmd
}

func configureHelp(root *cobra.Command) {
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if cmd == root {
			printRootHelp(cmd.OutOrStdout())
			return
		}
		printCommandHelp(cmd.OutOrStdout(), cmd)
	})
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print SignalForge CLI version metadata",
		Run: func(cmd *cobra.Command, _ []string) {
			out := cmd.OutOrStdout()
			printBanner(out, "SignalForge CLI")
			printLine(out, "ok", "signalforge", buildinfo.DisplayVersion())
			printLine(out, "info", "commit", buildinfo.Commit)
			printLine(out, "info", "date", buildinfo.Date)
		},
	}
}

func newUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Check for SignalForge CLI updates"}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check GitHub releases for a newer SignalForge CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			result, err := updater.Check(ctx, updater.Options{CurrentVersion: buildinfo.DisplayVersion(), ReleaseAPI: updateReleaseAPI()})
			if err != nil {
				return err
			}
			printUpdateResult(cmd, result)
			updater.MarkChecked(time.Now())
			return nil
		},
	})
	return cmd
}

func newHubCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "hub", Short: "Hub checks and operations"}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check hub health and version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newClient(opts)
			if err != nil {
				return err
			}
			health, err := client.Health()
			if err != nil {
				return err
			}
			version, err := client.Version()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printBanner(out, "Hub Check")
			printLine(out, "info", "hub", client.BaseURL())
			printLine(out, "ok", "health", fallback(health.Status, "ok"))
			printLine(out, "info", "version", fallback(version.Version, "unknown"))
			printLine(out, "info", "commit", fallback(version.Commit, "unknown"))
			return nil
		},
	})
	return cmd
}

func newRecorderCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "recorder", Short: "Recorder setup, source-key, and audio input checks"}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check hub health and source upload key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newClient(opts)
			if err != nil {
				return err
			}
			if _, err := client.Health(); err != nil {
				return err
			}
			if err := client.ProbeSourceKey(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printBanner(out, "Recorder Check")
			printLine(out, "ok", "hub health ok", client.BaseURL())
			printLine(out, "ok", "source key ok", "ready")
			return nil
		},
	})

	inspectSettings := opts.recorder
	inspectCmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a recorder input file or folder",
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := recorder.InspectInput(inspectSettings.Input)
			if err != nil {
				return err
			}
			printInputStatus(cmd, status)
			return nil
		},
	}
	bindRecorderFlags(inspectCmd, &inspectSettings)
	cmd.AddCommand(inspectCmd)

	uploadSettings := opts.recorder
	uploadCmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload one audio file through the recorder ingest path",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newClient(opts)
			if err != nil {
				return err
			}
			status, err := recorder.ValidateFileInput(uploadSettings.Input)
			if err != nil {
				return err
			}
			fields := api.UploadFields{
				Metadata:  uploadSettings.Metadata,
				AudioName: filepath.Base(status.Path),
				AudioType: status.AudioType,
				StartedAt: status.ModifiedAt,
			}
			if err := client.UploadFile(status.Path, fields); err != nil {
				return err
			}
			printLine(cmd.OutOrStdout(), "ok", "uploaded", status.Path)
			return nil
		},
	}
	bindRecorderFlags(uploadCmd, &uploadSettings)
	cmd.AddCommand(uploadCmd)

	watchSettings := opts.recorder
	watchOnce := false
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a folder and upload stable audio files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newClient(opts)
			if err != nil {
				return err
			}
			if watchOnce {
				_, err := uploadFolderBatch(cmd, client, watchSettings)
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), signals()...)
			defer stop()
			poll := watchSettings.Poll
			if poll <= 0 {
				poll = time.Second
			}
			out := cmd.OutOrStdout()
			printBanner(out, "Recorder Watch")
			printLine(out, "info", "watching", watchSettings.Input)
			printLine(out, "info", "processed", recorder.ProcessedPath(watchSettings, ".keep"))
			for {
				if _, err := uploadFolderBatch(cmd, client, watchSettings); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					printLine(out, "warn", "watch", "stopped")
					return nil
				case <-time.After(poll):
				}
			}
		},
	}
	bindRecorderFlags(watchCmd, &watchSettings)
	watchCmd.Flags().BoolVar(&watchOnce, "once", false, "process the current ready batch and exit")
	cmd.AddCommand(watchCmd)

	tuiSettings := opts.recorder
	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Open the recorder setup and ingest console",
		RunE: func(_ *cobra.Command, _ []string) error {
			client, err := newClient(opts)
			if err != nil {
				return err
			}
			return tui.Run(tui.Options{Client: client, Recorder: tuiSettings})
		},
	}
	bindRecorderFlags(tuiCmd, &tuiSettings)
	cmd.AddCommand(tuiCmd)
	return cmd
}

func newTUICommand(opts *options) *cobra.Command {
	settings := opts.recorder
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Open the SignalForge terminal dashboard",
		RunE: func(_ *cobra.Command, _ []string) error {
			client, err := newClient(opts)
			if err != nil {
				return err
			}
			return tui.Run(tui.Options{Client: client, Recorder: settings})
		},
	}
	bindRecorderFlags(cmd, &settings)
	return cmd
}

func newClient(opts *options) (*api.Client, error) {
	return api.NewClient(opts.hubURL, opts.sourceKey, opts.timeout)
}

func fallback(value, fallbackValue string) string {
	if value == "" {
		return fallbackValue
	}
	return value
}

func printUpdateResult(cmd *cobra.Command, result updater.Result) {
	out := cmd.OutOrStdout()
	printBanner(out, "Update Check")
	printLine(out, "info", "current", fallback(result.CurrentVersion, "unknown"))
	printLine(out, "info", "latest", fallback(result.LatestVersion, "unknown"))
	if result.UpdateAvailable {
		printLine(out, "warn", "status", "update available")
	} else {
		printLine(out, "ok", "status", "up to date")
	}
	if result.AssetURL != "" {
		printLine(out, "info", "asset", result.AssetName)
		printLine(out, "info", "download", result.AssetURL)
	} else if result.ReleaseURL != "" {
		printLine(out, "info", "release", result.ReleaseURL)
	}
}

func runAutoUpdateCheck(cmd *cobra.Command) {
	path := cmd.CommandPath()
	if path == "signalforge" {
		return
	}
	if strings.Contains(path, " completion") || strings.Contains(path, " update") || strings.Contains(path, " version") {
		return
	}
	if !updater.ShouldAutoCheck(time.Now(), 24*time.Hour) {
		return
	}
	updater.MarkChecked(time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := updater.Check(ctx, updater.Options{CurrentVersion: buildinfo.DisplayVersion(), ReleaseAPI: updateReleaseAPI()})
	if err != nil || !result.UpdateAvailable {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "\nupdate available: signalforge %s", result.LatestVersion)
	if result.AssetURL != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), " (%s)", result.AssetURL)
	} else if result.ReleaseURL != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), " (%s)", result.ReleaseURL)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "")
}

func bindRecorderFlags(cmd *cobra.Command, settings *recorder.Settings) {
	cmd.Flags().StringVar(&settings.Input, "input", settings.Input, "audio file or folder to inspect/upload")
	cmd.Flags().StringVar(&settings.Processed, "processed", settings.Processed, "processed folder for watched audio")
	cmd.Flags().DurationVar(&settings.Poll, "poll", settings.Poll, "folder watch poll interval")
	cmd.Flags().DurationVar(&settings.Stable, "stable", settings.Stable, "file age required before upload")
	cmd.Flags().BoolVar(&settings.Reprocess, "reprocess", settings.Reprocess, "upload ready files without moving them to processed")
	cmd.Flags().IntVar(&settings.Metadata.System, "system", settings.Metadata.System, "system ID")
	cmd.Flags().StringVar(&settings.Metadata.SystemLabel, "system-label", settings.Metadata.SystemLabel, "system label")
	cmd.Flags().IntVar(&settings.Metadata.Talkgroup, "talkgroup", settings.Metadata.Talkgroup, "talkgroup ID")
	cmd.Flags().StringVar(&settings.Metadata.TalkgroupLabel, "talkgroup-label", settings.Metadata.TalkgroupLabel, "talkgroup label")
	cmd.Flags().StringVar(&settings.Metadata.TalkgroupGroup, "talkgroup-group", settings.Metadata.TalkgroupGroup, "talkgroup group")
	cmd.Flags().StringVar(&settings.Metadata.TalkgroupTag, "talkgroup-tag", settings.Metadata.TalkgroupTag, "talkgroup tag")
	cmd.Flags().IntVar(&settings.Metadata.Frequency, "frequency", settings.Metadata.Frequency, "frequency in Hz")
}

func printInputStatus(cmd *cobra.Command, status recorder.InputStatus) {
	out := cmd.OutOrStdout()
	tone := inputTone(status)
	printBanner(out, "Recorder Inspect")
	printLine(out, "info", "input", fallback(status.Path, "not configured"))
	printLine(out, tone, "mode", status.Mode)
	printLine(out, tone, "status", status.Message)
	if status.Mode == "file" {
		printLine(out, tone, "audio type", fallback(status.AudioType, "unsupported"))
		printLine(out, "info", "size", fmt.Sprintf("%d bytes", status.SizeBytes))
	}
	if status.Mode == "folder" {
		printLine(out, tone, "audio files", fmt.Sprintf("%d", status.SupportedCount))
		printLine(out, "info", "skipped files", fmt.Sprintf("%d", status.SkippedCount))
	}
}

func uploadFolderBatch(cmd *cobra.Command, client *api.Client, settings recorder.Settings) (int, error) {
	files, err := recorder.ReadyFiles(settings, time.Now())
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	for _, file := range files {
		fields := api.UploadFields{
			Metadata:  settings.Metadata,
			AudioName: file.Name,
			AudioType: file.AudioType,
			StartedAt: file.ModifiedAt,
		}
		if err := client.UploadFile(file.Path, fields); err != nil {
			return 0, err
		}
		if settings.Reprocess {
			printLine(cmd.OutOrStdout(), "ok", "uploaded", file.Path)
			continue
		}
		destination, err := recorder.MoveToProcessed(settings, file.Path)
		if err != nil {
			return 0, err
		}
		printLine(cmd.OutOrStdout(), "ok", "uploaded", fmt.Sprintf("%s -> %s", file.Path, destination))
	}
	return len(files), nil
}

func printBanner(out io.Writer, title string) {
	fmt.Fprintf(out, "\n%s\n", color(ansiCyan, "SignalForge Console"))
	fmt.Fprintf(out, "%s\n", color(ansiDim, "// "+title))
}

func printLine(out io.Writer, tone, label, value string) {
	fmt.Fprintf(out, "%s %s: %s\n", statusTag(tone), label, value)
}

func statusTag(tone string) string {
	switch tone {
	case "ok":
		return color(ansiGreen, "[OK]")
	case "warn":
		return color(ansiYellow, "[!!]")
	case "error":
		return color(ansiRed, "[XX]")
	default:
		return color(ansiCyan, "[..]")
	}
}

func color(code, value string) string {
	if os.Getenv("NO_COLOR") != "" {
		return value
	}
	return code + value + ansiReset
}

func printRootHelp(out io.Writer) {
	fmt.Fprintf(out, "\n%s\n", color(ansiCyan, "SignalForge Console"))
	fmt.Fprintf(out, "%s\n\n", color(ansiDim, "// recorder setup // hub checks // package updates"))
	printLine(out, "ok", "platform", runtime.GOOS+"/"+runtime.GOARCH)
	printLine(out, "info", "hub", "https://p7hub.projectseven.us")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s\n", color(ansiCyan, "Quick Start"))
	fmt.Fprintf(out, "  %s\n", color(ansiDim, "signalforge recorder check --source-key sk_live_REPLACE_WITH_SOURCE_KEY"))
	fmt.Fprintf(out, "  %s\n", color(ansiDim, "signalforge recorder inspect --input ./calls"))
	fmt.Fprintf(out, "  %s\n", color(ansiDim, "signalforge recorder watch --input ./calls --source-key sk_live_REPLACE_WITH_SOURCE_KEY"))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s\n", color(ansiCyan, "Commands"))
	commandRow(out, "HUB", "hub", "check hub health and version")
	commandRow(out, "REC", "recorder", "inspect, upload, watch, and open recorder console")
	commandRow(out, "TUI", "tui", "open the full-screen recorder dashboard")
	commandRow(out, "UPD", "update", "check the public package release feed")
	commandRow(out, "VER", "version", "show build metadata")
	commandRow(out, "TAB", "completion", "generate shell completion scripts")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s signalforge <command> --help\n", color(ansiDim, "more:"))
	fmt.Fprintf(out, "%s SIGNALFORGE_NO_UPDATE_CHECK=1 disables quiet daily update checks\n", color(ansiDim, "env:"))
}

func printCommandHelp(out io.Writer, cmd *cobra.Command) {
	fmt.Fprintf(out, "\n%s\n", color(ansiCyan, "SignalForge Console"))
	fmt.Fprintf(out, "%s\n\n", color(ansiDim, "// "+cmd.CommandPath()))
	if cmd.Short != "" {
		printLine(out, "info", "about", cmd.Short)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s\n", color(ansiCyan, "Usage"))
	fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", color(ansiCyan, "Commands"))
		for _, child := range cmd.Commands() {
			if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
				continue
			}
			commandRow(out, "CMD", child.Name(), child.Short)
		}
	}
	printFlagSet(out, cmd.NonInheritedFlags(), "Flags")
	printFlagSet(out, cmd.InheritedFlags(), "Global Flags")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s signalforge <command> --help\n", color(ansiDim, "more:"))
}

func commandRow(out io.Writer, group, name, summary string) {
	fmt.Fprintf(out, "  %s %-12s %s\n", color(ansiGreen, "["+group+"]"), color(ansiYellow, name), summary)
}

func printFlagSet(out io.Writer, flags *pflag.FlagSet, title string) {
	if flags == nil || !flags.HasFlags() {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s\n", color(ansiCyan, title))
	flags.VisitAll(func(flag *pflag.Flag) {
		name := "--" + flag.Name
		if flag.Shorthand != "" {
			name = "-" + flag.Shorthand + ", " + name
		}
		if flag.DefValue != "" && flag.DefValue != "false" {
			fmt.Fprintf(out, "  %s %-24s %s %s\n", color(ansiGreen, "[..]"), color(ansiYellow, name), flag.Usage, color(ansiDim, "default: "+flag.DefValue))
			return
		}
		fmt.Fprintf(out, "  %s %-24s %s\n", color(ansiGreen, "[..]"), color(ansiYellow, name), flag.Usage)
	})
}

func inputTone(status recorder.InputStatus) string {
	if status.Mode == "missing" || (status.Mode == "file" && !status.Supported) {
		return "error"
	}
	if status.Mode == "none" || status.Mode == "" {
		return "warn"
	}
	return "ok"
}

func signals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func updateReleaseAPI() string {
	return os.Getenv("SIGNALFORGE_UPDATE_URL")
}
