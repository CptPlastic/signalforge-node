package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

const ReleaseRepo = "CptPlastic/signalforge.org"

func DisplayVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
