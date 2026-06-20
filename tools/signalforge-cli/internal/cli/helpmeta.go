package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

const annotationGroup = "signalforge/group"

// preferredCommandName maps cobra Use names to the short form shown in help.
var preferredCommandName = map[string]string{
	"signalforge": "sf",
	"rec":         "rec",
	"recorder":    "rec",
	"update":      "upd",
	"version":     "ver",
	"onboard":     "onb",
	"service":     "svc",
	"completion":  "tab",
}

var rootCommandGroup = map[string]string{
	"hub":        "HUB",
	"onboard":    "ONB",
	"onb":        "ONB",
	"service":    "SVC",
	"rec":        "REC",
	"recorder":   "REC",
	"tui":        "TUI",
	"update":     "UPD",
	"upd":        "UPD",
	"version":    "VER",
	"ver":        "VER",
	"completion": "TAB",
	"tab":        "TAB",
}

func stampCommandGroups(root *cobra.Command) {
	for _, cmd := range root.Commands() {
		group := rootCommandGroup[cmd.Name()]
		if group == "" {
			group = strings.ToUpper(cmd.Name())
			if len(group) > 3 {
				group = group[:3]
			}
		}
		stampGroup(cmd, group)
	}
}

func stampGroup(cmd *cobra.Command, group string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[annotationGroup] = group
	for _, child := range cmd.Commands() {
		stampGroup(child, group)
	}
}

func commandGroup(cmd *cobra.Command) string {
	if cmd.Annotations != nil {
		if group := cmd.Annotations[annotationGroup]; group != "" {
			return group
		}
	}
	if cmd.Parent() != nil {
		return commandGroup(cmd.Parent())
	}
	return "CMD"
}

func commandLabel(cmd *cobra.Command) string {
	return cmd.Name()
}

func preferredUseLine(cmd *cobra.Command) string {
	line := cmd.UseLine()
	for long, short := range preferredCommandName {
		line = strings.ReplaceAll(line, long, short)
	}
	return line
}

func preferredCommandPath(cmd *cobra.Command) string {
	parts := strings.Fields(cmd.CommandPath())
	for i, part := range parts {
		if short, ok := preferredCommandName[part]; ok {
			parts[i] = short
		}
	}
	return strings.Join(parts, " ")
}

func rootHelpName(cmd *cobra.Command) string {
	if short, ok := preferredCommandName[cmd.Name()]; ok {
		return short
	}
	return cmd.Name()
}
