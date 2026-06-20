package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/profile"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/recorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/service"
	"github.com/spf13/cobra"
)

func newOnboardCommand(opts *options) *cobra.Command {
	var (
		nonInteractive  bool
		showProfile     bool
		installService  bool
		uninstallService bool
		inputDir        string
		processedDir    string
		enableCanary    bool
		canaryInterval  time.Duration
		enableBeacon    bool
		beaconFile      string
		beaconInterval  time.Duration
		enableFolder    bool
		reprocess       bool
	)

	cmd := &cobra.Command{
		Use:     "onboard",
		Aliases: []string{"onb", "setup", "init"},
		Short:   "Interactive walkthrough for hub, ingest folder, and canary monitoring",
		Long:    "Configure SignalForge monitoring and save a profile under your user config directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if showProfile {
				return printProfileStatus(out)
			}
			if uninstallService {
				status, err := service.Uninstall()
				if err != nil {
					return err
				}
				printServiceStatus(cmd, status)
				return nil
			}

			prof := profile.Default()
			prof.HubURL = opts.hubURL
			prof.SourceKey = opts.sourceKey
			if prof.HubURL == "" {
				prof.HubURL = config.DefaultHubURL
			}

			if existing, err := profile.Load(); err == nil {
				if strings.TrimSpace(existing.HubURL) != "" && prof.HubURL == config.DefaultHubURL {
					prof.HubURL = existing.HubURL
				}
				if prof.SourceKey == "" {
					prof.SourceKey = existing.SourceKey
				}
				if existing.Folder.Directory != "" {
					prof.Folder = existing.Folder
				}
				if existing.Canary.Enabled {
					prof.Canary = existing.Canary
				}
				if existing.Beacon.Enabled || strings.TrimSpace(existing.Beacon.FilePath) != "" {
					prof.Beacon = existing.Beacon
				}
				prof.Metadata = existing.Metadata
			}

			prompt := newPrompter(cmd.InOrStdin(), out, nonInteractive)
			printBanner(out, "SignalForge Onboard")

			hubURL, err := prompt.askRequired("Hub URL", prof.HubURL)
			if err != nil {
				return err
			}
			prof.HubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")

			sourceKey, err := prompt.askRequired("Source upload key (from Hub → Sources)", prof.SourceKey)
			if err != nil {
				return err
			}
			prof.SourceKey = strings.TrimSpace(sourceKey)

			if err := verifyHubAndKey(cmd, prof.HubURL, prof.SourceKey, opts.timeout); err != nil {
				return err
			}
			printLine(out, "ok", "hub", prof.HubURL)
			printLine(out, "ok", "source key", "validated")

			modeIndex, err := prompt.choose(
				"Monitoring mode",
				[]string{
					"Folder watch — upload audio files from a directory (SDRTrunk, Trunk Recorder, etc.)",
					"Canary only — heartbeat uploads on a timer (pipeline health check)",
					"Folder watch + canary heartbeat",
				},
				0,
			)
			if err != nil {
				return err
			}
			prof.Folder.Enabled = modeIndex == 0 || modeIndex == 2
			prof.Canary.Enabled = modeIndex == 1 || modeIndex == 2

			if nonInteractive {
				if inputDir != "" {
					prof.Folder.Directory = inputDir
				}
				if processedDir != "" {
					prof.Folder.ProcessedDirectory = processedDir
				}
				prof.Folder.ReprocessProcessed = reprocess
				if enableFolder {
					prof.Folder.Enabled = true
				}
				if enableCanary {
					prof.Canary.Enabled = true
				}
				if canaryInterval > 0 {
					prof.Canary.IntervalSec = int(canaryInterval.Seconds())
				}
				if enableBeacon {
					prof.Beacon.Enabled = true
				}
				if beaconFile != "" {
					prof.Beacon.FilePath = expandPath(beaconFile)
				}
				if beaconInterval > 0 {
					prof.Beacon.IntervalSec = int(beaconInterval.Seconds())
				}
			}

			if prof.Folder.Enabled {
				defaultInput := prof.Folder.Directory
				if defaultInput == "" {
					defaultInput = "./ingest"
				}
				inputPath, err := prompt.askRequired("Monitor folder (audio drop directory)", defaultInput)
				if err != nil {
					return err
				}
				inputPath = expandPath(inputPath)
				status, err := recorder.InspectInput(inputPath)
				if err != nil {
					return err
				}
				if status.Mode == "missing" {
					createIt := nonInteractive
					if !createIt {
						createIt, err = prompt.askYesNo("Folder does not exist — create it", true)
					}
					if err != nil {
						return err
					}
					if createIt {
						if err := os.MkdirAll(inputPath, 0o755); err != nil {
							return err
						}
						status, err = recorder.InspectInput(inputPath)
						if err != nil {
							return err
						}
					} else {
						return fmt.Errorf("monitor folder %q does not exist", inputPath)
					}
				}
				prof.Folder.Directory = inputPath
				printLine(out, "ok", "monitor folder", status.Message)

				processedDefault := prof.Folder.ProcessedDirectory
				if processedDefault == "" {
					processedDefault = "processed"
				}
				processed, err := prompt.ask("Processed subfolder (relative to monitor folder)", processedDefault)
				if err != nil {
					return err
				}
				prof.Folder.ProcessedDirectory = strings.TrimSpace(processed)

				pollMs, err := prompt.ask("Poll interval (e.g. 1s, 500ms)", "1s")
				if err != nil {
					return err
				}
				poll, err := time.ParseDuration(pollMs)
				if err != nil || poll <= 0 {
					return fmt.Errorf("invalid poll interval %q", pollMs)
				}
				prof.Folder.PollMs = int(poll / time.Millisecond)

				stableMs, err := prompt.ask("Stable wait before upload (file must stop changing)", "2500ms")
				if err != nil {
					return err
				}
				stable, err := time.ParseDuration(stableMs)
				if err != nil || stable <= 0 {
					return fmt.Errorf("invalid stable interval %q", stableMs)
				}
				prof.Folder.StableMs = int(stable / time.Millisecond)

				reprocessProcessed, err := prompt.askYesNo("Re-upload files without moving them to processed (leave in ingest folder)", prof.Folder.ReprocessProcessed)
				if err != nil {
					return err
				}
				prof.Folder.ReprocessProcessed = reprocessProcessed
			}

			if prof.Canary.Enabled {
				intervalDefault := strconv.Itoa(prof.Canary.IntervalSec)
				if intervalDefault == "0" {
					intervalDefault = "300"
				}
				intervalText, err := prompt.ask("Canary heartbeat interval (e.g. 5m, 300s)", intervalDefault+"s")
				if err != nil {
					return err
				}
				interval, err := time.ParseDuration(intervalText)
				if err != nil || interval < 30*time.Second {
					return fmt.Errorf("canary interval must be at least 30s (got %q)", intervalText)
				}
				prof.Canary.IntervalSec = int(interval / time.Second)

				customCanaryMeta, err := prompt.askYesNo("Use separate talkgroup label for canary clips", false)
				if err != nil {
					return err
				}
				if customCanaryMeta {
					tgText, err := prompt.ask("Canary talkgroup ID (0 = use metadata default)", "0")
					if err != nil {
						return err
					}
					if tg, err := strconv.Atoi(strings.TrimSpace(tgText)); err == nil && tg > 0 {
						prof.Canary.Talkgroup = tg
					}
					label, err := prompt.ask("Canary talkgroup label", "CANARY")
					if err != nil {
						return err
					}
					prof.Canary.TalkgroupLabel = strings.TrimSpace(label)
				}
			}

			enableBeaconClip, err := prompt.askYesNo("Enable scheduled beacon clip (replay an audio file on interval)", prof.Beacon.Enabled)
			if err != nil {
				return err
			}
			prof.Beacon.Enabled = enableBeaconClip
			if prof.Beacon.Enabled {
				defaultPath := prof.Beacon.FilePath
				if defaultPath == "" && prof.Folder.Directory != "" {
					defaultPath = filepath.Join(prof.Folder.Directory, "beacon.wav")
				}
				beaconPath, err := prompt.askRequired("Beacon audio file (.wav, .mp3, .m4a, .flac)", defaultPath)
				if err != nil {
					return err
				}
				beaconPath = expandPath(beaconPath)
				if _, err := recorder.ValidateBeaconFile(beaconPath); err != nil {
					return err
				}
				prof.Beacon.FilePath = beaconPath

				intervalDefault := strconv.Itoa(prof.Beacon.IntervalSec)
				if intervalDefault == "0" {
					intervalDefault = "1800"
				}
				intervalText, err := prompt.ask("Beacon interval (e.g. 30m, 1800s)", intervalDefault+"s")
				if err != nil {
					return err
				}
				interval, err := time.ParseDuration(intervalText)
				if err != nil || interval < 30*time.Second {
					return fmt.Errorf("beacon interval must be at least 30s (got %q)", intervalText)
				}
				prof.Beacon.IntervalSec = int(interval / time.Second)

				customBeaconMeta, err := prompt.askYesNo("Use separate talkgroup label for beacon clips", false)
				if err != nil {
					return err
				}
				if customBeaconMeta {
					tgText, err := prompt.ask("Beacon talkgroup ID (0 = use metadata default)", "0")
					if err != nil {
						return err
					}
					if tg, err := strconv.Atoi(strings.TrimSpace(tgText)); err == nil && tg > 0 {
						prof.Beacon.Talkgroup = tg
					}
					label, err := prompt.ask("Beacon talkgroup label", "BEACON")
					if err != nil {
						return err
					}
					prof.Beacon.TalkgroupLabel = strings.TrimSpace(label)
				}
				printLine(out, "ok", "beacon", fmt.Sprintf("every %ds (%s)", prof.Beacon.IntervalSec, prof.Beacon.FilePath))
			}

			customMeta, err := prompt.askYesNo("Customize radio metadata (system/talkgroup/frequency)", false)
			if err != nil {
				return err
			}
			if customMeta {
				if err := promptMetadata(prompt, &prof.Metadata); err != nil {
					return err
				}
			}

			path, err := profile.Save(prof)
			if err != nil {
				return err
			}
			tomlPath, _ := profile.RecorderTOMLPath()
			configDir, _ := profile.Dir()

			printLine(out, "ok", "profile saved", path)
			if tomlPath != "" {
				printLine(out, "info", "recorder config", tomlPath)
			}
			if configDir != "" {
				printLine(out, "info", "shell env snippet", filepath.Join(configDir, "env"))
			}
			doInstall := installService
			if !nonInteractive && !installService {
				doInstall, err = prompt.askYesNo("Install background watch service (starts on login)", false)
				if err != nil {
					return err
				}
			}
			if doInstall {
				svcStatus, err := service.Install(prof)
				if err != nil {
					return err
				}
				printServiceStatus(cmd, svcStatus)
			}

			printOnboardNextSteps(out, prof, doInstall)
			return nil
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "yes", false, "Accept defaults and skip prompts (use with flags/env)")
	cmd.Flags().BoolVar(&showProfile, "show", false, "Print saved profile location and summary")
	cmd.Flags().BoolVar(&installService, "install-service", false, "Install background watch service after saving profile")
	cmd.Flags().BoolVar(&uninstallService, "uninstall-service", false, "Remove background watch service and exit")
	cmd.Flags().StringVar(&inputDir, "input", "", "Monitor folder (non-interactive)")
	cmd.Flags().StringVar(&processedDir, "processed", "processed", "Processed subfolder name")
	cmd.Flags().BoolVar(&enableFolder, "folder", false, "Enable folder watch in non-interactive mode")
	cmd.Flags().BoolVar(&enableCanary, "canary", false, "Enable canary heartbeat in non-interactive mode")
	cmd.Flags().DurationVar(&canaryInterval, "canary-interval", 5*time.Minute, "Canary upload interval")
	cmd.Flags().BoolVar(&enableBeacon, "beacon", false, "Enable scheduled beacon clip in non-interactive mode")
	cmd.Flags().StringVar(&beaconFile, "beacon-file", "", "Beacon audio file path")
	cmd.Flags().DurationVar(&beaconInterval, "beacon-interval", 30*time.Minute, "Beacon upload interval")
	cmd.Flags().BoolVar(&reprocess, "reprocess", false, "Upload files without moving them to processed")
	return cmd
}

