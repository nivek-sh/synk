package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synk/internal/config"
	"synk/internal/profilecache"
)

func TestApplyStdoutUsesFakeBW(t *testing.T) {
	temp := t.TempDir()
	bwPath := writeFakeBW(t, temp, `[
  {
    "type": 5,
    "name": "GitHub General",
    "login": {"username": "git"},
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "Host", "value": "github.com"},
      {"name": "Profiles", "value": "general"},
      {"name": "HostName", "value": "github.com"},
      {"name": "User", "value": "git"},
      {"name": "IdentityFile", "value": "~/.ssh/id_general"}
    ]
  },
  {
    "type": 5,
    "name": "GitHub Pro",
    "login": {"username": "git"},
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "Host", "value": "github.com"},
      {"name": "Profiles", "value": "pro"},
      {"name": "HostName", "value": "ssh.github.com"},
      {"name": "User", "value": "git"}
    ],
    "sshKey": {"publicKey": "ssh-ed25519 AAAA github"}
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	cfg.ActiveProfiles = []string{"general", "pro"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--config", cfgPath, "apply", "--stdout"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "# Profile: pro | Source: GitHub Pro") {
		t.Fatalf("missing pro profile in output:\n%s", output)
	}
	if strings.Contains(output, "id_general") {
		t.Fatalf("general identity should be overridden:\n%s", output)
	}
	if strings.Contains(output, "IdentityFile") {
		t.Fatalf("IdentityFile should only render when explicitly configured:\n%s", output)
	}
}

func TestDoctorAllowsLockedVault(t *testing.T) {
	temp := t.TempDir()
	bwPath := writeFakeBWWithStatus(t, temp, "locked", "[]")
	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--config", cfgPath, "doctor"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "warn bw status: locked") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDoctorFailsWhenVaultIsUnauthenticated(t *testing.T) {
	temp := t.TempDir()
	bwPath := writeFakeBWWithStatus(t, temp, "unauthenticated", "[]")
	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--config", cfgPath, "doctor"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected doctor to fail")
	}
	if !strings.Contains(stdout.String(), "fail bw status: unauthenticated") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestProfileSetPersistsOrder(t *testing.T) {
	temp := t.TempDir()
	cfgPath := filepath.Join(temp, "config.toml")

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--config", cfgPath, "profile", "set", "nk,pro"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "general,nk,pro" {
		t.Fatalf("stdout = %q", got)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if strings.Join(cfg.ActiveProfiles, ",") != "general,nk,pro" {
		t.Fatalf("profiles = %#v", cfg.ActiveProfiles)
	}
}

func TestProfileMovePersistsOrder(t *testing.T) {
	temp := t.TempDir()
	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.ActiveProfiles = []string{"general", "nk", "pro"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--config", cfgPath, "profile", "move", "pro", "before", "nk"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "general,pro,nk" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestProfileStatusShowsOverrideState(t *testing.T) {
	temp := t.TempDir()
	bwPath := writeFakeBW(t, temp, `[
  {
    "type": 5,
    "name": "General GitHub",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "Host", "value": "github"},
      {"name": "Profiles", "value": "general"},
      {"name": "HostName", "value": "github.com"},
      {"name": "User", "value": "git"}
    ]
  },
  {
    "type": 5,
    "name": "NK GitHub",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "Host", "value": "github"},
      {"name": "Profiles", "value": "nk"},
      {"name": "HostName", "value": "ssh.github.com"},
      {"name": "User", "value": "git"}
    ]
  },
  {
    "type": 5,
    "name": "Pro CI",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "Host", "value": "ci"},
      {"name": "Profiles", "value": "pro"},
      {"name": "HostName", "value": "ci.example.com"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	cfg.ActiveProfiles = []string{"general", "nk"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--config", cfgPath, "profile", "status", "--no-color"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "general\tfull 1/1 overridden") {
		t.Fatalf("missing full override status:\n%s", output)
	}
	if !strings.Contains(output, "nk\tclear") {
		t.Fatalf("missing clear status:\n%s", output)
	}
	if !strings.Contains(output, "pro\tinactive") {
		t.Fatalf("missing inactive discovered profile:\n%s", output)
	}
}

