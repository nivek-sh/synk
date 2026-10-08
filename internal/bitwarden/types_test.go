package bitwarden

import (
	"strings"
	"testing"
)

func TestExtractSSHEntriesMapsCustomFields(t *testing.T) {
	items := []Item{
		{
			Name:  "GitHub Pro",
			Type:  SSHKeyItemType,
			Notes: "Use port 443 from restrictive networks.",
			Login: &Login{
				Username: "git",
			},
			Fields: []Field{
				{Name: "Enabled", Value: "true"},
				{Name: "Host", Value: "github.com"},
				{Name: "Profiles", Value: "pro"},
				{Name: "HostName", Value: "ssh.github.com"},
				{Name: "User", Value: "git"},
				{Name: "Port", Value: "443"},
				{Name: "Ignored Custom Field", Value: "ignored"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Host != "github.com" {
		t.Fatalf("Host = %q", entry.Host)
	}
	if entry.Profile != "pro" {
		t.Fatalf("Profile = %q", entry.Profile)
	}
	if entry.Directives["HostName"] != "ssh.github.com" {
		t.Fatalf("HostName = %q", entry.Directives["HostName"])
	}
	if entry.Directives["User"] != "git" {
		t.Fatalf("User = %q", entry.Directives["User"])
	}
	if entry.Directives["Port"] != "443" {
		t.Fatalf("Port = %q", entry.Directives["Port"])
	}
	if _, ok := entry.Directives["PublicKey"]; ok {
		t.Fatal("PublicKey leaked into rendered directives")
	}
	if entry.Notes != "Use port 443 from restrictive networks." {
		t.Fatalf("Notes = %q", entry.Notes)
	}
}

func TestExtractSSHEntriesCarriesNativePublicKeyMetadata(t *testing.T) {
	items := []Item{{
		ID:   "item-123",
		Name: "Cloud",
		Type: SSHKeyItemType,
		SSHKey: &SSHKey{
			PublicKey:      "ssh-ed25519 AAAA cloud",
			KeyFingerprint: "SHA256:test",
		},
		Fields: []Field{
			{Name: "Enabled", Value: "true"},
			{Name: "HostName", Value: "cloud.example.com"},
			{Name: "User", Value: "deploy"},
		},
	}}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	entry := entries[0]
	if entry.SourceID != "item-123" || entry.PublicKey != "ssh-ed25519 AAAA cloud" || entry.KeyFingerprint != "SHA256:test" {
		t.Fatalf("entry metadata = %#v", entry)
	}
}

func TestExtractSSHEntriesRequiresUser(t *testing.T) {
	items := []Item{
		{
			Name:  "Host",
			Type:  SSHKeyItemType,
			Login: &Login{Username: "login-user"},
			Fields: []Field{
				{Name: "Enabled", Value: "true"},
				{Name: "Host", Value: "host"},
				{Name: "HostName", Value: "host.example.com"},
			},
		},
	}

	_, err := ExtractSSHEntries(items)
	if err == nil {
		t.Fatal("expected missing User error")
	}
	if got := err.Error(); got != `SSH key item "Host" is missing required custom field User` {
		t.Fatalf("error = %q", got)
	}
}

func TestExtractSSHEntriesRequiresHostName(t *testing.T) {
	items := []Item{
		{
			Name: "Host",
			Type: SSHKeyItemType,
			Fields: []Field{
				{Name: "Enabled", Value: "true"},
				{Name: "Host", Value: "host"},
				{Name: "User", Value: "deploy"},
			},
		},
	}

	_, err := ExtractSSHEntries(items)
	if err == nil {
		t.Fatal("expected missing HostName error")
	}
	if got := err.Error(); got != `SSH key item "Host" is missing required custom field HostName` {
		t.Fatalf("error = %q", got)
	}
}

func TestExtractSSHEntriesDisabledItemsAreIgnored(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "false string", value: "false"},
		{name: "true bool", value: true},
		{name: "false bool", value: false},
		{name: "zero", value: "0"},
		{name: "no", value: "no"},
		{name: "off", value: "off"},
		{name: "null", value: nil},
		{name: "empty", value: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := []Item{
				{
					Name: "Disabled",
					Type: SSHKeyItemType,
					Fields: []Field{
						{Name: "Enabled", Value: tt.value},
						{Name: "HostName", Value: "disabled.example.com"},
						{Name: "User", Value: "root"},
					},
				},
			}

			entries, err := ExtractSSHEntries(items)
			if err != nil {
				t.Fatalf("ExtractSSHEntries() error = %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("expected disabled item to be ignored, got %d entries", len(entries))
			}
		})
	}
}

func TestExtractSSHEntriesMissingEnabledIsIgnored(t *testing.T) {
	items := []Item{
		{
			Name: "Missing Enabled",
			Type: SSHKeyItemType,
			Fields: []Field{
				{Name: "HostName", Value: "missing.example.com"},
				{Name: "User", Value: "root"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected item without Enabled to be ignored, got %d entries", len(entries))
	}
}

func TestExtractSSHEntriesUsesSluggedItemNameWhenHostIsMissing(t *testing.T) {
	items := []Item{
		{
			Name: "Por Ejemplo Este Texto",
			Type: SSHKeyItemType,
			Fields: []Field{
				{Name: "Enabled", Value: "true"},
				{Name: "HostName", Value: "203.0.113.10"},
				{Name: "User", Value: "deploy"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if got := entries[0].Host; got != "por-ejemplo-este-texto" {
		t.Fatalf("Host = %q", got)
	}
}

func TestExtractSSHEntriesSupportsMultipleProfiles(t *testing.T) {
	items := []Item{
		{
			Name: "Shared",
			Type: SSHKeyItemType,
			Fields: []Field{
				{Name: "Enabled", Value: "true"},
				{Name: "Host", Value: "shared"},
				{Name: "Profiles", Value: "general,pro,pro"},
				{Name: "HostName", Value: "shared.example.com"},
				{Name: "User", Value: "deploy"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Profile != "general" || entries[1].Profile != "pro" {
		t.Fatalf("profiles = %q, %q", entries[0].Profile, entries[1].Profile)
	}
}

func TestExtractSSHEntriesIgnoresSingularProfileField(t *testing.T) {
	items := []Item{
		{
			Name: "Singular Profile",
			Type: SSHKeyItemType,
			Fields: []Field{
				{Name: "Enabled", Value: "true"},
				{Name: "Host", Value: "singular"},
				{Name: "Profile", Value: "pro"},
				{Name: "HostName", Value: "singular.example.com"},
				{Name: "User", Value: "deploy"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Profile != "general" {
		t.Fatalf("Profile = %q", entries[0].Profile)
	}
}

func TestExtractSSHEntriesStillAcceptsLegacySSHPrefix(t *testing.T) {
	items := []Item{
		{
			Name: "Legacy",
			Type: SSHKeyItemType,
			Fields: []Field{
				{Name: "ssh_Enabled", Value: "true"},
				{Name: "ssh_Host", Value: "legacy"},
				{Name: "ssh_HostName", Value: "legacy.example.com"},
				{Name: "ssh_User", Value: "deploy"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if got := entries[0].Directives["User"]; got != "deploy" {
		t.Fatalf("User = %q", got)
	}
}

func TestExtractSSHEntriesIgnoresNonSSHKeyItems(t *testing.T) {
	items := []Item{
		{
			Name: "Login With SSH Fields",
			Type: 1,
			Fields: []Field{
				{Name: "HostName", Value: "example.com"},
				{Name: "User", Value: "deploy"},
			},
		},
	}

	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatalf("ExtractSSHEntries() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected non-SSH key item to be ignored, got %d", len(entries))
	}
}

func TestExtractSSHEntriesReadsMarkedLegacyNote(t *testing.T) {
	secret := "-----BEGIN DSA PRIVATE KEY-----\r\nexample\r\n-----END DSA PRIVATE KEY-----"
	items := []Item{{
		ID:   "legacy-123",
		Name: "Old server",
		Type: SecureNoteItemType,
		Fields: []Field{
			{Name: "Legacy SSH", Type: BooleanFieldType, Value: "true"},
			{Name: "Private Key", Value: secret},
			{Name: "Enabled", Value: "true"},
			{Name: "HostName", Value: "old.example.com"},
			{Name: "User", Value: "deploy"},
		},
	}}
	entries, err := ExtractSSHEntries(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].PrivateKey != strings.ReplaceAll(secret, "\r\n", "\n")+"\n" {
		t.Fatalf("legacy note was not extracted correctly: entries=%d", len(entries))
	}
	if entries[0].SourceID != "legacy-123" || entries[0].Host != "old-server" {
		t.Fatalf("legacy note metadata = %#v", entries[0])
	}
	if _, ok := entries[0].Directives["Private Key"]; ok {
		t.Fatal("private key entered SSH directives")
	}
}

func TestExtractSSHEntriesOnlyUsesCheckedSecureNotes(t *testing.T) {
	for _, tc := range []struct {
		name string
		item Item
	}{
		{name: "unchecked", item: Item{Type: SecureNoteItemType, Fields: []Field{{Name: "Legacy SSH", Type: BooleanFieldType, Value: "false"}}}},
		{name: "wrong field type", item: Item{Type: SecureNoteItemType, Fields: []Field{{Name: "Legacy SSH", Type: 0, Value: "true"}}}},
		{name: "wrong item type", item: Item{Type: 1, Fields: []Field{{Name: "Legacy SSH", Type: BooleanFieldType, Value: "true"}}}},
		{name: "missing marker", item: Item{Type: SecureNoteItemType, Fields: []Field{{Name: "Private Key", Value: "secret"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.item.IsLegacySSHNote() {
				t.Fatal("unexpected legacy note")
			}
			entries, err := ExtractSSHEntries([]Item{tc.item})
			if err != nil || len(entries) != 0 {
				t.Fatalf("entries=%d err=%v", len(entries), err)
			}
		})
	}
}

func TestExtractSSHEntriesLegacyNoteRequiresPrivateKey(t *testing.T) {
	item := Item{
		Name: "Old server",
		Type: SecureNoteItemType,
		Fields: []Field{
			{Name: "Legacy SSH", Type: BooleanFieldType, Value: true},
			{Name: "Enabled", Value: "true"},
			{Name: "HostName", Value: "old.example.com"},
			{Name: "User", Value: "deploy"},
		},
	}
	_, err := ExtractSSHEntries([]Item{item})
	if err == nil || !strings.Contains(err.Error(), "missing required custom field Private Key") {
		t.Fatalf("error = %v", err)
	}
	item.Fields = append(item.Fields, Field{Name: "Private Key", Value: "secret"}, Field{Name: "IdentityFile", Value: "~/.ssh/other"})
	_, err = ExtractSSHEntries([]Item{item})
	if err == nil || !strings.Contains(err.Error(), "cannot combine Private Key with IdentityFile") {
		t.Fatalf("error = %v", err)
	}
	item.Fields = []Field{
		{Name: "Legacy SSH", Type: BooleanFieldType, Value: "true"},
		{Name: "Enabled", Value: "false"},
		{Name: "Private Key", Value: true},
	}
	entries, err := ExtractSSHEntries([]Item{item})
	if err != nil || len(entries) != 0 {
		t.Fatalf("disabled note: entries=%d err=%v", len(entries), err)
	}
}
