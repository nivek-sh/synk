package sshconfig

import (
	"bufio"
	"fmt"
	"strings"
)

func ParseManaged(content string) ([]Entry, error) {
	entries := []Entry{}
	var current *Entry
	pendingProfile := ""
	pendingSource := ""

	flush := func() {
		if current == nil {
			return
		}
		current.Notes = strings.TrimSpace(current.Notes)
		entries = append(entries, *current)
		current = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# Profile: ") {
			flush()
			metadata := strings.TrimPrefix(trimmed, "# Profile: ")
			parts := strings.SplitN(metadata, " | Source: ", 2)
			pendingProfile = strings.TrimSpace(parts[0])
			pendingSource = ""
			if len(parts) == 2 {
				pendingSource = strings.TrimSpace(parts[1])
			}
			continue
		}
		if strings.HasPrefix(trimmed, "Host ") {
			flush()
			host := strings.TrimSpace(strings.TrimPrefix(trimmed, "Host "))
			if host == "" || strings.ContainsAny(host, " \t") {
				return nil, fmt.Errorf("parse managed config line %d: unsupported Host value %q", lineNumber, host)
			}
			current = &Entry{
				Host:       host,
				Profile:    pendingProfile,
				Source:     pendingSource,
				Directives: map[string]string{},
			}
			pendingProfile = ""
			pendingSource = ""
			continue
		}
		if current == nil || trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			note := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if note != "" {
				if current.Notes != "" {
					current.Notes += "\n"
				}
				current.Notes += note
			}
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			return nil, fmt.Errorf("parse managed config line %d: malformed directive", lineNumber)
		}
		current.Directives[parts[0]] = strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return entries, nil
}
