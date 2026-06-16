package hunt

import (
	"fmt"
	"sync"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
)

// State tracks control-channel hunt progress.
type State struct {
	mu            sync.RWMutex
	Frequencies   []float64
	Index         int
	Locked        bool
	LockedMHz     float64
	LockedSite    string
	CurrentMHz    float64
	LastHuntAt    time.Time
	Attempts      int
}

// Hunter rotates through OKWIN control-channel frequencies until lock.
type Hunter struct {
	state   State
	rotate  time.Duration
	onTune  func(mhz float64) error
	onLock  func(mhz float64, site string)
}

func NewHunter(sys config.System, rotate time.Duration) *Hunter {
	if rotate <= 0 {
		rotate = 3 * time.Second
	}
	return &Hunter{
		state: State{Frequencies: sys.AllControlFrequenciesMHz()},
		rotate: rotate,
	}
}

func (h *Hunter) SetTuneCallback(fn func(mhz float64) error) {
	h.onTune = fn
}

func (h *Hunter) SetLockCallback(fn func(mhz float64, site string)) {
	h.onLock = fn
}

func (h *Hunter) Frequencies() []float64 {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()
	out := make([]float64, len(h.state.Frequencies))
	copy(out, h.state.Frequencies)
	return out
}

func (h *Hunter) Current() (mhz float64, locked bool) {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()
	if h.state.Locked {
		return h.state.LockedMHz, true
	}
	return h.state.CurrentMHz, false
}

func (h *Hunter) Lock(mhz float64, site string) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.Locked = true
	h.state.LockedMHz = mhz
	h.state.LockedSite = site
	h.state.CurrentMHz = mhz
	if h.onLock != nil {
		h.onLock(mhz, site)
	}
}

func (h *Hunter) Unlock() {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.Locked = false
	h.state.LockedMHz = 0
	h.state.LockedSite = ""
}

// Step advances to the next frequency when not locked.
func (h *Hunter) Step() error {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	if h.state.Locked {
		return nil
	}
	if len(h.state.Frequencies) == 0 {
		return fmt.Errorf("no control frequencies configured; run sf trunk import-rr")
	}
	h.state.Index = (h.state.Index + 1) % len(h.state.Frequencies)
	h.state.CurrentMHz = h.state.Frequencies[h.state.Index]
	h.state.LastHuntAt = time.Now()
	h.state.Attempts++
	if h.onTune != nil {
		return h.onTune(h.state.CurrentMHz)
	}
	return nil
}

// Run starts the hunt loop until ctx done.
func (h *Hunter) Run(ctxDone <-chan struct{}) {
	ticker := time.NewTicker(h.rotate)
	defer ticker.Stop()
	_ = h.Step()
	for {
		select {
		case <-ctxDone:
			return
		case <-ticker.C:
			h.state.mu.RLock()
			locked := h.state.Locked
			h.state.mu.RUnlock()
			if locked {
				continue
			}
			_ = h.Step()
		}
	}
}

func (h *Hunter) Summary() string {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()
	if h.state.Locked {
		return fmt.Sprintf("LOCKED %.4f MHz site=%s", h.state.LockedMHz, h.state.LockedSite)
	}
	if len(h.state.Frequencies) == 0 {
		return "no control frequencies"
	}
	return fmt.Sprintf("HUNTING %.4f MHz (%d/%d freqs, %d attempts)",
		h.state.CurrentMHz, h.state.Index+1, len(h.state.Frequencies), h.state.Attempts)
}
