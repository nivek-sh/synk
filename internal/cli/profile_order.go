package cli

import (
	"fmt"
	"sort"
	"strings"

	"synk/internal/config"
	"synk/internal/sshconfig"
)

type profileOverrideState string

const (
	profileStateInactive profileOverrideState = "inactive"
	profileStateEmpty    profileOverrideState = "empty"
	profileStateClear    profileOverrideState = "clear"
	profileStatePartial  profileOverrideState = "partial"
	profileStateFull     profileOverrideState = "full"
)

type profileStatus struct {
	Name             string
	Active           bool
	HostCount        int
	OverriddenCount  int
	OverridingCount  int
	OverrideState    profileOverrideState
	OverriddenBy     []string
	OverridesProfile []string
}

func moveProfileOrder(profiles []string, profile string, operation []string) ([]string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return nil, fmt.Errorf("profile is required")
	}
	if profile == config.DefaultProfile {
		return nil, fmt.Errorf("%q is always first and cannot be moved", config.DefaultProfile)
	}
	if len(operation) == 0 {
		return nil, fmt.Errorf("move operation is required")
	}

	profiles = config.NormalizeProfiles(profiles)
	index := indexOf(profiles, profile)
	if index < 0 {
		return nil, fmt.Errorf("profile %q is not active; use `synk profile add %s` first or `synk profile edit`", profile, profile)
	}

	switch operation[0] {
	case "up":
		if index <= 1 {
			return profiles, nil
		}
		profiles[index-1], profiles[index] = profiles[index], profiles[index-1]
	case "down":
		if index >= len(profiles)-1 {
			return profiles, nil
		}
		profiles[index+1], profiles[index] = profiles[index], profiles[index+1]
	case "top":
		profiles = moveProfileToIndex(profiles, index, 1)
	case "bottom":
		profiles = moveProfileToIndex(profiles, index, len(profiles))
	case "before", "after":
		if len(operation) != 2 {
			return nil, fmt.Errorf("usage: profile move %s %s TARGET", profile, operation[0])
		}
		target := strings.TrimSpace(operation[1])
		if target == "" {
			return nil, fmt.Errorf("target profile is required")
		}
		if target == profile {
			return profiles, nil
		}
		targetIndex := indexOf(profiles, target)
		if targetIndex < 0 {
			return nil, fmt.Errorf("target profile %q is not active", target)
		}
		if target == config.DefaultProfile && operation[0] == "before" {
			return nil, fmt.Errorf("%q is always first; no profile can be moved before it", config.DefaultProfile)
		}
		if operation[0] == "after" {
			targetIndex++
		}
		profiles = moveProfileToIndex(profiles, index, targetIndex)
	default:
		return nil, fmt.Errorf("unknown move operation %q", operation[0])
	}

	return config.NormalizeProfiles(profiles), nil
}

func moveProfileToIndex(profiles []string, from, to int) []string {
	if from < 0 || from >= len(profiles) {
		return profiles
	}
	if to < 1 {
		to = 1
	}
	if to > len(profiles) {
		to = len(profiles)
	}
	if from == to {
		return profiles
	}

	profile := profiles[from]
	next := append([]string{}, profiles[:from]...)
	next = append(next, profiles[from+1:]...)
	if from < to {
		to--
	}
	if to < 1 {
		to = 1
	}
	if to > len(next) {
		to = len(next)
	}
	next = append(next[:to], append([]string{profile}, next[to:]...)...)
	return next
}

func indexOf(values []string, value string) int {
	for idx, candidate := range values {
		if candidate == value {
			return idx
		}
	}
	return -1
}

func profileOrderWithDiscovered(activeProfiles []string, entries []sshconfig.Entry) []string {
	profiles := config.NormalizeProfiles(activeProfiles)
	seen := map[string]bool{}
	for _, profile := range profiles {
		seen[profile] = true
	}

	discovered := []string{}
	for _, entry := range entries {
		profile := strings.TrimSpace(entry.Profile)
		if profile == "" {
			profile = config.DefaultProfile
		}
		if !seen[profile] {
			seen[profile] = true
			discovered = append(discovered, profile)
		}
	}
	sort.Strings(discovered)
	return append(profiles, discovered...)
}

func analyzeProfileOverrides(entries []sshconfig.Entry, activeProfiles []string) []profileStatus {
	activeProfiles = config.NormalizeProfiles(activeProfiles)
	active := map[string]bool{}
	priority := map[string]int{}
	for idx, profile := range activeProfiles {
		active[profile] = true
		priority[profile] = idx
	}

	hostByProfile := map[string]map[string]bool{}
	for _, entry := range entries {
		profile := strings.TrimSpace(entry.Profile)
		if profile == "" {
			profile = config.DefaultProfile
		}
		host := strings.TrimSpace(entry.Host)
		if host == "" || !active[profile] {
			continue
		}
		if hostByProfile[profile] == nil {
			hostByProfile[profile] = map[string]bool{}
		}
		hostByProfile[profile][host] = true
	}

	statuses := make([]profileStatus, 0, len(activeProfiles))
	for _, profile := range activeProfiles {
		status := profileStatus{
			Name:          profile,
			Active:        true,
			OverrideState: profileStateClear,
		}
		hosts := hostByProfile[profile]
		status.HostCount = len(hosts)
		if status.HostCount == 0 {
			status.OverrideState = profileStateEmpty
			statuses = append(statuses, status)
			continue
		}

		overriddenBy := map[string]bool{}
		overrides := map[string]bool{}
		for host := range hosts {
			hostOverridden := false
			for otherProfile, otherHosts := range hostByProfile {
				if otherProfile == profile || !otherHosts[host] {
					continue
				}
				if priority[otherProfile] > priority[profile] {
					hostOverridden = true
					overriddenBy[otherProfile] = true
				}
				if priority[otherProfile] < priority[profile] {
					overrides[otherProfile] = true
				}
			}
			if hostOverridden {
				status.OverriddenCount++
			}
		}
		status.OverridingCount = len(overrides)
		status.OverriddenBy = sortedMapKeys(overriddenBy)
		status.OverridesProfile = sortedMapKeys(overrides)

		switch {
		case status.OverriddenCount == 0:
			status.OverrideState = profileStateClear
		case status.OverriddenCount == status.HostCount:
			status.OverrideState = profileStateFull
		default:
			status.OverrideState = profileStatePartial
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func sortedMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func statusByName(statuses []profileStatus) map[string]profileStatus {
	byName := map[string]profileStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	return byName
}

func uniqueHostCountForProfile(entries []sshconfig.Entry, profile string) int {
	hosts := map[string]bool{}
	for _, entry := range entries {
		entryProfile := strings.TrimSpace(entry.Profile)
		if entryProfile == "" {
			entryProfile = config.DefaultProfile
		}
		if entryProfile != profile {
			continue
		}
		host := strings.TrimSpace(entry.Host)
		if host != "" {
			hosts[host] = true
		}
	}
	return len(hosts)
}
