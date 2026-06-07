package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/profile"
)

const Name = "signalforge-watch"

type Status struct {
	Installed      bool
	Running        bool
	Detail         string
	UnitPath       string
	WatchProcesses []WatchProcess
	Lock           *WatchLock
}

func WatchArgs(prof profile.Profile) ([]string, error) {
	if !prof.Folder.Enabled && !prof.Canary.Enabled {
		return nil, fmt.Errorf("enable folder watch or canary in your profile before installing the service")
	}
	args := []string{"rec", "watch"}
	if prof.Folder.Enabled && strings.TrimSpace(prof.Folder.Directory) != "" {
		args = append(args, "-i", prof.Folder.Directory)
	}
	if prof.Canary.Enabled {
		args = append(args, "--canary")
	}
	if prof.Folder.ReprocessProcessed {
		args = append(args, "--reprocess")
	}
	return args, nil
}

func ExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func EnvFilePath() (string, error) {
	dir, err := profile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "env"), nil
}

func LogDir() (string, error) {
	dir, err := profile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs"), nil
}

func Install(prof profile.Profile) (Status, error) {
	execPath, err := ExecutablePath()
	if err != nil {
		return Status{}, err
	}
	args, err := WatchArgs(prof)
	if err != nil {
		return Status{}, err
	}
	envFile, err := EnvFilePath()
	if err != nil {
		return Status{}, err
	}
	if _, err := os.Stat(envFile); err != nil {
		return Status{}, fmt.Errorf("env file missing at %s — run sf onboard first", envFile)
	}
	logDir, err := LogDir()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return Status{}, err
	}
	return installPlatform(execPath, args, envFile, logDir)
}

func Uninstall() (Status, error) {
	return uninstallPlatform()
}

func CurrentStatus() (Status, error) {
	status, err := statusPlatform()
	if err != nil {
		return Status{}, err
	}
	processes, err := FindWatchProcesses()
	if err != nil {
		return status, err
	}
	status.WatchProcesses = processes
	lock, err := ReadWatchLock()
	if err != nil {
		return status, err
	}
	if lock != nil && !processAlive(lock.PID) {
		_ = removeWatchLock()
		lock = nil
	}
	status.Lock = lock
	if len(processes) > 0 && !status.Running {
		status.Detail = fmt.Sprintf("%d orphan watch process(es); launchd service not loaded", len(processes))
	}
	return status, nil
}

func ParseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values, nil
}

