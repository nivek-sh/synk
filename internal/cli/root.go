package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"synk/internal/bitwarden"
	"synk/internal/config"
	"synk/internal/profilecache"
	"synk/internal/sshconfig"
)

var Version = "dev"

var errNeedsBitwardenUnlock = errors.New("Bitwarden vault is locked")

type rootOptions struct {
	configPath string
}

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}

	cmd := &cobra.Command{
		Use:          "synk",
		Short:        "Generate OpenSSH config from Bitwarden profiles",
		SilenceUsage: true,
		Version:      Version,
	}
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "config file path")

	cmd.AddCommand(newInitCommand(opts))
	cmd.AddCommand(newDoctorCommand(opts))
	cmd.AddCommand(newApplyCommand(opts))
	cmd.AddCommand(newListCommand(opts))
	cmd.AddCommand(newShowCommand(opts))
	cmd.AddCommand(newPreviewCommand(opts))
	cmd.AddCommand(newStatusCommand(opts))
	cmd.AddCommand(newDiffCommand(opts))
	cmd.AddCommand(newDisableCommand(opts))
	cmd.AddCommand(newProfileCommand(opts))
	cmd.AddCommand(newCacheCommand(opts))

	return cmd
}

func newInitCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create local synk config and install the SSH Include",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(opts.configPath)
			if err != nil {
				return err
			}
			expandedCfgPath, err := config.ExpandPath(cfgPath)
			if err != nil {
				return err
			}
			_, cfgStatErr := os.Stat(expandedCfgPath)
			cfgExists := cfgStatErr == nil
			if cfgStatErr != nil && !os.IsNotExist(cfgStatErr) {
				return cfgStatErr
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			includeExists, err := sshconfig.HasInclude("", cfg.ManagedConfigPath)
			if err != nil {
				return err
			}
			expandedManagedPath, err := config.ExpandPath(cfg.ManagedConfigPath)
			if err != nil {
				return err
			}
			_, managedStatErr := os.Stat(expandedManagedPath)
			managedExists := managedStatErr == nil
			if managedStatErr != nil && !os.IsNotExist(managedStatErr) {
				return managedStatErr
			}
			if cfgExists && includeExists && managedExists {
				fmt.Fprintf(cmd.OutOrStdout(), "synk is already initialized; no changes made\n")
				return nil
			}
			if !cfgExists {
				if err := config.Save(cfgPath, cfg); err != nil {
					return err
				}
			}
			install, err := sshconfig.InstallInclude("", cfg.ManagedConfigPath)
			if err != nil {
				return err
			}
			if !managedExists {
				if err := sshconfig.WriteManagedConfig(cfg.ManagedConfigPath, sshconfig.Render(nil, cfg.ActiveProfiles)); err != nil {
					return err
				}
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "config: %s\n", cfgPath)
			fmt.Fprintf(out, "ssh include: %s\n", install.IncludeLine)
			if install.BackupPath != "" {
				fmt.Fprintf(out, "backup: %s\n", install.BackupPath)
			}
			if install.Changed {
				fmt.Fprintf(out, "updated: %s\n", install.SSHConfigPath)
			}
			fmt.Fprintf(out, "managed config: %s\n", cfg.ManagedConfigPath)
			return nil
		},
	}
}

func newDoctorCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Bitwarden, session and SSH config paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			client := bitwarden.NewCLI(cfg.BWPath)
			failed := false

			if client.Available() {
				fmt.Fprintf(out, "ok   bw executable: %s\n", cfg.BWPath)
			} else {
				fmt.Fprintf(out, "fail bw executable: %s\n", cfg.BWPath)
				failed = true
			}

			if client.Available() {
				status, err := client.Status(cmd.Context())
				if err != nil {
					fmt.Fprintf(out, "fail bw status: %v\n", err)
					failed = true
				} else {
					switch status.Status {
					case "unlocked":
						fmt.Fprintf(out, "ok   bw status: %s\n", status.Status)
					case "locked":
						fmt.Fprintf(out, "warn bw status: %s (synk will unlock when needed)\n", status.Status)
					default:
						fmt.Fprintf(out, "fail bw status: %s\n", status.Status)
						failed = true
					}
				}
			}

			if _, err := config.ExpandPath(cfg.ManagedConfigPath); err != nil {
				fmt.Fprintf(out, "fail managed config: %v\n", err)
				failed = true
			} else {
				fmt.Fprintf(out, "ok   managed config: %s\n", cfg.ManagedConfigPath)
			}

			if failed {
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
}

