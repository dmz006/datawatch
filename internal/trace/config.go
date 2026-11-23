package trace

import (
	"fmt"
	"sort"
	"strings"
)

// Tracing modes (tracing.mode).
const (
	ModeOff     = "off"
	ModeSummary = "summary"
	ModeFull    = "full"
)

// Defaults applied when a setting is unset (zero).
const (
	DefaultMode        = ModeSummary
	DefaultArgCapBytes = 512
	// DefaultRetentionDays is the default for tracing.retention_days; 0
	// means events are kept forever (no age-based pruning). An operator can
	// still set a positive value to keep the old drop-old-events behaviour.
	DefaultRetentionDays = 0
	// MaxArgCapBytes bounds tracing.arg_cap_bytes.
	MaxArgCapBytes = 1 << 20
	// MaxRetentionDays bounds tracing.retention_days.
	MaxRetentionDays = 3650
)

// Settings is the tracing configuration (tracing.* keys). Zero values
// read as the defaults through WithDefaults.
type Settings struct {
	// Mode is off, summary or full; empty reads summary.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// ArgCapBytes caps the stored argument summary; 0 reads the default.
	ArgCapBytes int `yaml:"arg_cap_bytes,omitempty" json:"arg_cap_bytes,omitempty"`
	// RetentionDays is how long events are kept, in days; 0 (the default)
	// keeps them forever — no age-based pruning.
	RetentionDays int `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
	// Backends enables tracing per backend; a backend not listed is on.
	Backends map[string]bool `yaml:"backends,omitempty" json:"backends,omitempty"`
}

// Defaults returns the default settings: summary mode, a 512-byte argument
// cap, no age-based pruning (retention 0 = keep forever), all backends on.
func Defaults() Settings {
	return Settings{
		Mode:          DefaultMode,
		ArgCapBytes:   DefaultArgCapBytes,
		RetentionDays: DefaultRetentionDays,
		Backends:      map[string]bool{},
	}
}

// WithDefaults returns a copy with unset fields filled in. RetentionDays
// is deliberately not rewritten: 0 is now a real value (no age-based
// pruning), not an "unset" marker, so an operator-set value stays exactly
// as set. The Backends map is copied, never shared.
func (s Settings) WithDefaults() Settings {
	out := s
	if out.Mode == "" {
		out.Mode = DefaultMode
	}
	if out.ArgCapBytes == 0 {
		out.ArgCapBytes = DefaultArgCapBytes
	}
	out.Backends = make(map[string]bool, len(s.Backends))
	for k, v := range s.Backends {
		out.Backends[k] = v
	}
	return out
}

// BackendEnabled reports whether tracing is on for the backend: the mode
// is not off and the backend is not explicitly disabled.
func (s Settings) BackendEnabled(backend string) bool {
	if s.WithDefaults().Mode == ModeOff {
		return false
	}
	on, ok := s.Backends[backend]
	return !ok || on
}

// Validate rejects an unknown mode, a negative or oversized cap or
// retention, and an empty backend name. Zero values are valid (defaults).
func (s Settings) Validate() error {
	switch s.Mode {
	case "", ModeOff, ModeSummary, ModeFull:
	default:
		return fmt.Errorf("tracing.mode must be %q, %q or %q, got %q", ModeOff, ModeSummary, ModeFull, s.Mode)
	}
	if s.ArgCapBytes < 0 || s.ArgCapBytes > MaxArgCapBytes {
		return fmt.Errorf("tracing.arg_cap_bytes must be between 0 and %d, got %d", MaxArgCapBytes, s.ArgCapBytes)
	}
	if s.RetentionDays < 0 || s.RetentionDays > MaxRetentionDays {
		// 0 is valid (keep forever); only negative or > MaxRetentionDays is
		// refused.
		return fmt.Errorf("tracing.retention_days must be between 0 and %d, got %d", MaxRetentionDays, s.RetentionDays)
	}
	names := make([]string, 0, len(s.Backends))
	for k := range s.Backends {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("tracing.backends: backend name must not be empty")
		}
	}
	return nil
}