func promptMetadata(prompt *prompter, meta *recorder.Metadata) error {
	defaults := recorder.DefaultSettings().Metadata
	systemLabel, err := prompt.ask("System label", meta.SystemLabel)
	if err != nil {
		return err
	}
	meta.SystemLabel = systemLabel
	if meta.SystemLabel == "" {
		meta.SystemLabel = defaults.SystemLabel
	}

	tgText, err := prompt.ask("Talkgroup ID", strconv.Itoa(meta.Talkgroup))
	if err != nil {
		return err
	}
	if tg, err := strconv.Atoi(strings.TrimSpace(tgText)); err == nil && tg > 0 {
		meta.Talkgroup = tg
	}

	tgLabel, err := prompt.ask("Talkgroup label", meta.TalkgroupLabel)
	if err != nil {
		return err
	}
	meta.TalkgroupLabel = tgLabel

	group, err := prompt.ask("Talkgroup group", meta.TalkgroupGroup)
	if err != nil {
		return err
	}
	meta.TalkgroupGroup = group

	freqText, err := prompt.ask("Frequency (Hz)", strconv.Itoa(meta.Frequency))
	if err != nil {
		return err
	}
	if freq, err := strconv.Atoi(strings.TrimSpace(freqText)); err == nil && freq > 0 {
		meta.Frequency = freq
	}
	return nil
}

