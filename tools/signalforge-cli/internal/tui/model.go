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
)

var (
	frameStyle = lipgloss.NewStyle().Padding(1, 2)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("41")).
			Padding(1, 2).
			Width(78)
	sectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
)

type checkResult struct {
	lines  []string
	input  recorder.InputStatus
	err    error
	upload string
}

type Options struct {
	Client   *api.Client
	Recorder recorder.Settings
}

type model struct {
	client   *api.Client
	recorder recorder.Settings
	lines    []string
	input    recorder.InputStatus
	err      error
	busy     bool
	upload   string
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
		m.input = msg.input
		m.err = msg.err
		m.upload = msg.upload
	}
	return m, nil
}

func (m model) View() string {
	var body strings.Builder
	body.WriteString(titleStyle.Render("SIGNALFORGE // RECORDER CONSOLE") + "\n")
	body.WriteString(mutedStyle.Render("one binary // hub check // source key // audio ingest") + "\n\n")
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
	body.WriteString("\n" + sectionStyle.Render("RECORDER INPUT") + "\n")
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
	body.WriteString("\n" + sectionStyle.Render("METADATA") + "\n")
	body.WriteString(row("system", fmt.Sprintf("%d %s", m.recorder.Metadata.System, m.recorder.Metadata.SystemLabel), "") + "\n")
	body.WriteString(row("talkgroup", fmt.Sprintf("%d %s", m.recorder.Metadata.Talkgroup, m.recorder.Metadata.TalkgroupLabel), "") + "\n")
	body.WriteString(row("group", m.recorder.Metadata.TalkgroupGroup, "") + "\n")
	if m.upload != "" {
		body.WriteString("\n" + okStyle.Render(m.upload) + "\n")
	}
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("r refresh   u upload file   q quit"))
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
	return checkResult{lines: lines, input: input}
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
	style := mutedStyle
	switch tone {
	case "ok":
		style = okStyle
	case "warn":
		style = warnStyle
	case "error":
		style = errorStyle
	}
	return keyStyle.Render(fmt.Sprintf("%-10s", label)) + style.Render(value)
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
