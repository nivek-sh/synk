package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareManagedIdentitiesUsesNativePublicKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	entries, keys, privateKeys, warnings, err := PrepareManagedIdentities([]Entry{{
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
	if len(warnings) != 0 || len(keys) != 1 || len(privateKeys) != 0 {
		t.Fatalf("keys=%#v privateKeys=%#v warnings=%#v", keys, privateKeys, warnings)
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
	keyPath := filepath.Join(dir, "bw-item-123.pub")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedPublicKeys(keys, dir); err != nil {
		t.Fatal(err)
	}
	unchangedInfo, err := os.Stat(keyPath)
	if err != nil || !os.SameFile(info, unchangedInfo) {
		t.Fatalf("unchanged public key was replaced: %v", err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if ManagedPublicKeyCurrent(keys[0]) {
		t.Fatal("public key with unsafe permissions reported current")
	}
	if err := WriteManagedPublicKeys(keys, dir); err != nil {
		t.Fatal(err)
	}
	repairedInfo, err := os.Stat(keyPath)
	if err != nil || repairedInfo.Mode().Perm() != 0o600 {
		t.Fatalf("public key permissions were not repaired: %v", err)
	}
}

func TestPrepareManagedIdentitiesRespectsExplicitIdentityFile(t *testing.T) {
	entries, keys, _, _, err := PrepareManagedIdentities([]Entry{{
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
	_, _, _, _, err := PrepareManagedIdentities([]Entry{{
		Host:       "dev",
		Source:     "dev",
		Directives: map[string]string{},
	}}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no public key") {
		t.Fatalf("error = %v", err)
	}
}

func TestManagedPrivateKeyLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	content := "-----BEGIN DSA PRIVATE KEY-----\nQUJDRA==\n-----END DSA PRIVATE KEY-----\n"
	entries, publicKeys, privateKeys, _, err := PrepareManagedIdentities([]Entry{{
		Host:       "legacy",
		Source:     "Legacy",
		SourceID:   "item-legacy",
		PrivateKey: content,
		Directives: map[string]string{"HostName": "example.com", "User": "deploy"},
	}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(publicKeys) != 0 || len(privateKeys) != 1 {
		t.Fatalf("public=%d private=%d", len(publicKeys), len(privateKeys))
	}
	key := privateKeys[0]
	if entries[0].Directives["IdentityFile"] != key.DisplayPath || entries[0].Directives["IdentitiesOnly"] != "yes" {
		t.Fatalf("directives = %#v", entries[0].Directives)
	}
	if err := WriteManagedPrivateKeys(privateKeys, dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(key.Path)
	if err != nil || string(data) != content {
		t.Fatalf("key content mismatch: %v", err)
	}
	info, err := os.Stat(key.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key permissions: %v", err)
	}
	if !ManagedPrivateKeyCurrent(key) {
		t.Fatal("fresh private key reported stale")
	}
	if err := WriteManagedPrivateKeys(privateKeys, dir); err != nil {
		t.Fatal(err)
	}
	unchangedInfo, err := os.Stat(key.Path)
	if err != nil || !os.SameFile(info, unchangedInfo) {
		t.Fatalf("unchanged private key was replaced: %v", err)
	}
	if err := os.Chmod(key.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if ManagedPrivateKeyCurrent(key) {
		t.Fatal("unsafe key permissions reported current")
	}
	if err := WriteManagedPrivateKeys(privateKeys, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if ManagedPrivateKeyCurrent(key) {
		t.Fatal("unsafe key directory permissions reported current")
	}
	if err := WriteManagedPrivateKeys(privateKeys, dir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStaleManagedPrivateKeys(nil, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(key.Path); !os.IsNotExist(err) {
		t.Fatalf("stale private key retained: %v", err)
	}
}

func TestNormalizePrivateKeyRestoresFlattenedArmor(t *testing.T) {
	content := "-----BEGIN OPENSSH PRIVATE KEY-----\nQUJDRA==\n-----END OPENSSH PRIVATE KEY-----\n"
	flattened := "-----BEGIN OPENSSH PRIVATE KEY----- QUJDRA== -----END OPENSSH PRIVATE KEY-----"
	got, err := normalizePrivateKey(flattened)
	if err != nil {
		t.Fatal(err)
	}
	if got != content {
		t.Fatalf("normalized private key has wrong line structure")
	}
	got, err = normalizePrivateKey(content)
	if err != nil || got != content {
		t.Fatalf("already multiline private key changed: %v", err)
	}
	longPayload := strings.Repeat("A", 80)
	flattened = "-----BEGIN OPENSSH PRIVATE KEY----- " + longPayload + " -----END OPENSSH PRIVATE KEY-----"
	got, err = normalizePrivateKey(flattened)
	if err != nil || got != "-----BEGIN OPENSSH PRIVATE KEY-----\n"+longPayload[:70]+"\n"+longPayload[70:]+"\n-----END OPENSSH PRIVATE KEY-----\n" {
		t.Fatalf("flattened long key was not wrapped correctly: %v", err)
	}
	multiline := "-----BEGIN DSA PRIVATE KEY-----\n" + strings.Repeat("A", 64) + "\nQUJDRA==\n-----END DSA PRIVATE KEY-----\n"
	got, err = normalizePrivateKey(multiline)
	if err != nil || got != multiline {
		t.Fatalf("multiline key was reformatted: %v", err)
	}
}

func TestNormalizePrivateKeyRejectsInvalidFlattenedArmor(t *testing.T) {
	for _, value := range []string{
		"not a private key",
		"-----BEGIN OPENSSH PRIVATE KEY----- invalid! -----END OPENSSH PRIVATE KEY-----",
		"-----BEGIN DSA PRIVATE KEY----- QUJDRA== -----END RSA PRIVATE KEY-----",
	} {
		if _, err := normalizePrivateKey(value); err == nil || strings.Contains(err.Error(), value) {
			t.Fatalf("invalid key was accepted or exposed in error")
		}
	}
}

func TestRemoveStaleManagedPrivateKeysKeepsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"bw-stale.key", "personal.key", "bw-public.pub"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("example"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveStaleManagedPrivateKeys(nil, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bw-stale.key")); !os.IsNotExist(err) {
		t.Fatalf("stale key retained: %v", err)
	}
	for _, name := range []string{"personal.key", "bw-public.pub"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s removed: %v", name, err)
		}
	}
}

func TestManagedKeysRejectSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "keys")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	key := ManagedPrivateKey{Path: filepath.Join(link, "bw-id.key"), DisplayPath: filepath.Join(link, "bw-id.key"), Content: "secret"}
	if err := WriteManagedPrivateKeys([]ManagedPrivateKey{key}, link); err == nil {
		t.Fatal("private key write accepted symlink directory")
	}
	if err := RemoveStaleManagedPrivateKeys(nil, link); err == nil {
		t.Fatal("private key cleanup accepted symlink directory")
	}
	if err := WriteManagedPublicKeys(nil, link); err == nil {
		t.Fatal("public key write accepted symlink directory")
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
