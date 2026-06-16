package p25

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hunt"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/trunking"
)

// CallEvent is emitted when a voice call completes.
type CallEvent struct {
	System        config.System
	Talkgroup     int
	TalkgroupLabel string
	TalkgroupGroup string
	TalkgroupTag   string
	FrequencyHz   int
	AudioPath     string
	StartedAt     time.Time
	Duration      time.Duration
	Phase2        bool
}

// Engine coordinates P25 trunk decode for OKWIN.
type Engine struct {
	mu         sync.RWMutex
	sys        config.System
	tgDB       *trunking.TalkgroupDB
	hunter     *hunt.Hunter
	pool       *sdr.Pool
	phase2     bool
	active     int
	lastCall   time.Time
	onCall     func(CallEvent)
	recording  string
}

func NewEngine(sys config.System, pool *sdr.Pool, tgDB *trunking.TalkgroupDB, recordingsDir string) *Engine {
	e := &Engine{
		sys:       sys,
		tgDB:      tgDB,
		hunter:    hunt.NewHunter(sys, 3*time.Second),
		pool:      pool,
		phase2:    sys.P25Phase2Enabled,
		recording: recordingsDir,
	}
	e.hunter.SetTuneCallback(func(mhz float64) error {
		return e.tuneControl(mhz)
	})
	return e
}

func (e *Engine) SetCallHandler(fn func(CallEvent)) {
	e.onCall = fn
}

func (e *Engine) Hunter() *hunt.Hunter {
	return e.hunter
}

func (e *Engine) Start(ctx context.Context) error {
	if len(e.sys.AllControlFrequenciesMHz()) == 0 {
		return fmt.Errorf("system %q has no control channels; import RR data first", e.sys.Name)
	}
	go e.hunter.Run(ctx.Done())
	go e.runDecodeLoop(ctx)
	return nil
}

func (e *Engine) tuneControl(mhz float64) error {
	devices := e.pool.ByRole(sdr.RoleControlHunt)
	if len(devices) == 0 {
		devices = e.pool.Devices()
	}
	if len(devices) == 0 {
		return fmt.Errorf("no SDR available for control channel")
	}
	_ = devices[0]
	// Native RTL-SDR tuning is delegated to the backend driver layer.
	return nil
}

func (e *Engine) runDecodeLoop(ctx context.Context) {
	backend := newBackend(e.sys, e.pool, e.recording, e.phase2)
	events := backend.Events()
	for {
		select {
		case <-ctx.Done():
			backend.Stop()
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			e.handleBackendEvent(ev)
		}
	}
}

func (e *Engine) handleBackendEvent(ev backendEvent) {
	switch ev.Kind {
	case "cc_lock":
		e.hunter.Lock(ev.FrequencyMHz, ev.Site)
	case "cc_loss":
		e.hunter.Unlock()
	case "call_complete":
		tgLabel, tgGroup, tgTag := ev.TalkgroupLabel, ev.TalkgroupGroup, ev.TalkgroupTag
		if e.tgDB != nil {
			if tg, ok := e.tgDB.Lookup(ev.Talkgroup); ok {
				if tgLabel == "" {
					tgLabel = tg.AlphaTag
				}
				if tgGroup == "" {
					tgGroup = tg.Group
				}
				if tgTag == "" {
					tgTag = tg.Tag
				}
			}
		}
		call := CallEvent{
			System:         e.sys,
			Talkgroup:      ev.Talkgroup,
			TalkgroupLabel: tgLabel,
			TalkgroupGroup: tgGroup,
			TalkgroupTag:   tgTag,
			FrequencyHz:    int(ev.FrequencyMHz * 1e6),
			AudioPath:      ev.AudioPath,
			StartedAt:      ev.StartedAt,
			Duration:       ev.Duration,
			Phase2:         ev.Phase2,
		}
		e.mu.Lock()
		e.active++
		e.lastCall = time.Now()
		e.mu.Unlock()
		if e.onCall != nil {
			e.onCall(call)
		}
	}
}

func (e *Engine) Status() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return fmt.Sprintf("system=%s phase2=%v active_calls=%d last_call=%s %s",
		e.sys.Name, e.phase2, e.active, e.lastCall.Format(time.RFC3339), e.hunter.Summary())
}
