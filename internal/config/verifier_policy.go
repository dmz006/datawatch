package config

import (
	"fmt"
	"strings"
)

// Verifier policy modes (AutonomousConfig.VerifierPolicy.Mode).
const (
	// VerifierPolicyFixed uses the first eligible backend only. This is the
	// default and matches the behaviour before the policy existed.
	VerifierPolicyFixed = "fixed"
	// VerifierPolicyPreferFree takes the first backend with a free slot and
	// falls back down the list.
	VerifierPolicyPreferFree = "prefer-free"
	// VerifierPolicyBalance takes the backend with the lowest relative load.
	VerifierPolicyBalance = "balance"
	// VerifierPolicyGPUOnly keeps only local/GPU backends in the list.
	VerifierPolicyGPUOnly = "gpu-only"
	// VerifierPolicyClaudeOnly keeps only Claude backends in the list.
	VerifierPolicyClaudeOnly = "claude-only"
)

// ValidVerifierPolicyModes lists the accepted mode values, for error
// messages and UI validation.
var ValidVerifierPolicyModes = []string{
	VerifierPolicyFixed, VerifierPolicyPreferFree, VerifierPolicyBalance,
	VerifierPolicyGPUOnly, VerifierPolicyClaudeOnly,
}

// VerifierPolicyStrongerBackendDefault is the stronger verifier backend used
// when autonomous.verifier_policy.stronger_backend is empty.
const VerifierPolicyStrongerBackendDefault = "claude-code"

// VerifierPolicyStrongerModelDefault is the stronger verifier model used when
// autonomous.verifier_policy.stronger_model is empty. It is one of the names
// the Claude models list offers.
const VerifierPolicyStrongerModelDefault = "claude-sonnet-5-5"

// VerifierPolicyConfig chooses which verifier backend runs a verification.
// The zero value is valid and keeps the pre-policy behaviour (mode fixed over
// verification_backends / verification_backend).
//
// No omitempty on any field, deliberately: handleAutonomousConfig round-trips
// the live config through JSON onto this struct before persisting, and an
// elided zero would leave the previous stored value in place — the same
// gotcha as AutonomousConfig.VerificationBackends.
type VerifierPolicyConfig struct {
	// Mode is one of ValidVerifierPolicyModes. Empty means fixed.
	Mode string `yaml:"mode" json:"mode"`
	// Backends is the ordered list of eligible verifier backends. Empty falls
	// back to autonomous.verification_backends, then verification_backend.
	Backends []string `yaml:"backends" json:"backends"`
	// ClaudeSessionCap caps the Claude verifier sessions started in one
	// window (ClaudeCapWindowMinutes). 0 = unlimited.
	ClaudeSessionCap int `yaml:"claude_session_cap" json:"claude_session_cap"`
	// ClaudeCapWindowMinutes is the window length for ClaudeSessionCap.
	// 0 means the default (VerifierPolicyClaudeCapWindowDefault) and matters
	// only when a cap is set.
	ClaudeCapWindowMinutes int `yaml:"claude_cap_window_minutes" json:"claude_cap_window_minutes"`
	// ForceStrongerOnRetry sends a retried verification to the stronger
	// verifier.
	ForceStrongerOnRetry bool `yaml:"force_stronger_on_retry" json:"force_stronger_on_retry"`
	// FinalGateStronger sends the final verification gate to the stronger
	// verifier.
	FinalGateStronger bool `yaml:"final_gate_stronger" json:"final_gate_stronger"`
	// StrongerBackend and StrongerModel name the stronger verifier. Empty
	// means claude-code and the default Sonnet model.
	StrongerBackend string `yaml:"stronger_backend" json:"stronger_backend"`
	StrongerModel   string `yaml:"stronger_model" json:"stronger_model"`
}

// VerifierPolicyClaudeCapWindowDefault is the Claude session cap window, in
// minutes, used when a cap is set and the window is not.
const VerifierPolicyClaudeCapWindowDefault = 60

