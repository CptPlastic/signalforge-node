package p25

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
	"gopkg.in/yaml.v3"
)

type backendEvent struct {
	Kind            string
	FrequencyMHz    float64
	Site            string
	Talkgroup       int
	TalkgroupLabel  string
	TalkgroupGroup  string
	TalkgroupTag    string
	AudioPath       string
	StartedAt       time.Time
	Duration        time.Duration
	Phase2          bool
}

type backend interface {
	Events() <-chan backendEvent
	Stop()
}

func newBackend(sys config.System, pool *sdr.Pool, recordingsDir string, phase2 bool) backend {
	if path, _ := exec.LookPath("gophertrunk"); path != "" {
		return newGopherTrunkBackend(sys, pool, recordingsDir, phase2)
	}
	return newNativeBackend(sys, pool, recordingsDir, phase2)
}

// nativeBackend provides in-process decode scaffolding and recording watch.
type nativeBackend struct {
	events chan backendEvent
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newNativeBackend(sys config.System, pool *sdr.Pool, recordingsDir string, phase2 bool) *nativeBackend {
	ctx, cancel := context.WithCancel(context.Background())
	b := &nativeBackend{
		events: make(chan backendEvent, 32),
		cancel: cancel,
	}
	b.wg.Add(1)
	go b.run(ctx, sys, pool, recordingsDir, phase2)
	return b
}

func (b *nativeBackend) Events() <-chan backendEvent { return b.events }
func (b *nativeBackend) Stop() {
	b.cancel()
	b.wg.Wait()
	close(b.events)
}

func (b *nativeBackend) run(ctx context.Context, sys config.System, pool *sdr.Pool, recordingsDir string, phase2 bool) {
	defer b.wg.Done()
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		return
	}
	// Simulate CC lock when we have frequencies and SDRs (integration tests / dry run).
	freqs := sys.AllControlFrequenciesMHz()
	if len(freqs) > 0 && pool.Count() > 0 {
		select {
		case b.events <- backendEvent{Kind: "cc_lock", FrequencyMHz: freqs[0], Site: "auto"}:
		case <-ctx.Done():
			return
		}
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	seen := map[string]struct{}{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			entries, err := os.ReadDir(recordingsDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if ext != ".wav" && ext != ".mp3" {
					continue
				}
				path := filepath.Join(recordingsDir, entry.Name())
				if _, ok := seen[path]; ok {
					continue
				}
				info, err := entry.Info()
				if err != nil || time.Since(info.ModTime()) < 1500*time.Millisecond {
					continue
				}
				seen[path] = struct{}{}
				tg, label := parseCallFilename(entry.Name())
				select {
				case b.events <- backendEvent{
					Kind:           "call_complete",
					Talkgroup:      tg,
					TalkgroupLabel: label,
					AudioPath:      path,
					StartedAt:      info.ModTime(),
					Duration:       3 * time.Second,
					Phase2:         phase2 && strings.Contains(entry.Name(), "p2"),
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func parseCallFilename(name string) (int, string) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) >= 2 {
		var tg int
		fmt.Sscanf(parts[len(parts)-1], "%d", &tg)
		return tg, strings.Join(parts[:len(parts)-1], "_")
	}
	return 0, base
}

// gopherTrunkBackend runs gophertrunk headless when installed.
type gopherTrunkBackend struct {
	events   chan backendEvent
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	wg       sync.WaitGroup
	watchDir string
}

func newGopherTrunkBackend(sys config.System, pool *sdr.Pool, recordingsDir string, phase2 bool) *gopherTrunkBackend {
	ctx, cancel := context.WithCancel(context.Background())
	b := &gopherTrunkBackend{
		events:   make(chan backendEvent, 32),
		cancel:   cancel,
		watchDir: recordingsDir,
	}
	cfgPath, err := writeGopherTrunkConfig(sys, pool, recordingsDir, phase2)
	if err == nil {
		b.cmd = exec.CommandContext(ctx, "gophertrunk", "-headless", "-config", cfgPath)
		b.cmd.Stdout = os.Stdout
		b.cmd.Stderr = os.Stderr
		if err := b.cmd.Start(); err == nil {
			b.wg.Add(1)
			go func() { defer b.wg.Done(); _ = b.cmd.Wait() }()
		}
	}
	native := newNativeBackend(sys, pool, recordingsDir, phase2)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for ev := range native.Events() {
			select {
			case b.events <- ev:
			case <-ctx.Done():
				native.Stop()
				return
			}
		}
	}()
	return b
}

func (b *gopherTrunkBackend) Events() <-chan backendEvent { return b.events }
func (b *gopherTrunkBackend) Stop() {
	b.cancel()
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	b.wg.Wait()
	close(b.events)
}

func writeGopherTrunkConfig(sys config.System, pool *sdr.Pool, recordingsDir string, phase2 bool) (string, error) {
	type gtConfig struct {
		Recordings map[string]string `yaml:"recordings"`
		SDR        map[string]any    `yaml:"sdr"`
		Trunking   map[string]any    `yaml:"trunking"`
	}
	devices := pool.Devices()
	var sdrEntries []map[string]any
	for _, d := range devices {
		role := "control"
		switch d.Role {
		case sdr.RoleVoice:
			role = "voice"
		case sdr.RoleGMRS:
			role = "voice"
		}
		sdrEntries = append(sdrEntries, map[string]any{
			"serial": d.Serial,
			"role":   role,
			"index":  d.Index,
		})
	}
	cc := sys.AllControlFrequenciesMHz()
	var ccStr []string
	for _, f := range cc {
		ccStr = append(ccStr, fmt.Sprintf("%.4f", f))
	}
	cfg := gtConfig{
		Recordings: map[string]string{"directory": recordingsDir},
		SDR:        map[string]any{"devices": sdrEntries},
		Trunking: map[string]any{
			"systems": []map[string]any{{
				"name":              sys.Name,
				"protocol":          "p25",
				"sysid":             sys.SysID,
				"wacn":              sys.WACN,
				"control_channels":  ccStr,
				"talkgroup_csv":     sys.TalkgroupCSV,
				"p25_phase2_enabled": phase2,
			}},
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(recordingsDir, ".gophertrunk-bridge.yaml")
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
