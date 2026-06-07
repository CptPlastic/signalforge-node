package cli

import (
	"fmt"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/profile"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/service"
	"github.com/spf13/cobra"
)

func newServiceCommand(_ *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service",
		Aliases: []string{"svc"},
		Short:   "Manage the background watch service",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "install",
			Short: "Install and start the background watch service from saved profile",
			RunE: func(cmd *cobra.Command, _ []string) error {
				prof, err := profile.Load()
				if err != nil {
					return fmt.Errorf("load profile: %w (run sf onboard first)", err)
				}
				status, err := service.Install(prof)
				if err != nil {
					return err
				}
				printServiceStatus(cmd, status)
				return nil
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Stop and remove the background watch service",
			RunE: func(cmd *cobra.Command, _ []string) error {
				status, err := service.Uninstall()
				if err != nil {
					return err
				}
				printServiceStatus(cmd, status)
				return nil
			},
		},
		&cobra.Command{
			Use:     "status",
			Aliases: []string{"st"},
			Short:   "Show background watch service and running watch processes",
			RunE: func(cmd *cobra.Command, _ []string) error {
				status, err := service.CurrentStatus()
				if err != nil {
					return err
				}
				printServiceStatus(cmd, status)
				return nil
			},
		},
		&cobra.Command{
			Use:     "stop",
			Aliases: []string{"kill"},
			Short:   "Stop all running sf rec watch processes",
			RunE: func(cmd *cobra.Command, _ []string) error {
				stopped, err := service.StopWatchProcesses()
				if err != nil {
					return err
				}
				printStopResult(cmd, stopped)
				return nil
			},
		},
	)
	return cmd
}

func printServiceStatus(cmd *cobra.Command, status service.Status) {
	out := cmd.OutOrStdout()
	printBanner(out, "Watch Service")
	if status.UnitPath != "" {
		printLine(out, "info", "unit", status.UnitPath)
	}
	state := "not installed"
	if status.Installed {
		state = "installed"
		if status.Running {
			state = "running (launchd)"
		}
	}
	printLine(out, "info", "launchd", state)
	if len(status.WatchProcesses) == 0 {
		printLine(out, "ok", "processes", "none")
	} else {
		printLine(out, "warn", "processes", fmt.Sprintf("%d running", len(status.WatchProcesses)))
		for _, process := range status.WatchProcesses {
			printLine(out, "warn", fmt.Sprintf("pid %d", process.PID), process.Command)
		}
	}
	if status.Lock != nil {
		printLine(out, "info", "lock", fmt.Sprintf("pid %d since %s", status.Lock.PID, status.Lock.StartedAt.Format(time.RFC3339)))
	}
	if status.Detail != "" {
		printLine(out, "info", "detail", status.Detail)
	}
	logDir, err := service.LogDir()
	if err == nil {
		printLine(out, "info", "logs", logDir)
	}
	if len(status.WatchProcesses) > 0 {
		printLine(out, "info", "stop", "sf rec stop")
	}
}

func printStopResult(cmd *cobra.Command, stopped []service.WatchProcess) {
	out := cmd.OutOrStdout()
	printBanner(out, "Watch Stop")
	if len(stopped) == 0 {
		printLine(out, "ok", "processes", "none running")
		return
	}
	printLine(out, "ok", "stopped", fmt.Sprintf("%d process(es)", len(stopped)))
	for _, process := range stopped {
		printLine(out, "ok", fmt.Sprintf("pid %d", process.PID), process.Command)
	}
}
