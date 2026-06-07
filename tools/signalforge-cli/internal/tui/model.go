package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/api"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/recorder"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/service"
)

var (
	frameStyle = lipgloss.NewStyle().Padding(1, 2)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("43")).
			Padding(1, 2).
			Width(78)
	sectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true).MarginTop(1)
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	brandStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("230"))
)

type checkResult struct {
	lines       []string
	watchLines  []string
	input       recorder.InputStatus
	err         error
	upload      string
}

type Options struct {
	Client   *api.Client
	Recorder recorder.Settings
}

type model struct {
	client     *api.Client
	recorder   recorder.Settings
	lines      []string
	watchLines []string
	input      recorder.InputStatus
	err        error
	busy       bool
	upload     string
}

func Run(options Options) error {
	program := tea.NewProgram(model{client: options.Client, recorder: options.Recorder, busy: true}, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return m.checkHub
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			m.busy = true
			m.err = nil
			return m, m.checkHub
		case "u":
			m.busy = true
			m.err = nil
			return m, m.uploadInput
		}
	case checkResult:
		m.busy = false
		m.lines = msg.lines
		m.watchLines = msg.watchLines
		m.input = msg.input
		m.err = msg.err
		m.upload = msg.upload
	}
	return m, nil
}

func (m model) View() string {
	var body strings.Builder
	body.WriteString(brandStyle.Render("SignalForge Console") + "\n")
	body.WriteString(titleStyle.Render("// RECORDER LINK") + "\n")
	body.WriteString(mutedStyle.Render("one binary // hub check // source key // audio ingest") + "\n")
	body.WriteString(sectionStyle.Render("HUB") + "\n")
	body.WriteString(row("url", m.client.BaseURL(), "") + "\n")
	if m.busy {
		body.WriteString(row("state", "checking", "warn") + "\n")
	} else if m.err != nil {
		body.WriteString(row("error", m.err.Error(), "error") + "\n")
	} else {
		for _, line := range m.lines {
			body.WriteString(line + "\n")
		}
	}
	body.WriteString(sectionStyle.Render("RECORDER INPUT") + "\n")
	body.WriteString(row("path", fallback(m.input.Path, "set --input"), "") + "\n")
	body.WriteString(row("mode", fallback(m.input.Mode, "none"), statusTone(m.input)) + "\n")
	body.WriteString(row("state", fallback(m.input.Message, "waiting for input"), statusTone(m.input)) + "\n")
	if m.input.Mode == "file" {
		body.WriteString(row("type", fallback(m.input.AudioType, "unsupported"), statusTone(m.input)) + "\n")
		body.WriteString(row("size", fmt.Sprintf("%d bytes", m.input.SizeBytes), "") + "\n")
	}
	if m.input.Mode == "folder" {
		body.WriteString(row("audio", fmt.Sprintf("%d ready", m.input.SupportedCount), statusTone(m.input)) + "\n")
		body.WriteString(row("skipped", fmt.Sprintf("%d ignored", m.input.SkippedCount), "") + "\n")
	}
	body.WriteString(sectionStyle.Render("METADATA") + "\n")
	body.WriteString(row("system", fmt.Sprintf("%d %s", m.recorder.Metadata.System, m.recorder.Metadata.SystemLabel), "") + "\n")
	body.WriteString(row("talkgroup", fmt.Sprintf("%d %s", m.recorder.Metadata.Talkgroup, m.recorder.Metadata.TalkgroupLabel), "") + "\n")
	body.WriteString(row("group", m.recorder.Metadata.TalkgroupGroup, "") + "\n")
	body.WriteString(sectionStyle.Render("WATCH") + "\n")
	if len(m.watchLines) == 0 {
		body.WriteString(row("state", "unknown", "warn") + "\n")
	} else {
		for _, line := range m.watchLines {
			body.WriteString(line + "\n")
		}
	}
	if m.upload != "" {
		body.WriteString("\n" + okStyle.Render("[OK] "+m.upload) + "\n")
	}
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("r refresh   u upload   q quit"))
	return frameStyle.Render(panelStyle.Render(body.String()))
}

func (m model) checkHub() tea.Msg {
	lines := []string{}
	input, inputErr := recorder.InspectInput(m.recorder.Input)
	health, err := m.client.Health()
	if err != nil {
		return checkResult{input: input, err: err}
	}
	lines = append(lines, row("health", fallback(health.Status, "ok"), "ok"))
	version, err := m.client.Version()
	if err != nil {
		return checkResult{input: input, err: err}
	}
	lines = append(lines, row("version", fallback(version.Version, "unknown"), ""))
	lines = append(lines, row("commit", fallback(version.Commit, "unknown"), ""))
	if err := m.client.ProbeSourceKey(); err == nil {
		lines = append(lines, row("source", "key ok", "ok"))
	} else {
		lines = append(lines, row("source", err.Error(), "warn"))
	}
	if inputErr != nil {
		input.Message = inputErr.Error()
	}
	return checkResult{lines: lines, watchLines: watchStatusLines(), input: input}
}

