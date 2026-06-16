package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const trunkRecorderRepo = "https://github.com/TrunkRecorder/trunk-recorder.git"

// Install installs trunk-recorder and platform prerequisites.
func Install(ctx context.Context, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	return installPlatform(ctx, out)
}

func runCommand(ctx context.Context, out io.Writer, dir string, name string, args ...string) error {
	_, err := runCommandCapture(ctx, out, dir, name, args...)
	return err
}

func runCommandCapture(ctx context.Context, out io.Writer, dir string, name string, args ...string) (string, error) {
	fmt.Fprintf(out, "$ %s %s\n", name, joinArgs(args))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var captured strings.Builder
	cmd.Stdout = io.MultiWriter(out, &captured)
	cmd.Stderr = io.MultiWriter(out, &captured)
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return captured.String(), fmt.Errorf("%s %s: %w", name, joinArgs(args), err)
	}
	return captured.String(), nil
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	out := args[0]
	for _, arg := range args[1:] {
		out += " " + arg
	}
	return out
}

func unsupportedInstall() error {
	return fmt.Errorf("automatic install is not supported on %s — see docs/OPERATOR.md", runtime.GOOS)
}

func isBrewUntrustedError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "untrusted tap") ||
		strings.Contains(lower, "refusing to load formula") ||
		strings.Contains(lower, "brew trust")
}
