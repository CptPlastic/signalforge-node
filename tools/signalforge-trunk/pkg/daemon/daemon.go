package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/conventional"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/decode/trunkrecorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/hub"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/sdr"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/trunking"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/trunking/p25"
)

// Status is a snapshot of daemon health.
type Status struct {
	SDRCount      int
	RolePlan      sdr.RolePlan
	DecodeEngine  string
	Engines       []string
	GMRS          string
	UploadQueue   int
	HubConnected  bool
	LastUpload    time.Time
	LastError     string
	TRConfigPath  string
}

// Daemon orchestrates SDR pool, decode engines, and Hub upload.
type Daemon struct {
	cfg        config.Config
	configPath string
	pool       *sdr.Pool
	hub        *hub.Client
	queue      *hub.Queue
	engines    []*p25.Engine
	gmrs       *conventional.Scanner
	trRunner   *trunkrecorder.Runner
	mu         sync.RWMutex
	status     Status
	cancel     context.CancelFunc
}

func New(cfg config.Config, configPath string) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath = "trunk.yaml"
	}
	client, err := hub.NewClient(cfg.Hub.BaseURL, cfg.Hub.SourceKey, 20*time.Second)
	if err != nil {
		return nil, err
	}
	queueDir := cfg.Resolve(cfg.Upload.QueueDirectory, configPath)
	queue, err := hub.NewQueue(queueDir)
	if err != nil {
		return nil, err
	}
	recordingsDir := cfg.Resolve(cfg.Recordings.Directory, configPath)
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		return nil, err
	}
	return &Daemon{
		cfg:        cfg,
		configPath: configPath,
		pool:       sdr.NewPool(),
		hub:        client,
		queue:      queue,
		status:     Status{DecodeEngine: cfg.Engine()},
	}, nil
}

func (d *Daemon) Start(ctx context.Context) error {
	devices, err := d.pool.Discover()
	if err != nil {
		return fmt.Errorf("sdr discover: %w", err)
	}
	if len(devices) == 0 {
		return fmt.Errorf("no RTL-SDR devices found; attach dongles and retry")
	}
	plan := d.pool.Rebalance()
	d.mu.Lock()
	d.status.SDRCount = len(devices)
	d.status.RolePlan = plan
	d.mu.Unlock()

	if d.cfg.Hub.SourceKey != "" {
		if err := d.hub.ProbeSourceKey(); err != nil {
			d.setError(err.Error())
		} else {
			d.mu.Lock()
			d.status.HubConnected = true
			d.mu.Unlock()
		}
	}

	switch d.cfg.Engine() {
	case config.EngineTrunkRecorder:
		return d.startTrunkRecorder(ctx, devices)
	case config.EngineGopherTrunk, config.EngineExternal:
		return d.startLegacyEngines(ctx)
	default:
		return fmt.Errorf("unsupported decode.engine %q", d.cfg.Engine())
	}
}

func (d *Daemon) startTrunkRecorder(ctx context.Context, devices []sdr.Device) error {
	trPath, err := trunkrecorder.Render(d.cfg, d.configPath, devices)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.status.TRConfigPath = trPath
	d.status.Engines = []string{fmt.Sprintf("trunk-recorder (%s)", trPath)}
	d.mu.Unlock()

	bin := trunkrecorder.NormalizeBinary(d.cfg.Decode.TrunkRecorder.Binary)
	d.trRunner = trunkrecorder.NewRunner(bin, trPath)
	if err := d.trRunner.Start(ctx); err != nil {
		return err
	}

	captureDir := d.cfg.Resolve(d.cfg.Recordings.Directory, d.configPath)
	watcher := trunkrecorder.NewWatcher(captureDir, 2500*time.Millisecond, d.handleTRCall)
	go watcher.Run(ctx)

	go d.uploadLoop(ctx)
	go d.hotplugLoop(ctx)
	return nil
}

