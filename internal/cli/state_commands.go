package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"synk/internal/config"
	"synk/internal/sshconfig"
)

func newShowCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the currently installed synk SSH config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			expanded, err := config.ExpandPath(cfg.ManagedConfigPath)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(expanded)
			if os.IsNotExist(err) {
				return fmt.Errorf("managed config is not installed at %s; run `synk init` or `synk apply`", cfg.ManagedConfigPath)
			}
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}

func newPreviewCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "preview",
		Short: "Show exactly what apply would install from Bitwarden",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			state, err := buildDesiredState(cmd.Context(), cfg, opts.readSyncMode(), cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			printWarnings(state.Warnings, cmd.ErrOrStderr())
			_, err = io.WriteString(cmd.OutOrStdout(), state.Rendered)
			return err
		},
	}
}

func newStatusCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Compare Bitwarden state with the installed config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			desired, err := buildDesiredState(cmd.Context(), cfg, opts.readSyncMode(), cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			installed, err := loadInstalledState(cfg.ManagedConfigPath, cfg.ManagedKeysPath)
			if err != nil {
				return err
			}
			printWarnings(desired.Warnings, cmd.ErrOrStderr())
			changes := compareStates(installed, desired)
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Bitwarden: synced within the last 15 seconds")
			fmt.Fprintf(out, "Profiles: %s\n", strings.Join(cfg.ActiveProfiles, " -> "))
			fmt.Fprintf(out, "Installed: %s\n", cfg.ManagedConfigPath)
			if len(changes) == 0 {
				fmt.Fprintln(out, "State: up to date")
				return nil
			}
			fmt.Fprintf(out, "State: %d change(s) pending\n\n", len(changes))
			table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(table, "ACTION\tHOST\tDETAIL")
			for _, change := range changes {
				fmt.Fprintf(table, "%s\t%s\t%s\n", change.Action, change.Host, change.Detail)
			}
			return table.Flush()
		},
	}
}

func newDiffCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show the exact installed-to-desired change",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			desired, err := buildDesiredState(cmd.Context(), cfg, opts.readSyncMode(), cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			installed, err := loadInstalledState(cfg.ManagedConfigPath, cfg.ManagedKeysPath)
			if err != nil {
				return err
			}
			printWarnings(desired.Warnings, cmd.ErrOrStderr())
			changes := compareStates(installed, desired)
			if len(changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No changes. Installed state is up to date.")
				return nil
			}
			if diff := renderLineDiff(installed.Content, desired.Rendered); diff != "" {
				fmt.Fprint(cmd.OutOrStdout(), diff)
			}
			keyChanges := []stateChange{}
			for _, change := range changes {
				if change.Action == "repair-key" || change.Action == "remove-key" {
					keyChanges = append(keyChanges, change)
				}
			}
			if len(keyChanges) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\n# Managed keys")
				for _, change := range keyChanges {
					fmt.Fprintf(cmd.OutOrStdout(), "! %s: %s\n", change.Host, change.Detail)
				}
			}
			return nil
		},
	}
}

func newDisableCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Remove the synk Include from the user SSH config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			result, err := sshconfig.RemoveInclude("", cfg.ManagedConfigPath)
			if err != nil {
				return err
			}
			if !result.Changed {
				fmt.Fprintln(cmd.OutOrStdout(), "synk is already disabled; no changes made")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "disabled: removed %s\n", result.IncludeLine)
			fmt.Fprintf(cmd.OutOrStdout(), "backup: %s\n", result.BackupPath)
			fmt.Fprintln(cmd.OutOrStdout(), "managed config and keys were retained; run `synk init` to re-enable")
			return nil
		},
	}
}

type classifiedEntry struct {
	State string
	Entry sshconfig.Entry
}

func classifyAllEntries(raw, effective []sshconfig.Entry, activeProfiles []string) []classifiedEntry {
	active := map[string]bool{}
	for _, profile := range config.NormalizeProfiles(activeProfiles) {
		active[profile] = true
	}
	winners := map[string]sshconfig.Entry{}
	for _, entry := range effective {
		winners[entry.Host] = entry
	}
	classified := make([]classifiedEntry, 0, len(raw))
	for _, entry := range raw {
		state := "inactive"
		if active[entry.Profile] {
			winner := winners[entry.Host]
			if sameSourceEntry(entry, winner) {
				state = "effective"
			} else {
				state = "overridden by " + winner.Profile
			}
		}
		classified = append(classified, classifiedEntry{State: state, Entry: entry})
	}
	sort.Slice(classified, func(i, j int) bool {
		if classified[i].Entry.Host != classified[j].Entry.Host {
			return classified[i].Entry.Host < classified[j].Entry.Host
		}
		return classified[i].Entry.Profile < classified[j].Entry.Profile
	})
	return classified
}

func sameSourceEntry(left, right sshconfig.Entry) bool {
	if left.Host != right.Host || left.Profile != right.Profile {
		return false
	}
	if left.SourceID != "" || right.SourceID != "" {
		return left.SourceID == right.SourceID
	}
	return left.Source == right.Source
}

func entryDestination(entry sshconfig.Entry) string {
	host := strings.TrimSpace(entry.Directives["HostName"])
	port := strings.TrimSpace(entry.Directives["Port"])
	if port == "" {
		port = "22"
	}
	return host + ":" + port
}

func entryIdentity(entry sshconfig.Entry) string {
	if strings.TrimSpace(entry.Directives["IdentityFile"]) != "" {
		return "custom"
	}
	if entry.PrivateKey != "" {
		return "legacy private key"
	}
	fingerprint := strings.TrimSpace(entry.KeyFingerprint)
	if fingerprint == "" {
		fingerprint = sshconfig.PublicKeyFingerprint(entry.PublicKey)
	}
	if fingerprint == "" {
		return "missing"
	}
	if len(fingerprint) > 24 {
		fingerprint = fingerprint[:24] + "..."
	}
	return "bitwarden " + fingerprint
}
