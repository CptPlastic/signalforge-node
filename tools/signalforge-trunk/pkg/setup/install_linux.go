//go:build linux

package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func installPlatform(ctx context.Context, out io.Writer) error {
	if path, ok := binaryOnPath("trunk-recorder"); ok {
		fmt.Fprintf(out, "trunk-recorder already installed at %s\n", path)
		return nil
	}
	dir, err := TrunkRecorderRepoDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "install.sh")); err != nil {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		if err := runCommand(ctx, out, filepath.Dir(dir), "git", "clone", trunkRecorderRepo, dir); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Building trunk-recorder from source (requires sudo for packages and install)...")
	return runCommand(ctx, out, dir, "sh", "./install.sh")
}
