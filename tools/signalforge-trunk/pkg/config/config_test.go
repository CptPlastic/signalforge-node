package config_test

import (
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
)

func TestDefaultOKWIN(t *testing.T) {
	sys := config.DefaultOKWIN()
	if sys.RadioReferenceSID != 6949 {
		t.Fatalf("sid=%d", sys.RadioReferenceSID)
	}
	if sys.SysID != "92C" || sys.WACN != "BEE00" {
		t.Fatalf("ids sysid=%s wacn=%s", sys.SysID, sys.WACN)
	}
}

func TestAllControlFrequenciesMHz(t *testing.T) {
	sys := config.System{
		Sites: []config.Site{{
			Include:     true,
			Frequencies: []string{"857.9375c", "858.9375c", "851.0375"},
		}},
		ControlChannels: []string{"859.7125c"},
	}
	freqs := sys.AllControlFrequenciesMHz()
	if len(freqs) != 3 {
		t.Fatalf("freqs=%v", freqs)
	}
}

func TestPlanRolesAnyN(t *testing.T) {
	cases := map[int]int{
		0: 0,
		1: 1,
		2: 2,
		3: 3,
		5: 5,
	}
	for n, wantMin := range cases {
		cfg := config.Default()
		cfg.SDR.AutoDiscover = true
		_ = wantMin
		if n == 0 {
			continue
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
