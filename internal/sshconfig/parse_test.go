package sshconfig

import "testing"

func TestParseManagedRoundTrip(t *testing.T) {
	original := []Entry{{
		Host:    "dev",
		Profile: "pro",
		Source:  "Dev",
		Notes:   "Production server",
		Directives: map[string]string{
			"HostName":       "203.0.113.10",
			"User":           "root",
			"IdentityFile":   "~/.ssh/config.d/synk.keys/bw-id.pub",
			"IdentitiesOnly": "yes",
		},
	}}
	rendered := Render(original, []string{"general", "pro"})
	parsed, err := ParseManaged(rendered)
	if err != nil {
		t.Fatalf("ParseManaged() error = %v", err)
	}
	if len(parsed) != 1 || RenderEntryBlock(parsed[0]) != RenderEntryBlock(original[0]) {
		t.Fatalf("parsed = %#v", parsed)
	}
}
