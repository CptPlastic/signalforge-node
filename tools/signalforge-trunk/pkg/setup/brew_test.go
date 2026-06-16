package setup

import "testing"

func TestIsBrewUntrustedError(t *testing.T) {
	msg := `Error: Refusing to load formula trunkrecorder/install/trunk-recorder from untrusted tap trunkrecorder/install.
Run brew trust --formula trunkrecorder/install/trunk-recorder or brew trust trunkrecorder/install to trust it.`
	if !isBrewUntrustedError(msg) {
		t.Fatal("expected untrusted detection")
	}
	if isBrewUntrustedError("formula not found") {
		t.Fatal("unexpected match")
	}
}
