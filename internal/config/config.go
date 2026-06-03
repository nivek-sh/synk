package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const DefaultProfile = "general"

type Config struct {
	ActiveProfiles    []string `toml:"active_profiles"`
	ManagedConfigPath string   `toml:"managed_config_path"`
	BWPath            string   `toml:"bw_path"`
	AutoSync          bool     `toml:"auto_sync"`
	ProfileEditor     Editor   `toml:"profile_editor"`
}

type Editor struct {
	Refresh string `toml:"refresh"`
}

func Default() Config {
	return Config{
		ActiveProfiles:    []string{DefaultProfile},
		ManagedConfigPath: "~/.ssh/config.d/synk.conf",
		BWPath:            "bw",
		AutoSync:          false,
		ProfileEditor: Editor{
			Refresh: "auto",
		},
	}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "synk", "config.toml"), nil
}

func Load(path string) (Config, error) {
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return Config{}, err
		}
		path = defaultPath
	}

	expanded, err := ExpandPath(path)
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(expanded)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		cfg.Normalize()
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg.Normalize()
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}

	cfg.Normalize()
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

func (c *Config) Normalize() {
	if c.ManagedConfigPath == "" {
		c.ManagedConfigPath = Default().ManagedConfigPath
	}
	if c.BWPath == "" {
		c.BWPath = Default().BWPath
	}
	switch strings.ToLower(strings.TrimSpace(c.ProfileEditor.Refresh)) {
	case "auto", "manual", "never":
		c.ProfileEditor.Refresh = strings.ToLower(strings.TrimSpace(c.ProfileEditor.Refresh))
	default:
		c.ProfileEditor.Refresh = Default().ProfileEditor.Refresh
	}
	c.ActiveProfiles = NormalizeProfiles(c.ActiveProfiles)
}

func NormalizeProfiles(profiles []string) []string {
	seen := map[string]bool{}
	out := []string{}

	add := func(profile string) {
		profile = strings.TrimSpace(profile)
		if profile == "" || seen[profile] {
			return
		}
		seen[profile] = true
		out = append(out, profile)
	}

	add(DefaultProfile)
	for _, profile := range profiles {
		add(profile)
	}
	return out
}

func ParseProfiles(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}

	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	expanded, err := ExpandPath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(expanded)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".synk-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, expanded)
}
