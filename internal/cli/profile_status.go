package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"synk/internal/config"
	"synk/internal/sshconfig"
)

type colorMode bool

const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
	colorDim    = "\x1b[2m"
	colorBold   = "\x1b[1m"
)

func renderProfileStatus(out io.Writer, entries []sshconfig.Entry, activeProfiles []string, color colorMode) {
	activeProfiles = config.NormalizeProfiles(activeProfiles)
	statusesByName := statusByName(analyzeProfileOverrides(entries, activeProfiles))
	order := profileOrderWithDiscovered(activeProfiles, entries)

	fmt.Fprintln(out, "ORDER\tPROFILE\tSTATUS\tHOSTS\tOVERRIDE")
	for idx, profile := range order {
		status, ok := statusesByName[profile]
		if !ok {
			status = profileStatus{
				Name:          profile,
				Active:        false,
				HostCount:     uniqueHostCountForProfile(entries, profile),
				OverrideState: profileStateInactive,
			}
		}
		name := profileStatusColor(status, color)
		summary := profileStatusSummary(status)
		override := profileOverrideCounts(status)
		fmt.Fprintf(out, "%d\t%s\t%s\t%d\t%s\n", idx+1, name, summary, status.HostCount, override)
	}
}

func profileStatusSummary(status profileStatus) string {
	switch status.OverrideState {
	case profileStateInactive:
		return "inactive"
	case profileStateEmpty:
		return "empty"
	case profileStateClear:
		return "clear"
	case profileStatePartial:
		return fmt.Sprintf("partial %d/%d overridden", status.OverriddenCount, status.HostCount)
	case profileStateFull:
		return fmt.Sprintf("full %d/%d overridden", status.OverriddenCount, status.HostCount)
	default:
		return string(status.OverrideState)
	}
}

func profileOverrideCounts(status profileStatus) string {
	parts := []string{}
	if status.OverriddenCount > 0 {
		parts = append(parts, fmt.Sprintf("overridden %d/%d", status.OverriddenCount, status.HostCount))
	}
	if status.OverridingCount > 0 {
		parts = append(parts, fmt.Sprintf("overrides %d", status.OverridingCount))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func profileStatusColor(status profileStatus, color colorMode) string {
	name := status.Name
	if !color {
		return name
	}
	switch status.OverrideState {
	case profileStateFull:
		return colorRed + name + colorReset
	case profileStatePartial:
		return colorYellow + name + colorReset
	case profileStateClear:
		return colorGreen + name + colorReset
	case profileStateEmpty:
		return colorDim + name + colorReset
	default:
		return name
	}
}

func shouldUseColor(out io.Writer, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if out != os.Stdout {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func activeProfilesFromEditorOrder(order []string, active map[string]bool) []string {
	next := []string{}
	for _, profile := range order {
		if profile == config.DefaultProfile || active[profile] {
			next = append(next, profile)
		}
	}
	return config.NormalizeProfiles(next)
}