func verifyHubAndKey(cmd *cobra.Command, hubURL, sourceKey string, timeout time.Duration) error {
	client, err := newClient(&options{hubURL: hubURL, sourceKey: sourceKey, timeout: timeout})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	done := make(chan error, 2)
	go func() { _, err := client.Health(); done <- err }()
	go func() { done <- client.ProbeSourceKey() }()
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func printProfileStatus(out io.Writer) error {
	path, err := profile.Path()
	if err != nil {
		return fmt.Errorf("profile path: %w", err)
	}
	printBanner(out, "Saved Profile")
	printLine(out, "info", "path", path)
	prof, err := profile.Load()
	if err != nil {
		printLine(out, "warn", "status", "no profile saved yet — run sf onboard")
		return nil
	}
	printLine(out, "info", "hub", prof.HubURL)
	printLine(out, "info", "folder watch", fmt.Sprintf("%v (%s)", prof.Folder.Enabled, prof.Folder.Directory))
	printLine(out, "info", "canary", fmt.Sprintf("%v every %ds", prof.Canary.Enabled, prof.Canary.IntervalSec))
	if prof.Beacon.Enabled {
		printLine(out, "info", "beacon", fmt.Sprintf("every %ds (%s)", prof.Beacon.IntervalSec, prof.Beacon.FilePath))
	} else {
		printLine(out, "info", "beacon", "disabled")
	}
	return nil
}

func printOnboardNextSteps(out io.Writer, prof profile.Profile, serviceInstalled bool) {
	fmt.Fprintln(out)
	printLine(out, "info", "next", "verify saved settings")
	fmt.Fprintf(out, "  %s\n", "sf rec chk")
	if serviceInstalled {
		fmt.Fprintf(out, "  %s\n", "sf service status")
	} else if prof.Folder.Enabled && prof.Folder.Directory != "" {
		fmt.Fprintf(out, "  %s\n", fmt.Sprintf("sf rec w -i %q", prof.Folder.Directory))
		fmt.Fprintf(out, "  %s\n", "sf service install")
	}
	if !serviceInstalled && prof.Canary.Enabled && !prof.Folder.Enabled && !prof.Beacon.Enabled {
		fmt.Fprintf(out, "  %s\n", "sf rec w --canary")
	}
	if !serviceInstalled && prof.Beacon.Enabled && strings.TrimSpace(prof.Beacon.FilePath) != "" {
		fmt.Fprintf(out, "  %s\n", fmt.Sprintf("sf rec w --beacon --beacon-file %q", prof.Beacon.FilePath))
	}
	fmt.Fprintf(out, "  %s\n", "sf onboard --show")
}
