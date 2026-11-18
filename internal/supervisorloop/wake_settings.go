package supervisorloop

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Wake setting keys. The wake path lets a lifecycle event start a supervisor
// pass at once instead of waiting for the next scheduled check; the schedule
// stays as the fallback.
const (
	// KeyWakeEnabled turns the event wake path on. Off by default, so the
	// supervisor keeps running on its schedule alone until an operator
	// switches it on.
	KeyWakeEnabled = "supervisor_loop.wake.enabled"
	// KeyWakeKinds lists the event kinds that wake the supervisor. Every kind
	// must be one a subscription may filter on.
	KeyWakeKinds = "supervisor_loop.wake.kinds"
	// KeyWakeCoalesceSeconds is the quiet window, in seconds, during which
	// further wake events fold into one pass. 0 wakes on every event.
	KeyWakeCoalesceSeconds = "supervisor_loop.wake.coalesce_seconds"
	// KeyWakeFallbackIntervalMinutes is how often, in minutes, the supervisor
	// still runs when no event wakes it.
	KeyWakeFallbackIntervalMinutes = "supervisor_loop.wake.fallback_interval_minutes"
)

// Wake bounds and defaults.
const (
	// DefaultWakeCoalesceSeconds is the default quiet window.
	DefaultWakeCoalesceSeconds = 30
	// MaxWakeCoalesceSeconds caps the quiet window at one hour.
	MaxWakeCoalesceSeconds = 3600
	// DefaultWakeFallbackIntervalMinutes matches the supervisor's scheduled
	// check, which runs every 20 minutes, so switching the wake path on
	// changes nothing about the schedule.
	DefaultWakeFallbackIntervalMinutes = 20
	// MinWakeFallbackIntervalMinutes is the shortest fallback interval.
	MinWakeFallbackIntervalMinutes = 1
	// MaxWakeFallbackIntervalMinutes caps the fallback interval at one day.
	MaxWakeFallbackIntervalMinutes = 1440
	// MaxWakeKinds caps the kinds list so a hand-edited config cannot grow
	// it without bound.
	MaxWakeKinds = 64
)

// DefaultWakeKinds are the event kinds that wake the supervisor by default:
// an Automaton blocked, a task needing review, a failed task, a failed final
// gate, every judge verdict, every task of an Automaton done or cancelled,
// an Automaton stuck running, a task waiting too long for capacity, and a
// burn guard rung change. The strings are the stored event kinds.
func DefaultWakeKinds() []string {
	return []string{
		"prd.blocked",
		"task.review_needed",
		"task.failed",
		"task.final_gate_failed",
		"judge.verdict_block",
		"judge.verdict_warn",
		"judge.verdict_pass",
		"automaton.tasks_done_or_cancelled",
		"automaton.stuck_running",
		"task.capacity_wait_too_long",
		"burn_guard.rung_changed",
	}
}

// EventKindChecker reports whether an event kind is one a subscription may
// filter on. The autonomous package installs its registry check at start-up
// (SetEventKindChecker); this package cannot import it. With no checker
// installed only an empty kind is refused.
var eventKindChecker func(string) bool

// SetEventKindChecker installs the check Validate uses for every wake kind.
// nil removes it.
func SetEventKindChecker(fn func(string) bool) { eventKindChecker = fn }

// EventKindChecker returns the installed check, or nil when none is set.
func EventKindChecker() func(string) bool { return eventKindChecker }

// WakeSettings is the supervisor_loop.wake block.
type WakeSettings struct {
	Enabled                 bool
	Kinds                   []string
	CoalesceSeconds         int
	FallbackIntervalMinutes int
}

// DefaultWakeSettings returns the documented defaults: off, the default
// kinds, a 30 second quiet window and the scheduled interval as fallback.
func DefaultWakeSettings() WakeSettings {
	return WakeSettings{
		Enabled:                 false,
		Kinds:                   DefaultWakeKinds(),
		CoalesceSeconds:         DefaultWakeCoalesceSeconds,
		FallbackIntervalMinutes: DefaultWakeFallbackIntervalMinutes,
	}
}

// WakeKeys lists the wake setting keys in a stable order.
func WakeKeys() []string {
	return []string{KeyWakeEnabled, KeyWakeKinds, KeyWakeCoalesceSeconds, KeyWakeFallbackIntervalMinutes}
}

// IsWakeKey reports whether key is one of the wake setting keys.
func IsWakeKey(key string) bool {
	for _, k := range WakeKeys() {
		if k == key {
			return true
		}
	}
	return false
}

func (w WakeSettings) clone() WakeSettings {
	c := w
	c.Kinds = append([]string(nil), w.Kinds...)
	return c
}

