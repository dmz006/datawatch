// A partial update of the rules and context settings, shared by the MCP
// tools, the command line and the chat channel. The REST write replaces the
// whole value, so each surface reads the current value, applies the fields
// the operator gave and writes the merged result back.

package config

import (
	"fmt"
	"strconv"
	"strings"
)

// RulesContextUpdate names the fields to change. A nil field is left as it
// is. ProjectDir empty updates the global values; otherwise the override for
// that project directory is updated (and created when missing).
type RulesContextUpdate struct {
	ProjectDir     string
	RulesFile      *string
	ContextFiles   *[]string
	DigestMode     *string
	DigestMaxBytes *int
}

// Empty reports whether no field is set.
func (u RulesContextUpdate) Empty() bool {
	return u.RulesFile == nil && u.ContextFiles == nil && u.DigestMode == nil && u.DigestMaxBytes == nil
}

// SetRulesContextField sets one field by its key (rules_file, context_files,
// digest_mode, digest_max_bytes; hyphens are accepted). context_files takes
// a comma-separated list.
func (u *RulesContextUpdate) SetRulesContextField(key, value string) error {
	value = strings.TrimSpace(value)
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "-", "_") {
	case "rules_file":
		u.RulesFile = &value
	case "context_files":
		files := []string{}
		for _, f := range strings.Split(value, ",") {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		u.ContextFiles = &files
	case "digest_mode":
		m := NormalizeRulesContextMode(value)
		u.DigestMode = &m
	case "digest_max_bytes":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("digest_max_bytes: %q is not a whole number", value)
		}
		u.DigestMaxBytes = &n
	default:
		return fmt.Errorf("unknown setting %q (use rules_file, context_files, digest_mode or digest_max_bytes)", key)
	}
	return nil
}

// ApplyRulesContextUpdate returns cur with u applied, validated whole. cur is
// not modified.
func ApplyRulesContextUpdate(cur AutonomousRulesContextConfig, u RulesContextUpdate) (AutonomousRulesContextConfig, error) {
	if u.Empty() {
		return cur, fmt.Errorf("nothing to change: give rules_file, context_files, digest_mode or digest_max_bytes")
	}
	next := cur
	next.ContextFiles = append([]string(nil), cur.ContextFiles...)
	next.Projects = append([]AutonomousRulesContextProject(nil), cur.Projects...)
	dir := strings.TrimSpace(u.ProjectDir)
	if dir == "" {
		if u.RulesFile != nil {
			next.RulesFile = strings.TrimSpace(*u.RulesFile)
		}
		if u.ContextFiles != nil {
			next.ContextFiles = append([]string{}, *u.ContextFiles...)
		}
		if u.DigestMode != nil {
			next.DigestMode = NormalizeRulesContextMode(*u.DigestMode)
		}
		if u.DigestMaxBytes != nil {
			next.DigestMaxBytes = *u.DigestMaxBytes
		}
	} else {
		idx := -1
		for i, p := range next.Projects {
			if p.ProjectDir == dir {
				idx = i
				break
			}
		}
		if idx < 0 {
			next.Projects = append(next.Projects, AutonomousRulesContextProject{ProjectDir: dir})
			idx = len(next.Projects) - 1
		}
		p := &next.Projects[idx]
		if u.RulesFile != nil {
			p.RulesFile = strings.TrimSpace(*u.RulesFile)
		}
		if u.ContextFiles != nil {
			p.ContextFiles = append([]string{}, *u.ContextFiles...)
		}
		if u.DigestMode != nil {
			p.DigestMode = NormalizeRulesContextMode(*u.DigestMode)
		}
		if u.DigestMaxBytes != nil {
			p.DigestMaxBytes = *u.DigestMaxBytes
		}
	}
	if err := next.Validate(); err != nil {
		return cur, err
	}
	return next, nil
}
