package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type WatchProcess struct {
	PID     int
	Command string
}

func FindWatchProcesses() ([]WatchProcess, error) {
	matches := make([]WatchProcess, 0)
	seen := make(map[int]struct{})
	for _, pattern := range []string{"recorder watch", "rec watch", "rec w "} {
		out, err := exec.Command("pgrep", "-lf", pattern).Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				continue
			}
			return nil, fmt.Errorf("pgrep %q: %w", pattern, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			process, ok := parsePgrepLine(line)
			if !ok {
				continue
			}
			if !isWatchCommand(process.Command) {
				continue
			}
			if process.PID == os.Getpid() {
				continue
			}
			if _, dup := seen[process.PID]; dup {
				continue
			}
			seen[process.PID] = struct{}{}
			matches = append(matches, process)
		}
	}
	return matches, nil
}

func StopWatchProcesses() ([]WatchProcess, error) {
	processes, err := FindWatchProcesses()
	if err != nil {
		return nil, err
	}
	stopped := make([]WatchProcess, 0, len(processes))
	for _, process := range processes {
		if err := terminateProcess(process.PID); err != nil {
			return stopped, fmt.Errorf("stop pid %d: %w", process.PID, err)
		}
		stopped = append(stopped, process)
	}
	_ = removeWatchLock()
	return stopped, nil
}

func parsePgrepLine(line string) (WatchProcess, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return WatchProcess{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return WatchProcess{}, false
	}
	command := strings.TrimSpace(strings.Join(fields[1:], " "))
	return WatchProcess{PID: pid, Command: command}, true
}

func isWatchCommand(command string) bool {
	lower := strings.ToLower(command)
	if !strings.Contains(lower, "rec") {
		return false
	}
	return strings.Contains(lower, "recorder watch") ||
		strings.Contains(lower, "rec watch") ||
		strings.Contains(lower, "rec w ")
}

func terminateProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}

func formatWatchProcesses(processes []WatchProcess) string {
	if len(processes) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for index, process := range processes {
		if index > 0 {
			buf.WriteString("; ")
		}
		fmt.Fprintf(&buf, "pid %d (%s)", process.PID, process.Command)
	}
	return buf.String()
}
