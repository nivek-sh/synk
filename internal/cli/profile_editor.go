package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"synk/internal/bitwarden"
	"synk/internal/config"
	"synk/internal/profilecache"
	"synk/internal/sshconfig"
)

const (
	editorRefreshAuto   = "auto"
	editorRefreshManual = "manual"
	editorRefreshNever  = "never"
)

type editorRefreshState string

const (
	editorRefreshIdle       editorRefreshState = "idle"
	editorRefreshRefreshing editorRefreshState = "refreshing"
	editorRefreshLocked     editorRefreshState = "locked"
	editorRefreshDone       editorRefreshState = "done"
	editorRefreshError      editorRefreshState = "error"
)

type profileEditorModel struct {
	cfg            config.Config
	cfgPath        string
	refreshMode    string
	syncVault      bool
	forceSync      bool
	order          []string
	active         map[string]bool
	cursor         int
	entries        []sshconfig.Entry
	cacheUpdatedAt time.Time
	refreshStarted time.Time
	refreshState   editorRefreshState
	status         string
	saved          bool
	cancelled      bool
}

type refreshCheckMsg struct {
	needsUnlock bool
	err         error
}

type refreshLaunchMsg struct {
	startedAt time.Time
	err       error
}

type refreshPollMsg struct {
	cache  profilecache.Cache
	status profilecache.Status
	err    error
}

type unlockResultMsg struct {
	session string
	err     error
}

func runProfileEditor(in io.Reader, out io.Writer, cfg config.Config, cfgPath string, initialEntries []sshconfig.Entry, cacheUpdatedAt time.Time, refreshMode string, syncVault, forceSync bool) ([]string, bool, error) {
	input, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return nil, false, fmt.Errorf("profile edit requires an interactive terminal")
	}
	output, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(output.Fd())) {
		return nil, false, fmt.Errorf("profile edit requires an interactive terminal")
	}

	model := newProfileEditorModel(cfg, cfgPath, initialEntries, cacheUpdatedAt, refreshMode, syncVault, forceSync)
	program := tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output), tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return nil, false, err
	}
	finalModel, ok := result.(profileEditorModel)
	if !ok {
		return nil, false, fmt.Errorf("profile editor returned unexpected model")
	}
	if finalModel.cancelled {
		return nil, false, nil
	}
	return activeProfilesFromEditorOrder(finalModel.order, finalModel.active), finalModel.saved, nil
}

func newProfileEditorModel(cfg config.Config, cfgPath string, entries []sshconfig.Entry, cacheUpdatedAt time.Time, refreshMode string, syncVault, forceSync bool) profileEditorModel {
	refreshMode = normalizeEditorRefresh(refreshMode)
	activeProfiles := config.NormalizeProfiles(cfg.ActiveProfiles)
	active := map[string]bool{}
	for _, profile := range activeProfiles {
		active[profile] = true
	}
	active[config.DefaultProfile] = true

	state := profileEditorModel{
		cfg:            cfg,
		cfgPath:        cfgPath,
		refreshMode:    refreshMode,
		syncVault:      syncVault,
		forceSync:      forceSync,
		order:          profileOrderWithDiscovered(activeProfiles, entries),
		active:         active,
		entries:        entries,
		cacheUpdatedAt: cacheUpdatedAt,
		refreshState:   editorRefreshIdle,
	}
	if len(entries) == 0 {
		state.status = "cache: empty"
	} else {
		state.status = "cache: " + formatCacheAge(cacheUpdatedAt)
	}
	if refreshMode == editorRefreshNever {
		state.status += " | refresh: never"
	} else {
		state.refreshState = editorRefreshRefreshing
		state.status += " | refreshing Bitwarden..."
	}
	return state
}

func normalizeEditorRefresh(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case editorRefreshAuto:
		return editorRefreshAuto
	case editorRefreshManual:
		return editorRefreshManual
	case editorRefreshNever:
		return editorRefreshNever
	default:
		return editorRefreshAuto
	}
}

func (m profileEditorModel) Init() tea.Cmd {
	if m.refreshMode == editorRefreshNever {
		return nil
	}
	return m.checkRefreshCmd()
}

