package hunt_test

import (
	"testing"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hunt"
)

func TestHunterLockAndStep(t *testing.T) {
	sys := config.System{
		ControlChannels: []string{"857.9375c", "858.9375c"},
	}
	h := hunt.NewHunter(sys, 10*time.Millisecond)
	if len(h.Frequencies()) != 2 {
		t.Fatalf("freqs=%v", h.Frequencies())
	}
	_ = h.Step()
	mhz, locked := h.Current()
	if locked || mhz == 0 {
		t.Fatalf("mhz=%f locked=%v", mhz, locked)
	}
	h.Lock(857.9375, "Oklahoma City")
	_, locked = h.Current()
	if !locked {
		t.Fatal("expected lock")
	}
}
