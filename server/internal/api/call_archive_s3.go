package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// syncArchiveDirsToS3 uploads day folders to an S3-compatible bucket via s3cmd.
func syncArchiveDirsToS3(
	ctx context.Context,
	logger *slog.Logger,
	s3URI string,
	s3Cfg string,
	archiveDir string,
	dayDirs []string,
) (int, error) {
	if strings.TrimSpace(s3URI) == "" {
		return 0, nil
	}
	bucket, prefix, err := parseS3URI(s3URI)
	if err != nil {
		return 0, err
	}
	cfgPath := strings.TrimSpace(s3Cfg)
	if cfgPath == "" {
		return 0, fmt.Errorf("CALL_ARCHIVE_S3CFG is required when CALL_ARCHIVE_S3_URI is set")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return 0, fmt.Errorf("s3cmd config %s: %w", cfgPath, err)
	}
	if _, err := exec.LookPath("s3cmd"); err != nil {
		return 0, fmt.Errorf("s3cmd not found in PATH: %w", err)
	}

	uploaded := 0
	for _, day := range dayDirs {
		localDir := filepath.Join(archiveDir, day)
		if info, err := os.Stat(localDir); err != nil || !info.IsDir() {
			continue
		}
		remoteURI := joinS3URI(bucket, prefix, day)
		if err := runS3cmdSync(ctx, cfgPath, localDir, remoteURI); err != nil {
			return uploaded, fmt.Errorf("sync %s to %s: %w", localDir, remoteURI, err)
		}
		uploaded++
		logger.Info("call archive uploaded to object storage", "local", localDir, "remote", remoteURI)
	}
	return uploaded, nil
}

func runS3cmdSync(ctx context.Context, cfgPath, localDir, remoteURI string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "s3cmd",
		"-c", cfgPath,
		"sync",
		ensureTrailingSlash(localDir),
		ensureTrailingSlash(remoteURI),
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func parseS3URI(uri string) (bucket, prefix string, err error) {
	trimmed := strings.TrimSpace(uri)
	if !strings.HasPrefix(trimmed, "s3://") {
		return "", "", fmt.Errorf("CALL_ARCHIVE_S3_URI must start with s3://")
	}
	rest := strings.TrimPrefix(trimmed, "s3://")
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return "", "", fmt.Errorf("CALL_ARCHIVE_S3_URI is missing bucket name")
	}
	parts := strings.SplitN(rest, "/", 2)
	bucket = parts[0]
	if bucket == "" {
		return "", "", fmt.Errorf("CALL_ARCHIVE_S3_URI is missing bucket name")
	}
	if len(parts) == 2 {
		prefix = strings.Trim(parts[1], "/")
	}
	return bucket, prefix, nil
}

func joinS3URI(bucket, prefix, day string) string {
	day = strings.Trim(day, "/")
	if prefix == "" {
		return fmt.Sprintf("s3://%s/%s/", bucket, day)
	}
	return fmt.Sprintf("s3://%s/%s/%s/", bucket, prefix, day)
}

func ensureTrailingSlash(path string) string {
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

func removeArchiveDayDirs(archiveDir string, dayDirs []string) error {
	for _, day := range dayDirs {
		if err := os.RemoveAll(filepath.Join(archiveDir, day)); err != nil {
			return err
		}
	}
	return nil
}