func (d *Daemon) startLegacyEngines(ctx context.Context) error {
	tgDB := trunking.NewTalkgroupDB()
	recordingsDir := d.cfg.Resolve(d.cfg.Recordings.Directory, d.configPath)
	for _, sys := range d.cfg.Trunking.Systems {
		if sys.TalkgroupCSV != "" {
			csvPath := d.cfg.Resolve(sys.TalkgroupCSV, d.configPath)
			_ = tgDB.LoadCSV(csvPath)
		}
		engine := p25.NewEngine(sys, d.pool, tgDB, recordingsDir)
		engine.SetCallHandler(d.handleCall)
		if err := engine.Start(ctx); err != nil {
			return err
		}
		d.engines = append(d.engines, engine)
		d.mu.Lock()
		d.status.Engines = append(d.status.Engines, engine.Status())
		d.mu.Unlock()
	}

	for _, role := range d.cfg.Scanner.Roles {
		if role.Type == "conventional" && len(role.Channels) > 0 {
			sc, err := conventional.NewScanner(role.Name, role.Channels, d.pool)
			if err == nil {
				d.gmrs = sc
				sc.SetCallHandler(d.handleGMRSCall)
				go sc.Start(ctx, recordingsDir)
				d.mu.Lock()
				d.status.GMRS = sc.Summary()
				d.mu.Unlock()
			}
		}
	}

	go d.uploadLoop(ctx)
	go d.hotplugLoop(ctx)
	return nil
}

func (d *Daemon) handleTRCall(call trunkrecorder.CompletedCall) {
	sys := d.systemForTRCall(call.Meta)
	label := call.Meta.TalkgroupTag
	if label == "" {
		label = call.Meta.TalkgroupDescription
	}
	group := call.Meta.TalkgroupGroup
	if group == "" {
		group = call.Meta.TalkgroupGroupTag
	}
	fields := hub.UploadFields{
		Metadata: hub.Metadata{
			System:         sys.RadioReferenceSID,
			SystemLabel:    sys.Name,
			Talkgroup:      call.Meta.Talkgroup,
			TalkgroupLabel: label,
			TalkgroupGroup: group,
			TalkgroupTag:   call.Meta.TalkgroupGroupTag,
			Frequency:      call.FrequencyHz(),
		},
		AudioName: filepath.Base(call.AudioPath),
		StartedAt: call.StartedAt(),
		Duration:  call.Duration(),
	}
	if err := d.hub.UploadFile(call.AudioPath, fields); err != nil {
		_ = d.queue.Enqueue(call.AudioPath, fields)
		d.setError(err.Error())
		return
	}
	d.mu.Lock()
	d.status.LastUpload = time.Now()
	d.status.LastError = ""
	d.mu.Unlock()
}

func (d *Daemon) systemForTRCall(meta trunkrecorder.CallMeta) config.System {
	sn := strings.ToUpper(strings.TrimSpace(meta.ShortName))
	for _, sys := range d.cfg.Trunking.Systems {
		if trunkrecorder.ShortName(sys.Name) == sn {
			return sys
		}
	}
	for _, role := range d.cfg.Scanner.Roles {
		if role.Type == "conventional" && trunkrecorder.ShortName(role.Name) == sn {
			return config.System{Name: role.Name, RadioReferenceSID: 0}
		}
	}
	if len(d.cfg.Trunking.Systems) > 0 {
		return d.cfg.Trunking.Systems[0]
	}
	return config.System{Name: meta.ShortName}
}

func (d *Daemon) handleGMRSCall(call conventional.CallEvent) {
	fields := hub.UploadFields{
		Metadata: hub.Metadata{
			System:         0,
			SystemLabel:    "GMRS",
			Talkgroup:      call.Talkgroup,
			TalkgroupLabel: call.Label,
			TalkgroupGroup: "GMRS",
			TalkgroupTag:   "voice",
			Frequency:      int(call.ChannelMHz * 1e6),
		},
		AudioName: filepath.Base(call.AudioPath),
		StartedAt: call.StartedAt,
		Duration:  call.Duration,
	}
	if err := d.hub.UploadFile(call.AudioPath, fields); err != nil {
		_ = d.queue.Enqueue(call.AudioPath, fields)
		d.setError(err.Error())
		return
	}
	d.mu.Lock()
	d.status.LastUpload = time.Now()
	d.status.LastError = ""
	d.mu.Unlock()
}