func TestCacheRefreshWritesProfileCache(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(temp, "cache"))
	bwPath := writeFakeBWSyncRequired(t, temp, `[
  {
    "type": 5,
    "name": "Cloud",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "HostName", "value": "203.0.113.10"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--config", cfgPath, "cache", "refresh"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "cached 1 host/profile entries") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	cachePath, err := profilecache.DefaultPath()
	if err != nil {
		t.Fatalf("profilecache.DefaultPath() error = %v", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache) error = %v", err)
	}
	var cache profilecache.Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("Unmarshal(cache) error = %v", err)
	}
	if len(cache.Entries) != 1 || cache.Entries[0].Host != "cloud" || cache.Entries[0].Profile != "general" {
		t.Fatalf("cache entries = %#v", cache.Entries)
	}
}

func TestCacheRefreshBackgroundWritesStatus(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(temp, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(temp, "runtime"))
	bwPath := writeFakeBW(t, temp, `[
  {
    "type": 5,
    "name": "Cloud",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "HostName", "value": "203.0.113.10"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", cfgPath, "cache", "refresh", "--background"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	statusPath, err := profilecache.DefaultStatusPath()
	if err != nil {
		t.Fatalf("profilecache.DefaultStatusPath() error = %v", err)
	}
	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("ReadFile(status) error = %v", err)
	}
	var status profilecache.Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("Unmarshal(status) error = %v", err)
	}
	if status.State != "done" {
		t.Fatalf("status = %#v", status)
	}
}

func TestCacheRefreshBackgroundLockedWritesStatus(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(temp, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(temp, "runtime"))
	t.Setenv("BW_SESSION", "")
	bwPath := writeFakeBWWithStatus(t, temp, "locked", "[]")

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", cfgPath, "cache", "refresh", "--background"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected locked background refresh to fail")
	}

	statusPath, err := profilecache.DefaultStatusPath()
	if err != nil {
		t.Fatalf("profilecache.DefaultStatusPath() error = %v", err)
	}
	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("ReadFile(status) error = %v", err)
	}
	var status profilecache.Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("Unmarshal(status) error = %v", err)
	}
	if status.State != "locked" {
		t.Fatalf("status = %#v", status)
	}
}

func TestListAutoUnlocksWhenVaultIsLocked(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(temp, "runtime"))
	t.Setenv("BW_SESSION", "stale-env-session")
	bwPath := writeFakeBWLockedUnlockable(t, temp, `[
  {
    "type": 5,
    "name": "Cloud",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "HostName", "value": "203.0.113.10"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetIn(strings.NewReader("master-password\n"))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--config", cfgPath, "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cloud  general  Cloud") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Master password") {
		t.Fatalf("expected bw unlock prompt on stderr, got %q", stderr.String())
	}
}

func TestListSyncRunsBitwardenSync(t *testing.T) {
	temp := t.TempDir()
	bwPath := writeFakeBWSyncRequired(t, temp, `[
  {
    "type": 5,
    "name": "Cloud",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "HostName", "value": "203.0.113.10"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--config", cfgPath, "list", "--sync"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cloud  general  Cloud") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestListReusesCachedSessionWithoutStatus(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(temp, "runtime"))
	t.Setenv("BW_SESSION", "stale-env-session")
	bwPath := writeFakeBWCacheAware(t, temp, `[
  {
    "type": 5,
    "name": "Cloud",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "HostName", "value": "203.0.113.10"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	first := NewRootCommand()
	first.SetIn(strings.NewReader("master-password\n"))
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--config", cfgPath, "list"})
	if err := first.Execute(); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	counterBefore, err := os.ReadFile(filepath.Join(temp, "status-count"))
	if err != nil {
		t.Fatalf("ReadFile(status-count) error = %v", err)
	}

	second := NewRootCommand()
	var stdout bytes.Buffer
	second.SetOut(&stdout)
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--config", cfgPath, "list"})
	if err := second.Execute(); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	counterAfter, err := os.ReadFile(filepath.Join(temp, "status-count"))
	if err != nil {
		t.Fatalf("ReadFile(status-count) error = %v", err)
	}
	if string(counterAfter) != string(counterBefore) {
		t.Fatalf("expected cached session to skip bw status, before=%q after=%q", string(counterBefore), string(counterAfter))
	}
	if !strings.Contains(stdout.String(), "cloud  general  Cloud") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestListRetriesUnlockWhenCachedSessionReturnsEmptyOutput(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(temp, "runtime"))
	bwPath := writeFakeBWEmptyOnBadSession(t, temp, `[
  {
    "type": 5,
    "name": "Cloud",
    "fields": [
      {"name": "Enabled", "value": "true"},
      {"name": "HostName", "value": "203.0.113.10"},
      {"name": "User", "value": "deploy"}
    ]
  }
]`)

	cache := filepath.Join(temp, "runtime", "synk", "bw-session.json")
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatalf("MkdirAll(cache) error = %v", err)
	}
	if err := os.WriteFile(cache, []byte(`{"session":"stale-session","created_at":"2999-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(cache) error = %v", err)
	}

	cfgPath := filepath.Join(temp, "config.toml")
	cfg := config.Default()
	cfg.BWPath = bwPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetIn(strings.NewReader("master-password\n"))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--config", cfgPath, "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cloud  general  Cloud") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Master password") {
		t.Fatalf("expected unlock prompt after stale cache, got %q", stderr.String())
	}
}

func writeFakeBW(t *testing.T, dir string, itemsJSON string) string {
	return writeFakeBWWithStatus(t, dir, "unlocked", itemsJSON)
}