// validate refuses an unknown kind, a duplicate, an empty list while the
// wake is on, and a number outside its bounds, naming the key.
func (w WakeSettings) validate() error {
	if len(w.Kinds) > MaxWakeKinds {
		return fmt.Errorf("%s: at most %d kinds, got %d", KeyWakeKinds, MaxWakeKinds, len(w.Kinds))
	}
	seen := map[string]bool{}
	for _, k := range w.Kinds {
		if k == "" {
			return fmt.Errorf("%s: an empty event kind", KeyWakeKinds)
		}
		if seen[k] {
			return fmt.Errorf("%s: duplicate event kind %q", KeyWakeKinds, k)
		}
		seen[k] = true
		if eventKindChecker != nil && !eventKindChecker(k) {
			return fmt.Errorf("%s: unknown event kind %q", KeyWakeKinds, k)
		}
	}
	if w.Enabled && len(w.Kinds) == 0 {
		return fmt.Errorf("%s: at least one event kind is needed while %s is on", KeyWakeKinds, KeyWakeEnabled)
	}
	if w.CoalesceSeconds < 0 || w.CoalesceSeconds > MaxWakeCoalesceSeconds {
		return fmt.Errorf("%s: must be between 0 and %d, got %d", KeyWakeCoalesceSeconds, MaxWakeCoalesceSeconds, w.CoalesceSeconds)
	}
	if w.FallbackIntervalMinutes < MinWakeFallbackIntervalMinutes || w.FallbackIntervalMinutes > MaxWakeFallbackIntervalMinutes {
		return fmt.Errorf("%s: must be between %d and %d, got %d", KeyWakeFallbackIntervalMinutes, MinWakeFallbackIntervalMinutes, MaxWakeFallbackIntervalMinutes, w.FallbackIntervalMinutes)
	}
	return nil
}

// toKinds accepts a []string, a []any of strings, a JSON list in a string, or
// a comma- or newline-separated string. Each kind is trimmed; blanks drop out.
func toKinds(v any) ([]string, bool) {
	var raw []string
	switch x := v.(type) {
	case nil:
		return []string{}, true
	case []string:
		raw = x
	case []any:
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			raw = append(raw, s)
		}
	case string:
		t := strings.TrimSpace(x)
		if strings.HasPrefix(t, "[") {
			var list []string
			if err := json.Unmarshal([]byte(t), &list); err != nil {
				return nil, false
			}
			raw = list
		} else {
			raw = strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == '\n' || r == ';' || r == ' ' || r == '\t' })
		}
	default:
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out, true
}

// apply sets one wake key. The bool reports whether key is a wake key.
func (w *WakeSettings) apply(key string, v any) (bool, error) {
	bad := func(want string) error { return fmt.Errorf("%s: want %s, got %v", key, want, v) }
	switch key {
	case KeyWakeEnabled:
		b, ok := v.(bool)
		if !ok {
			return true, bad("bool")
		}
		w.Enabled = b
	case KeyWakeKinds:
		kinds, ok := toKinds(v)
		if !ok {
			return true, bad("list of event kinds")
		}
		w.Kinds = kinds
	case KeyWakeCoalesceSeconds, KeyWakeFallbackIntervalMinutes:
		n, ok := toInt(v)
		if !ok {
			return true, bad("integer")
		}
		if key == KeyWakeCoalesceSeconds {
			w.CoalesceSeconds = n
		} else {
			w.FallbackIntervalMinutes = n
		}
	default:
		return false, nil
	}
	return true, nil
}

// flat writes the wake keys into out, keyed like Keys. Kinds are a fresh
// slice so a caller cannot change the settings through the map.
func (w WakeSettings) flat(out map[string]any) {
	out[KeyWakeEnabled] = w.Enabled
	out[KeyWakeKinds] = append([]string{}, w.Kinds...)
	out[KeyWakeCoalesceSeconds] = w.CoalesceSeconds
	out[KeyWakeFallbackIntervalMinutes] = w.FallbackIntervalMinutes
}

// Wakes reports whether an event of this kind should wake the supervisor:
// the wake path is on and the kind is listed.
func (w WakeSettings) Wakes(kind string) bool {
	if !w.Enabled {
		return false
	}
	for _, k := range w.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// SortedKinds returns the kinds in sorted order, as a fresh slice.
func (w WakeSettings) SortedKinds() []string {
	out := append([]string{}, w.Kinds...)
	sort.Strings(out)
	return out
}

// Get returns one setting's value as Flat lists it. ok is false for an
// unknown key.
func (s Settings) Get(key string) (v any, ok bool) {
	v, ok = s.Flat()[key]
	return v, ok
}
