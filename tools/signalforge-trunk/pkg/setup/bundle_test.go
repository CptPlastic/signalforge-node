package setup_test

import (
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/setup"
)

func TestBundledSamplePath(t *testing.T) {
	path, err := setup.BundledSamplePath()
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := setup.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
}

func TestResolveSampleCSVUsesBundle(t *testing.T) {
	path, err := setup.ResolveSampleCSV("")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
}
