package trunkrecorder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner manages a trunk-recorder subprocess.
type Runner struct {
	binary string
	config string
	cmd    *exec.Cmd
}

func NewRunner(binary, configPath string) *Runner {
	if binary == "" {
		binary = "trunk-recorder"
	}
	return &Runner{binary: binary, config: configPath}
}

func (r *Runner) Start(ctx context.Context) error {
	if _, err := exec.LookPath(r.binary); err != nil {
		return fmt.Errorf("%s not found on PATH; install Trunk Recorder first", r.binary)
	}
	if _, err := os.Stat(r.config); err != nil {
		return fmt.Errorf("trunk recorder config %q: %w", r.config, err)
	}
	r.cmd = exec.CommandContext(ctx, r.binary, "--config="+r.config)
	r.cmd.Stdout = os.Stdout
	r.cmd.Stderr = os.Stderr
	r.cmd.Dir = filepath.Dir(r.config)
	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("start trunk-recorder: %w", err)
	}
	return nil
}

func (r *Runner) Stop() {
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		_, _ = r.cmd.Process.Wait()
	}
}

func (r *Runner) Running() bool {
	return r.cmd != nil && r.cmd.Process != nil
}

// BinaryOnPath reports whether trunk-recorder is installed.
func BinaryOnPath(name string) (string, bool) {
	if name == "" {
		name = "trunk-recorder"
	}
	path, err := exec.LookPath(name)
	return path, err == nil
}

func NormalizeBinary(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "trunk-recorder"
	}
	return name
}
