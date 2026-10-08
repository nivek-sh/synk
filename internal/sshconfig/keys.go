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

type ManagedPrivateKey struct {
	Path        string
	DisplayPath string
	Content     string
	ItemID      string
}

func PrepareManagedIdentities(entries []Entry, keyDir string) ([]Entry, []ManagedPublicKey, []ManagedPrivateKey, []string, error) {
	if strings.TrimSpace(keyDir) == "" {
		return nil, nil, nil, nil, fmt.Errorf("managed key directory is empty")
	}
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	prepared := make([]Entry, 0, len(entries))
	keysByPath := map[string]ManagedPublicKey{}
	privateKeysByPath := map[string]ManagedPrivateKey{}
	warnings := []string{}
	for _, original := range entries {
		entry := original.Clone()
		if entry.PrivateKey != "" {
			if strings.TrimSpace(entry.Directives["IdentityFile"]) != "" {
				return nil, nil, nil, nil, fmt.Errorf("legacy SSH note %q cannot combine Private Key with IdentityFile", entry.Source)
			}
			privateKey, err := normalizePrivateKey(entry.PrivateKey)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("legacy SSH note %q: %w", entry.Source, err)
			}
			filename, err := managedPrivateKeyFilename(entry.SourceID)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("legacy SSH note %q: %w", entry.Source, err)
			}
			displayPath := filepath.Join(keyDir, filename)
			key := ManagedPrivateKey{
				Path:        filepath.Join(expandedDir, filename),
				DisplayPath: displayPath,
				Content:     privateKey,
				ItemID:      entry.SourceID,
			}
			if existing, ok := privateKeysByPath[key.Path]; ok && existing.Content != key.Content {
				return nil, nil, nil, nil, fmt.Errorf("managed private key path collision at %s", displayPath)
			}
			privateKeysByPath[key.Path] = key
			entry.PrivateKey = privateKey
			entry.Directives["IdentityFile"] = displayPath
			if configured := strings.TrimSpace(entry.Directives["IdentitiesOnly"]); configured == "" {
				entry.Directives["IdentitiesOnly"] = "yes"
			}
			prepared = append(prepared, entry)
			continue
		}
		if strings.TrimSpace(entry.Directives["IdentityFile"]) != "" {
			prepared = append(prepared, entry)
			continue
		}

		publicKey, fingerprint, err := normalizePublicKey(entry.PublicKey, entry.KeyFingerprint)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("SSH key item %q: %w", entry.Source, err)
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
			return nil, nil, nil, nil, fmt.Errorf("managed public key path collision at %s", displayPath)
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
	privateKeys := make([]ManagedPrivateKey, 0, len(privateKeysByPath))
	for _, key := range privateKeysByPath {
		privateKeys = append(privateKeys, key)
	}
	sort.Slice(privateKeys, func(i, j int) bool { return privateKeys[i].Path < privateKeys[j].Path })
	return prepared, keys, privateKeys, warnings, nil
}

func WriteManagedPrivateKeys(keys []ManagedPrivateKey, keyDir string) error {
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	if err := ensureManagedKeyDirectory(expandedDir); err != nil {
		return err
	}
	for _, key := range keys {
		if filepath.Dir(key.Path) != expandedDir {
			return fmt.Errorf("managed private key escapes configured directory: %s", key.DisplayPath)
		}
		if ManagedPrivateKeyCurrent(key) {
			continue
		}
		if err := atomicWrite(key.Path, []byte(key.Content), 0o600); err != nil {
			return fmt.Errorf("write managed private key %s: %w", key.DisplayPath, err)
		}
	}
	return nil
}

func RemoveStaleManagedPrivateKeys(keys []ManagedPrivateKey, keyDir string) error {
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return err
	}
	if err := checkManagedKeyDirectory(expandedDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
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
		if entry.IsDir() || !strings.HasPrefix(name, "bw-") || !strings.HasSuffix(name, ".key") || desired[name] {
			continue
		}
		if err := os.Remove(filepath.Join(expandedDir, name)); err != nil {
			return fmt.Errorf("remove stale managed private key %s: %w", name, err)
		}
	}
	return nil
}

func ManagedPrivateKeyCurrent(key ManagedPrivateKey) bool {
	dirInfo, err := os.Lstat(filepath.Dir(key.Path))
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o700 {
		return false
	}
	return managedKeyFileCurrent(key.Path, key.Content)
}

func normalizePrivateKey(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	const beginPrefix = "-----BEGIN "
	if !strings.HasPrefix(raw, beginPrefix) {
		return "", fmt.Errorf("Private Key must be an armored OpenSSH or PEM private key")
	}
	labelEnd := strings.Index(raw[len(beginPrefix):], "-----")
	if labelEnd < 0 {
		return "", fmt.Errorf("Private Key has an incomplete BEGIN line")
	}
	label := raw[len(beginPrefix) : len(beginPrefix)+labelEnd]
	if !strings.HasSuffix(label, "PRIVATE KEY") {
		return "", fmt.Errorf("Private Key has an unsupported BEGIN line")
	}
	header := beginPrefix + label + "-----"
	footer := "-----END " + label + "-----"
	if !strings.HasSuffix(raw, footer) {
		return "", fmt.Errorf("Private Key has no matching END line")
	}
	body := raw[len(header) : len(raw)-len(footer)]
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("Private Key has no encoded content")
	}
	multiline := strings.HasPrefix(body, "\n") && strings.HasSuffix(body, "\n")
	if strings.Contains(body, ":") {
		if !multiline {
			return "", fmt.Errorf("Private Key has flattened PEM headers that cannot be reconstructed")
		}
		return raw + "\n", nil
	}
	payload := strings.Join(strings.Fields(body), "")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(decoded) == 0 {
		return "", fmt.Errorf("Private Key contains invalid base64")
	}
	if multiline {
		return raw + "\n", nil
	}
	var normalized strings.Builder
	normalized.WriteString(header)
	normalized.WriteByte('\n')
	for len(payload) > 70 {
		normalized.WriteString(payload[:70])
		normalized.WriteByte('\n')
		payload = payload[70:]
	}
	normalized.WriteString(payload)
	normalized.WriteByte('\n')
	normalized.WriteString(footer)
	normalized.WriteByte('\n')
	return normalized.String(), nil
}

func WriteManagedPublicKeys(keys []ManagedPublicKey, keyDir string) error {
	expandedDir, err := config.ExpandPath(keyDir)
	if err != nil {
		return err
	}
	if err := ensureManagedKeyDirectory(expandedDir); err != nil {
		return err
	}
	for _, key := range keys {
		if filepath.Dir(key.Path) != expandedDir {
			return fmt.Errorf("managed public key escapes configured directory: %s", key.Path)
		}
		if ManagedPublicKeyCurrent(key) {
			continue
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
	if err := checkManagedKeyDirectory(expandedDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
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
	return managedKeyFileCurrent(key.Path, key.Content)
}

func managedKeyFileCurrent(path, content string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == content
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

func managedPrivateKeyFilename(itemID string) (string, error) {
	id := strings.TrimSpace(itemID)
	if id == "" {
		return "", fmt.Errorf("Bitwarden item has no ID")
	}
	for _, r := range id {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return "", fmt.Errorf("Bitwarden item ID contains an invalid character")
		}
	}
	return "bw-" + id + ".key", nil
}

func ensureManagedKeyDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := checkManagedKeyDirectory(path); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func checkManagedKeyDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("managed key directory is not a regular directory: %s", path)
	}
	return nil
}
