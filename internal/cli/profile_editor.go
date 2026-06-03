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

type profileEditorState struct {
	order   []string
	active  map[string]bool
	cursor  int
	entries []sshconfig.Entry
}

func runProfileEditor(in io.Reader, out io.Writer, activeProfiles []string, entries []sshconfig.Entry) ([]string, bool, error) {
	input, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return nil, false, fmt.Errorf("profile edit requires an interactive terminal")
	}
	output, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(output.Fd())) {
		return nil, false, fmt.Errorf("profile edit requires an interactive terminal")
	}

	oldState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return nil, false, err
	}
	defer func() {
		_ = term.Restore(int(input.Fd()), oldState)
		fmt.Fprint(output, "\x1b[?25h\x1b[0m")
	}()

	state := newProfileEditorState(activeProfiles, entries)
	renderProfileEditor(output, state)

	buffer := make([]byte, 8)
	for {
		n, err := input.Read(buffer)
		if err != nil {
			return nil, false, err
		}
		key := parseEditorKey(buffer[:n])
		switch key {
		case "up":
			if state.cursor > 0 {
				state.cursor--
			}
		case "down":
			if state.cursor < len(state.order)-1 {
				state.cursor++
			}
		case "move-up":
			moveEditorSelection(&state, -1)
		case "move-down":
			moveEditorSelection(&state, 1)
		case "toggle":
			profile := state.order[state.cursor]
			if profile != config.DefaultProfile {
				state.active[profile] = !state.active[profile]
			}
		case "save":
			fmt.Fprint(output, "\x1b[H\x1b[2J")
			return activeProfilesFromEditorOrder(state.order, state.active), true, nil
		case "cancel":
			fmt.Fprint(output, "\x1b[H\x1b[2J")
			return nil, false, nil
		}
		renderProfileEditor(output, state)
	}
}

func newProfileEditorState(activeProfiles []string, entries []sshconfig.Entry) profileEditorState {
	order := profileOrderWithDiscovered(activeProfiles, entries)
	activeProfiles = config.NormalizeProfiles(activeProfiles)
	active := map[string]bool{}
	for _, profile := range activeProfiles {
		active[profile] = true
	}
	active[config.DefaultProfile] = true

	return profileEditorState{
		order:   order,
		active:  active,
		entries: entries,
	}
}

func parseEditorKey(data []byte) string {
	value := string(data)
	switch value {
	case "\x1b[A", "k":
		return "up"
	case "\x1b[B", "j":
		return "down"
	case "K":
		return "move-up"
	case "J":
		return "move-down"
	case " ":
		return "toggle"
	case "\r", "\n":
		return "save"
	case "q", "Q", "\x1b":
		return "cancel"
	default:
		return ""
	}
}

func moveEditorSelection(state *profileEditorState, direction int) {
	if state.cursor < 0 || state.cursor >= len(state.order) {
		return
	}
	if state.order[state.cursor] == config.DefaultProfile {
		return
	}

	target := state.cursor + direction
	if target < 1 || target >= len(state.order) {
		return
	}
	state.order[state.cursor], state.order[target] = state.order[target], state.order[state.cursor]
	state.cursor = target
}

func renderProfileEditor(out io.Writer, state profileEditorState) {
	activeProfiles := activeProfilesFromEditorOrder(state.order, state.active)
	statuses := statusByName(analyzeProfileOverrides(state.entries, activeProfiles))
	nameWidth := maxProfileNameWidth(state.order)

	editorWriteLine(out, "\x1b[?25l\x1b[2J\x1b[H"+colorBold+"synk profile edit"+colorReset)
	editorWriteLine(out, colorDim+"Order: top = base, bottom = highest override priority"+colorReset)
	editorWriteLine(out, colorDim+"Keys: arrows/j/k select  Shift+J/K move  Space enable  Enter save  q cancel"+colorReset)
	editorWriteLine(out, "")

	for idx, profile := range state.order {
		cursor := " "
		if idx == state.cursor {
			cursor = ">"
		}
		check := "[ ]"
		if state.active[profile] || profile == config.DefaultProfile {
			check = "[x]"
		}
		if profile == config.DefaultProfile {
			check = "[x]"
		}

		status, ok := statuses[profile]
		if !ok {
			status = profileStatus{
				Name:          profile,
				Active:        state.active[profile],
				OverrideState: profileStateInactive,
			}
		}

		name := profile
		if state.active[profile] || profile == config.DefaultProfile {
			name = profileStatusColor(status, true)
		} else {
			name = colorDim + profile + colorReset
		}

		lineStatus := profileStatusSummary(status)
		if !state.active[profile] && profile != config.DefaultProfile {
			lineStatus = "inactive"
		}
		namePadding := strings.Repeat(" ", nameWidth-len(profile)+2)
		editorWriteLine(out, fmt.Sprintf("%s %s %s%s%s", cursor, check, name, namePadding, lineStatus))
		if details := profileOverrideSummary(status); details != "-" && state.active[profile] {
			editorWriteLine(out, fmt.Sprintf("      %s%s%s", colorDim, details, colorReset))
		}
	}
	editorWriteLine(out, "")
}

func editorWriteLine(out io.Writer, line string) {
	fmt.Fprint(out, line, "\r\n")
}

func maxProfileNameWidth(profiles []string) int {
	width := len("PROFILE")
	for _, profile := range profiles {
		if len(profile) > width {
			width = len(profile)
		}
	}
	return width
}
