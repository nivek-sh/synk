package sshconfig

import (
	"strings"
	"testing"
)

func TestMergeAppliesProfilePrecedence(t *testing.T) {
	entries := []Entry{
		{
			Host:    "github.com",
			Profile: "general",
			Source:  "General GitHub",
			Directives: map[string]string{
				"HostName":     "github.com",
				"User":         "git",
				"IdentityFile": "~/.ssh/id_general",
			},
		},
		{
			Host:    "internal",
			Profile: "nk",
			Source:  "NK Internal",
			Directives: map[string]string{
				"HostName": "198.51.100.20",
				"User":     "deploy",
			},
		},
		{
			Host:    "github.com",
			Profile: "pro",
			Source:  "Pro GitHub",
			Directives: map[string]string{
				"HostName":     "ssh.github.com",
				"User":         "git",
				"IdentityFile": "~/.ssh/id_pro",
			},
		},
	}

	merged, err := Merge(entries, []string{"general", "nk", "pro"})
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if len(merged.Entries) != 2 {
		t.Fatalf("expected 2 winners, got %d", len(merged.Entries))
	}
	if merged.Entries[0].Host != "github.com" || merged.Entries[0].Profile != "pro" {
		t.Fatalf("first entry = %#v", merged.Entries[0])
	}
	if merged.Entries[1].Host != "internal" || merged.Entries[1].Profile != "nk" {
		t.Fatalf("second entry = %#v", merged.Entries[1])
	}
}

func TestMergeRejectsDuplicatesWithinSameProfile(t *testing.T) {
	entries := []Entry{
		{Host: "github.com", Profile: "general", Source: "one"},
		{Host: "github.com", Profile: "general", Source: "two"},
	}

	_, err := Merge(entries, []string{"general"})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "duplicate host") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestMergeWarnsForEmptyRequestedProfiles(t *testing.T) {
	merged, err := Merge([]Entry{{Host: "github.com", Profile: "general"}}, []string{"general", "pro"})
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if len(merged.Warnings) != 1 {
		t.Fatalf("warnings = %#v", merged.Warnings)
	}
	if !strings.Contains(merged.Warnings[0], "pro") {
		t.Fatalf("warning = %q", merged.Warnings[0])
	}
}
