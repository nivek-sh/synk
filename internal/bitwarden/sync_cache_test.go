package bitwarden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncCacheFreshnessAndIsolation(t *testing.T) {
	cache := SyncCache{Path: filepath.Join(t.TempDir(), "runtime", "bw-sync.json"), TTL: RecentSyncWindow}
	if cache.Fresh("session-a", "bw") {
		t.Fatal("missing sync marker was fresh")
	}
	if err := cache.Save("session-a", "bw"); err != nil {
		t.Fatal(err)
	}
	if !cache.Fresh("session-a", "bw") {
		t.Fatal("recent sync marker was not fresh")
	}
	if cache.Fresh("session-b", "bw") || cache.Fresh("session-a", "other-bw") || cache.Fresh("", "bw") {
		t.Fatal("sync marker was shared across sessions or executables")
	}
	data, err := os.ReadFile(cache.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "session-a") {
		t.Fatal("sync marker contains the session token")
	}
	info, err := os.Stat(cache.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sync marker permissions = %o", info.Mode().Perm())
	}
	stale, err := json.Marshal(syncRecord{
		Identity: syncIdentity("session-a", "bw"),
		SyncedAt: time.Now().Add(-RecentSyncWindow - time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.Path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if cache.Fresh("session-a", "bw") {
		t.Fatal("expired sync marker was fresh")
	}
}
