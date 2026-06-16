package setup

import (
	"os"
	"path/filepath"
)

// BundledSamplePath writes the built-in OKWIN starter bundle and returns its path.
func BundledSamplePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, bundledSampleName)
	if err := os.WriteFile(path, okwinBundleCSV, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ResolveSampleCSV picks the best RR CSV path for setup.
func ResolveSampleCSV(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if path, err := BundledSamplePath(); err == nil {
		return path, nil
	}
	candidates := []string{
		"samples/okwin-bundle.csv",
		"tools/signalforge-trunk/samples/okwin-bundle.csv",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			abs, err := filepath.Abs(candidate)
			if err == nil {
				return abs, nil
			}
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