func watchStatusLines() []string {
	status, err := service.CurrentStatus()
	if err != nil {
		return []string{row("error", err.Error(), "error")}
	}
	lines := []string{}
	launchd := "not installed"
	tone := "ok"
	if status.Installed {
		launchd = "installed"
		if status.Running {
			launchd = "running"
		}
	}
	if len(status.WatchProcesses) > 0 {
		tone = "warn"
	}
	lines = append(lines, row("launchd", launchd, tone))
	if len(status.WatchProcesses) == 0 {
		lines = append(lines, row("processes", "none", "ok"))
		return lines
	}
	lines = append(lines, row("processes", fmt.Sprintf("%d running", len(status.WatchProcesses)), "warn"))
	for _, process := range status.WatchProcesses {
		lines = append(lines, row(fmt.Sprintf("pid %d", process.PID), process.Command, "warn"))
	}
	lines = append(lines, row("stop", "sf rec stop", ""))
	return lines
}

func (m model) uploadInput() tea.Msg {
	if strings.TrimSpace(m.recorder.Input) != "" {
		if status, err := recorder.InspectInput(m.recorder.Input); err == nil && status.Mode == "folder" {
			return m.uploadFolder(status)
		}
	}
	status, err := recorder.ValidateFileInput(m.recorder.Input)
	if err != nil {
		return checkResult{lines: m.lines, input: status, err: err}
	}
	fields := api.UploadFields{
		Metadata:  m.recorder.Metadata,
		AudioName: filepath.Base(status.Path),
		AudioType: status.AudioType,
		StartedAt: status.ModifiedAt,
	}
	if err := m.client.UploadFile(status.Path, fields); err != nil {
		return checkResult{lines: m.lines, input: status, err: err}
	}
	return checkResult{lines: m.lines, input: status, upload: "uploaded " + filepath.Base(status.Path)}
}

func (m model) uploadFolder(status recorder.InputStatus) tea.Msg {
	files, err := recorder.ReadyFiles(m.recorder, timeNow())
	if err != nil {
		return checkResult{lines: m.lines, input: status, err: err}
	}
	if len(files) == 0 {
		status.Message = "no stable audio files ready"
		return checkResult{lines: m.lines, input: status}
	}
	for _, file := range files {
		fields := api.UploadFields{
			Metadata:  m.recorder.Metadata,
			AudioName: file.Name,
			AudioType: file.AudioType,
			StartedAt: file.ModifiedAt,
		}
		if err := m.client.UploadFile(file.Path, fields); err != nil {
			return checkResult{lines: m.lines, input: status, err: err}
		}
		if !m.recorder.Reprocess {
			if _, err := recorder.MoveToProcessed(m.recorder, file.Path); err != nil {
				return checkResult{lines: m.lines, input: status, err: err}
			}
		}
	}
	refreshed, err := recorder.InspectInput(m.recorder.Input)
	if err != nil {
		refreshed = status
	}
	return checkResult{lines: m.lines, input: refreshed, upload: fmt.Sprintf("uploaded %d file(s)", len(files))}
}

func fallback(value, fallbackValue string) string {
	if value == "" {
		return fallbackValue
	}
	return value
}

func row(label, value, tone string) string {
	style := valueStyle
	switch tone {
	case "ok":
		style = okStyle
	case "warn":
		style = warnStyle
	case "error":
		style = errorStyle
	}
	return tag(tone) + " " + keyStyle.Render(fmt.Sprintf("%-10s", label+":")) + style.Render(value)
}

func tag(tone string) string {
	switch tone {
	case "ok":
		return okStyle.Render("[OK]")
	case "warn":
		return warnStyle.Render("[!!]")
	case "error":
		return errorStyle.Render("[XX]")
	default:
		return sectionStyle.Render("[..]")
	}
}

func statusTone(status recorder.InputStatus) string {
	if status.Mode == "missing" || (status.Mode == "file" && !status.Supported) {
		return "error"
	}
	if status.Mode == "none" || status.Mode == "" {
		return "warn"
	}
	return "ok"
}

func timeNow() time.Time {
	return time.Now()
}