func (m profileEditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			m.saved = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.order)-1 {
				m.cursor++
			}
		case "K":
			m.moveSelection(-1)
		case "J":
			m.moveSelection(1)
		case " ":
			m.toggleSelection()
		case "r":
			if m.refreshMode != editorRefreshNever && m.refreshState != editorRefreshRefreshing {
				m.refreshState = editorRefreshRefreshing
				m.status = "checking Bitwarden..."
				return m, m.checkRefreshCmd()
			}
		case "u":
			if m.refreshMode != editorRefreshNever {
				m.status = "opening unlock prompt..."
				return m, m.unlockCmd()
			}
		}
	case refreshCheckMsg:
		if msg.err != nil {
			m.refreshState = editorRefreshError
			m.status = "refresh check failed: " + msg.err.Error()
			return m, nil
		}
		if msg.needsUnlock {
			m.refreshState = editorRefreshLocked
			if m.refreshMode == editorRefreshAuto {
				m.status = "Bitwarden locked; opening unlock prompt..."
				return m, m.unlockCmd()
			}
			m.status = "Bitwarden locked; press u to unlock and refresh"
			return m, nil
		}
		m.refreshState = editorRefreshRefreshing
		m.status = "refreshing Bitwarden in background..."
		return m, m.launchRefreshCmd("")
	case refreshLaunchMsg:
		if msg.err != nil {
			m.refreshState = editorRefreshError
			m.status = "start refresh failed: " + msg.err.Error()
			return m, nil
		}
		m.refreshStarted = msg.startedAt
		m.refreshState = editorRefreshRefreshing
		m.status = "refreshing Bitwarden in background..."
		return m, m.pollRefreshCmd()
	case refreshPollMsg:
		if msg.err != nil {
			m.refreshState = editorRefreshError
			m.status = "read cache status failed: " + msg.err.Error()
			return m, nil
		}
		if !msg.cache.UpdatedAt.IsZero() && !msg.cache.UpdatedAt.Before(m.refreshStarted) && msg.cache.UpdatedAt.After(m.cacheUpdatedAt) {
			entries := msg.cache.ToSSHEntries()
			m.entries = entries
			m.cacheUpdatedAt = msg.cache.UpdatedAt
			m.order = mergeDiscoveredProfiles(m.order, entries)
			m.clampCursor()
			m.refreshState = editorRefreshDone
			m.status = fmt.Sprintf("cache refreshed: %d host/profile entries", len(entries))
			return m, nil
		}
		switch msg.status.State {
		case "locked":
			m.refreshState = editorRefreshLocked
			if m.refreshMode == editorRefreshAuto {
				m.status = "Bitwarden locked; opening unlock prompt..."
				return m, m.unlockCmd()
			}
			if msg.status.Message != "" {
				m.status = "Bitwarden locked: " + msg.status.Message
			} else {
				m.status = "Bitwarden locked; press u to unlock and refresh"
			}
			return m, nil
		case "error":
			m.refreshState = editorRefreshError
			m.status = "refresh failed"
			if msg.status.Message != "" {
				m.status += ": " + msg.status.Message
			}
			return m, nil
		default:
			return m, m.pollRefreshCmd()
		}
	case unlockResultMsg:
		if msg.err != nil {
			m.refreshState = editorRefreshLocked
			m.status = "unlock failed: " + msg.err.Error()
			return m, nil
		}
		if err := bitwarden.DefaultSessionCache().Save(msg.session); err != nil {
			m.refreshState = editorRefreshError
			m.status = "save session failed: " + err.Error()
			return m, nil
		}
		m.refreshState = editorRefreshRefreshing
		m.status = "refreshing Bitwarden in background..."
		return m, m.launchRefreshCmd(msg.session)
	}
	return m, nil
}

