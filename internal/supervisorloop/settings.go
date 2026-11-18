package supervisorloop

import "fmt"

// Setting keys.
const (
	KeyEnabled          = "supervisor_loop.enabled"
	KeyValidatorRetries = "supervisor_loop.validator_retries"
	KeyEscalateAfter    = "supervisor_loop.escalate_after_failures"
	KeyRunRetention     = "supervisor_loop.run_retention"
	// KeyArchiveCancelledAfterMinutes is the grace, in minutes, before a
	// cancelled Automaton is archived. 0 archives it at once.
	KeyArchiveCancelledAfterMinutes = "supervisor_loop.archive_cancelled_after_minutes"
	KeyLaneMin                      = "supervisor_loop.lane_count.min"
	KeyLaneMax                      = "supervisor_loop.lane_count.max"
	keyStepSuffixEnabled            = ".enabled"
	keyJobSuffixStyle               = ".style"
	keyJobSuffixBackend             = ".backend"
	keyJobSuffixModel               = ".model"
	settingsPrefix                  = "supervisor_loop."
)

// StepNames are the mechanical steps with an enabled switch.
var StepNames = []string{
	"close_finished_records", "deploy_after_ship", "retry_failed_infra", "cleanup_sweep",
	"apply_answers", "lane_count", "start_next_chain_step", "run_merge_until_conflict",
}

// ModelJobNames are the model jobs with style/backend/model settings.
var ModelJobNames = []string{"draft_spec", "classify_ci_failure", "pick_backend"}

// JobSetting holds the per-job runtime choices of a model job.
type JobSetting struct {
	Style   Style
	Backend string
	Model   string
}

// Settings is the supervisor_loop configuration block.
type Settings struct {
	Enabled          bool
	Steps            map[string]bool
	Jobs             map[string]JobSetting
	ValidatorRetries int
	EscalateAfter    int
	RunRetention     int
	// ArchiveCancelledAfterMinutes is the grace before a cancelled Automaton
	// is archived. 0 means archive at once.
	ArchiveCancelledAfterMinutes int
	// LaneMin and LaneMax are the operator's lane bounds for the lane_count
	// step. LaneMax 0 means no bounds are configured, and the step moves
	// nothing.
	LaneMin int
	LaneMax int
	// Wake is the supervisor_loop.wake block (wake_settings.go): the event
	// kinds that start a pass at once, with the schedule as fallback.
	Wake WakeSettings
}

// DefaultSettings returns the documented defaults.
func DefaultSettings() Settings {
	s := Settings{
		Enabled: true, Steps: map[string]bool{}, Jobs: map[string]JobSetting{},
		ValidatorRetries: 2, EscalateAfter: 2, RunRetention: 500, ArchiveCancelledAfterMinutes: 60,
		Wake: DefaultWakeSettings(),
	}
	for _, n := range StepNames {
		s.Steps[n] = true
	}
	for _, n := range ModelJobNames {
		s.Jobs[n] = JobSetting{Style: StyleOneshot}
	}
	return s
}

// Validate refuses a bad value and names the offending key.
func (s Settings) Validate() error {
	if s.ValidatorRetries < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", KeyValidatorRetries, s.ValidatorRetries)
	}
	if s.EscalateAfter < 1 {
		return fmt.Errorf("%s: must be >= 1, got %d", KeyEscalateAfter, s.EscalateAfter)
	}
	if s.RunRetention < 1 {
		return fmt.Errorf("%s: must be >= 1, got %d", KeyRunRetention, s.RunRetention)
	}
	if s.ArchiveCancelledAfterMinutes < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", KeyArchiveCancelledAfterMinutes, s.ArchiveCancelledAfterMinutes)
	}
	if s.LaneMin < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", KeyLaneMin, s.LaneMin)
	}
	if s.LaneMax < 0 || (s.LaneMax > 0 && s.LaneMax < s.LaneMin) {
		return fmt.Errorf("%s: must be 0 (unset) or >= %s (%d), got %d", KeyLaneMax, KeyLaneMin, s.LaneMin, s.LaneMax)
	}
	for _, n := range ModelJobNames {
		j := s.Jobs[n]
		if !j.Style.Valid() {
			return fmt.Errorf("%s%s%s: must be oneshot or session, got %q", settingsPrefix, n, keyJobSuffixStyle, j.Style)
		}
	}
	return s.Wake.validate()
}

// Set applies one key/value, returning an error naming the key when the key is
// unknown or the value is the wrong type or out of range.
func (s *Settings) Set(key string, v any) error {
	next := s.clone()
	if err := next.apply(key, v); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*s = next
	return nil
}

