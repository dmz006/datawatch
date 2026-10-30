package autonomous

import "github.com/dmz006/datawatch/internal/config"

// LaneFillConfig carries the lane fill knobs (autonomous.lane_fill.*). The
// json names match the config file keys.
type LaneFillConfig struct {
	// Mode: "off", "shadow" or "on"; empty means shadow. Shadow computes and
	// records every decision and starts nothing.
	Mode string `json:"mode"`
	// GPULanes is the number of GPU lanes. Default 2.
	GPULanes int `json:"gpu_lanes"`
	// ClaudeLanes is the number of Claude lanes. Default 3; 0 means none.
	ClaudeLanes int `json:"claude_lanes"`
	// IdleAlertMinutes: alert once when a lane sits idle with eligible work
	// queued this long. Default 30; 0 = off.
	IdleAlertMinutes int `json:"idle_alert_minutes"`
	// DecomposeAheadMin is the minimum number of decomposed Automata kept
	// ready per lane. Default 2.
	DecomposeAheadMin int `json:"decompose_ahead_min"`
	// PollSeconds is the planner poll interval. Default 60, minimum 5.
	PollSeconds int `json:"poll_seconds"`
	// GPUBackend and GPUModel choose what the GPU lanes run. Defaults
	// pi-prompt and the backend's own model.
	GPUBackend string `json:"gpu_backend"`
	GPUModel   string `json:"gpu_model"`
	// ClaudeBackend chooses what the Claude lanes run. Default claude-code.
	ClaudeBackend string `json:"claude_backend"`
	// CountVerifierWait: when true (the default, unset is nil) a Claude lane
	// whose Automata wait on a verifier slot counts as available; when false
	// the lane stays busy. A pointer with omitempty, so an explicit false
	// survives a round trip while an absent key keeps the default.
	CountVerifierWait *bool `json:"count_verifier_wait,omitempty"`
}

// DefaultLaneFillConfig returns the shipped lane fill settings (shadow mode).
func DefaultLaneFillConfig() LaneFillConfig {
	return LaneFillConfigFromYAML(config.DefaultAutonomousLaneFill())
}

// LaneFillConfigFromYAML bridges the config file block to the manager config.
func LaneFillConfigFromYAML(c config.AutonomousLaneFillConfig) LaneFillConfig {
	return LaneFillConfig{
		Mode:              c.Mode,
		GPULanes:          c.GPULanes,
		ClaudeLanes:       c.ClaudeLanes,
		IdleAlertMinutes:  c.IdleAlertMinutes,
		DecomposeAheadMin: c.DecomposeAheadMin,
		PollSeconds:       c.PollSeconds,
		GPUBackend:        c.GPUBackend,
		GPUModel:          c.GPUModel,
		ClaudeBackend:     c.ClaudeBackend,
		CountVerifierWait: c.CountVerifierWait,
	}
}

// yaml returns the same settings as the config file type, so the Effective
// helpers are defined once, in the config package.
func (c LaneFillConfig) yaml() config.AutonomousLaneFillConfig {
	return config.AutonomousLaneFillConfig{
		Mode:              c.Mode,
		GPULanes:          c.GPULanes,
		ClaudeLanes:       c.ClaudeLanes,
		IdleAlertMinutes:  c.IdleAlertMinutes,
		DecomposeAheadMin: c.DecomposeAheadMin,
		PollSeconds:       c.PollSeconds,
		GPUBackend:        c.GPUBackend,
		GPUModel:          c.GPUModel,
		ClaudeBackend:     c.ClaudeBackend,
		CountVerifierWait: c.CountVerifierWait,
	}
}

// EffectiveMode returns the mode in effect: an empty value means shadow.
func (c LaneFillConfig) EffectiveMode() string { return c.yaml().EffectiveMode() }

// EffectiveGPULanes returns the GPU lane count in effect.
func (c LaneFillConfig) EffectiveGPULanes() int { return c.yaml().EffectiveGPULanes() }

// EffectiveClaudeLanes returns the Claude lane count in effect (0 = none).
func (c LaneFillConfig) EffectiveClaudeLanes() int { return c.yaml().EffectiveClaudeLanes() }

// EffectiveIdleAlertMinutes returns the idle alert threshold in effect.
func (c LaneFillConfig) EffectiveIdleAlertMinutes() int { return c.yaml().EffectiveIdleAlertMinutes() }

// EffectiveDecomposeAheadMin returns the decompose-ahead minimum in effect.
func (c LaneFillConfig) EffectiveDecomposeAheadMin() int {
	return c.yaml().EffectiveDecomposeAheadMin()
}

// EffectivePollSeconds returns the poll interval in effect (minimum 5).
func (c LaneFillConfig) EffectivePollSeconds() int { return c.yaml().EffectivePollSeconds() }

// EffectiveGPUBackend returns the GPU lane backend in effect.
func (c LaneFillConfig) EffectiveGPUBackend() string { return c.yaml().EffectiveGPUBackend() }

// EffectiveClaudeBackend returns the Claude lane backend in effect.
func (c LaneFillConfig) EffectiveClaudeBackend() string { return c.yaml().EffectiveClaudeBackend() }

// EffectiveCountVerifierWait returns the verifier-wait rule in effect: unset
// means true (a Claude lane whose Automata wait on a verifier slot counts as
// available; an explicit false keeps it busy).
func (c LaneFillConfig) EffectiveCountVerifierWait() bool {
	return c.yaml().EffectiveCountVerifierWait()
}
