package trunking_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/trunking"
)

func TestTalkgroupDBLoadCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tg.csv")
	csv := "Decimal,Hex,Mode,Alpha Tag,Description,Tag,Group\n2711,a97,D,CentralOK RMA 2A,RMA,Interop,Central OK\n"
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	db := trunking.NewTalkgroupDB()
	if err := db.LoadCSV(path); err != nil {
		t.Fatal(err)
	}
	tg, ok := db.Lookup(2711)
	if !ok || tg.AlphaTag != "CentralOK RMA 2A" {
		t.Fatalf("tg=%+v ok=%v", tg, ok)
	}
}
