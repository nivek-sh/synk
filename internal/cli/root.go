package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"synk/internal/bitwarden"
	"synk/internal/config"
	"synk/internal/sshconfig"
)

var Version = "dev"

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
	cmd.AddCommand(newProfileCommand(opts))

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
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := config.Save(cfgPath, cfg); err != nil {
				return err
			}
			install, err := sshconfig.InstallInclude("", cfg.ManagedConfigPath)
			if err != nil {
				return err
			}
			if err := sshconfig.WriteManagedConfig(cfg.ManagedConfigPath, sshconfig.Render(nil, cfg.ActiveProfiles)); err != nil {
				return err
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
					fmt.Fprintf(out, "ok   bw status: %s\n", status.Status)
					if status.Status != "unlocked" {
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
	var syncVault bool
	var dryRun bool
	var stdout bool

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Generate the effective OpenSSH config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, warnings, err := fetchEntries(cmd.Context(), cfg, syncVault || cfg.AutoSync, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}

			rendered := sshconfig.Render(entries, cfg.ActiveProfiles)

			if stdout || dryRun {
				_, err := io.WriteString(cmd.OutOrStdout(), rendered)
				return err
			}

			if err := sshconfig.WriteManagedConfig(cfg.ManagedConfigPath, rendered); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", cfg.ManagedConfigPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&syncVault, "sync", false, "run bw sync before reading items")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print generated config without writing files")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "print generated config without writing files")
	return cmd
}

func newListCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List effective SSH hosts from Bitwarden",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, warnings, err := fetchEntries(cmd.Context(), cfg, cfg.AutoSync, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			_ = warnings

			out := cmd.OutOrStdout()
			table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(table, "HOST\tPROFILE\tSOURCE")
			for _, entry := range entries {
				fmt.Fprintf(table, "%s\t%s\t%s\n", entry.Host, entry.Profile, entry.Source)
			}
			return table.Flush()
		},
	}
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
		Use:   "list",
		Short: "List active profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			for idx, profile := range cfg.ActiveProfiles {
				fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\n", idx+1, profile)
			}
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

	var statusSync bool
	var statusNoColor bool
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Show profile order and override status from Bitwarden",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(opts.configPath)
			if err != nil {
				return err
			}
			entries, err := fetchRawEntries(cmd.Context(), cfg, statusSync || cfg.AutoSync, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			renderProfileStatus(cmd.OutOrStdout(), entries, cfg.ActiveProfiles, colorMode(shouldUseColor(cmd.OutOrStdout(), statusNoColor)))
			return nil
		},
	}
	statusCommand.Flags().BoolVar(&statusSync, "sync", false, "run bw sync before reading items")
	statusCommand.Flags().BoolVar(&statusNoColor, "no-color", false, "disable ANSI colors")
	cmd.AddCommand(statusCommand)

	var editSync bool
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
			entries, err := fetchRawEntries(cmd.Context(), cfg, editSync || cfg.AutoSync, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			next, saved, err := runProfileEditor(cmd.InOrStdin(), cmd.OutOrStdout(), cfg.ActiveProfiles, entries)
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
	editCommand.Flags().BoolVar(&editSync, "sync", false, "run bw sync before reading items")
	cmd.AddCommand(editCommand)

	return cmd
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
