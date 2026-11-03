package autonomous

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
)

// ChecksPolicy is the resolved mode of each automatic check for one project.
// Modes are "off", "shadow" or "on".
type ChecksPolicy struct {
	PreStartVerifier string
	BlockWhenMissing bool
	PlanRules        string
	AfterDone        string
	RegistryReadme   string
}

// Blocks reports whether the pre-start verifier may block a task start
// (mode on only; shadow records findings without blocking).
func (p ChecksPolicy) Blocks() bool { return p.PreStartVerifier == config.ChecksModeOn }

var (
	checksMu  sync.RWMutex
	checksCfg config.AutonomousChecksConfig
)

// SetChecksConfig installs the global autonomous.checks config that
// ChecksPolicyFor reads. Call it at startup and on config reload.
func SetChecksConfig(c config.AutonomousChecksConfig) {
	checksMu.Lock()
	checksCfg = c
	checksMu.Unlock()
}

// ChecksPolicyFor returns the resolved policy for a project directory from
// the installed config. An unset config yields the defaults.
func ChecksPolicyFor(projectDir string) ChecksPolicy {
	checksMu.RLock()
	c := checksCfg
	checksMu.RUnlock()
	p, err := ResolveChecksPolicy(c, projectDir)
	if err != nil {
		// Invalid values are rejected at load/set time; fall back to defaults
		// rather than guessing a mode.
		p, _ = ResolveChecksPolicy(config.AutonomousChecksConfig{}, projectDir)
	}
	return p
}

// ResolveChecksPolicy resolves the policy for projectDir from c: a per-project
// override wins over the global value, which wins over the default (on, with
// block_when_missing true). Unknown mode values return an error.
func ResolveChecksPolicy(c config.AutonomousChecksConfig, projectDir string) (ChecksPolicy, error) {
	if err := c.Validate(); err != nil {
		return ChecksPolicy{}, err
	}
	o := lookupChecksOverride(c.Projects, projectDir)
	pick := func(over, global string) string {
		if m := config.NormalizeChecksMode(over); m != "" {
			return m
		}
		if m := config.NormalizeChecksMode(global); m != "" {
			return m
		}
		return config.ChecksModeOn
	}
	block := true
	if c.BlockWhenMissing != nil {
		block = *c.BlockWhenMissing
	}
	if o.BlockWhenMissing != nil {
		block = *o.BlockWhenMissing
	}
	return ChecksPolicy{
		PreStartVerifier: pick(o.PreStartVerifier, c.PreStartVerifier),
		BlockWhenMissing: block,
		PlanRules:        pick(o.PlanRules, c.PlanRules),
		AfterDone:        pick(o.AfterDone, c.AfterDone),
		RegistryReadme:   pick(o.RegistryReadme, c.RegistryReadme),
	}, nil
}

func lookupChecksOverride(m map[string]config.AutonomousChecksOverride, dir string) config.AutonomousChecksOverride {
	if len(m) == 0 || strings.TrimSpace(dir) == "" {
		return config.AutonomousChecksOverride{}
	}
	if o, ok := m[dir]; ok {
		return o
	}
	clean := filepath.Clean(dir)
	for k, o := range m {
		if filepath.Clean(k) == clean {
			return o
		}
	}
	return config.AutonomousChecksOverride{}
}
