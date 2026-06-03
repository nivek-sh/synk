package cli

import (
	"strings"
	"testing"

	"synk/internal/sshconfig"
)

func TestMoveProfileOrder(t *testing.T) {
	tests := []struct {
		name      string
		profiles  []string
		profile   string
		operation []string
		want      string
	}{
		{
			name:      "up",
			profiles:  []string{"general", "nk", "pro"},
			profile:   "pro",
			operation: []string{"up"},
			want:      "general,pro,nk",
		},
		{
			name:      "down",
			profiles:  []string{"general", "nk", "pro"},
			profile:   "nk",
			operation: []string{"down"},
			want:      "general,pro,nk",
		},
		{
			name:      "top stays after general",
			profiles:  []string{"general", "nk", "pro", "client"},
			profile:   "client",
			operation: []string{"top"},
			want:      "general,client,nk,pro",
		},
		{
			name:      "before",
			profiles:  []string{"general", "nk", "pro", "client"},
			profile:   "client",
			operation: []string{"before", "pro"},
			want:      "general,nk,client,pro",
		},
		{
			name:      "bottom",
			profiles:  []string{"general", "nk", "pro"},
			profile:   "nk",
			operation: []string{"bottom"},
			want:      "general,pro,nk",
		},
		{
			name:      "after general",
			profiles:  []string{"general", "nk", "pro"},
			profile:   "pro",
			operation: []string{"after", "general"},
			want:      "general,pro,nk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := moveProfileOrder(tt.profiles, tt.profile, tt.operation)
			if err != nil {
				t.Fatalf("moveProfileOrder() error = %v", err)
			}
			if strings.Join(got, ",") != tt.want {
				t.Fatalf("moveProfileOrder() = %s, want %s", strings.Join(got, ","), tt.want)
			}
		})
	}
}

func TestMoveProfileOrderRejectsGeneralMove(t *testing.T) {
	_, err := moveProfileOrder([]string{"general", "nk"}, "general", []string{"down"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "always first") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestAnalyzeProfileOverrides(t *testing.T) {
	entries := []sshconfig.Entry{
		{Host: "github", Profile: "general"},
		{Host: "db", Profile: "general"},
		{Host: "github", Profile: "nk"},
		{Host: "db", Profile: "pro"},
		{Host: "ci", Profile: "pro"},
	}

	statuses := statusByName(analyzeProfileOverrides(entries, []string{"general", "nk", "pro"}))
	if statuses["general"].OverrideState != profileStateFull {
		t.Fatalf("general state = %q", statuses["general"].OverrideState)
	}
	if statuses["general"].OverriddenCount != 2 {
		t.Fatalf("general overridden count = %d", statuses["general"].OverriddenCount)
	}
	if statuses["nk"].OverrideState != profileStateClear {
		t.Fatalf("nk state = %q", statuses["nk"].OverrideState)
	}
	if statuses["pro"].OverridingCount != 1 {
		t.Fatalf("pro overriding count = %d", statuses["pro"].OverridingCount)
	}
}
