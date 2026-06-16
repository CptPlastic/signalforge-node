package setup_test

import (
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/setup"
)

func TestCheckReturnsTrunkRecorderDep(t *testing.T) {
	report := setup.Check()
	if report.Platform == "" {
		t.Fatal("platform empty")
	}
	found := false
	for _, item := range report.Items {
		if item.Name == "trunk-recorder" {
			found = true
		}
	}
	if !found {
		t.Fatalf("items=%v", report.Items)
	}
}
