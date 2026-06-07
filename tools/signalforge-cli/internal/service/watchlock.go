package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/profile"
)

const watchLockName = "watch.lock"

type WatchLock struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	StartedAt time.Time `json:"startedAt"`
}

func WatchLockPath() (string, error) {
	dir, err := profile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, watchLockName), nil
}

func AcquireWatchLock(command string) error {
	existing, _ := FindWatchProcesses()
	if len(existing) > 0 {
		return fmt.Errorf("watch already running: %s (run: sf rec stop)", formatWatchProcesses(existing))
	}
	if lock, err := ReadWatchLock(); err == nil && lock != nil && processAlive(lock.PID) {
		return fmt.Errorf("watch lock held by pid %d (run: sf rec stop)", lock.PID)
	}
	_ = removeWatchLock()

	path, err := WatchLockPath()
	if err != nil {
		return err
	}
	lock := WatchLock{
		PID:       os.Getpid(),
		Command:   command,
		StartedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func ReleaseWatchLock() error {
	lock, err := ReadWatchLock()
	if err != nil {
		return err
	}
	if lock != nil && lock.PID != os.Getpid() {
		return nil
	}
	return removeWatchLock()
}

func ReadWatchLock() (*WatchLock, error) {
	path, err := WatchLockPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var lock WatchLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func removeWatchLock() error {
	path, err := WatchLockPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
