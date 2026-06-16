package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
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
	fmt.Fprintf(out, "$ %s %s\n", name, joinArgs(args))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, joinArgs(args), err)
	}
	return nil
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
