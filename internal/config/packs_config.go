package config

import (
	"fmt"
	"strings"
)

// PacksBuiltinName is the name of the standard-task pack that ships inside the
// binary. It is always a valid entry in packs.active and needs no install.
const PacksBuiltinName = "builtin"

// PacksConfig is the packs config section (packs.*): the switch for the
// standard-task pack feature and the ordered list of packs that are active.
//
// Enabled is false by default, so a daemon that never sets packs.* behaves as
// it did before the feature existed. Active defaults to ["builtin"].
type PacksConfig struct {
	// Enabled turns the standard-task pack feature on (packs.enabled).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Active lists the packs in use (packs.active): "builtin" or the name of an
	// installed pack.
	Active []string `yaml:"active" json:"active"`
}

// DefaultPacksConfig returns the shipped defaults: feature off, only the
// builtin pack active.
func DefaultPacksConfig() PacksConfig {
	return PacksConfig{
		Enabled: false,
		Active:  []string{PacksBuiltinName},
	}
}

// Validate checks that every name in Active is "builtin" or appears in
// installed, the names of the packs installed on this daemon. Names are
// compared exactly. An empty or blank name, and a repeated name, are errors.
func (c PacksConfig) Validate(installed []string) error {
	have := make(map[string]struct{}, len(installed))
	for _, n := range installed {
		have[n] = struct{}{}
	}
	seen := make(map[string]struct{}, len(c.Active))
	for i, name := range c.Active {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("packs.active[%d]: pack name must not be empty", i)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("packs.active: %q is listed more than once", name)
		}
		seen[name] = struct{}{}
		if name == PacksBuiltinName {
			continue
		}
		if _, ok := have[name]; !ok {
			return fmt.Errorf("packs.active: %q is neither %q nor an installed pack", name, PacksBuiltinName)
		}
	}
	return nil
}
