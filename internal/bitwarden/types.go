package bitwarden

import (
	"fmt"
	"strings"
	"unicode"

	"synk/internal/sshconfig"
)

type Item struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Type   int     `json:"type"`
	Notes  string  `json:"notes"`
	Login  *Login  `json:"login"`
	Fields []Field `json:"fields"`
	SSHKey *SSHKey `json:"sshKey"`
}

const SSHKeyItemType = 5

type Login struct {
	Username string `json:"username"`
}

type Field struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type SSHKey struct {
	PublicKey      string `json:"publicKey"`
	KeyFingerprint string `json:"keyFingerprint"`
}

func ExtractSSHEntries(items []Item) ([]sshconfig.Entry, error) {
	entries := []sshconfig.Entry{}
	for _, item := range items {
		if !item.IsSSHKey() {
			continue
		}
		fields := map[string]string{}
		hasSSHConfigField := false

		for _, field := range item.Fields {
			name, ok := canonicalFieldName(field.Name)
			if !ok {
				continue
			}
			if name == "Enabled" {
				fields[name] = cleanEnabledValue(field.Value)
			} else {
				fields[name] = cleanValue(field.Value)
			}
			hasSSHConfigField = true
		}

		if !isEnabled(fields["Enabled"]) {
			continue
		}
		if !hasSSHConfigField {
			continue
		}

		host := strings.TrimSpace(fields["Host"])
		if host == "" {
			host = slugHost(item.Name)
		}
		if host == "" {
			continue
		}

		profiles := profileNames(fields)
		directives := map[string]string{}
		for name, value := range fields {
			if value == "" || isReservedField(name) {
				continue
			}
			directives[canonicalDirective(name)] = value
		}

		if strings.TrimSpace(directives["User"]) == "" {
			return nil, fmt.Errorf("SSH key item %q is missing required custom field User", itemLabel(item))
		}
		if strings.TrimSpace(directives["HostName"]) == "" {
			return nil, fmt.Errorf("SSH key item %q is missing required custom field HostName", itemLabel(item))
		}

		for _, profile := range profiles {
			publicKey := ""
			fingerprint := ""
			if item.SSHKey != nil {
				publicKey = strings.TrimSpace(item.SSHKey.PublicKey)
				fingerprint = strings.TrimSpace(item.SSHKey.KeyFingerprint)
			}
			entries = append(entries, sshconfig.Entry{
				Host:           host,
				Profile:        profile,
				Source:         itemLabel(item),
				SourceID:       strings.TrimSpace(item.ID),
				Notes:          item.Notes,
				PublicKey:      publicKey,
				KeyFingerprint: fingerprint,
				Directives:     directives,
			})
		}
	}
	return entries, nil
}

func (i Item) IsSSHKey() bool {
	return i.Type == SSHKeyItemType
}

func canonicalFieldName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(strings.ToLower(name), "ssh_") {
		name = name[4:]
	}

	switch strings.ToLower(name) {
	case "host":
		return "Host", true
	case "profiles":
		return "Profiles", true
	case "enabled":
		return "Enabled", true
	case "hostname":
		return "HostName", true
	case "user":
		return "User", true
	case "port":
		return "Port", true
	case "identityfile":
		return "IdentityFile", true
	case "identitiesonly":
		return "IdentitiesOnly", true
	case "proxyjump":
		return "ProxyJump", true
	case "proxycommand":
		return "ProxyCommand", true
	case "forwardagent":
		return "ForwardAgent", true
	case "serveraliveinterval":
		return "ServerAliveInterval", true
	case "serveralivecountmax":
		return "ServerAliveCountMax", true
	case "addkeystoagent":
		return "AddKeysToAgent", true
	case "usekeychain":
		return "UseKeychain", true
	default:
		return "", false
	}
}

func profileNames(fields map[string]string) []string {
	raw := fields["Profiles"]
	if strings.TrimSpace(raw) == "" {
		return []string{"general"}
	}

	parts := strings.Split(raw, ",")
	out := []string{}
	seen := map[string]bool{}
	for _, part := range parts {
		profile := strings.TrimSpace(part)
		if profile == "" || seen[profile] {
			continue
		}
		seen[profile] = true
		out = append(out, profile)
	}
	if len(out) == 0 {
		return []string{"general"}
	}
	return out
}

func isEnabled(value string) bool {
	return strings.ToLower(strings.TrimSpace(value)) == "true"
}

func isReservedField(name string) bool {
	switch strings.ToLower(name) {
	case "host", "profiles", "enabled", "publickey":
		return true
	default:
		return false
	}
}

func canonicalDirective(name string) string {
	return name
}

func cleanValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return cleanStringValue(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return cleanStringValue(fmt.Sprint(typed))
	}
}

func cleanEnabledValue(value any) string {
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return cleanStringValue(typed)
}

func cleanStringValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func itemLabel(item Item) string {
	if strings.TrimSpace(item.Name) != "" {
		return item.Name
	}
	if strings.TrimSpace(item.ID) != "" {
		return item.ID
	}
	return "unknown item"
}

func slugHost(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var out strings.Builder
	lastHyphen := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && out.Len() > 0 {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(out.String(), "-")
}
