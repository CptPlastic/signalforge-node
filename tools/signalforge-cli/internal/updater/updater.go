package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/buildinfo"
)

const (
	defaultReleaseAPI = "https://api.github.com/repos/" + buildinfo.ReleaseRepo + "/releases/latest"
	cacheFileName     = "update-check.json"
)

type Options struct {
	CurrentVersion string
	ReleaseAPI     string
	HTTPClient     *http.Client
}

type Result struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseURL      string
	AssetName       string
	AssetURL        string
	UpdateAvailable bool
}

type releaseResponse struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type cacheState struct {
	CheckedAt int64 `json:"checkedAt"`
}

func Check(ctx context.Context, options Options) (Result, error) {
	current := normalizeVersion(options.CurrentVersion)
	if current == "" {
		current = normalizeVersion(buildinfo.DisplayVersion())
	}
	apiURL := strings.TrimSpace(options.ReleaseAPI)
	if apiURL == "" {
		apiURL = defaultReleaseAPI
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SignalForge CLI")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("release check returned %s", resp.Status)
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Result{}, err
	}
	latest := normalizeVersion(release.TagName)
	asset := bestAsset(release.Assets)
	return Result{
		CurrentVersion:  current,
		LatestVersion:   latest,
		ReleaseURL:      release.HTMLURL,
		AssetName:       asset.Name,
		AssetURL:        asset.URL,
		UpdateAvailable: isNewer(latest, current),
	}, nil
}

func ShouldAutoCheck(now time.Time, interval time.Duration) bool {
	if os.Getenv("SIGNALFORGE_NO_UPDATE_CHECK") == "1" {
		return false
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	state, err := readCache()
	if err != nil {
		return true
	}
	return now.Sub(time.Unix(state.CheckedAt, 0)) >= interval
}

func MarkChecked(now time.Time) {
	_ = writeCache(cacheState{CheckedAt: now.Unix()})
}

func CachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "signalforge", cacheFileName), nil
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "refs/tags/")
	value = strings.TrimPrefix(value, "signalforge-cli-v")
	value = strings.TrimPrefix(value, "v")
	return value
}

func isNewer(latest, current string) bool {
	if latest == "" || current == "" || current == "dev" {
		return false
	}
	latestParts, latestOK := parseVersion(latest)
	currentParts, currentOK := parseVersion(current)
	if !latestOK || !currentOK {
		return latest != current
	}
	for index := range latestParts {
		if latestParts[index] > currentParts[index] {
			return true
		}
		if latestParts[index] < currentParts[index] {
			return false
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	var parts [3]int
	fields := strings.Split(value, ".")
	if len(fields) == 0 || len(fields) > 3 {
		return parts, false
	}
	for index, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return parts, false
		}
		for _, ch := range field {
			if ch < '0' || ch > '9' {
				return parts, false
			}
			parts[index] = parts[index]*10 + int(ch-'0')
		}
	}
	return parts, true
}

func bestAsset(assets []releaseAsset) releaseAsset {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, goos) && strings.Contains(name, goarch) {
			return asset
		}
	}
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, goos) {
			return asset
		}
	}
	if len(assets) > 0 {
		return assets[0]
	}
	return releaseAsset{}
}

func readCache() (cacheState, error) {
	path, err := CachePath()
	if err != nil {
		return cacheState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheState{}, err
	}
	var state cacheState
	if err := json.Unmarshal(data, &state); err != nil {
		return cacheState{}, err
	}
	if state.CheckedAt <= 0 {
		return cacheState{}, errors.New("empty update check cache")
	}
	return state, nil
}

func writeCache(state cacheState) error {
	path, err := CachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
