package config

import "testing"

func TestNormalizeStreamPublicURL(t *testing.T) {
	got, err := NormalizeStreamPublicURL(" https://stream.p7hub.projectseven.us/ ")
	if err != nil || got != "https://stream.p7hub.projectseven.us" {
		t.Fatalf("got %q err %v", got, err)
	}
	got, err = NormalizeStreamPublicURL("https://stream.example:8443")
	if err != nil || got != "https://stream.example:8443" {
		t.Fatalf("port got %q err %v", got, err)
	}
	if _, err := NormalizeStreamPublicURL(""); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"stream.example", "https://stream.example/public/stream", "ftp://stream.example", "https://user:pass@stream.example"} {
		if _, err := NormalizeStreamPublicURL(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