func (m profileEditorModel) View() string {
	statuses := statusByName(analyzeProfileOverrides(m.entries, activeProfilesFromEditorOrder(m.order, m.active)))

	headerStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Faint(true)
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	selectedStyle := lipgloss.NewStyle().Bold(true)
	tableHeaderStyle := lipgloss.NewStyle().Bold(true).Underline(true)

	var out strings.Builder
	out.WriteString(headerStyle.Render("synk profile edit"))
	out.WriteString("\n")
	out.WriteString(dimStyle.Render("Order: top = base, bottom = highest override priority"))
	out.WriteString("\n")
	out.WriteString(dimStyle.Render("Keys: arrows/j/k select  Shift+J/K move  Space enable  r refresh  u unlock  Enter save  q cancel"))
	out.WriteString("\n")
	out.WriteString(refreshStateStyle(m.refreshState, greenStyle, yellowStyle, redStyle, dimStyle).Render(m.status))
	out.WriteString("\n\n")
	out.WriteString(tableHeaderStyle.Render(fmt.Sprintf("%-3s %-6s %-5s %-18s %-24s %-5s %s", "", "ACTIVE", "ORDER", "PROFILE", "STATUS", "HOSTS", "OVERRIDE")))
	out.WriteString("\n")

	for idx, profile := range m.order {
		cursor := " "
		if idx == m.cursor {
			cursor = ">"
		}
		check := "[ ]"
		if m.active[profile] || profile == config.DefaultProfile {
			check = "[x]"
		}

		status, ok := statuses[profile]
		if !ok {
			status = profileStatus{
				Name:          profile,
				Active:        m.active[profile],
				HostCount:     uniqueHostCountForProfile(m.entries, profile),
				OverrideState: profileStateInactive,
			}
		}

		lineStatus := profileStatusSummary(status)
		if !m.active[profile] && profile != config.DefaultProfile {
			lineStatus = "inactive"
		}
		line := fmt.Sprintf("%-3s %-6s %-5d %-18s %-24s %-5d %s",
			cursor,
			check,
			idx+1,
			profile,
			lineStatus,
			status.HostCount,
			profileOverrideCounts(status),
		)
		if idx == m.cursor {
			line = selectedStyle.Render(line)
		} else if m.active[profile] || profile == config.DefaultProfile {
			line = styleProfileLine(status, greenStyle, yellowStyle, redStyle, dimStyle, line)
		} else {
			line = dimStyle.Render(line)
		}
		out.WriteString(line)
		out.WriteString("\n")
	}

	if selected, ok := m.selectedStatus(statuses); ok {
		out.WriteString("\n")
		out.WriteString(headerStyle.Render("Selected"))
		out.WriteString("\n")
		out.WriteString(fmt.Sprintf("Profile: %s\n", selected.Name))
		out.WriteString(fmt.Sprintf("Hosts:   %d\n", selected.HostCount))
	}

	out.WriteString("\n")
	return out.String()
}

func (m profileEditorModel) selectedStatus(statuses map[string]profileStatus) (profileStatus, bool) {
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return profileStatus{}, false
	}
	profile := m.order[m.cursor]
	if status, ok := statuses[profile]; ok {
		return status, true
	}
	return profileStatus{
		Name:          profile,
		Active:        m.active[profile],
		HostCount:     uniqueHostCountForProfile(m.entries, profile),
		OverrideState: profileStateInactive,
	}, true
}

func (m *profileEditorModel) moveSelection(direction int) {
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return
	}
	if m.order[m.cursor] == config.DefaultProfile {
		return
	}

	target := m.cursor + direction
	if target < 1 || target >= len(m.order) {
		return
	}
	m.order[m.cursor], m.order[target] = m.order[target], m.order[m.cursor]
	m.cursor = target
}

func (m *profileEditorModel) toggleSelection() {
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return
	}
	profile := m.order[m.cursor]
	if profile != config.DefaultProfile {
		m.active[profile] = !m.active[profile]
	}
}

