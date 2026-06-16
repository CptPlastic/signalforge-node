package conventional

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
)

// CallEvent is a completed conventional FM transmission.
type CallEvent struct {
	Name        string
	ChannelMHz  float64
	Talkgroup   int
	Label       string
	AudioPath   string
	StartedAt   time.Time
	Duration    time.Duration
}

// Scanner monitors GMRS conventional channels.
type Scanner struct {
	mu       sync.RWMutex
	name     string
	channels []float64
	pool     *sdr.Pool
	onCall   func(CallEvent)
	index    int
}

func NewScanner(name string, channels []string, pool *sdr.Pool) (*Scanner, error) {
	var mhz []float64
	for _, ch := range channels {
		if v, ok := config.ParseFrequencyMHz(ch); ok {
			mhz = append(mhz, v)
		}
	}
	if len(mhz) == 0 {
		return nil, fmt.Errorf("no valid GMRS channels configured")
	}
	return &Scanner{name: name, channels: mhz, pool: pool}, nil
}

func (s *Scanner) SetCallHandler(fn func(CallEvent)) {
	s.onCall = fn
}

func (s *Scanner) Start(ctx context.Context, recordingsDir string) {
	if recordingsDir != "" {
		go s.watchRecordings(ctx, recordingsDir)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.step(recordingsDir)
		}
	}
}

func (s *Scanner) watchRecordings(ctx context.Context, dir string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	seen := map[string]struct{}{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if entry.IsDir() || (!strings.HasPrefix(name, "gmrs") && !strings.Contains(name, "gmrs_")) {
					continue
				}
				ext := filepath.Ext(name)
				if ext != ".wav" && ext != ".mp3" {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				if _, ok := seen[path]; ok {
					continue
				}
				info, err := entry.Info()
				if err != nil || time.Since(info.ModTime()) < 1500*time.Millisecond {
					continue
				}
				seen[path] = struct{}{}
				ch := s.channels[0]
				if s.onCall != nil {
					s.onCall(CallEvent{
						Name:       s.name,
						ChannelMHz: ch,
						Talkgroup:  GMRSTalkgroupID(ch),
						Label:      fmt.Sprintf("GMRS %.4f MHz", ch),
						AudioPath:  path,
						StartedAt:  info.ModTime(),
						Duration:   3 * time.Second,
					})
				}
			}
		}
	}
}

func (s *Scanner) step(recordingsDir string) {
	devices := s.pool.ByRole(sdr.RoleGMRS)
	if len(devices) == 0 {
		return
	}
	s.mu.Lock()
	ch := s.channels[s.index%len(s.channels)]
	s.index++
	s.mu.Unlock()
	_ = devices[0]
	_ = ch
	_ = recordingsDir
}

func (s *Scanner) Channels() []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]float64, len(s.channels))
	copy(out, s.channels)
	return out
}

func (s *Scanner) Summary() string {
	return fmt.Sprintf("%s: %d channels", s.name, len(s.Channels()))
}

// GMRSTalkgroupID maps GMRS channel MHz to a stable talkgroup id for Hub upload.
func GMRSTalkgroupID(mhz float64) int {
	return int(mhz * 10000)
}
