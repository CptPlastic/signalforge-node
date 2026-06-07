//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchdLabel = "org.signalforge.watch"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func installPlatform(execPath string, args []string, envFile, logDir string) (Status, error) {
	plist, err := plistPath()
	if err != nil {
		return Status{}, err
	}
	env, err := ParseEnvFile(envFile)
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return Status{}, err
	}
	content := renderLaunchdPlist(execPath, args, env, logDir)
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return Status{}, err
	}
	uid := os.Getuid()
	target := fmt.Sprintf("gui/%d/%s", uid, launchdLabel)
	_ = exec.Command("launchctl", "bootout", target).Run()
	if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plist).CombinedOutput(); err != nil {
		return Status{}, fmt.Errorf("launchctl bootstrap: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("launchctl", "enable", target).Run()
	_ = exec.Command("launchctl", "kickstart", "-k", target).Run()
	status, _ := statusPlatform()
	status.UnitPath = plist
	status.Installed = true
	status.Detail = "launchd agent loaded"
	return status, nil
}

func uninstallPlatform() (Status, error) {
	plist, err := plistPath()
	if err != nil {
		return Status{}, err
	}
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
	_ = exec.Command("launchctl", "bootout", target).Run()
	_ = os.Remove(plist)
	return Status{Installed: false, Detail: "launchd agent removed"}, nil
}

func statusPlatform() (Status, error) {
	plist, err := plistPath()
	if err != nil {
		return Status{}, err
	}
	if _, err := os.Stat(plist); err != nil {
		return Status{Installed: false, Detail: "not installed"}, nil
	}
	status := Status{Installed: true, UnitPath: plist}
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
	out, err := exec.Command("launchctl", "print", target).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		status.Detail = "installed (not loaded)"
		return status, nil
	}
	if strings.Contains(text, "state = running") {
		status.Running = true
		status.Detail = "running"
	} else {
		status.Detail = "installed"
	}
	return status, nil
}

func renderLaunchdPlist(execPath string, args []string, env map[string]string, logDir string) string {
	programArgs := append([]string{execPath}, args...)
	var argsXML strings.Builder
	for _, arg := range programArgs {
		argsXML.WriteString("      <string>")
		argsXML.WriteString(escapeXML(arg))
		argsXML.WriteString("</string>\n")
	}
	var envXML strings.Builder
	for key, value := range env {
		envXML.WriteString("      <key>")
		envXML.WriteString(escapeXML(key))
		envXML.WriteString("</key>\n      <string>")
		envXML.WriteString(escapeXML(value))
		envXML.WriteString("</string>\n")
	}
	logPath := filepath.Join(logDir, "watch.log")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>EnvironmentVariables</key>
  <dict>
%s  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, launchdLabel, argsXML.String(), envXML.String(), logPath, logPath)
}

func escapeXML(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return value
}
