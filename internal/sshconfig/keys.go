package sshconfig

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"synk/internal/config"
)

type ManagedPublicKey struct {
	Path        string
	DisplayPath string
	Content     string
	ItemID      string
	Fingerprint string
}

func PrepareManagedIdentities(entries []Entry, keyDir string) ([]Entry, []ManagedPublicKey, []string, error) {
	if strings.TrimSpace(keyDir) == "" {
		return nil, nil, nil, fmt.Errorf("managed key directory is empty")
	}
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return nil, nil, nil, err
	}

	prepared := make([]Entry, 0, len(entries))
	keysByPath := map[string]ManagedPublicKey{}
	warnings := []string{}
	for _, original := range entries {
		entry := original.Clone()
		if strings.TrimSpace(entry.Directives["IdentityFile"]) != "" {
			prepared = append(prepared, entry)
			continue
		}

		publicKey, fingerprint, err := normalizePublicKey(entry.PublicKey, entry.KeyFingerprint)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("SSH key item %q: %w", entry.Source, err)
		}
		filename := managedKeyFilename(entry.SourceID, publicKey)
		displayPath := filepath.Join(keyDir, filename)
		path := filepath.Join(expandedDir, filename)
		entry.Directives["IdentityFile"] = displayPath
		if configured := strings.TrimSpace(entry.Directives["IdentitiesOnly"]); configured == "" {
			entry.Directives["IdentitiesOnly"] = "yes"
		} else if !strings.EqualFold(configured, "yes") {
			warnings = append(warnings, fmt.Sprintf("host %q uses a managed Bitwarden key but IdentitiesOnly is %q", entry.Host, configured))
		}

		key := ManagedPublicKey{
			Path:        path,
			DisplayPath: displayPath,
			Content:     publicKey + "\n",
			ItemID:      entry.SourceID,
			Fingerprint: fingerprint,
		}
		if existing, ok := keysByPath[path]; ok && existing.Content != key.Content {
			return nil, nil, nil, fmt.Errorf("managed public key path collision at %s", displayPath)
		}
		keysByPath[path] = key
		entry.PublicKey = publicKey
		entry.KeyFingerprint = fingerprint
		prepared = append(prepared, entry)
	}

	keys := make([]ManagedPublicKey, 0, len(keysByPath))
	for _, key := range keysByPath {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Path < keys[j].Path })
	return prepared, keys, warnings, nil
}

func WriteManagedPublicKeys(keys []ManagedPublicKey, keyDir string) error {
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(expandedDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(expandedDir, 0o700); err != nil {
		return err
	}
	for _, key := range keys {
		if filepath.Dir(key.Path) != expandedDir {
			return fmt.Errorf("managed public key escapes configured directory: %s", key.Path)
		}
		if err := atomicWrite(key.Path, []byte(key.Content), 0o600); err != nil {
			return fmt.Errorf("write managed public key %s: %w", key.DisplayPath, err)
		}
	}
	return nil
}

func RemoveStaleManagedPublicKeys(keys []ManagedPublicKey, keyDir string) error {
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, key := range keys {
		desired[filepath.Base(key.Path)] = true
	}
	entries, err := os.ReadDir(expandedDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "bw-") || !strings.HasSuffix(name, ".pub") || desired[name] {
			continue
		}
		if err := os.Remove(filepath.Join(expandedDir, name)); err != nil {
			return fmt.Errorf("remove stale managed public key %s: %w", name, err)
		}
	}
	return nil
}

func ManagedPublicKeyCurrent(key ManagedPublicKey) bool {
	data, err := os.ReadFile(key.Path)
	return err == nil && string(data) == key.Content
}

func PublicKeyFingerprint(publicKey string) string {
	_, fingerprint, err := normalizePublicKey(publicKey, "")
	if err != nil {
		return ""
	}
	return fingerprint
}

func normalizePublicKey(raw, suppliedFingerprint string) (string, string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("has no public key and no explicit IdentityFile")
	}
	if strings.ContainsAny(strings.TrimSpace(raw), "\r\n") {
		return "", "", fmt.Errorf("public key must contain exactly one line")
	}
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return "", "", fmt.Errorf("public key is malformed")
	}
	if !strings.HasPrefix(fields[0], "ssh-") && !strings.HasPrefix(fields[0], "ecdsa-") && !strings.HasPrefix(fields[0], "sk-") {
		return "", "", fmt.Errorf("unsupported public key type %q", fields[0])
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(decoded) == 0 {
		return "", "", fmt.Errorf("public key payload is not valid base64")
	}
	key := strings.Join(fields, " ")
	fingerprint := strings.TrimSpace(suppliedFingerprint)
	if fingerprint == "" {
		digest := sha256.Sum256(decoded)
		fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
	}
	return key, fingerprint, nil
}

func managedKeyFilename(itemID, publicKey string) string {
	id := strings.TrimSpace(itemID)
	var safe strings.Builder
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			safe.WriteRune(r)
		}
	}
	if safe.Len() == 0 {
		digest := sha256.Sum256([]byte(publicKey))
		safe.WriteString(fmt.Sprintf("%x", digest[:12]))
	}
	return "bw-" + safe.String() + ".pub"
}
