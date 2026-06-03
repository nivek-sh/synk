package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synk/internal/config"
)

type InstallResult struct {
	SSHConfigPath string
	IncludeLine   string
	BackupPath    string
	Changed       bool
}

func DefaultUserConfigPath() string {
	return "~/.ssh/config"
}

func InstallInclude(userConfigPath, managedConfigPath string) (InstallResult, error) {
	if userConfigPath == "" {
		userConfigPath = DefaultUserConfigPath()
	}
	if managedConfigPath == "" {
		managedConfigPath = "~/.ssh/config.d/synk.conf"
	}

	expandedUserConfig, err := config.ExpandPath(userConfigPath)
	if err != nil {
		return InstallResult{}, err
	}
	includeLine := "Include " + managedConfigPath
	result := InstallResult{
		SSHConfigPath: expandedUserConfig,
		IncludeLine:   includeLine,
	}

	if err := os.MkdirAll(filepath.Dir(expandedUserConfig), 0o700); err != nil {
		return InstallResult{}, err
	}

	existing, err := os.ReadFile(expandedUserConfig)
	if err != nil && !os.IsNotExist(err) {
		return InstallResult{}, err
	}
	if containsLine(string(existing), includeLine) {
		return result, nil
	}

	if len(existing) > 0 {
		backupPath := expandedUserConfig + ".synk." + time.Now().Format("20060102150405") + ".bak"
		if err := os.WriteFile(backupPath, existing, 0o600); err != nil {
			return InstallResult{}, fmt.Errorf("backup ssh config: %w", err)
		}
		result.BackupPath = backupPath
	}

	var next strings.Builder
	next.WriteString(includeLine)
	next.WriteString("\n")
	if len(existing) > 0 {
		next.WriteString("\n")
		next.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			next.WriteString("\n")
		}
	}

	if err := atomicWrite(expandedUserConfig, []byte(next.String()), 0o600); err != nil {
		return InstallResult{}, err
	}
	result.Changed = true
	return result, nil
}

func WriteManagedConfig(path string, content string) error {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return err
	}
	return atomicWrite(expanded, []byte(content), 0o600)
}

func containsLine(content, line string) bool {
	for _, candidate := range strings.Split(content, "\n") {
		if strings.TrimSpace(candidate) == line {
			return true
		}
	}
	return false
}
