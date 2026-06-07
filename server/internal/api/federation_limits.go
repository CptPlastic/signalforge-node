package api

import "strings"

const (
	federationImportCapUnlisted = 500
	federationImportCapListed   = 1000
)

// federationImportCapForDirectoryStatus returns how many federated calls this hub
// may import per peer, based on local directory listing status.
func federationImportCapForDirectoryStatus(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "listed", "verified":
		return federationImportCapListed
	default:
		return federationImportCapUnlisted
	}
}

func federationImportBatchLimit(remaining int) int {
	if remaining <= 0 {
		return 0
	}
	if remaining > 100 {
		return 100
	}
	return remaining
}
