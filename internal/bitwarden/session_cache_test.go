package bitwarden

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionCacheSaveLoadAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bw-session.json")
	cache := SessionCache{Path: path, TTL: time.Hour}

	if err := cache.Save("session-token"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	session, err := cache.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if session != "session-token" {
		t.Fatalf("session = %q", session)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestSessionCacheExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bw-session.json")
	cache := SessionCache{Path: path, TTL: time.Nanosecond}
	if err := cache.Save("session-token"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	time.Sleep(time.Millisecond)

	session, err := cache.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if session != "" {
		t.Fatalf("expected expired session, got %q", session)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected cache file to be removed, stat err = %v", err)
	}
}