// NormalizeVerifierPolicyMode resolves a raw mode to its canonical form.
// Empty normalizes to "fixed". Comparison is case-insensitive. Any other
// value is an error; a typo must never quietly mean "fixed".
func NormalizeVerifierPolicyMode(s string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(s))
	if m == "" {
		return VerifierPolicyFixed, nil
	}
	for _, v := range ValidVerifierPolicyModes {
		if m == v {
			return m, nil
		}
	}
	return "", fmt.Errorf("invalid verifier_policy.mode %q: must be one of %s (or empty for %q)",
		s, strings.Join(ValidVerifierPolicyModes, ", "), VerifierPolicyFixed)
}

// NormalizeVerifierPolicyBackends trims each entry and drops empty and
// duplicate entries, keeping the first occurrence so list order is preserved.
func NormalizeVerifierPolicyBackends(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, b := range in {
		b = strings.TrimSpace(b)
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	return out
}

// NormalizeVerifierPolicyStrongerBackend returns the effective stronger
// verifier backend: the trimmed configured value, or the default when empty.
func NormalizeVerifierPolicyStrongerBackend(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return VerifierPolicyStrongerBackendDefault
	}
	return v
}

// NormalizeVerifierPolicyStrongerModel returns the effective stronger
// verifier model: the trimmed configured value, or the default when empty.
func NormalizeVerifierPolicyStrongerModel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return VerifierPolicyStrongerModelDefault
	}
	return v
}

// NormalizeVerifierPolicyClaudeCapWindow returns the effective cap window in
// minutes: the configured value when positive, otherwise the default.
func NormalizeVerifierPolicyClaudeCapWindow(minutes int) int {
	if minutes <= 0 {
		return VerifierPolicyClaudeCapWindowDefault
	}
	return minutes
}

// Normalized returns a copy with every field resolved to its effective value.
// It returns an error when the mode is not valid or a numeric field is
// negative.
func (p VerifierPolicyConfig) Normalized() (VerifierPolicyConfig, error) {
	if err := p.Validate(); err != nil {
		return VerifierPolicyConfig{}, err
	}
	mode, _ := NormalizeVerifierPolicyMode(p.Mode)
	p.Mode = mode
	p.Backends = NormalizeVerifierPolicyBackends(p.Backends)
	p.ClaudeCapWindowMinutes = NormalizeVerifierPolicyClaudeCapWindow(p.ClaudeCapWindowMinutes)
	p.StrongerBackend = NormalizeVerifierPolicyStrongerBackend(p.StrongerBackend)
	p.StrongerModel = NormalizeVerifierPolicyStrongerModel(p.StrongerModel)
	return p, nil
}

// Validate rejects an unknown mode and negative numeric fields.
func (p VerifierPolicyConfig) Validate() error {
	if _, err := NormalizeVerifierPolicyMode(p.Mode); err != nil {
		return err
	}
	if p.ClaudeSessionCap < 0 {
		return fmt.Errorf("verifier_policy.claude_session_cap: %d must be >= 0", p.ClaudeSessionCap)
	}
	if p.ClaudeCapWindowMinutes < 0 {
		return fmt.Errorf("verifier_policy.claude_cap_window_minutes: %d must be >= 0", p.ClaudeCapWindowMinutes)
	}
	return nil
}

// IsClaudeVerifierBackend reports whether a verifier backend name is a Claude
// backend (claude-code, or a name starting with "claude").
func IsClaudeVerifierBackend(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "claude")
}

// EffectiveBackends returns the eligible verifier backends in list order:
// the policy's own list, else the pool, else the single configured backend.
// The gpu-only and claude-only presets then filter the list to local (non
// Claude) or Claude backends.
func (p VerifierPolicyConfig) EffectiveBackends(pool []string, single string) []string {
	list := NormalizeVerifierPolicyBackends(p.Backends)
	if len(list) == 0 {
		list = NormalizeVerifierPolicyBackends(pool)
	}
	if len(list) == 0 {
		list = NormalizeVerifierPolicyBackends([]string{single})
	}
	mode, _ := NormalizeVerifierPolicyMode(p.Mode)
	if mode != VerifierPolicyGPUOnly && mode != VerifierPolicyClaudeOnly {
		return list
	}
	out := make([]string, 0, len(list))
	for _, b := range list {
		if IsClaudeVerifierBackend(b) == (mode == VerifierPolicyClaudeOnly) {
			out = append(out, b)
		}
	}
	return out
}
