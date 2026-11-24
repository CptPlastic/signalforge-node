package api

import "testing"

func TestIsUpdateAvailableOrdersByPublishTime(t *testing.T) {
	currentCommit := "a89c29ebc4c5a63b91f4a6639de552e4eb91729c"
	currentTag := "a89c29e"
	builtAt := "2026-11-23T22:59:00Z"

	older := &updateManifest{
		Commit:      "bca8701b06c8e27d975940ba5a4e3dd63d1bb9de",
		ShortCommit: "bca8701",
		ImageTag:    "bca8701",
		Version:     "bca8701",
		PublishedAt: "2026-11-20T19:58:45Z",
	}
	if isUpdateAvailable(currentCommit, currentTag, builtAt, older) {
		t.Fatal("older manifest prompted a downgrade")
	}

	same := &updateManifest{
		Commit:      currentCommit,
		ShortCommit: currentTag,
		ImageTag:    currentTag,
		Version:     currentTag,
		PublishedAt: "2026-11-23T23:05:00Z",
	}
	if isUpdateAvailable(currentCommit, currentTag, builtAt, same) {
		t.Fatal("same release was treated as an update")
	}
	if isUpdateAvailable(currentCommit, "", builtAt, same) {
		t.Fatal("full commit did not match the published short tag")
	}

	newer := &updateManifest{
		Commit:      "ccccccc1234567890ccccccc1234567890cccc",
		ShortCommit: "ccccccc",
		ImageTag:    "ccccccc",
		Version:     "ccccccc",
		PublishedAt: "2026-11-24T01:00:00Z",
	}
	if !isUpdateAvailable(currentCommit, currentTag, builtAt, newer) {
		t.Fatal("newer manifest was not an update")
	}
	if isUpdateAvailable(currentCommit, currentTag, "unknown", newer) {
		t.Fatal("unordered build date was treated as an update")
	}
	if isUpdateAvailable(currentCommit, currentTag, builtAt, &updateManifest{
		Commit:   newer.Commit,
		ImageTag: newer.ImageTag,
	}) {
		t.Fatal("manifest without publishedAt was treated as an update")
	}
}
