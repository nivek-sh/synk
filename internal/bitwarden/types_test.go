package bitwarden

import "testing"

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
