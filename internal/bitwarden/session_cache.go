package bitwarden

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultSessionTTL = 8 * time.Hour

type SessionCache struct {
	Path string
	TTL  time.Duration
}

type cachedSession struct {
	Session   string    `json:"session"`
	CreatedAt time.Time `json:"created_at"`
}

func DefaultSessionCache() SessionCache {
	return SessionCache{
		Path: defaultSessionCachePath(),
		TTL:  sessionTTLFromEnv(),
	}
}

func (c SessionCache) Load() (string, error) {
	if c.Path == "" {
		return "", nil
	}
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	var cached cachedSession
	if err := json.Unmarshal(data, &cached); err != nil {
		_ = c.Clear()
		return "", nil
	}
	if strings.TrimSpace(cached.Session) == "" {
		_ = c.Clear()
		return "", nil
	}
	if c.TTL > 0 && time.Since(cached.CreatedAt) > c.TTL {
		_ = c.Clear()
		return "", nil
	}
	return cached.Session, nil
}

func (c SessionCache) Save(session string) error {
	session = strings.TrimSpace(session)
	if c.Path == "" || session == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(cachedSession{
		Session:   session,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return atomicWrite(c.Path, data, 0o600)
}

func (c SessionCache) Clear() error {
	if c.Path == "" {
		return nil
	}
	err := os.Remove(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func defaultSessionCachePath() string {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, "synk", "bw-session.json")
	}
	uid := strconv.Itoa(os.Getuid())
	return filepath.Join(os.TempDir(), "synk-"+uid, "bw-session.json")
}

func sessionTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SYNK_BW_SESSION_TTL"))
	if raw == "" {
		return defaultSessionTTL
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl < 0 {
		return defaultSessionTTL
	}
	return ttl
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".synk-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
