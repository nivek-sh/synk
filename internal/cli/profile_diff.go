package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"synk/internal/config"
	"synk/internal/sshconfig"
)

type profileDiffMarker string

const (
	diffMarkerEqual profileDiffMarker = "="
	diffMarkerKeep  profileDiffMarker = "+"
	diffMarkerDrop  profileDiffMarker = "-"
)

type profileDiffBlock struct {
	Marker profileDiffMarker
	Entry  sshconfig.Entry
}

func renderProfileDiff(out io.Writer, entries []sshconfig.Entry, activeProfiles []string, color colorMode) error {
	blocks, err := buildProfileDiff(entries, activeProfiles)
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		fmt.Fprintln(out, "No active SSH entries found.")
		return nil
	}

	activeProfiles = config.NormalizeProfiles(activeProfiles)
	fmt.Fprintf(out, "# Active profiles: %s\n\n", strings.Join(activeProfiles, " -> "))
	for idx, block := range blocks {
		if idx > 0 {
			fmt.Fprintln(out)
		}
		renderProfileDiffBlock(out, block, color)
	}
	return nil
}

func buildProfileDiff(entries []sshconfig.Entry, activeProfiles []string) ([]profileDiffBlock, error) {
	activeProfiles = config.NormalizeProfiles(activeProfiles)
	priority := profilePriority(activeProfiles)

	merged, err := sshconfig.Merge(entries, activeProfiles)
	if err != nil {
		return nil, err
	}

	byHost := entriesByActiveHost(entries, priority)
	blocks := []profileDiffBlock{}
	for _, winner := range merged.Entries {
		alternatives := byHost[winner.Host]
		if len(alternatives) <= 1 {
			blocks = append(blocks, profileDiffBlock{
				Marker: diffMarkerEqual,
				Entry:  winner,
			})
			continue
		}

		blocks = append(blocks, profileDiffBlock{
			Marker: diffMarkerKeep,
			Entry:  winner,
		})
		for _, loser := range losingAlternatives(alternatives, winner, priority) {
			blocks = append(blocks, profileDiffBlock{
				Marker: diffMarkerDrop,
				Entry:  loser,
			})
		}
	}
	return blocks, nil
}

func profilePriority(activeProfiles []string) map[string]int {
	priority := map[string]int{}
	for idx, profile := range activeProfiles {
		priority[profile] = idx
	}
	return priority
}

func entriesByActiveHost(entries []sshconfig.Entry, priority map[string]int) map[string][]sshconfig.Entry {
	byHost := map[string][]sshconfig.Entry{}
	for _, entry := range entries {
		entry.Profile = strings.TrimSpace(entry.Profile)
		entry.Host = strings.TrimSpace(entry.Host)
		if entry.Profile == "" {
			entry.Profile = config.DefaultProfile
		}
		if entry.Host == "" {
			continue
		}
		if _, ok := priority[entry.Profile]; !ok {
			continue
		}
		byHost[entry.Host] = append(byHost[entry.Host], entry.Clone())
	}
	return byHost
}

func losingAlternatives(entries []sshconfig.Entry, winner sshconfig.Entry, priority map[string]int) []sshconfig.Entry {
	losers := []sshconfig.Entry{}
	for _, entry := range entries {
		if entry.Profile == winner.Profile {
			continue
		}
		losers = append(losers, entry)
	}
	sort.Slice(losers, func(i, j int) bool {
		leftPriority := priority[losers[i].Profile]
		rightPriority := priority[losers[j].Profile]
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		if losers[i].Host != losers[j].Host {
			return losers[i].Host < losers[j].Host
		}
		return losers[i].Source < losers[j].Source
	})
	return losers
}

func renderProfileDiffBlock(out io.Writer, block profileDiffBlock, color colorMode) {
	prefix := string(block.Marker)
	for _, line := range strings.Split(sshconfig.RenderEntryBlock(block.Entry), "\n") {
		rendered := fmt.Sprintf("%s %s", prefix, line)
		fmt.Fprintln(out, colorProfileDiffLine(block.Marker, rendered, color))
	}
}

func colorProfileDiffLine(marker profileDiffMarker, line string, color colorMode) string {
	if !color {
		return line
	}
	switch marker {
	case diffMarkerKeep:
		return colorGreen + line + colorReset
	case diffMarkerDrop:
		return colorRed + line + colorReset
	case diffMarkerEqual:
		return colorDim + line + colorReset
	default:
		return line
	}
}

func filterEntriesByHost(entries []sshconfig.Entry, host string) []sshconfig.Entry {
	host = strings.TrimSpace(host)
	filtered := []sshconfig.Entry{}
	for _, entry := range entries {
		if entry.Host == host {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
