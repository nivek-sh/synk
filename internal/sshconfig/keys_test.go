package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareManagedIdentitiesUsesNativePublicKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	entries, keys, warnings, err := PrepareManagedIdentities([]Entry{{
		Host:      "dev",
		Profile:   "pro",
		Source:    "dev",
		SourceID:  "item-123",
		PublicKey: "ssh-ed25519 AAAA dev",
		Directives: map[string]string{
			"HostName": "203.0.113.10",
			"User":     "root",
		},
	}}, dir)
	if err != nil {
		t.Fatalf("PrepareManagedIdentities() error = %v", err)
	}
	if len(warnings) != 0 || len(keys) != 1 {
		t.Fatalf("keys=%#v warnings=%#v", keys, warnings)
	}
	if got := entries[0].Directives["IdentityFile"]; got != filepath.Join(dir, "bw-item-123.pub") {
		t.Fatalf("IdentityFile = %q", got)
	}
	if entries[0].Directives["IdentitiesOnly"] != "yes" {
		t.Fatalf("IdentitiesOnly = %q", entries[0].Directives["IdentitiesOnly"])
	}
	if err := WriteManagedPublicKeys(keys, dir); err != nil {
		t.Fatalf("WriteManagedPublicKeys() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "bw-item-123.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ssh-ed25519 AAAA dev\n" {
		t.Fatalf("public key = %q", string(data))
	}
}

func TestPrepareManagedIdentitiesRespectsExplicitIdentityFile(t *testing.T) {
	entries, keys, _, err := PrepareManagedIdentities([]Entry{{
		Host:      "dev",
		Source:    "dev",
		PublicKey: "ssh-ed25519 AAAA dev",
		Directives: map[string]string{
			"IdentityFile": "~/.ssh/custom",
		},
	}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 || entries[0].Directives["IdentityFile"] != "~/.ssh/custom" {
		t.Fatalf("entries=%#v keys=%#v", entries, keys)
	}
}

func TestPrepareManagedIdentitiesRejectsMissingPublicKey(t *testing.T) {
	_, _, _, err := PrepareManagedIdentities([]Entry{{
		Host:       "dev",
		Source:     "dev",
		Directives: map[string]string{},
	}}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no public key") {
		t.Fatalf("error = %v", err)
	}
}

func TestRemoveStaleManagedPublicKeysOnlyRemovesSynkFiles(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "bw-keep.pub")
	stale := filepath.Join(dir, "bw-stale.pub")
	userFile := filepath.Join(dir, "personal.pub")
	for _, path := range []string{keep, stale, userFile} {
		if err := os.WriteFile(path, []byte("key\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveStaleManagedPublicKeys([]ManagedPublicKey{{Path: keep}}, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale key still exists: %v", err)
	}
	for _, path := range []string{keep, userFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
}
