package setup

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Dep describes one trunk recorder prerequisite.
type Dep struct {
	Name        string
	Status      string // ok, missing, warn
	Detail      string
	Installable bool
}

// Report is the dependency scan for the current machine.
type Report struct {
	Platform string
	Items    []Dep
}

func (r Report) Ready() bool {
	for _, item := range r.Items {
		if item.Name == "trunk-recorder" && item.Status != "ok" {
			return false
		}
	}
	return true
}

func (r Report) NeedsInstall() bool {
	for _, item := range r.Items {
		if item.Installable && item.Status == "missing" {
			return true
		}
	}
	return false
}

// Check scans for trunk-recorder and platform tooling.
func Check() Report {
	platform := runtime.GOOS
	var items []Dep

	if path, ok := binaryOnPath("trunk-recorder"); ok {
		items = append(items, Dep{Name: "trunk-recorder", Status: "ok", Detail: path})
	} else {
		items = append(items, Dep{
			Name:        "trunk-recorder",
			Status:      "missing",
			Detail:      "decode engine not installed",
			Installable: platform == "darwin" || platform == "linux",
		})
	}

	switch platform {
	case "darwin":
		if path, ok := binaryOnPath("brew"); ok {
			items = append(items, Dep{Name: "homebrew", Status: "ok", Detail: path})
		} else {
			items = append(items, Dep{
				Name:   "homebrew",
				Status: "missing",
				Detail: "required to install trunk-recorder — https://brew.sh",
			})
		}
	case "linux":
		if path, ok := binaryOnPath("git"); ok {
			items = append(items, Dep{Name: "git", Status: "ok", Detail: path})
		} else {
			items = append(items, Dep{Name: "git", Status: "missing", Detail: "required to build trunk-recorder", Installable: true})
		}
		if _, ok := binaryOnPath("apt-get"); ok {
			items = append(items, Dep{Name: "apt", Status: "ok", Detail: "package manager available"})
		} else {
			items = append(items, Dep{Name: "apt", Status: "warn", Detail: "apt not found; manual build may be required"})
		}
		items = append(items, Dep{
			Name:   "rtl-sdr drivers",
			Status: rtlDriverStatus(),
			Detail: rtlDriverDetail(),
		})
	default:
		items = append(items, Dep{
			Name:   "platform",
			Status: "warn",
			Detail: "automatic install supported on macOS and Linux only",
		})
	}

	return Report{Platform: platform, Items: items}
}

func binaryOnPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}

func rtlDriverStatus() string {
	if runtime.GOOS != "linux" {
		return "ok"
	}
	if _, err := os.Stat("/etc/modprobe.d/blacklist-rtl.conf"); err == nil {
		return "ok"
	}
	if _, err := os.Stat("/etc/modprobe.d/blacklist-rtlsdr.conf"); err == nil {
		return "ok"
	}
	return "warn"
}

func rtlDriverDetail() string {
	if rtlDriverStatus() == "ok" {
		return "RTL-SDR blacklist present"
	}
	return "install will add blacklist-rtl.conf (reboot required on Linux)"
}

func TrunkRecorderRepoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(home, "/") + "/.signalforge/trunk-recorder-src", nil
}
