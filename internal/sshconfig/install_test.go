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
	if err := WriteManagedConfig(path, "Host example\n"); err != nil {
		t.Fatal(err)
	}
	unchangedInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(info, unchangedInfo) {
		t.Fatalf("unchanged config was replaced: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedConfig(path, "Host example\n"); err != nil {
		t.Fatal(err)
	}
	repairedInfo, err := os.Stat(path)
	if err != nil || repairedInfo.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions were not repaired: %v", err)
	}
}

func TestRemoveIncludeIsIdempotentAndPreservesOtherConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sshDir, "config")
	original := "Include ~/.ssh/config.d/synk.conf\n\nHost github.com\n    User git\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := RemoveInclude("", "~/.ssh/config.d/synk.conf")
	if err != nil {
		t.Fatalf("RemoveInclude() error = %v", err)
	}
	if !first.Changed || first.BackupPath == "" {
		t.Fatalf("first result = %#v", first)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Host github.com\n    User git\n" {
		t.Fatalf("config = %q", string(data))
	}
	second, err := RemoveInclude("", "~/.ssh/config.d/synk.conf")
	if err != nil {
		t.Fatalf("second RemoveInclude() error = %v", err)
	}
	if second.Changed {
		t.Fatal("second removal should be a no-op")
	}
}
