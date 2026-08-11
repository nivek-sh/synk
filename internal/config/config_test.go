package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeProfilesAlwaysIncludesGeneralFirst(t *testing.T) {
	got := NormalizeProfiles([]string{"nk", "general", "pro", "nk"})
	want := []string{"general", "nk", "pro"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeProfiles() = %#v, want %#v", got, want)
	}
}

func TestSaveLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Config{
		ActiveProfiles:    []string{"nk", "pro"},
		ManagedConfigPath: "~/custom/synk.conf",
		ManagedKeysPath:   "~/custom/keys",
		BWPath:            "/usr/bin/bw",
		AutoSync:          true,
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantProfiles := []string{"general", "nk", "pro"}
	if !reflect.DeepEqual(loaded.ActiveProfiles, wantProfiles) {
		t.Fatalf("ActiveProfiles = %#v, want %#v", loaded.ActiveProfiles, wantProfiles)
	}
	if loaded.ManagedConfigPath != cfg.ManagedConfigPath {
		t.Fatalf("ManagedConfigPath = %q", loaded.ManagedConfigPath)
	}
	if loaded.ManagedKeysPath != cfg.ManagedKeysPath {
		t.Fatalf("ManagedKeysPath = %q", loaded.ManagedKeysPath)
	}
	if loaded.BWPath != cfg.BWPath {
		t.Fatalf("BWPath = %q", loaded.BWPath)
	}
	if !loaded.AutoSync {
		t.Fatal("AutoSync = false")
	}
	if loaded.ProfileEditor.Refresh != "auto" {
		t.Fatalf("ProfileEditor.Refresh = %q", loaded.ProfileEditor.Refresh)
	}
}

func TestNormalizeProfileEditorRefresh(t *testing.T) {
	cfg := Default()
	cfg.ProfileEditor.Refresh = "MANUAL"
	cfg.Normalize()
	if cfg.ProfileEditor.Refresh != "manual" {
		t.Fatalf("ProfileEditor.Refresh = %q", cfg.ProfileEditor.Refresh)
	}

	cfg.ProfileEditor.Refresh = "surprise"
	cfg.Normalize()
	if cfg.ProfileEditor.Refresh != "auto" {
		t.Fatalf("ProfileEditor.Refresh = %q", cfg.ProfileEditor.Refresh)
	}
}

func TestLoadExpandsTildePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "config.toml")
	cfg := Default()
	cfg.BWPath = "/tmp/bw"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load("~/config.toml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.BWPath != "/tmp/bw" {
		t.Fatalf("BWPath = %q", loaded.BWPath)
	}
}

func TestExpandPathUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := ExpandPath("~/synk")
	if err != nil {
		t.Fatalf("ExpandPath() error = %v", err)
	}
	want := filepath.Join(home, "synk")
	if got != want {
		t.Fatalf("ExpandPath() = %q, want %q", got, want)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.ActiveProfiles) != 1 || cfg.ActiveProfiles[0] != DefaultProfile {
		t.Fatalf("ActiveProfiles = %#v", cfg.ActiveProfiles)
	}
	if cfg.BWPath != "bw" {
		t.Fatalf("BWPath = %q", cfg.BWPath)
	}
}

func TestSaveCreatesPrivateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
