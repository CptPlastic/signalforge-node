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

func TestCanaryWAVIsAudible(t *testing.T) {
	audio, duration := CanaryWAV()
	if duration <= 0 {
		t.Fatalf("expected positive duration, got %s", duration)
	}
	if len(audio) <= 44 {
		t.Fatal("expected wav payload")
	}
	nonZero := 0
	for i := 44; i < len(audio); i += 2 {
		if audio[i] != 0 || audio[i+1] != 0 {
			nonZero++
		}
	}
	if nonZero < 100 {
		t.Fatalf("expected audible samples, got %d non-zero", nonZero)
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