func (s Settings) clone() Settings {
	c := s
	c.Steps = map[string]bool{}
	for k, v := range s.Steps {
		c.Steps[k] = v
	}
	c.Jobs = map[string]JobSetting{}
	for k, v := range s.Jobs {
		c.Jobs[k] = v
	}
	c.Wake = s.Wake.clone()
	return c
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func (s *Settings) apply(key string, v any) error {
	bad := func(want string) error { return fmt.Errorf("%s: want %s, got %v", key, want, v) }
	if handled, err := s.Wake.apply(key, v); handled {
		return err
	}
	switch key {
	case KeyEnabled:
		b, ok := v.(bool)
		if !ok {
			return bad("bool")
		}
		s.Enabled = b
		return nil
	case KeyValidatorRetries, KeyEscalateAfter, KeyRunRetention, KeyArchiveCancelledAfterMinutes, KeyLaneMin, KeyLaneMax:
		n, ok := toInt(v)
		if !ok {
			return bad("integer")
		}
		switch key {
		case KeyValidatorRetries:
			s.ValidatorRetries = n
		case KeyArchiveCancelledAfterMinutes:
			s.ArchiveCancelledAfterMinutes = n
		case KeyLaneMin:
			s.LaneMin = n
		case KeyLaneMax:
			s.LaneMax = n
		case KeyEscalateAfter:
			s.EscalateAfter = n
		default:
			s.RunRetention = n
		}
		return nil
	}
	for _, n := range StepNames {
		if key == settingsPrefix+n+keyStepSuffixEnabled {
			b, ok := v.(bool)
			if !ok {
				return bad("bool")
			}
			s.Steps[n] = b
			return nil
		}
	}
	for _, n := range ModelJobNames {
		p := settingsPrefix + n
		j := s.Jobs[n]
		switch key {
		case p + keyJobSuffixStyle, p + keyJobSuffixBackend, p + keyJobSuffixModel:
			str, ok := v.(string)
			if !ok {
				return bad("string")
			}
			switch key {
			case p + keyJobSuffixStyle:
				j.Style = Style(str)
			case p + keyJobSuffixBackend:
				j.Backend = str
			default:
				j.Model = str
			}
			s.Jobs[n] = j
			return nil
		}
	}
	return fmt.Errorf("%s: unknown setting", key)
}

// ParseSettings builds Settings from a flat key map (for example a decoded
// hand-edited config). A wrong value falls back to its default; the returned
// slice lists the keys that were reset so callers can warn.
func ParseSettings(raw map[string]any) (Settings, []string) {
	s := DefaultSettings()
	var reset []string
	for k, v := range raw {
		if err := s.Set(k, v); err != nil {
			reset = append(reset, k)
		}
	}
	return s, reset
}

// Keys lists every setting key, in a stable order.
func Keys() []string {
	keys := []string{KeyEnabled}
	for _, n := range StepNames {
		keys = append(keys, settingsPrefix+n+keyStepSuffixEnabled)
	}
	for _, n := range ModelJobNames {
		p := settingsPrefix + n
		keys = append(keys, p+keyJobSuffixStyle, p+keyJobSuffixBackend, p+keyJobSuffixModel)
	}
	keys = append(keys, KeyValidatorRetries, KeyEscalateAfter, KeyRunRetention, KeyArchiveCancelledAfterMinutes, KeyLaneMin, KeyLaneMax)
	return append(keys, WakeKeys()...)
}

// Flat returns the settings in effect as a flat map keyed like Keys.
func (s Settings) Flat() map[string]any {
	out := map[string]any{
		KeyEnabled: s.Enabled, KeyValidatorRetries: s.ValidatorRetries, KeyEscalateAfter: s.EscalateAfter,
		KeyRunRetention: s.RunRetention, KeyArchiveCancelledAfterMinutes: s.ArchiveCancelledAfterMinutes, KeyLaneMin: s.LaneMin, KeyLaneMax: s.LaneMax,
	}
	for _, n := range StepNames {
		on, ok := s.Steps[n]
		out[settingsPrefix+n+keyStepSuffixEnabled] = !ok || on
	}
	for _, n := range ModelJobNames {
		j := s.Jobs[n]
		style := j.Style
		if style == "" {
			style = StyleOneshot
		}
		p := settingsPrefix + n
		out[p+keyJobSuffixStyle], out[p+keyJobSuffixBackend], out[p+keyJobSuffixModel] = string(style), j.Backend, j.Model
	}
	s.Wake.flat(out)
	return out
}

// IsKey reports whether key is a supervisor_loop setting key.
func IsKey(key string) bool {
	for _, k := range Keys() {
		if k == key {
			return true
		}
	}
	return false
}
