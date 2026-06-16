package sdr_test

import (
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
)

func TestPlanRolesForN(t *testing.T) {
	tests := []struct {
		n                      int
		control, voice, gmrs   int
	}{
		{1, 1, 0, 0},
		{2, 1, 1, 0},
		{3, 1, 1, 1},
		{4, 1, 2, 1},
		{5, 1, 2, 1},
	}
	for _, tc := range tests {
		plan := sdr.PlanRolesForN(tc.n)
		if plan.ControlHunt != tc.control || plan.Voice != tc.voice || plan.GMRS != tc.gmrs {
			t.Fatalf("n=%d got %+v want control=%d voice=%d gmrs=%d", tc.n, plan, tc.control, tc.voice, tc.gmrs)
		}
	}
}

func TestPoolRebalanceEmpty(t *testing.T) {
	pool := sdr.NewPool()
	plan := pool.Rebalance()
	if plan.ControlHunt != 0 {
		t.Fatalf("plan=%+v", plan)
	}
}
