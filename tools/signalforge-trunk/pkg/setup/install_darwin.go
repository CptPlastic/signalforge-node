//go:build darwin

package setup

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

const (
	brewTrunkTap      = "trunkrecorder/install"
	brewTrunkFormula  = "trunkrecorder/install/trunk-recorder"
	brewTrunkPackage  = "trunk-recorder"
)

func installPlatform(ctx context.Context, out io.Writer) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("homebrew not found — install from https://brew.sh then re-run sf trk setup")
	}
	if path, ok := binaryOnPath("trunk-recorder"); ok {
		fmt.Fprintf(out, "trunk-recorder already installed at %s\n", path)
		return nil
	}
	if err := runCommand(ctx, out, "", "brew", "tap", brewTrunkTap); err != nil {
		return err
	}
	if err := brewTrustTrunkRecorder(ctx, out); err != nil {
		fmt.Fprintf(out, "[!!] trust: %v\n", err)
	}
	if err := brewInstallTrunkRecorder(ctx, out); err != nil {
		return err
	}
	if _, ok := binaryOnPath("trunk-recorder"); !ok {
		return fmt.Errorf("trunk-recorder install finished but binary not found on PATH")
	}
	return nil
}

func brewTrustTrunkRecorder(ctx context.Context, out io.Writer) error {
	if !brewHasTrustCommand() {
		return nil
	}
	fmt.Fprintf(out, "[..] trust: authorizing %s for Homebrew 6 tap trust\n", brewTrunkFormula)
	if err := runCommand(ctx, out, "", "brew", "trust", "--formula", brewTrunkFormula); err == nil {
		fmt.Fprintf(out, "[OK] trust: formula trusted\n")
		return nil
	}
	fmt.Fprintf(out, "[..] trust: trying tap-level trust for %s\n", brewTrunkTap)
	if err := runCommand(ctx, out, "", "brew", "trust", brewTrunkTap); err != nil {
		return fmt.Errorf("run manually: brew trust --formula %s", brewTrunkFormula)
	}
	fmt.Fprintf(out, "[OK] trust: tap trusted\n")
	return nil
}

func brewInstallTrunkRecorder(ctx context.Context, out io.Writer) error {
	output, err := runCommandCapture(ctx, out, "", "brew", "install", brewTrunkPackage)
	if err == nil {
		return nil
	}
	if !isBrewUntrustedError(output) {
		return err
	}
	fmt.Fprintf(out, "[!!] install: Homebrew requires explicit tap trust (Homebrew 6+)\n")
	if trustErr := brewTrustTrunkRecorder(ctx, out); trustErr != nil {
		return fmt.Errorf("%w\n\nmanual fix:\n  brew trust --formula %s\n  brew install %s",
			trustErr, brewTrunkFormula, brewTrunkPackage)
	}
	_, err = runCommandCapture(ctx, out, "", "brew", "install", brewTrunkPackage)
	return err
}

func brewHasTrustCommand() bool {
	cmd := exec.Command("brew", "trust", "--help")
	return cmd.Run() == nil
}
