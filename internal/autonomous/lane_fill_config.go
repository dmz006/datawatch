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
	// StaleAfterHours: hours an Automaton may sit queued before lane fill
	// treats it as stale. Nil means 72; an explicit 0 turns the rule off.
	StaleAfterHours *int `json:"stale_after_hours,omitempty"`
	// RequireConfirmedProposal, CheckMainCarries, RequireWorktree and
	// LaneCountsFromCaps are eligibility and counting rules; nil means true.
	RequireConfirmedProposal *bool `json:"require_confirmed_proposal,omitempty"`
	CheckMainCarries         *bool `json:"check_main_carries,omitempty"`
	RequireWorktree          *bool `json:"require_worktree,omitempty"`
	LaneCountsFromCaps       *bool `json:"lane_counts_from_caps,omitempty"`
	// GPUMode and ClaudeMode set the mode for one lane kind; empty inherits
	// Mode, otherwise "off", "shadow" or "on".
	GPUMode    string `json:"gpu_mode"`
	ClaudeMode string `json:"claude_mode"`
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

		StaleAfterHours:          c.StaleAfterHours,
		RequireConfirmedProposal: c.RequireConfirmedProposal,
		CheckMainCarries:         c.CheckMainCarries,
		RequireWorktree:          c.RequireWorktree,
		LaneCountsFromCaps:       c.LaneCountsFromCaps,
		GPUMode:                  c.GPUMode,
		ClaudeMode:               c.ClaudeMode,
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

		StaleAfterHours:          c.StaleAfterHours,
		RequireConfirmedProposal: c.RequireConfirmedProposal,
		CheckMainCarries:         c.CheckMainCarries,
		RequireWorktree:          c.RequireWorktree,
		LaneCountsFromCaps:       c.LaneCountsFromCaps,
		GPUMode:                  c.GPUMode,
		ClaudeMode:               c.ClaudeMode,
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

// EffectiveModeFor returns the mode in effect for one lane kind: the per-kind
// value when set, otherwise the shared mode.
func (c LaneFillConfig) EffectiveModeFor(t LaneType) string {
	return c.yaml().EffectiveModeFor(string(t))
}

// EffectiveStaleAfterHours returns the stale threshold in effect (nil = 72,
// explicit 0 = rule off).
func (c LaneFillConfig) EffectiveStaleAfterHours() int { return c.yaml().EffectiveStaleAfterHours() }

// EffectiveRequireConfirmedProposal returns the confirmed-proposal rule in
// effect (unset = true).
func (c LaneFillConfig) EffectiveRequireConfirmedProposal() bool {
	return c.yaml().EffectiveRequireConfirmedProposal()
}

// EffectiveCheckMainCarries returns the main-carries check in effect (unset =
// true).
func (c LaneFillConfig) EffectiveCheckMainCarries() bool { return c.yaml().EffectiveCheckMainCarries() }

// EffectiveRequireWorktree returns the worktree rule in effect (unset = true).
func (c LaneFillConfig) EffectiveRequireWorktree() bool { return c.yaml().EffectiveRequireWorktree() }

// EffectiveLaneCountsFromCaps returns the lane-counts-from-caps rule in
// effect (unset = true).
func (c LaneFillConfig) EffectiveLaneCountsFromCaps() bool {
	return c.yaml().EffectiveLaneCountsFromCaps()
}
