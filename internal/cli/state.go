package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"synk/internal/config"
	"synk/internal/sshconfig"
)

type desiredState struct {
	Entries    []sshconfig.Entry
	Rendered   string
	PublicKeys []sshconfig.ManagedPublicKey
	Warnings   []string
}

type installedState struct {
	Exists          bool
	Content         string
	Entries         []sshconfig.Entry
	ManagedKeyFiles []string
}

type stateChange struct {
	Action string
	Host   string
	Detail string
}

func buildDesiredState(ctx context.Context, cfg config.Config, inReader io.Reader, promptWriter io.Writer) (desiredState, error) {
	entries, warnings, err := fetchEntries(ctx, cfg, true, inReader, promptWriter)
	if err != nil {
		return desiredState{}, err
	}
	prepared, keys, keyWarnings, err := sshconfig.PrepareManagedIdentities(entries, cfg.ManagedKeysPath)
	if err != nil {
		return desiredState{}, err
	}
	warnings = append(warnings, keyWarnings...)
	return desiredState{
		Entries:    prepared,
		Rendered:   sshconfig.Render(prepared, cfg.ActiveProfiles),
		PublicKeys: keys,
		Warnings:   warnings,
	}, nil
}

func loadInstalledState(path, keyDir string) (installedState, error) {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return installedState{}, err
	}
	data, err := os.ReadFile(expanded)
	state := installedState{}
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return installedState{}, err
	} else {
		entries, err := sshconfig.ParseManaged(string(data))
		if err != nil {
			return installedState{}, err
		}
		state.Exists = true
		state.Content = string(data)
		state.Entries = entries
	}

	expandedKeyDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return installedState{}, err
	}
	keyEntries, err := os.ReadDir(expandedKeyDir)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return installedState{}, err
	}
	for _, entry := range keyEntries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, "bw-") && strings.HasSuffix(name, ".pub") {
			state.ManagedKeyFiles = append(state.ManagedKeyFiles, filepath.Join(keyDir, name))
		}
	}
	sort.Strings(state.ManagedKeyFiles)
	return state, nil
}

func compareStates(installed installedState, desired desiredState) []stateChange {
	installedByHost := map[string]sshconfig.Entry{}
	for _, entry := range installed.Entries {
		installedByHost[entry.Host] = entry
	}
	desiredByHost := map[string]sshconfig.Entry{}
	keyByDisplayPath := map[string]sshconfig.ManagedPublicKey{}
	desiredKeyPaths := map[string]bool{}
	for _, key := range desired.PublicKeys {
		keyByDisplayPath[key.DisplayPath] = key
		desiredKeyPaths[key.DisplayPath] = true
	}

	changes := []stateChange{}
	for _, entry := range desired.Entries {
		desiredByHost[entry.Host] = entry
		current, ok := installedByHost[entry.Host]
		if !ok {
			changes = append(changes, stateChange{Action: "add", Host: entry.Host, Detail: "present in Bitwarden, not installed"})
			continue
		}
		if sshconfig.RenderEntryBlock(current) != sshconfig.RenderEntryBlock(entry) {
			changes = append(changes, stateChange{Action: "update", Host: entry.Host, Detail: "configuration or identity changed"})
			continue
		}
		if identityPath := strings.TrimSpace(entry.Directives["IdentityFile"]); identityPath != "" {
			if key, managed := keyByDisplayPath[identityPath]; managed && !sshconfig.ManagedPublicKeyCurrent(key) {
				changes = append(changes, stateChange{Action: "repair-key", Host: entry.Host, Detail: "managed public key is missing or outdated"})
			}
		}
	}
	for _, entry := range installed.Entries {
		if _, ok := desiredByHost[entry.Host]; !ok {
			changes = append(changes, stateChange{Action: "remove", Host: entry.Host, Detail: "installed, no longer desired"})
		}
	}
	for _, keyPath := range installed.ManagedKeyFiles {
		if !desiredKeyPaths[keyPath] {
			changes = append(changes, stateChange{Action: "remove-key", Host: "-", Detail: keyPath})
		}
	}
	if installed.Content != desired.Rendered && !hasConfigChange(changes) {
		changes = append(changes, stateChange{Action: "update-config", Host: "-", Detail: "generated header or formatting changed"})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Action != changes[j].Action {
			return changes[i].Action < changes[j].Action
		}
		return changes[i].Host < changes[j].Host
	})
	return changes
}

func hasConfigChange(changes []stateChange) bool {
	for _, change := range changes {
		switch change.Action {
		case "add", "update", "remove", "update-config":
			return true
		}
	}
	return false
}

func printWarnings(warnings []string, out io.Writer) {
	for _, warning := range warnings {
		fmt.Fprintf(out, "warning: %s\n", warning)
	}
}

func renderLineDiff(current, desired string) string {
	if current == desired {
		return ""
	}
	a := strings.Split(strings.TrimSuffix(current, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(desired, "\n"), "\n")
	if current == "" {
		a = nil
	}
	if desired == "" {
		b = nil
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out strings.Builder
	out.WriteString("--- installed\n+++ desired\n")
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			out.WriteString("  " + a[i] + "\n")
			i++
			j++
		case j < len(b) && (i == len(a) || dp[i][j+1] > dp[i+1][j]):
			out.WriteString("+ " + b[j] + "\n")
			j++
		default:
			out.WriteString("- " + a[i] + "\n")
			i++
		}
	}
	return out.String()
}
