//go:build !darwin && !linux

package setup

import (
	"context"
	"io"
)

func installPlatform(ctx context.Context, out io.Writer) error {
	return unsupportedInstall()
}
