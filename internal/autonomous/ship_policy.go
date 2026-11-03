package autonomous

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
)

// ShipPolicy is the resolved ship settings for one project. Push, Tag and
// CIWatch are modes: "off" skips the step, "shadow" plans and records the
// exact command without running it, "on" runs it.
type ShipPolicy struct {
	Push        string
	Tag         string
	CIWatch     string
	TagFormat   string
	Remote      string
	CIWorkflows []string
}

var (
	shipMu  sync.RWMutex
	shipCfg config.AutonomousShipConfig
)

// SetShipConfig installs the global autonomous.ship config that ShipPolicyFor
// reads. Call it at startup and on config reload.
func SetShipConfig(c config.AutonomousShipConfig) {
	shipMu.Lock()
	shipCfg = c
	shipMu.Unlock()
}

// ShipPolicyFor returns the resolved policy for a project directory from the
// installed config. An unset or invalid config yields the defaults.
func ShipPolicyFor(projectDir string) ShipPolicy {
	shipMu.RLock()
	c := shipCfg
	shipMu.RUnlock()
	p, err := ResolveShipPolicy(c, projectDir)
	if err != nil {
		p, _ = ResolveShipPolicy(config.AutonomousShipConfig{}, projectDir)
	}
	return p
}

// ResolveShipPolicy resolves the policy for projectDir from c: a per-project
// override wins over the global value, which wins over the default. Invalid
// modes or tag formats return an error.
func ResolveShipPolicy(c config.AutonomousShipConfig, projectDir string) (ShipPolicy, error) {
	if err := c.Validate(); err != nil {
		return ShipPolicy{}, err
	}
	o := lookupShipOverride(c.Projects, projectDir)
	mode := func(over, global string) string {
		if m := config.NormalizeChecksMode(over); m != "" {
			return m
		}
		if m := config.NormalizeChecksMode(global); m != "" {
			return m
		}
		return config.ChecksModeOn
	}
	str := func(over, global, def string) string {
		if v := strings.TrimSpace(over); v != "" {
			return v
		}
		if v := strings.TrimSpace(global); v != "" {
			return v
		}
		return def
	}
	wf := c.CIWorkflows
	if o.CIWorkflows != nil {
		wf = o.CIWorkflows
	}
	return ShipPolicy{
		Push:        mode(o.Push, c.Push),
		Tag:         mode(o.Tag, c.Tag),
		CIWatch:     mode(o.CIWatch, c.CIWatch),
		TagFormat:   str(o.TagFormat, c.TagFormat, config.ShipDefaultTagFormat),
		Remote:      str(o.Remote, c.Remote, config.ShipDefaultRemote),
		CIWorkflows: append([]string{}, wf...),
	}, nil
}

func lookupShipOverride(m map[string]config.AutonomousShipOverride, dir string) config.AutonomousShipOverride {
	if len(m) == 0 || strings.TrimSpace(dir) == "" {
		return config.AutonomousShipOverride{}
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
	return config.AutonomousShipOverride{}
}
