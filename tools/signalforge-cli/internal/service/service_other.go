//go:build !linux && !darwin

package service

import "fmt"

func installPlatform(_ string, _ []string, _, _ string) (Status, error) {
	return Status{}, fmt.Errorf("background service install is only supported on Linux (systemd) and macOS (launchd)")
}

func uninstallPlatform() (Status, error) {
	return Status{}, fmt.Errorf("background service install is only supported on Linux (systemd) and macOS (launchd)")
}

func statusPlatform() (Status, error) {
	return Status{Detail: "unsupported platform"}, nil
}
