// The session manager's live UI prompt settings: defaults when no getter is
// wired, the getter read on every call (so a config change takes effect with
// no restart), nil getter back to defaults, and the default parity with
// internal/config.

package session

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestUIPromptSettings_ManagerDefaults(t *testing.T) {
	m := &Manager{}
	got := m.UIPromptSettings()
	want := DefaultUIPromptSettings()
	if got != want {
		t.Fatalf("no getter: got %+v, want %+v", got, want)
	}
	if !got.Enabled || got.DelaySeconds != 5 || got.ClearBoundSeconds != 30 || got.MaxAttempts != 2 {
		t.Errorf("defaults = %+v, want {true 5 30 2}", got)
	}
}

func TestUIPromptSettings_ManagerLiveRead(t *testing.T) {
	m := &Manager{}
	sc := &config.SessionConfig{}
	m.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{
			Enabled:           sc.EffectiveDismissUIPrompts(),
			DelaySeconds:      sc.EffectiveUIPromptDismissDelaySeconds(),
			ClearBoundSeconds: sc.EffectiveUIPromptClearBoundSeconds(),
			MaxAttempts:       sc.EffectiveUIPromptMaxAttempts(),
		}
	})
	if got, want := m.UIPromptSettings(), DefaultUIPromptSettings(); got != want {
		t.Fatalf("unset config: got %+v, want %+v", got, want)
	}
	// A change to the config the getter reads is visible on the next call —
	// the daemon needs no restart.
	b, d0, b45, a3 := false, 0, 45, 3
	sc.DismissUIPrompts = &b
	sc.UIPromptDismissDelaySeconds = &d0
	sc.UIPromptClearBoundSeconds = &b45
	sc.UIPromptMaxAttempts = &a3
	want := UIPromptSettings{Enabled: false, DelaySeconds: 0, ClearBoundSeconds: 45, MaxAttempts: 3}
	if got := m.UIPromptSettings(); got != want {
		t.Fatalf("after config change: got %+v, want %+v", got, want)
	}
	// A nil getter goes back to the defaults.
	m.SetUIPromptSettings(nil)
	if got := m.UIPromptSettings(); got != DefaultUIPromptSettings() {
		t.Fatalf("after nil getter: got %+v, want %+v", got, DefaultUIPromptSettings())
	}
}

func TestUIPromptSettings_DefaultsMatchConfigPackage(t *testing.T) {
	if config.DefaultUIPromptDismissDelaySeconds != UIPromptDefaultDelaySeconds {
		t.Errorf("delay default: config %d, session %d",
			config.DefaultUIPromptDismissDelaySeconds, UIPromptDefaultDelaySeconds)
	}
	if config.DefaultUIPromptClearBoundSeconds != UIPromptDefaultClearBoundSeconds {
		t.Errorf("bound default: config %d, session %d",
			config.DefaultUIPromptClearBoundSeconds, UIPromptDefaultClearBoundSeconds)
	}
	if config.DefaultUIPromptMaxAttempts != UIPromptDefaultMaxAttempts {
		t.Errorf("attempts default: config %d, session %d",
			config.DefaultUIPromptMaxAttempts, UIPromptDefaultMaxAttempts)
	}
	cfg := config.SessionConfig{}
	if !cfg.EffectiveDismissUIPrompts() || !UIPromptDefaultEnabled {
		t.Errorf("dismiss default must be on in both packages")
	}
}
