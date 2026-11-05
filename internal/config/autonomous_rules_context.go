package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Digest modes shared by every autonomous.rules_context key.
const (
	RulesContextDigestModeOff     = "off"
	RulesContextDigestModeNoTools = "no_tools"
	RulesContextDigestModeAll     = "all"
)

const (
	// RulesContextDigestMaxBytesDefault — digest_max_bytes value applied
	// at resolution time when the key is left unset (0 = unset).
	RulesContextDigestMaxBytesDefault = 4096
	// RulesContextDigestMaxBytesMin/Max — hard bounds an explicit
	// digest_max_bytes must fall within.
	RulesContextDigestMaxBytesMin = 512
	RulesContextDigestMaxBytesMax = 65536
	// MaxRulesContextFiles — at most this many context files may be
	// listed per scope (global or one project override).
	MaxRulesContextFiles = 8
)

// AutonomousRulesContextConfig — autonomous.rules_context sub-config.
// RulesFile and ContextFiles name the project's rules doc and its
// "state of the project" doc(s), relative to the PRD's project_dir
// (never absolute, never escaping the root). DigestMode and
// DigestMaxBytes size the rules/context digest (default no_tools,
// 4096 bytes). Projects holds per-project overrides keyed by project
// directory; an empty list means every project follows the global
// values, which in turn fall back to the legacy autonomous.rules_file
// and autonomous.context_file keys and then the built-in defaults
// (AGENT.md / CONTEXT.md). See AutonomousRulesContextProject.
type AutonomousRulesContextConfig struct {
	RulesFile      string   `yaml:"rules_file,omitempty" json:"rules_file,omitempty"`
	ContextFiles   []string `yaml:"context_files,omitempty" json:"context_files,omitempty"`
	DigestMode     string   `yaml:"digest_mode,omitempty" json:"digest_mode,omitempty"`
	DigestMaxBytes int      `yaml:"digest_max_bytes,omitempty" json:"digest_max_bytes,omitempty"`

	Projects []AutonomousRulesContextProject `yaml:"projects,omitempty" json:"projects,omitempty"`
}

// AutonomousRulesContextProject is one project's override. Every setting
// except ProjectDir is optional: an empty value inherits the global value
// (or, failing that, the legacy key or the default).
type AutonomousRulesContextProject struct {
	ProjectDir     string   `yaml:"project_dir" json:"project_dir"`
	RulesFile      string   `yaml:"rules_file,omitempty" json:"rules_file,omitempty"`
	ContextFiles   []string `yaml:"context_files,omitempty" json:"context_files,omitempty"`
	DigestMode     string   `yaml:"digest_mode,omitempty" json:"digest_mode,omitempty"`
	DigestMaxBytes int      `yaml:"digest_max_bytes,omitempty" json:"digest_max_bytes,omitempty"`
}

// NormalizeRulesContextMode trims and lower-cases a digest mode; empty
// stays empty.
func NormalizeRulesContextMode(m string) string { return strings.ToLower(strings.TrimSpace(m)) }

// ValidateRulesContextMode accepts "", "off", "no_tools" and "all"
// (case-insensitive) for the named key and rejects everything else with a
// clear error.
func ValidateRulesContextMode(key, mode string) error {
	switch NormalizeRulesContextMode(mode) {
	case "", RulesContextDigestModeOff, RulesContextDigestModeNoTools, RulesContextDigestModeAll:
		return nil
	}
	return fmt.Errorf("%s: %q is not a valid digest mode (must be \"off\", \"no_tools\" or \"all\")", key, mode)
}

// ValidateRulesContextFilename accepts a non-empty file name that is
// relative to the project root: no leading '/', no parent-directory
// escape, no control characters.
func ValidateRulesContextFilename(key, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s: file name must not be empty", key)
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return fmt.Errorf("%s: %q must be relative to the project directory, not absolute", key, name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%s: %q must not contain \"..\" (parent-directory escapes are not allowed)", key, name)
	}
	if strings.ContainsAny(name, "\x00\n\r") {
		return fmt.Errorf("%s: %q is not a valid file name", key, name)
	}
	return nil
}

// ValidateRulesContextMaxBytes accepts 0 (unset; the default applies at
// resolution time) or a value within [512, 65536].
func ValidateRulesContextMaxBytes(key string, n int) error {
	if n == 0 {
		return nil
	}
	if n < RulesContextDigestMaxBytesMin || n > RulesContextDigestMaxBytesMax {
		return fmt.Errorf("%s: %d is out of range (must be between %d and %d)", key, n,
			RulesContextDigestMaxBytesMin, RulesContextDigestMaxBytesMax)
	}
	return nil
}

func validateRulesContextScope(key, rulesFile string, contextFiles []string, mode string, maxBytes int) error {
	if rulesFile != "" {
		if err := ValidateRulesContextFilename(key+".rules_file", rulesFile); err != nil {
			return err
		}
	}
	if len(contextFiles) > MaxRulesContextFiles {
		return fmt.Errorf("%s.context_files: at most %d context files may be listed, got %d", key, MaxRulesContextFiles, len(contextFiles))
	}
	for i, f := range contextFiles {
		if err := ValidateRulesContextFilename(fmt.Sprintf("%s.context_files[%d]", key, i), f); err != nil {
			return err
		}
	}
	if err := ValidateRulesContextMode(key+".digest_mode", mode); err != nil {
		return err
	}
	return ValidateRulesContextMaxBytes(key+".digest_max_bytes", maxBytes)
}

// Validate checks every value, global and per-project.
func (c AutonomousRulesContextConfig) Validate() error {
	if err := validateRulesContextScope("autonomous.rules_context", c.RulesFile, c.ContextFiles, c.DigestMode, c.DigestMaxBytes); err != nil {
		return err
	}
	for i, p := range c.Projects {
		key := fmt.Sprintf("autonomous.rules_context.projects[%d]", i)
		if strings.TrimSpace(p.ProjectDir) == "" {
			return fmt.Errorf("%s.project_dir: must not be empty", key)
		}
		if err := validateRulesContextScope(key, p.RulesFile, p.ContextFiles, p.DigestMode, p.DigestMaxBytes); err != nil {
			return err
		}
	}
	return nil
}
