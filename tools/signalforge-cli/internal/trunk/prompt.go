package trunk

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type prompter struct {
	in  *bufio.Reader
	out io.Writer
	yes bool
}

func newPrompter(in io.Reader, out io.Writer, nonInteractive bool) *prompter {
	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	return &prompter{in: reader, out: out, yes: nonInteractive}
}

func (p *prompter) ask(label, defaultValue string) (string, error) {
	if p.yes {
		if strings.TrimSpace(defaultValue) != "" {
			return defaultValue, nil
		}
		return "", nil
	}
	prompt := label
	if defaultValue != "" {
		prompt = fmt.Sprintf("%s [%s]: ", label, defaultValue)
	} else {
		prompt = label + ": "
	}
	if _, err := fmt.Fprint(p.out, prompt); err != nil {
		return "", err
	}
	text, err := p.in.ReadString('\n')
	if err != nil {
		return "", err
	}
	answer := strings.TrimSpace(text)
	if answer == "" {
		return defaultValue, nil
	}
	return answer, nil
}

func (p *prompter) askRequired(label, defaultValue string) (string, error) {
	for {
		value, err := p.ask(label, defaultValue)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
		fmt.Fprintln(p.out, "  value required")
	}
}

func (p *prompter) askYesNo(label string, defaultYes bool) (bool, error) {
	if p.yes {
		return defaultYes, nil
	}
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}
	answer, err := p.ask(label+" ("+defaultLabel+")", "")
	if err != nil {
		return false, err
	}
	if answer == "" {
		return defaultYes, nil
	}
	switch strings.ToLower(answer) {
	case "y", "yes", "true", "1", "on":
		return true, nil
	case "n", "no", "false", "0", "off":
		return false, nil
	default:
		return defaultYes, nil
	}
}

func (p *prompter) choose(label string, options []string, defaultIndex int) (int, error) {
	if p.yes {
		if defaultIndex < 0 || defaultIndex >= len(options) {
			return 0, nil
		}
		return defaultIndex, nil
	}
	fmt.Fprintln(p.out, label)
	for i, option := range options {
		marker := " "
		if i == defaultIndex {
			marker = "*"
		}
		fmt.Fprintf(p.out, "  %s %d) %s\n", marker, i+1, option)
	}
	for {
		answer, err := p.ask("Choose", strconv.Itoa(defaultIndex+1))
		if err != nil {
			return 0, err
		}
		index, err := strconv.Atoi(answer)
		if err != nil || index < 1 || index > len(options) {
			fmt.Fprintln(p.out, "  enter a number from the list")
			continue
		}
		return index - 1, nil
	}
}

func expandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