func newApplyCommand(opts *rootOptions) *cobra.Command {
	var dryRun bool
	var stdout bool
	var legacySync bool

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Generate the effective OpenSSH config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			state, err := buildDesiredState(cmd.Context(), cfg, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			printWarnings(state.Warnings, cmd.ErrOrStderr())

			if stdout || dryRun {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: apply preview flags are deprecated; use `synk preview`")
				_, err := io.WriteString(cmd.OutOrStdout(), state.Rendered)
				return err
			}

			if err := sshconfig.WriteManagedPublicKeys(state.PublicKeys, cfg.ManagedKeysPath); err != nil {
				return err
			}
			if err := sshconfig.WriteManagedConfig(cfg.ManagedConfigPath, state.Rendered); err != nil {
				return err
			}
			if err := sshconfig.RemoveStaleManagedPublicKeys(state.PublicKeys, cfg.ManagedKeysPath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", cfg.ManagedConfigPath)
			fmt.Fprintf(cmd.OutOrStdout(), "managed %d public keys in %s\n", len(state.PublicKeys), cfg.ManagedKeysPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&legacySync, "sync", false, "deprecated: apply always syncs Bitwarden")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print generated config without writing files")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "print generated config without writing files")
	_ = cmd.Flags().MarkDeprecated("sync", "apply always syncs Bitwarden")
	_ = legacySync
	return cmd
}

func newListCommand(opts *rootOptions) *cobra.Command {
	var all bool
	var legacySync bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List desired SSH hosts from a fresh Bitwarden vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			rawEntries, err := fetchRawEntries(cmd.Context(), cfg, true, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			merged, err := sshconfig.Merge(rawEntries, cfg.ActiveProfiles)
			if err != nil {
				return err
			}
			printWarnings(merged.Warnings, cmd.ErrOrStderr())

			out := cmd.OutOrStdout()
			table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			if all {
				fmt.Fprintln(table, "STATE\tHOST\tDESTINATION\tUSER\tPROFILE\tIDENTITY\tSOURCE")
				for _, entry := range classifyAllEntries(rawEntries, merged.Entries, cfg.ActiveProfiles) {
					fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", entry.State, entry.Entry.Host, entryDestination(entry.Entry), entry.Entry.Directives["User"], entry.Entry.Profile, entryIdentity(entry.Entry), entry.Entry.Source)
				}
			} else {
				fmt.Fprintln(table, "HOST\tDESTINATION\tUSER\tPROFILE\tIDENTITY\tSOURCE")
				for _, entry := range merged.Entries {
					fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", entry.Host, entryDestination(entry), entry.Directives["User"], entry.Profile, entryIdentity(entry), entry.Source)
				}
			}
			return table.Flush()
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include inactive and overridden Bitwarden entries")
	cmd.Flags().BoolVar(&legacySync, "sync", false, "deprecated: list always syncs Bitwarden")
	_ = cmd.Flags().MarkDeprecated("sync", "list always syncs Bitwarden")
	_ = legacySync
	return cmd
}

func newProfileCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage active profile order",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set PROFILE[,PROFILE...]",
		Short: "Set active profile order",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateProfiles(opts.configPath, func(cfg *config.Config) {
				cfg.ActiveProfiles = config.NormalizeProfiles(config.ParseProfiles(args[0]))
			}, cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add PROFILE...",
		Short: "Append active profiles",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateProfiles(opts.configPath, func(cfg *config.Config) {
				next := append([]string{}, cfg.ActiveProfiles...)
				for _, arg := range args {
					next = append(next, config.ParseProfiles(arg)...)
				}
				cfg.ActiveProfiles = config.NormalizeProfiles(next)
			}, cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove PROFILE...",
		Short: "Remove active profiles except general",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			remove := map[string]bool{}
			for _, arg := range args {
				for _, profile := range config.ParseProfiles(arg) {
					if profile != config.DefaultProfile {
						remove[profile] = true
					}
				}
			}
			return updateProfiles(opts.configPath, func(cfg *config.Config) {
				next := []string{}
				for _, profile := range cfg.ActiveProfiles {
					if !remove[profile] {
						next = append(next, profile)
					}
				}
				cfg.ActiveProfiles = config.NormalizeProfiles(next)
			}, cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show active profile order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), strings.Join(cfg.ActiveProfiles, ","))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "move PROFILE up|down|top|bottom|before TARGET|after TARGET",
		Short: "Move an active profile without rewriting the full order",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateProfilesE(opts.configPath, func(cfg *config.Config) error {
				next, err := moveProfileOrder(cfg.ActiveProfiles, args[0], args[1:])
				if err != nil {
					return err
				}
				cfg.ActiveProfiles = next
				return nil
			}, cmd.OutOrStdout())
		},
	})

	var listNoColor bool
	profileListCommand := &cobra.Command{
		Use:   "list",
		Short: "List discovered profiles and their override state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, err := fetchRawEntries(cmd.Context(), cfg, true, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			renderProfileStatus(cmd.OutOrStdout(), entries, cfg.ActiveProfiles, colorMode(shouldUseColor(cmd.OutOrStdout(), listNoColor)))
			return nil
		},
	}
	profileListCommand.Flags().BoolVar(&listNoColor, "no-color", false, "disable ANSI colors")
	cmd.AddCommand(profileListCommand)

	var statusSync bool
	var statusNoColor bool
	statusCommand := &cobra.Command{
		Use:        "status",
		Short:      "Deprecated alias for profile list",
		Deprecated: "use `synk profile list`",
		Args:       cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, err := fetchRawEntries(cmd.Context(), cfg, true, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			renderProfileStatus(cmd.OutOrStdout(), entries, cfg.ActiveProfiles, colorMode(shouldUseColor(cmd.OutOrStdout(), statusNoColor)))
			return nil
		},
	}
	statusCommand.Flags().BoolVar(&statusSync, "sync", false, "deprecated: profile status always syncs Bitwarden")
	statusCommand.Flags().BoolVar(&statusNoColor, "no-color", false, "disable ANSI colors")
	_ = statusCommand.Flags().MarkDeprecated("sync", "profile status always syncs Bitwarden")
	_ = statusSync
	cmd.AddCommand(statusCommand)

	var explainNoColor bool
	explainCommand := &cobra.Command{
		Use:   "explain [HOST]",
		Short: "Explain which profile entries win and which are overridden",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, err := fetchRawEntries(cmd.Context(), cfg, true, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if len(args) == 1 {
				entries = filterEntriesByHost(entries, args[0])
			}
			return renderProfileDiff(cmd.OutOrStdout(), entries, cfg.ActiveProfiles, colorMode(shouldUseColor(cmd.OutOrStdout(), explainNoColor)))
		},
	}
	explainCommand.Flags().BoolVar(&explainNoColor, "no-color", false, "disable ANSI colors")
	cmd.AddCommand(explainCommand)

	var diffSync bool
	var diffNoColor bool
	diffCommand := &cobra.Command{
		Use:        "diff",
		Short:      "Deprecated alias for profile explain",
		Deprecated: "use `synk profile explain`",
		Args:       cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, err := fetchRawEntries(cmd.Context(), cfg, true, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return renderProfileDiff(cmd.OutOrStdout(), entries, cfg.ActiveProfiles, colorMode(shouldUseColor(cmd.OutOrStdout(), diffNoColor)))
		},
	}
	diffCommand.Flags().BoolVar(&diffSync, "sync", false, "deprecated: profile diff always syncs Bitwarden")
	diffCommand.Flags().BoolVar(&diffNoColor, "no-color", false, "disable ANSI colors")
	_ = diffCommand.Flags().MarkDeprecated("sync", "profile diff always syncs Bitwarden")
	_ = diffSync
	cmd.AddCommand(diffCommand)

	var editLocal bool
	var editRefresh string
	var editNoSync bool
	editCommand := &cobra.Command{
		Use:   "edit",
		Short: "Interactively reorder and enable profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(opts.configPath)
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			refreshMode := cfg.ProfileEditor.Refresh
			if editRefresh != "" {
				refreshMode = editRefresh
			}
			if editLocal {
				refreshMode = editorRefreshNever
			}
			refreshMode = normalizeEditorRefresh(refreshMode)

			cache, err := profilecache.Load("")
			if err != nil {
				return err
			}
			next, saved, err := runProfileEditor(cmd.InOrStdin(), cmd.OutOrStdout(), cfg, cfgPath, cache.ToSSHEntries(), cache.UpdatedAt, refreshMode, !editNoSync)
			if err != nil {
				return err
			}
			if !saved {
				fmt.Fprintln(cmd.OutOrStdout(), "cancelled")
				return nil
			}
			cfg.ActiveProfiles = next
			if err := config.Save(cfgPath, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved: %s\n", strings.Join(cfg.ActiveProfiles, ","))
			return nil
		},
	}
	editCommand.Flags().StringVar(&editRefresh, "refresh", "", "profile editor refresh mode: auto, manual or never")
	editCommand.Flags().BoolVar(&editLocal, "local", false, "use config and profile cache without refreshing Bitwarden")
	editCommand.Flags().BoolVar(&editNoSync, "no-sync", false, "skip bw sync during background cache refresh")
	cmd.AddCommand(editCommand)

	return cmd
}

func newCacheCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage synk caches",
	}

	var noSync bool
	var background bool
	refreshCommand := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh cached Bitwarden SSH host metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			return refreshProfileCache(cmd.Context(), cfg, !noSync, background, cmd.InOrStdin(), cmd.ErrOrStderr(), cmd.OutOrStdout())
		},
	}
	refreshCommand.Flags().BoolVar(&background, "background", false, "refresh without prompting and write cache status")
	refreshCommand.Flags().BoolVar(&noSync, "no-sync", false, "skip bw sync before reading items")
	cmd.AddCommand(refreshCommand)
	return cmd
}

func refreshProfileCache(ctx context.Context, cfg config.Config, syncVault bool, background bool, in io.Reader, errOut io.Writer, out io.Writer) error {
	startedAt := time.Now()
	if background {
		_ = profilecache.SaveStatus("", profilecache.Status{
			State:     "running",
			StartedAt: startedAt,
			UpdatedAt: startedAt,
		})
	}

	var entries []sshconfig.Entry
	var err error
	if background {
		entries, err = fetchRawEntriesNoUnlock(ctx, cfg, syncVault)
	} else {
		entries, err = fetchRawEntries(ctx, cfg, syncVault, in, errOut)
	}
	if err != nil {
		if background {
			state := "error"
			if errors.Is(err, errNeedsBitwardenUnlock) {
				state = "locked"
			}
			_ = profilecache.SaveStatus("", profilecache.Status{
				State:     state,
				Message:   err.Error(),
				StartedAt: startedAt,
				UpdatedAt: time.Now(),
			})
		}
		return err
	}

	cache, err := profilecache.Save("", entries)
	if err != nil {
		if background {
			_ = profilecache.SaveStatus("", profilecache.Status{
				State:     "error",
				Message:   err.Error(),
				StartedAt: startedAt,
				UpdatedAt: time.Now(),
			})
		}
		return err
	}
	if background {
		_ = profilecache.SaveStatus("", profilecache.Status{
			State:     "done",
			Message:   fmt.Sprintf("cached %d host/profile entries", len(cache.Entries)),
			StartedAt: startedAt,
			UpdatedAt: cache.UpdatedAt,
		})
		return nil
	}
	fmt.Fprintf(out, "cached %d host/profile entries at %s\n", len(cache.Entries), cache.UpdatedAt.Format(time.RFC3339))
	return nil
}

