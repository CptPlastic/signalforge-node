package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

const ReleaseRepo = "CptPlastic/p7-scanner"

func DisplayVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
