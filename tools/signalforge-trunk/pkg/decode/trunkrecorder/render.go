package trunkrecorder

import (
	"fmt"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
)

// Render writes trunk-recorder.json and copies talkgroup CSVs beside it.
func Render(cfg config.Config, configPath string, devices []sdr.Device) (string, error) {
	if err := CopyTalkgroupsToBase(cfg, configPath); err != nil {
		return "", err
	}
	tr, err := Generate(cfg, configPath, devices)
	if err != nil {
		return "", err
	}
	outPath := cfg.Resolve(cfg.Decode.TrunkRecorder.ConfigPath, configPath)
	if err := Write(outPath, tr); err != nil {
		return "", err
	}
	return outPath, nil
}

// RenderOrDiscover renders TR config after SDR discovery when devices is nil.
func RenderOrDiscover(cfg config.Config, configPath string, devices []sdr.Device) (string, error) {
	if len(devices) == 0 {
		pool := sdr.NewPool()
		var err error
		devices, err = pool.Discover()
		if err != nil {
			return "", err
		}
		if len(devices) == 0 {
			return "", fmt.Errorf("no RTL-SDR devices found")
		}
	}
	return Render(cfg, configPath, devices)
}
