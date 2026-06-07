//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", Name+".service"), nil
}

func installPlatform(execPath string, args []string, envFile, logDir string) (Status, error) {
	unit, err := unitPath()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		return Status{}, err
	}
	content := renderSystemdUnit(execPath, args, envFile, logDir)
	if err := os.WriteFile(unit, []byte(content), 0o644); err != nil {
		return Status{}, err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return Status{}, err
	}
	if err := runSystemctl("enable", "--now", Name+".service"); err != nil {
		return Status{}, err
	}
	status, _ := statusPlatform()
	status.UnitPath = unit
	status.Installed = true
	status.Detail = "systemd user service enabled"
	return status, nil
}

func uninstallPlatform() (Status, error) {
	unit, err := unitPath()
	if err != nil {
		return Status{}, err
	}
	_ = runSystemctl("disable", "--now", Name+".service")
	_ = os.Remove(unit)
	_ = runSystemctl("daemon-reload")
	return Status{Installed: false, Detail: "systemd user service removed"}, nil
}

func statusPlatform() (Status, error) {
	unit, err := unitPath()
	if err != nil {
		return Status{}, err
	}
	if _, err := os.Stat(unit); err != nil {
		return Status{Installed: false, Detail: "not installed"}, nil
	}
	status := Status{Installed: true, UnitPath: unit}
	out, err := exec.Command("systemctl", "--user", "is-active", Name+".service").CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil && text == "active" {
		status.Running = true
		status.Detail = "active"
		return status, nil
	}
	status.Detail = text
	if text == "" {
		status.Detail = "installed but not active"
	}
	return status, nil
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func renderSystemdUnit(execPath string, args []string, envFile, logDir string) string {
	joined := append([]string{execPath}, args...)
	execStart := strings.Join(joined, " ")
	return fmt.Sprintf(`[Unit]
Description=SignalForge folder watch and canary uploads
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%s
ExecStart=%s
Restart=on-failure
RestartSec=15
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, envFile, execStart, filepath.Join(logDir, "watch.log"), filepath.Join(logDir, "watch.log"))
}
