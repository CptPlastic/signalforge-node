package updater

import (
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "0.2.0", current: "0.1.0", want: true},
		{latest: "0.1.1", current: "0.1.0", want: true},
		{latest: "0.1.0", current: "0.1.0", want: false},
		{latest: "0.1.0", current: "dev", want: false},
		{latest: "signalforge-cli-v0.2.0", current: "v0.1.0", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.latest+"/"+tt.current, func(t *testing.T) {
			if got := isNewer(normalizeVersion(tt.latest), normalizeVersion(tt.current)); got != tt.want {
				t.Fatalf("isNewer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeReleaseResponseSelectsSignalForgeCLIRelease(t *testing.T) {
	release, err := decodeReleaseResponse(strings.NewReader(`[
		{"tag_name":"v1.2.10","html_url":"https://example.invalid/old","assets":[]},
		{"tag_name":"signalforge-cli-v0.1.0","html_url":"https://example.invalid/cli","assets":[{"name":"signalforge-windows-amd64.exe","browser_download_url":"https://example.invalid/cli.exe"}]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "signalforge-cli-v0.1.0" {
		t.Fatalf("unexpected release: %+v", release)
	}
}
