package cli

import (
	"bytes"
	"testing"
)

func TestRootHelpListsShortCommands(t *testing.T) {
	t.Setenv("SIGNALFORGE_NO_UPDATE_CHECK", "1")
	t.Setenv("NO_COLOR", "1")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("root help failed: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"[REC] rec", "[HUB] hub", "sf rec chk"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Fatalf("expected %q in root help:\n%s", want, text)
		}
	}
}
