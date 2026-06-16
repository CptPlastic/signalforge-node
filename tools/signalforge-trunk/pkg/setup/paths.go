package setup

import (
	"os"
	"path/filepath"
)

// DataDir returns ~/.config/signalforge (or platform equivalent).
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", homeErr
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "signalforge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// DefaultConfigPath is the recommended trunk.yaml location.
func DefaultConfigPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trunk.yaml"), nil
}
