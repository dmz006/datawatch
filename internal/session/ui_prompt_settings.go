package session

import "sync"

// UIPromptSettings holds the live UI prompt handling knobs the daemon config
// exposes as session.dismiss_ui_prompts, session.ui_prompt_dismiss_delay_seconds,
// session.ui_prompt_clear_bound_seconds and session.ui_prompt_max_attempts.
type UIPromptSettings struct {
	// Enabled is session.dismiss_ui_prompts (default true when unset).
	Enabled bool
	// DelaySeconds is session.ui_prompt_dismiss_delay_seconds: whole seconds
	// to wait before sending the dismissal (default 5 when unset; 0 =
	// send at once).
	DelaySeconds int
	// ClearBoundSeconds is session.ui_prompt_clear_bound_seconds: whole
	// seconds >= 1 (default 30 when unset).
	ClearBoundSeconds int
	// MaxAttempts is session.ui_prompt_max_attempts: attempts cap, whole
	// number >= 1 (default 2 when unset).
	MaxAttempts int
}

// Defaults for the UI prompt settings: what each key reads when it is
// unset in the daemon config. These repeat
// internal/config.DefaultUIPrompt* (the config package cannot import
// internal/session); a parity test holds the two copies equal.
const (
	UIPromptDefaultEnabled           = true
	UIPromptDefaultDelaySeconds      = 5
	UIPromptDefaultClearBoundSeconds = 30
	UIPromptDefaultMaxAttempts       = 2
)

// DefaultUIPromptSettings returns the UI prompt settings for an unset
// config: dismissal enabled, 5 s delay, 30 s clear bound, 2 attempts.
func DefaultUIPromptSettings() UIPromptSettings {
	return UIPromptSettings{
		Enabled:           UIPromptDefaultEnabled,
		DelaySeconds:      UIPromptDefaultDelaySeconds,
		ClearBoundSeconds: UIPromptDefaultClearBoundSeconds,
		MaxAttempts:       UIPromptDefaultMaxAttempts,
	}
}

// uiPromptRoute keeps the live settings getter behind its own lock, so a
// setter called after start never races a read.
type uiPromptRoute struct {
	mu  sync.RWMutex
	get func() UIPromptSettings
}

// SetUIPromptSettings installs the getter the manager consults for the live
// UI prompt settings. Pass nil to go back to the defaults. The getter is
// read on every call — it is expected to re-read the daemon config — so a
// config change takes effect with no restart.
func (m *Manager) SetUIPromptSettings(fn func() UIPromptSettings) {
	m.uiPrompt.mu.Lock()
	m.uiPrompt.get = fn
	m.uiPrompt.mu.Unlock()
}

// UIPromptSettings returns the live UI prompt settings. Without a getter —
// or when the daemon has not wired one — the defaults apply.
func (m *Manager) UIPromptSettings() UIPromptSettings {
	m.uiPrompt.mu.RLock()
	get := m.uiPrompt.get
	m.uiPrompt.mu.RUnlock()
	if get == nil {
		return DefaultUIPromptSettings()
	}
	return get()
}
