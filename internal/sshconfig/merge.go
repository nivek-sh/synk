package sshconfig

import (
	"fmt"
	"sort"
	"strings"
)

const defaultProfile = "general"

type MergeResult struct {
	Entries  []Entry
	Warnings []string
}

func Merge(entries []Entry, activeProfiles []string) (MergeResult, error) {
	activeProfiles = normalizeProfiles(activeProfiles)

	priority := map[string]int{}
	for idx, profile := range activeProfiles {
		priority[profile] = idx
	}

	countByProfile := map[string]int{}
	seen := map[string]map[string]string{}
	winners := map[string]Entry{}
	winnerPriority := map[string]int{}

	for _, entry := range entries {
		entry.Profile = strings.TrimSpace(entry.Profile)
		entry.Host = strings.TrimSpace(entry.Host)
		if entry.Profile == "" {
			entry.Profile = defaultProfile
		}
		if entry.Host == "" {
			continue
		}

		entryPriority, enabled := priority[entry.Profile]
		if !enabled {
			continue
		}

		countByProfile[entry.Profile]++
		if seen[entry.Profile] == nil {
			seen[entry.Profile] = map[string]string{}
		}
		if source, ok := seen[entry.Profile][entry.Host]; ok {
			return MergeResult{}, fmt.Errorf("duplicate host %q in profile %q from %q and %q", entry.Host, entry.Profile, source, entry.Source)
		}
		seen[entry.Profile][entry.Host] = entry.Source

		if currentPriority, ok := winnerPriority[entry.Host]; !ok || entryPriority >= currentPriority {
			winners[entry.Host] = entry.Clone()
			winnerPriority[entry.Host] = entryPriority
		}
	}

	merged := make([]Entry, 0, len(winners))
	for _, entry := range winners {
		merged = append(merged, entry)
	}
	sort.Slice(merged, func(i, j int) bool {
		leftPriority := priority[merged[i].Profile]
		rightPriority := priority[merged[j].Profile]
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return merged[i].Host < merged[j].Host
	})

	warnings := []string{}
	for _, profile := range activeProfiles {
		if profile == defaultProfile {
			continue
		}
		if countByProfile[profile] == 0 {
			warnings = append(warnings, fmt.Sprintf("profile %q has no SSH entries", profile))
		}
	}

	return MergeResult{Entries: merged, Warnings: warnings}, nil
}

func normalizeProfiles(profiles []string) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(profile string) {
		profile = strings.TrimSpace(profile)
		if profile == "" || seen[profile] {
			return
		}
		seen[profile] = true
		out = append(out, profile)
	}

	add(defaultProfile)
	for _, profile := range profiles {
		add(profile)
	}
	return out
}
