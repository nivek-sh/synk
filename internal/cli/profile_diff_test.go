package cli

import (
	"bytes"
	"strings"
	"testing"

	"synk/internal/sshconfig"
)

func TestRenderProfileDiffShowsEffectiveConfigWithMarkers(t *testing.T) {
	entries := []sshconfig.Entry{
		{
			Host:    "github",
			Profile: "general",
			Source:  "GitHub General",
			Directives: map[string]string{
				"HostName": "github.com",
				"User":     "git",
			},
		},
		{
			Host:    "github",
			Profile: "nk",
			Source:  "GitHub NK",
			Directives: map[string]string{
				"HostName": "ssh.github.com",
				"User":     "git",
			},
		},
		{
			Host:    "github",
			Profile: "pro",
			Source:  "GitHub Pro",
			Directives: map[string]string{
				"HostName": "ssh.github.com",
				"User":     "git",
				"Port":     "443",
			},
		},
		{
			Host:    "cloud",
			Profile: "nk",
			Source:  "Cloud",
			Directives: map[string]string{
				"HostName": "203.0.113.10",
				"User":     "deploy",
			},
		},
	}

	var out bytes.Buffer
	if err := renderProfileDiff(&out, entries, []string{"general", "nk", "pro"}, false); err != nil {
		t.Fatalf("renderProfileDiff() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"# Active profiles: general -> nk -> pro\n",
		"+ # Profile: pro | Source: GitHub Pro\n",
		"+ Host github\n",
		"+     HostName ssh.github.com\n",
		"+     User git\n",
		"+     Port 443\n",
		"- # Profile: nk | Source: GitHub NK\n",
		"- Host github\n",
		"- # Profile: general | Source: GitHub General\n",
		"- Host github\n",
		"= # Profile: nk | Source: Cloud\n",
		"= Host cloud\n",
		"=     HostName 203.0.113.10\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestProfileOverrideCountsAreCompact(t *testing.T) {
	status := profileStatus{
		HostCount:        10,
		OverriddenCount:  3,
		OverridingCount:  2,
		OverriddenBy:     []string{"pro", "work"},
		OverridesProfile: []string{"general", "nk"},
	}

	got := profileOverrideCounts(status)
	if got != "overridden 3/10, overrides 2" {
		t.Fatalf("profileOverrideCounts() = %q", got)
	}
	if strings.Contains(got, "pro") || strings.Contains(got, "general") {
		t.Fatalf("profileOverrideCounts leaked names: %q", got)
	}
}
