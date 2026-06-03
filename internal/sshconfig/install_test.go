package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallIncludeCreatesConfigAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userConfig := "~/.ssh/config"
	managed := "~/.ssh/config.d/synk.conf"

	first, err := InstallInclude(userConfig, managed)
	if err != nil {
		t.Fatalf("InstallInclude() error = %v", err)
	}
	if !first.Changed {
		t.Fatal("expected first install to change config")
	}

	second, err := InstallInclude(userConfig, managed)
	if err != nil {
		t.Fatalf("InstallInclude() second error = %v", err)
	}
	if second.Changed {
		t.Fatal("expected second install to be idempotent")
	}

	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if count := strings.Count(string(data), "Include ~/.ssh/config.d/synk.conf"); count != 1 {
		t.Fatalf("include count = %d", count)
	}
}

func TestInstallIncludeBacksUpExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	existingPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(existingPath, []byte("Host old\n    User me\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := InstallInclude("~/.ssh/config", "~/.ssh/config.d/synk.conf")
	if err != nil {
		t.Fatalf("InstallInclude() error = %v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("expected backup path")
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}
	if string(backup) != "Host old\n    User me\n" {
		t.Fatalf("backup = %q", string(backup))
	}
}

func TestWriteManagedConfigCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.d", "synk.conf")
	if err := WriteManagedConfig(path, "Host example\n"); err != nil {
		t.Fatalf("WriteManagedConfig() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
