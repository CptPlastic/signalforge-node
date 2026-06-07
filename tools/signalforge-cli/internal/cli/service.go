package cli

import (
	"fmt"

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
			Short:   "Show background watch service status",
			RunE: func(cmd *cobra.Command, _ []string) error {
				status, err := service.CurrentStatus()
				if err != nil {
					return err
				}
				printServiceStatus(cmd, status)
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
			state = "running"
		}
	}
	printLine(out, "info", "state", state)
	if status.Detail != "" {
		printLine(out, "info", "detail", status.Detail)
	}
	logDir, err := service.LogDir()
	if err == nil {
		printLine(out, "info", "logs", logDir)
	}
}
