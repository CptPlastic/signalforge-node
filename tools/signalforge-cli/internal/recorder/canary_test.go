package recorder

import (
	"testing"
	"time"
)

func TestNormalizeCanaryIntervalEnforcesMinimum(t *testing.T) {
	if got := NormalizeCanaryInterval(2 * time.Second); got != MinCanaryInterval {
		t.Fatalf("expected minimum %s, got %s", MinCanaryInterval, got)
	}
	if got := NormalizeCanaryInterval(2 * time.Minute); got != 2*time.Minute {
		t.Fatalf("expected 2m, got %s", got)
	}
}

func TestIsCanaryHeartbeatFile(t *testing.T) {
	if !IsCanaryHeartbeatFile("canary-1710000000.wav") {
		t.Fatal("expected canary heartbeat filename")
	}
	if IsCanaryHeartbeatFile("call.wav") {
		t.Fatal("expected normal call filename")
	}
}
