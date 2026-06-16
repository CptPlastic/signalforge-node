package trunkrecorder

import (
	"os"
	"path/filepath"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
)

func copyTalkgroups(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// CopyTalkgroupsToBase copies talkgroup CSVs next to trunk-recorder.json.
func CopyTalkgroupsToBase(cfg config.Config, configPath string) error {
	base := cfg.BaseDir(configPath)
	for _, sys := range cfg.Trunking.Systems {
		if sys.TalkgroupCSV == "" {
			continue
		}
		src := cfg.Resolve(sys.TalkgroupCSV, configPath)
		dst := filepath.Join(base, filepath.Base(sys.TalkgroupCSV))
		if src == dst {
			continue
		}
		if err := copyTalkgroups(src, dst); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
