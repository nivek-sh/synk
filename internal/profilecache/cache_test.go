package profilecache

import (
	"path/filepath"
	"testing"

	"synk/internal/sshconfig"
)

func TestSaveLoadRoundTripKeepsOnlyProfileMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile-hosts.json")
	_, err := Save(path, []sshconfig.Entry{
		{
			Host:    "github",
			Profile: "pro",
			Source:  "GitHub Pro",
			Directives: map[string]string{
				"HostName": "ssh.github.com",
				"User":     "git",
			},
			Notes: "not cached",
		},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cache, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cache.Entries) != 1 {
		t.Fatalf("entries = %#v", cache.Entries)
	}
	entry := cache.ToSSHEntries()[0]
	if entry.Host != "github" || entry.Profile != "pro" || entry.Source != "GitHub Pro" {
		t.Fatalf("entry = %#v", entry)
	}
	if len(entry.Directives) != 0 || entry.Notes != "" {
		t.Fatalf("cached sensitive fields: %#v", entry)
	}
}
