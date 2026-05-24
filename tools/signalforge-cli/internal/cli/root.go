package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/api"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/recorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/tui"
	"github.com/spf13/cobra"
)

type options struct {
	hubURL    string
	sourceKey string
	timeout   time.Duration
	recorder  recorder.Settings
}

func NewRootCommand() *cobra.Command {
	cfg := config.FromEnv()
	opts := &options{hubURL: cfg.HubURL, sourceKey: cfg.SourceKey, timeout: cfg.Timeout, recorder: recorder.DefaultSettings()}

	cmd := &cobra.Command{
		Use:   "signalforge",
		Short: "SignalForge operator CLI",
		Long:  "SignalForge is a cross-platform operator CLI for checking hubs, recorder keys, and federation-ready nodes.",
	}
	cmd.PersistentFlags().StringVar(&opts.hubURL, "hub-url", opts.hubURL, "SignalForge Hub base URL")
	cmd.PersistentFlags().StringVar(&opts.sourceKey, "source-key", opts.sourceKey, "source upload API key")
	cmd.PersistentFlags().DurationVar(&opts.timeout, "timeout", opts.timeout, "HTTP timeout")
	cmd.AddCommand(newHubCommand(opts), newRecorderCommand(opts), newTUICommand(opts))
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
			fmt.Fprintf(cmd.OutOrStdout(), "hub: %s\n", client.BaseURL())
			fmt.Fprintf(cmd.OutOrStdout(), "health: %s\n", fallback(health.Status, "ok"))
			fmt.Fprintf(cmd.OutOrStdout(), "version: %s\n", fallback(version.Version, "unknown"))
			fmt.Fprintf(cmd.OutOrStdout(), "commit: %s\n", fallback(version.Commit, "unknown"))
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
			fmt.Fprintf(cmd.OutOrStdout(), "hub health ok: %s\n", client.BaseURL())
			fmt.Fprintln(cmd.OutOrStdout(), "source key ok")
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
			fmt.Fprintf(cmd.OutOrStdout(), "uploaded: %s\n", status.Path)
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
			fmt.Fprintf(cmd.OutOrStdout(), "watching: %s\n", watchSettings.Input)
			fmt.Fprintf(cmd.OutOrStdout(), "processed: %s\n", recorder.ProcessedPath(watchSettings, ".keep"))
			for {
				if _, err := uploadFolderBatch(cmd, client, watchSettings); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					fmt.Fprintln(cmd.OutOrStdout(), "watch stopped")
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
	fmt.Fprintf(out, "input: %s\n", fallback(status.Path, "not configured"))
	fmt.Fprintf(out, "mode: %s\n", status.Mode)
	fmt.Fprintf(out, "status: %s\n", status.Message)
	if status.Mode == "file" {
		fmt.Fprintf(out, "audio type: %s\n", fallback(status.AudioType, "unsupported"))
		fmt.Fprintf(out, "size: %d bytes\n", status.SizeBytes)
	}
	if status.Mode == "folder" {
		fmt.Fprintf(out, "audio files: %d\n", status.SupportedCount)
		fmt.Fprintf(out, "skipped files: %d\n", status.SkippedCount)
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
			fmt.Fprintf(cmd.OutOrStdout(), "uploaded: %s\n", file.Path)
			continue
		}
		destination, err := recorder.MoveToProcessed(settings, file.Path)
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "uploaded: %s -> %s\n", file.Path, destination)
	}
	return len(files), nil
}

func signals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
