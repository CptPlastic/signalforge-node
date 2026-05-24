package updater

import "testing"

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
