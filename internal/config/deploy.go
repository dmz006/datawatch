// Deploy config: the autonomous.deploy block.
//
// The deploy block says how a shipped build is turned into a running daemon:
// build the new binary, install it over the old one, restart, and check that
// it is healthy. Every command is a shell command line and an empty one is
// skipped, so the zero value is valid and does nothing.
package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/tmuxscope"
)

const (
	// DeployDefaultDrainTimeout is how long to wait for running final-gate
	// tasks before the deploy is deferred.
	DeployDefaultDrainTimeout = 10 * time.Minute
	// DeployDefaultHealthTimeout is how long the health check may take.
	DeployDefaultHealthTimeout = 2 * time.Minute
)

// DeployConfig is the autonomous.deploy block.
type DeployConfig struct {
	// BuildCommand is a shell command line that builds the new binary.
	// Empty means skip.
	BuildCommand string `yaml:"build_command,omitempty" json:"build_command,omitempty"`
	// InstallCommand is a shell command line that installs the new binary.
	// Empty means skip. It requires BinaryPath.
	InstallCommand string `yaml:"install_command,omitempty" json:"install_command,omitempty"`
	// BinaryPath is the absolute path of the installed binary that
	// InstallCommand replaces; the previous binary is copied from here
	// before it is replaced.
	BinaryPath string `yaml:"binary_path,omitempty" json:"binary_path,omitempty"`
	// RestartCommand is a shell command line that restarts the daemon.
	// Empty means skip.
	RestartCommand string `yaml:"restart_command,omitempty" json:"restart_command,omitempty"`
	// HealthCheck is a shell command line whose exit code 0 means healthy.
	// Empty means no health check.
	HealthCheck string `yaml:"health_check,omitempty" json:"health_check,omitempty"`
	// DrainTimeout is a duration string: how long to wait for running
	// final-gate tasks before deferring. Unset reads 10 minutes.
	DrainTimeout string `yaml:"drain_timeout,omitempty" json:"drain_timeout,omitempty"`
	// HealthTimeout is a duration string. Unset reads 2 minutes.
	HealthTimeout string `yaml:"health_timeout,omitempty" json:"health_timeout,omitempty"`
}

// IsZero reports whether no key is set, so yaml.v3 can drop an unset block.
func (c DeployConfig) IsZero() bool {
	return c == DeployConfig{}
}

// Clone returns a deep copy. The block holds only strings today, so a value
// copy is deep; it is kept as a method so a later map or slice is copied here.
func (c DeployConfig) Clone() DeployConfig {
	out := c
	return out
}

func parseDeployDuration(v string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return d, nil
}

func (c DeployConfig) effectiveDuration(v string, def time.Duration) time.Duration {
	if strings.TrimSpace(v) == "" {
		return def
	}
	d, err := parseDeployDuration(v)
	if err != nil || d == 0 {
		return def
	}
	return d
}

// EffectiveDrainTimeout returns the drain timeout, or the default when unset.
func (c DeployConfig) EffectiveDrainTimeout() time.Duration {
	return c.effectiveDuration(c.DrainTimeout, DeployDefaultDrainTimeout)
}

// EffectiveHealthTimeout returns the health timeout, or the default when unset.
func (c DeployConfig) EffectiveHealthTimeout() time.Duration {
	return c.effectiveDuration(c.HealthTimeout, DeployDefaultHealthTimeout)
}

// Validate rejects an invalid value, naming the key. The zero value is valid.
func (c DeployConfig) Validate() error {
	const p = "autonomous.deploy."
	for _, d := range []struct{ key, val string }{
		{"drain_timeout", c.DrainTimeout},
		{"health_timeout", c.HealthTimeout},
	} {
		if strings.TrimSpace(d.val) == "" {
			continue
		}
		if _, err := parseDeployDuration(d.val); err != nil {
			return fmt.Errorf("%s%s: %q is not a valid duration: %v", p, d.key, d.val, err)
		}
	}
	if c.BinaryPath != "" {
		if !filepath.IsAbs(c.BinaryPath) {
			return fmt.Errorf("%sbinary_path: %q must be an absolute path", p, c.BinaryPath)
		}
		if filepath.Clean(c.BinaryPath) != c.BinaryPath {
			return fmt.Errorf("%sbinary_path: %q must be clean (no .., //, or trailing slash)", p, c.BinaryPath)
		}
	}
	if strings.TrimSpace(c.InstallCommand) != "" && c.BinaryPath == "" {
		return fmt.Errorf("%sinstall_command: requires binary_path so the previous binary can be kept", p)
	}
	for _, cmd := range []struct{ key, val string }{
		{"build_command", c.BuildCommand},
		{"install_command", c.InstallCommand},
		{"restart_command", c.RestartCommand},
		{"health_check", c.HealthCheck},
	} {
		if tmuxscope.CallsUnscopedTmuxKill(cmd.val) {
			return fmt.Errorf("%s%s: refused, it kills tmux without a throwaway socket or a scrubbed environment", p, cmd.key)
		}
	}
	return nil
}
