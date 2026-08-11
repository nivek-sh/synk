package cli

import (
	"os"
	"path/filepath"
	"testing"

	"synk/internal/sshconfig"
)

func TestCompareStatesDetectsHeaderOnlyChange(t *testing.T) {
	entry := sshconfig.Entry{
		Host:    "dev",
		Profile: "pro",
		Source:  "Dev",
		Directives: map[string]string{
			"HostName": "203.0.113.10",
			"User":     "root",
		},
	}
	installedContent := sshconfig.Render([]sshconfig.Entry{entry}, []string{"general", "pro"})
	desiredContent := sshconfig.Render([]sshconfig.Entry{entry}, []string{"general", "nk", "pro"})
	changes := compareStates(installedState{Content: installedContent, Entries: []sshconfig.Entry{entry}}, desiredState{Rendered: desiredContent, Entries: []sshconfig.Entry{entry}})
	if len(changes) != 1 || changes[0].Action != "update-config" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestLoadAndCompareStatesDetectsStaleManagedKey(t *testing.T) {
	temp := t.TempDir()
	keyDir := filepath.Join(temp, "keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "bw-stale.pub"), []byte("ssh-ed25519 AAAA stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installed, err := loadInstalledState(filepath.Join(temp, "missing.conf"), keyDir)
	if err != nil {
		t.Fatal(err)
	}
	changes := compareStates(installed, desiredState{})
	if len(changes) != 1 || changes[0].Action != "remove-key" {
		t.Fatalf("changes = %#v", changes)
	}
}
