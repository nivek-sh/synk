package bitwarden

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const RecentSyncWindow = 15 * time.Second

// SyncCache records only when a particular CLI session successfully read a
// freshly synced vault. It never stores vault contents or the session token.
type SyncCache struct {
	Path string
	TTL  time.Duration
}

type syncRecord struct {
	Identity string    `json:"identity"`
	SyncedAt time.Time `json:"synced_at"`
}

func DefaultSyncCache() SyncCache {
	return SyncCache{
		Path: filepath.Join(filepath.Dir(defaultSessionCachePath()), "bw-sync.json"),
		TTL:  RecentSyncWindow,
	}
}

func (c SyncCache) Fresh(session, bwPath string) bool {
	if session == "" || c.Path == "" || c.TTL <= 0 {
		return false
	}
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return false
	}
	var record syncRecord
	if json.Unmarshal(data, &record) != nil || record.Identity != syncIdentity(session, bwPath) {
		return false
	}
	age := time.Since(record.SyncedAt)
	return age >= 0 && age < c.TTL
}

func (c SyncCache) Save(session, bwPath string) error {
	if session == "" || c.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(syncRecord{
		Identity: syncIdentity(session, bwPath),
		SyncedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return atomicWrite(c.Path, data, 0o600)
}

func (c SyncCache) Clear() {
	if c.Path != "" {
		_ = os.Remove(c.Path)
	}
}

func syncIdentity(session, bwPath string) string {
	sum := sha256.Sum256([]byte(session + "\x00" + bwPath))
	return hex.EncodeToString(sum[:])
}