func updateProfiles(configPath string, mutate func(*config.Config), out io.Writer) error {
	return updateProfilesE(configPath, func(cfg *config.Config) error {
		mutate(cfg)
		return nil
	}, out)
}

func updateProfilesE(configPath string, mutate func(*config.Config) error, out io.Writer) error {
	cfgPath, err := resolveConfigPath(configPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if err := mutate(&cfg); err != nil {
		return err
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Fprintln(out, strings.Join(cfg.ActiveProfiles, ","))
	return nil
}

func fetchEntries(ctx context.Context, cfg config.Config, syncVault bool, in io.Reader, promptOut io.Writer) ([]sshconfig.Entry, []string, error) {
	rawEntries, err := fetchRawEntries(ctx, cfg, syncVault, in, promptOut)
	if err != nil {
		return nil, nil, err
	}
	merged, err := sshconfig.Merge(rawEntries, cfg.ActiveProfiles)
	if err != nil {
		return nil, nil, err
	}
	return merged.Entries, merged.Warnings, nil
}

func fetchRawEntries(ctx context.Context, cfg config.Config, syncVault bool, in io.Reader, promptOut io.Writer) ([]sshconfig.Entry, error) {
	client := bitwarden.NewCLI(cfg.BWPath)
	cache := bitwarden.DefaultSessionCache()
	if session, err := cache.Load(); err != nil {
		return nil, err
	} else if session != "" {
		client.Session = session
	}

	if client.Session != "" || strings.TrimSpace(os.Getenv("BW_SESSION")) != "" {
		entries, err := fetchRawEntriesWithClient(ctx, client, syncVault)
		if err == nil {
			return entries, nil
		}
		if !bitwarden.LooksLikeSessionError(err) {
			return nil, err
		}
		_ = cache.Clear()
		client.Session = ""
	}

	if err := client.EnsureUnlocked(ctx, in, promptOut); err != nil {
		return nil, err
	}
	if client.Session != "" {
		if err := cache.Save(client.Session); err != nil {
			return nil, err
		}
	}
	return fetchRawEntriesWithClient(ctx, client, syncVault)
}

func fetchRawEntriesNoUnlock(ctx context.Context, cfg config.Config, syncVault bool) ([]sshconfig.Entry, error) {
	client := bitwarden.NewCLI(cfg.BWPath)
	cache := bitwarden.DefaultSessionCache()
	if session, err := cache.Load(); err != nil {
		return nil, err
	} else if session != "" {
		client.Session = session
	}

	if client.Session == "" && strings.TrimSpace(os.Getenv("BW_SESSION")) == "" {
		status, err := client.Status(ctx)
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case "unlocked":
		case "locked":
			return nil, errNeedsBitwardenUnlock
		default:
			return nil, bitwarden.VaultLockedError{Status: status.Status}
		}
	}

	entries, err := fetchRawEntriesWithClient(ctx, client, syncVault)
	if err != nil {
		if bitwarden.LooksLikeSessionError(err) {
			_ = cache.Clear()
			return nil, errNeedsBitwardenUnlock
		}
		return nil, err
	}
	return entries, nil
}

func fetchRawEntriesWithClient(ctx context.Context, client bitwarden.CLI, syncVault bool) ([]sshconfig.Entry, error) {
	if syncVault {
		if err := client.Sync(ctx); err != nil {
			return nil, err
		}
	}
	items, err := client.ListSSHKeyItems(ctx)
	if err != nil {
		return nil, err
	}
	return bitwarden.ExtractSSHEntries(items)
}

func resolveConfigPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return config.DefaultPath()
}

func loadConfig(path string) (config.Config, error) {
	cfgPath, err := resolveConfigPath(path)
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(cfgPath)
}