func writeFakeBWWithStatus(t *testing.T, dir string, status string, itemsJSON string) string {
	t.Helper()
	path := filepath.Join(dir, "bw")
	statusJSON := `{"status":"` + status + `"}`
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"status\" ]; then printf '%s\\n' '" + statusJSON + "'; exit 0; fi\n" +
		"if [ \"$1\" = \"sync\" ]; then exit 0; fi\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"items\" ]; then cat <<'JSON'\n" +
		itemsJSON + "\n" +
		"JSON\n" +
		"exit 0; fi\n" +
		"echo unsupported >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake bw) error = %v", err)
	}
	return path
}

func writeFakeBWLockedUnlockable(t *testing.T, dir string, itemsJSON string) string {
	t.Helper()
	path := filepath.Join(dir, "bw")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"status\" ]; then\n" +
		"  if [ \"${BW_SESSION:-}\" = \"test-session\" ]; then printf '%s\\n' '{\"status\":\"unlocked\"}'; else printf '%s\\n' '{\"status\":\"locked\"}'; fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"unlock\" ] && [ \"$2\" = \"--raw\" ]; then\n" +
		"  printf '%s\\n' '? Master password: [input is hidden]' >&2\n" +
		"  printf '%s\\n' 'test-session'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"sync\" ]; then exit 0; fi\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"items\" ]; then\n" +
		"  if [ \"${BW_SESSION:-}\" != \"test-session\" ]; then echo missing session >&2; exit 1; fi\n" +
		"  cat <<'JSON'\n" +
		itemsJSON + "\n" +
		"JSON\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unsupported >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake bw) error = %v", err)
	}
	return path
}

func writeFakeBWCacheAware(t *testing.T, dir string, itemsJSON string) string {
	t.Helper()
	path := filepath.Join(dir, "bw")
	statusCount := filepath.Join(dir, "status-count")
	if err := os.WriteFile(statusCount, []byte("0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(status-count) error = %v", err)
	}
	script := "#!/bin/sh\n" +
		"count_file='" + statusCount + "'\n" +
		"if [ \"$1\" = \"status\" ]; then\n" +
		"  count=$(cat \"$count_file\")\n" +
		"  count=$((count + 1))\n" +
		"  printf '%s\\n' \"$count\" > \"$count_file\"\n" +
		"  if [ \"${BW_SESSION:-}\" = \"test-session\" ]; then printf '%s\\n' '{\"status\":\"unlocked\"}'; else printf '%s\\n' '{\"status\":\"locked\"}'; fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"unlock\" ] && [ \"$2\" = \"--raw\" ]; then\n" +
		"  printf '%s\\n' 'test-session'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"sync\" ]; then exit 0; fi\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"items\" ]; then\n" +
		"  if [ \"${BW_SESSION:-}\" != \"test-session\" ]; then echo missing session >&2; exit 1; fi\n" +
		"  cat <<'JSON'\n" +
		itemsJSON + "\n" +
		"JSON\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unsupported >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake bw) error = %v", err)
	}
	return path
}

func writeFakeBWSyncRequired(t *testing.T, dir string, itemsJSON string) string {
	t.Helper()
	path := filepath.Join(dir, "bw")
	synced := filepath.Join(dir, "synced")
	script := "#!/bin/sh\n" +
		"synced='" + synced + "'\n" +
		"if [ \"$1\" = \"status\" ]; then printf '%s\\n' '{\"status\":\"unlocked\"}'; exit 0; fi\n" +
		"if [ \"$1\" = \"sync\" ]; then printf '%s\\n' synced > \"$synced\"; exit 0; fi\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"items\" ]; then\n" +
		"  if [ ! -f \"$synced\" ]; then echo sync required >&2; exit 1; fi\n" +
		"  cat <<'JSON'\n" +
		itemsJSON + "\n" +
		"JSON\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unsupported >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake bw) error = %v", err)
	}
	return path
}

func writeFakeBWEmptyOnBadSession(t *testing.T, dir string, itemsJSON string) string {
	t.Helper()
	path := filepath.Join(dir, "bw")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"status\" ]; then\n" +
		"  if [ \"${BW_SESSION:-}\" = \"test-session\" ]; then printf '%s\\n' '{\"status\":\"unlocked\"}'; else printf '%s\\n' '{\"status\":\"locked\"}'; fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"unlock\" ] && [ \"$2\" = \"--raw\" ]; then\n" +
		"  printf '%s\\n' '? Master password: [input is hidden]' >&2\n" +
		"  printf '%s\\n' 'test-session'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"items\" ]; then\n" +
		"  if [ \"${BW_SESSION:-}\" = \"test-session\" ]; then\n" +
		"    cat <<'JSON'\n" +
		itemsJSON + "\n" +
		"JSON\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unsupported >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake bw) error = %v", err)
	}
	return path
}