func (m *profileEditorModel) clampCursor() {
	if len(m.order) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.order) {
		m.cursor = len(m.order) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m profileEditorModel) checkRefreshCmd() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		client := bitwarden.NewCLI(cfg.BWPath)
		cache := bitwarden.DefaultSessionCache()
		session, err := cache.Load()
		if err != nil {
			return refreshCheckMsg{err: err}
		}
		if strings.TrimSpace(session) != "" || strings.TrimSpace(os.Getenv("BW_SESSION")) != "" {
			return refreshCheckMsg{}
		}

		status, err := client.Status(context.Background())
		if err != nil {
			return refreshCheckMsg{err: err}
		}
		switch status.Status {
		case "unlocked":
			return refreshCheckMsg{}
		case "locked":
			return refreshCheckMsg{needsUnlock: true}
		default:
			return refreshCheckMsg{err: bitwarden.VaultLockedError{Status: status.Status}}
		}
	}
}

func (m profileEditorModel) launchRefreshCmd(session string) tea.Cmd {
	cfgPath := m.cfgPath
	syncVault := m.syncVault
	forceSync := m.forceSync
	return func() tea.Msg {
		startedAt := time.Now()
		executable, err := os.Executable()
		if err != nil {
			return refreshLaunchMsg{err: err}
		}
		args := []string{}
		if strings.TrimSpace(cfgPath) != "" {
			args = append(args, "--config", cfgPath)
		}
		args = append(args, "cache", "refresh", "--background")
		if !syncVault {
			args = append(args, "--no-sync")
		} else if forceSync {
			args = append(args, "--force-sync")
		}

		cmd := exec.Command(executable, args...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.Stdin = nil
		cmd.Env = os.Environ()
		if strings.TrimSpace(session) != "" {
			cmd.Env = appendWithoutEnv(cmd.Env, "BW_SESSION")
			cmd.Env = append(cmd.Env, "BW_SESSION="+session)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return refreshLaunchMsg{err: err}
		}
		if err := cmd.Process.Release(); err != nil {
			return refreshLaunchMsg{err: err}
		}
		return refreshLaunchMsg{startedAt: startedAt}
	}
}

func (m profileEditorModel) pollRefreshCmd() tea.Cmd {
	return tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg {
		cache, err := profilecache.Load("")
		if err != nil {
			return refreshPollMsg{err: err}
		}
		status, err := profilecache.LoadStatus("")
		if err != nil {
			return refreshPollMsg{err: err}
		}
		return refreshPollMsg{
			cache:  cache,
			status: status,
		}
	})
}

func (m profileEditorModel) unlockCmd() tea.Cmd {
	bwPath := strings.TrimSpace(m.cfg.BWPath)
	if bwPath == "" {
		bwPath = "bw"
	}
	var stdout bytes.Buffer
	cmd := exec.Command(bwPath, "unlock", "--raw")
	cmd.Stdin = os.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return unlockResultMsg{err: err}
		}
		session := strings.TrimSpace(stdout.String())
		if session == "" {
			return unlockResultMsg{err: fmt.Errorf("bw unlock did not return a session token")}
		}
		return unlockResultMsg{session: session}
	})
}

func mergeDiscoveredProfiles(order []string, entries []sshconfig.Entry) []string {
	return profileOrderWithDiscovered(order, entries)
}

func appendWithoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, value := range env {
		if strings.HasPrefix(value, prefix) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func refreshStateStyle(state editorRefreshState, green, yellow, red, dim lipgloss.Style) lipgloss.Style {
	switch state {
	case editorRefreshDone:
		return green
	case editorRefreshLocked:
		return yellow
	case editorRefreshError:
		return red
	default:
		return dim
	}
}

func styleProfileLine(status profileStatus, green, yellow, red, dim lipgloss.Style, line string) string {
	switch status.OverrideState {
	case profileStateFull:
		return red.Render(line)
	case profileStatePartial:
		return yellow.Render(line)
	case profileStateClear:
		return green.Render(line)
	case profileStateEmpty:
		return dim.Render(line)
	default:
		return line
	}
}

func formatCacheAge(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return "empty"
	}
	age := time.Since(updatedAt)
	if age < time.Minute {
		return "fresh"
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm old", int(age.Minutes()))
	}
	if age < 48*time.Hour {
		return fmt.Sprintf("%dh old", int(age.Hours()))
	}
	return fmt.Sprintf("%dd old", int(age.Hours()/24))
}
