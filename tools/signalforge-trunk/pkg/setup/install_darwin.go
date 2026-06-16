//go:build darwin

package setup

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

func installPlatform(ctx context.Context, out io.Writer) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("homebrew not found — install from https://brew.sh then re-run sf trunk setup")
	}
	if path, ok := binaryOnPath("trunk-recorder"); ok {
		fmt.Fprintf(out, "trunk-recorder already installed at %s\n", path)
		return nil
	}
	if err := runCommand(ctx, out, "", "brew", "tap", "trunkrecorder/install"); err != nil {
		return err
	}
	return runCommand(ctx, out, "", "brew", "install", "trunk-recorder")
}