func (d *Daemon) handleCall(call p25.CallEvent) {
	fields := hub.UploadFields{
		Metadata: hub.Metadata{
			System:         call.System.RadioReferenceSID,
			SystemLabel:    call.System.Name,
			Talkgroup:      call.Talkgroup,
			TalkgroupLabel: call.TalkgroupLabel,
			TalkgroupGroup: call.TalkgroupGroup,
			TalkgroupTag:   call.TalkgroupTag,
			Frequency:      call.FrequencyHz,
		},
		AudioName: filepath.Base(call.AudioPath),
		StartedAt: call.StartedAt,
		Duration:  call.Duration,
	}
	if err := d.hub.UploadFile(call.AudioPath, fields); err != nil {
		_ = d.queue.Enqueue(call.AudioPath, fields)
		d.setError(err.Error())
		return
	}
	d.mu.Lock()
	d.status.LastUpload = time.Now()
	d.status.LastError = ""
	d.mu.Unlock()
}

func (d *Daemon) uploadLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := d.queue.Drain(d.hub)
			if err != nil {
				d.setError(err.Error())
				continue
			}
			if n > 0 {
				d.mu.Lock()
				d.status.LastUpload = time.Now()
				d.mu.Unlock()
			}
			d.runCanary()
		}
	}
}

func (d *Daemon) runCanary() {
	if d.cfg.Hub.SourceKey == "" {
		return
	}
	if err := d.hub.ProbeSourceKey(); err != nil {
		d.setError(err.Error())
		d.mu.Lock()
		d.status.HubConnected = false
		d.mu.Unlock()
		return
	}
	d.mu.Lock()
	d.status.HubConnected = true
	d.mu.Unlock()
}

func (d *Daemon) hotplugLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	lastCount := d.pool.Count()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			devices, err := d.pool.Discover()
			if err != nil {
				continue
			}
			if len(devices) != lastCount {
				plan := d.pool.Rebalance()
				d.mu.Lock()
				d.status.SDRCount = len(devices)
				d.status.RolePlan = plan
				d.mu.Unlock()
				lastCount = len(devices)
				if d.cfg.Engine() == config.EngineTrunkRecorder && d.trRunner != nil {
					d.restartTrunkRecorder(ctx, devices)
				}
			}
		}
	}
}

func (d *Daemon) restartTrunkRecorder(ctx context.Context, devices []sdr.Device) {
	d.trRunner.Stop()
	trPath, err := trunkrecorder.Render(d.cfg, d.configPath, devices)
	if err != nil {
		d.setError(fmt.Sprintf("hotplug render: %v", err))
		return
	}
	d.mu.Lock()
	d.status.TRConfigPath = trPath
	d.mu.Unlock()
	if err := d.trRunner.Start(ctx); err != nil {
		d.setError(fmt.Sprintf("hotplug restart trunk-recorder: %v", err))
	}
}

func (d *Daemon) setError(msg string) {
	d.mu.Lock()
	d.status.LastError = msg
	d.mu.Unlock()
}

func (d *Daemon) Pool() *sdr.Pool { return d.pool }

func (d *Daemon) Status() Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	st := d.status
	if d.cfg.Engine() != config.EngineTrunkRecorder {
		st.Engines = nil
		for _, e := range d.engines {
			st.Engines = append(st.Engines, e.Status())
		}
	}
	return st
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer cancel()
	defer d.stop()
	if err := d.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func (d *Daemon) stop() {
	if d.trRunner != nil {
		d.trRunner.Stop()
	}
}
