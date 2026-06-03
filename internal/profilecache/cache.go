package profilecache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synk/internal/sshconfig"
)

const Version = 1

type Cache struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Entries   []Entry   `json:"entries"`
}

type Status struct {
	State     string    `json:"state"`
	Message   string    `json:"message,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Entry struct {
	Host    string `json:"host"`
	Profile string `json:"profile"`
	Source  string `json:"source,omitempty"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "synk", "profile-hosts.json"), nil
}

func DefaultStatusPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "synk", "profile-refresh-status.json"), nil
}

func Load(path string) (Cache, error) {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return Cache{}, err
		}
		path = defaultPath
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Cache{}, nil
	}
	if err != nil {
		return Cache{}, err
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return Cache{}, err
	}
	if cache.Version != Version {
		return Cache{}, nil
	}
	return cache, nil
}

func LoadStatus(path string) (Status, error) {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultStatusPath()
		if err != nil {
			return Status{}, err
		}
		path = defaultPath
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}

	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func Save(path string, entries []sshconfig.Entry) (Cache, error) {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return Cache{}, err
		}
		path = defaultPath
	}

	cache := Cache{
		Version:   Version,
		UpdatedAt: time.Now(),
		Entries:   FromSSHEntries(entries),
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return Cache{}, err
	}
	data = append(data, '\n')
	return cache, atomicWrite(path, data, 0o600)
}

func SaveStatus(path string, status Status) error {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultStatusPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func FromSSHEntries(entries []sshconfig.Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		host := strings.TrimSpace(entry.Host)
		profile := strings.TrimSpace(entry.Profile)
		if host == "" || profile == "" {
			continue
		}
		out = append(out, Entry{
			Host:    host,
			Profile: profile,
			Source:  strings.TrimSpace(entry.Source),
		})
	}
	return out
}

func (c Cache) ToSSHEntries() []sshconfig.Entry {
	out := make([]sshconfig.Entry, 0, len(c.Entries))
	for _, entry := range c.Entries {
		host := strings.TrimSpace(entry.Host)
		profile := strings.TrimSpace(entry.Profile)
		if host == "" || profile == "" {
			continue
		}
		out = append(out, sshconfig.Entry{
			Host:    host,
			Profile: profile,
			Source:  strings.TrimSpace(entry.Source),
		})
	}
	return out
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

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
