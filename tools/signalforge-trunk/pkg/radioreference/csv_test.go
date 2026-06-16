package radioreference_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/radioreference"
)

func TestImportCSVBundle(t *testing.T) {
	path := filepath.Join("..", "..", "samples", "okwin-bundle.csv")
	sys, rows, err := radioreference.ImportCSVBundle(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sys.Name != "OKWIN" || sys.SysID != "92C" {
		t.Fatalf("sys=%+v", sys)
	}
	if len(sys.Sites) < 2 {
		t.Fatalf("sites=%d", len(sys.Sites))
	}
	if len(sys.AllControlFrequenciesMHz()) == 0 {
		t.Fatal("no control channels")
	}
	if len(rows) < 3 {
		t.Fatalf("rows=%d", len(rows))
	}
}

func TestMergeSystem(t *testing.T) {
	cfg := config.Default()
	sys := config.DefaultOKWIN()
	rows := [][]string{{"Decimal", "Alpha Tag"}, {"2711", "CentralOK RMA 2A"}}
	dir := t.TempDir()
	if err := radioreference.MergeSystem(&cfg, sys, rows, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, sys.TalkgroupCSV)); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Trunking.Systems) != 1 {
		t.Fatalf("systems=%d", len(cfg.Trunking.Systems))
	}
}
