// The usage.calculator block: settings for the usage and cost calculator
// (variant shown, policy, safety margin, cost per task, idle baseline, lane
// ceiling, weekly reset moment and the keeper of the shared readings).
// Normalisation, Validate and Effective helpers follow usage_backoff.go:
// an invalid saved value degrades to its default and logs a warning rather
// than failing daemon start, while the operator-facing surfaces call the
// Validate helpers so a typo is a 400 rather than a silent default.
package config

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	// UsageVariantPaceGauge, UsageVariantLanePlanner and UsageVariantBurnLedger
	// pick one calculator layout; UsageVariantAll shows all three side by side.
	UsageVariantPaceGauge   = "pace-gauge"
	UsageVariantLanePlanner = "lane-planner"
	UsageVariantBurnLedger  = "burn-ledger"
	UsageVariantAll         = "all"

	// UsagePolicyUseAll runs until the allowance is used up, UsagePolicyPace
	// aims a safety margin under the pace line and UsagePolicyFinishEarly
	// runs flat out until the remaining work is done.
	UsagePolicyUseAll      = "use-all"
	UsagePolicyPace        = "pace"
	UsagePolicyFinishEarly = "finish-early"

	UsageSafetyPointsDefault   = 5.0
	UsageSafetyPointsMax       = 50.0
	UsageCostPerTaskDefault    = 0.1
	UsageBaselinePer24hDefault = 3.0
	UsageBaselinePer24hMax     = 20.0
	UsageMaxLanesDefault       = 3
	UsageMaxLanesMax           = 8
	UsageResetWeekdayDefault   = "sunday"
	UsageResetTimeDefault      = "08:00"
	// UsageResetZoneDefault is intentionally empty: an unset reset zone follows
	// the server's local zone (see ResolveUsageServerZone) rather than a fixed
	// UTC.
	UsageResetZoneDefault = ""
)

// UsageCalculatorConfig is the usage.calculator section. Numeric fields carry
// no omitempty so an explicit 0 (a valid safety margin or baseline) survives a
// save and reload.
type UsageCalculatorConfig struct {
	// Variant: pace-gauge | lane-planner | burn-ledger | all (default all).
	Variant string `yaml:"variant,omitempty" json:"variant,omitempty"`
	// Policy: use-all | pace | finish-early (default use-all).
	Policy string `yaml:"policy,omitempty" json:"policy,omitempty"`
	// SafetyPoints is the margin under the pace line, 0 to 50 (default 5).
	SafetyPoints float64 `yaml:"safety_points" json:"safety_points"`
	// CostPerTaskPoints is the weekly points one task costs, > 0 (default
	// 0.1). A starting figure to calibrate against readings.
	CostPerTaskPoints float64 `yaml:"cost_per_task_points" json:"cost_per_task_points"`
	// BaselinePer24h is the idle burn in points per 24 hours, 0 to 20
	// (default 3).
	BaselinePer24h float64 `yaml:"baseline_per_24h" json:"baseline_per_24h"`
	// MaxLanes is the largest lane count the planner offers, 1 to 8
	// (default 3).
	MaxLanes int `yaml:"max_lanes" json:"max_lanes"`
	// ResetWeekday: sunday..saturday (default sunday).
	ResetWeekday string `yaml:"reset_weekday,omitempty" json:"reset_weekday,omitempty"`
	// ResetTime is the 24-hour HH:MM local clock time of the reset
	// (default 08:00).
	ResetTime string `yaml:"reset_time,omitempty" json:"reset_time,omitempty"`
	// ResetZone is an IANA zone name; a fixed offset is never accepted. Empty
	// (the default) means the reset follows the server's local zone, resolved
	// at read time by EffectiveUsageResetZone.
	ResetZone string `yaml:"reset_zone,omitempty" json:"reset_zone,omitempty"`
	// Keeper names the federation peer that keeps the shared readings; empty
	// means this daemon keeps them.
	Keeper string `yaml:"keeper,omitempty" json:"keeper,omitempty"`
}

// UsageConfig is the top-level usage block.
type UsageConfig struct {
	Calculator UsageCalculatorConfig `yaml:"calculator,omitempty" json:"calculator,omitempty"`
}

// DefaultUsageCalculator returns the calculator settings applied when none
// are configured.
func DefaultUsageCalculator() UsageCalculatorConfig {
	return UsageCalculatorConfig{
		Variant:           UsageVariantAll,
		Policy:            UsagePolicyUseAll,
		SafetyPoints:      UsageSafetyPointsDefault,
		CostPerTaskPoints: UsageCostPerTaskDefault,
		BaselinePer24h:    UsageBaselinePer24hDefault,
		MaxLanes:          UsageMaxLanesDefault,
		ResetWeekday:      UsageResetWeekdayDefault,
		ResetTime:         UsageResetTimeDefault,
		ResetZone:         UsageResetZoneDefault,
	}
}

// usageCalculatorLogf receives the warning lines raised while normalising a
// loaded config. Tests replace it to capture the output.
var usageCalculatorLogf = func(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

var (
	usageVariants     = []string{UsageVariantPaceGauge, UsageVariantLanePlanner, UsageVariantBurnLedger, UsageVariantAll}
	usagePolicies     = []string{UsagePolicyUseAll, UsagePolicyPace, UsagePolicyFinishEarly}
	usageWeekdays     = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	usageClockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	// usageFixedOffsetZone matches names that spell a fixed offset rather than
	// a place: "+05:00", "UTC+5", "GMT-3", "Etc/GMT+5".
	usageFixedOffsetZone = regexp.MustCompile(`(?i)^([+-]|((etc/)?(gmt|utc)[+-]?[0-9]))`)
)

const usageLocaltimeLink = "/etc/localtime" // production path of the system local-time symlink

// The three lookups ResolveUsageServerZone relies on are package variables so
// tests can fake the host's environment. The env lookup reads TZ, the symlink
// read resolves the production path above, and the local location is
// time.Local ("Local" when the host zone is unknown).
var (
	usageServerZoneEnv   = os.Getenv
	usageLocaltimeTarget = os.Readlink
	usageLocalLocation   = time.Local
)

// usageCandidateZone accepts name only when it names a real IANA zone the
// runtime can load. Empty, the host-dependent "Local" and fixed offsets are
// rejected.
func usageCandidateZone(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if err := ValidateUsageCalculatorResetZone(name); err != nil {
		return "", false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return "", false
	}
	return loc.String(), true
}

// usageZoneAfterZoneinfo returns the part of a local-time symlink target that
// follows its "zoneinfo" directory element, so a target such as
// /usr/share/zoneinfo/Europe/Lisbon yields Europe/Lisbon. It returns "" when
// the element is missing or nothing follows it.
func usageZoneAfterZoneinfo(target string) string {
	parts := strings.Split(strings.TrimSpace(target), "/")
	for i, p := range parts {
		if p != "zoneinfo" {
			continue
		}
		if i == len(parts)-1 {
			return ""
		}
		return strings.Join(parts[i+1:], "/")
	}
	return ""
}

// ResolveUsageServerZone returns the IANA zone the host this daemon runs in, so
// an unset reset zone can follow the server. It tries, in order: the TZ
// environment variable, the zone encoded in the system local-time symlink's
// target (the part after its "zoneinfo" directory element), and time.Local
// when it names a real zone rather than "Local". Each candidate must be a zone
// the runtime can load; a fixed offset never resolves. ok is false when no
// candidate passes.
func ResolveUsageServerZone() (string, bool) {
	if z, ok := usageCandidateZone(strings.TrimSpace(usageServerZoneEnv("TZ"))); ok {
		return z, true
	}
	if target, err := usageLocaltimeTarget(usageLocaltimeLink); err == nil {
		if z, ok := usageCandidateZone(usageZoneAfterZoneinfo(target)); ok {
			return z, true
		}
	}
	if usageLocalLocation != nil {
		if name := usageLocalLocation.String(); name != "Local" {
			if z, ok := usageCandidateZone(name); ok {
				return z, true
			}
		}
	}
	return "", false
}

// EffectiveUsageResetZone returns the zone the weekly reset runs in. A
// non-empty configured zone (an explicit UTC included) is used as-is with
// serverZone false. An unset zone follows the server (serverZone true),
// resolved by ResolveUsageServerZone; when no candidate resolves, the zone is
// UTC with fellBack true so the reset always stays scheduled.
func EffectiveUsageResetZone(c UsageCalculatorConfig) (zone string, serverZone bool, fellBack bool) {
	if z := strings.TrimSpace(c.ResetZone); z != "" {
		return z, false, false
	}
	if z, ok := ResolveUsageServerZone(); ok {
		return z, true, false
	}
	return "UTC", true, true
}

func usageChoice(v string, allowed []string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(v))
	for _, a := range allowed {
		if s == a {
			return a, true
		}
	}
	return "", false
}

// ValidateUsageCalculatorVariant reports an error for a value that is neither
// empty (the default) nor an allowed variant.
func ValidateUsageCalculatorVariant(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if _, ok := usageChoice(v, usageVariants); !ok {
		return fmt.Errorf("invalid usage.calculator.variant %q: must be one of %s", v, strings.Join(usageVariants, ", "))
	}
	return nil
}

// ValidateUsageCalculatorPolicy reports an error for a value that is neither
// empty (the default) nor an allowed policy.
func ValidateUsageCalculatorPolicy(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if _, ok := usageChoice(v, usagePolicies); !ok {
		return fmt.Errorf("invalid usage.calculator.policy %q: must be one of %s", v, strings.Join(usagePolicies, ", "))
	}
	return nil
}

func validateFiniteRange(key string, v, lo, hi float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < lo || v > hi {
		return fmt.Errorf("invalid usage.calculator.%s %v: must be a number from %v to %v", key, v, lo, hi)
	}
	return nil
}

// ValidateUsageCalculatorSafetyPoints requires 0 to 50.
func ValidateUsageCalculatorSafetyPoints(v float64) error {
	return validateFiniteRange("safety_points", v, 0, UsageSafetyPointsMax)
}

// ValidateUsageCalculatorCostPerTaskPoints requires a finite number above 0.
func ValidateUsageCalculatorCostPerTaskPoints(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return fmt.Errorf("invalid usage.calculator.cost_per_task_points %v: must be a number above 0", v)
	}
	return nil
}

// ValidateUsageCalculatorBaselinePer24h requires 0 to 20.
func ValidateUsageCalculatorBaselinePer24h(v float64) error {
	return validateFiniteRange("baseline_per_24h", v, 0, UsageBaselinePer24hMax)
}

// ValidateUsageCalculatorMaxLanes requires 1 to 8.
func ValidateUsageCalculatorMaxLanes(v int) error {
	if v < 1 || v > UsageMaxLanesMax {
		return fmt.Errorf("invalid usage.calculator.max_lanes %d: must be a whole number from 1 to %d", v, UsageMaxLanesMax)
	}
	return nil
}

// ValidateUsageCalculatorResetWeekday reports an error for a value that is
// neither empty (the default) nor a weekday name.
func ValidateUsageCalculatorResetWeekday(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if _, ok := usageChoice(v, usageWeekdays); !ok {
		return fmt.Errorf("invalid usage.calculator.reset_weekday %q: must be sunday through saturday", v)
	}
	return nil
}

// ValidateUsageCalculatorResetTime reports an error for a value that is
// neither empty (the default) nor a 24-hour HH:MM clock time.
func ValidateUsageCalculatorResetTime(v string) error {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil
	}
	if !usageClockPattern.MatchString(s) {
		return fmt.Errorf("invalid usage.calculator.reset_time %q: must be a 24-hour HH:MM clock time such as 08:00", v)
	}
	return nil
}

// ValidateUsageCalculatorResetZone reports an error for a value that is
// neither empty (the default) nor an IANA zone name the runtime knows. A
// fixed offset and the host-dependent "Local" are rejected.
func ValidateUsageCalculatorResetZone(v string) error {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil
	}
	if strings.EqualFold(s, "local") || usageFixedOffsetZone.MatchString(s) {
		return fmt.Errorf("invalid usage.calculator.reset_zone %q: use an IANA zone name such as America/New_York, not a fixed offset", v)
	}
	if _, err := time.LoadLocation(s); err != nil {
		return fmt.Errorf("invalid usage.calculator.reset_zone %q: not an IANA zone name", v)
	}
	return nil
}

// ValidateUsageCalculatorKeeper accepts an empty value (this daemon keeps the
// readings) or a peer name of letters, digits, dot, dash and underscore.
func ValidateUsageCalculatorKeeper(v string) error {
	s := strings.TrimSpace(v)
	if len(s) > 128 {
		return fmt.Errorf("invalid usage.calculator.keeper: longer than 128 characters")
	}
	for _, r := range s {
		ok := r == '.' || r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return fmt.Errorf("invalid usage.calculator.keeper %q: a peer name has only letters, digits, dot, dash and underscore", v)
		}
	}
	return nil
}

// ValidateUsageCalculator checks every field of c and returns the first
// error. Empty strings and an unset (zero) cost per task or lane count are
// not errors here when they mean "default"; use the per-key helpers for a
// value an operator just typed.
func ValidateUsageCalculator(c UsageCalculatorConfig) error {
	for _, err := range []error{
		ValidateUsageCalculatorVariant(c.Variant),
		ValidateUsageCalculatorPolicy(c.Policy),
		ValidateUsageCalculatorSafetyPoints(c.SafetyPoints),
		ValidateUsageCalculatorCostPerTaskPoints(c.CostPerTaskPoints),
		ValidateUsageCalculatorBaselinePer24h(c.BaselinePer24h),
		ValidateUsageCalculatorMaxLanes(c.MaxLanes),
		ValidateUsageCalculatorResetWeekday(c.ResetWeekday),
		ValidateUsageCalculatorResetTime(c.ResetTime),
		ValidateUsageCalculatorResetZone(c.ResetZone),
		ValidateUsageCalculatorKeeper(c.Keeper),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// normalizeUsageCalculator returns c with every invalid or unset field
// replaced by its default. warn receives one line per replaced value that was
// set but not allowed (a retired variant, an out-of-range number); nil is
// quiet. An unset string is the default without a warning.
func normalizeUsageCalculator(c UsageCalculatorConfig, warn func(format string, args ...interface{})) UsageCalculatorConfig {
	d := DefaultUsageCalculator()
	say := func(format string, args ...interface{}) {
		if warn != nil {
			warn(format, args...)
		}
	}
	pick := func(key, v string, allowed []string, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		if got, ok := usageChoice(v, allowed); ok {
			return got
		}
		say("[warn] usage.calculator.%s: %q is no longer an allowed value — using %q (allowed: %s)", key, v, def, strings.Join(allowed, ", "))
		return def
	}
	out := c
	out.Variant = pick("variant", c.Variant, usageVariants, d.Variant)
	out.Policy = pick("policy", c.Policy, usagePolicies, d.Policy)
	out.ResetWeekday = pick("reset_weekday", c.ResetWeekday, usageWeekdays, d.ResetWeekday)

	num := func(key string, err error, v, def float64) float64 {
		if err == nil {
			return v
		}
		say("[warn] usage.calculator.%s: %v — using %v", key, v, def)
		return def
	}
	out.SafetyPoints = num("safety_points", ValidateUsageCalculatorSafetyPoints(c.SafetyPoints), c.SafetyPoints, d.SafetyPoints)
	// An unset cost per task is the default quietly; a negative or non-finite
	// one is reported.
	if c.CostPerTaskPoints == 0 {
		out.CostPerTaskPoints = d.CostPerTaskPoints
	} else {
		out.CostPerTaskPoints = num("cost_per_task_points", ValidateUsageCalculatorCostPerTaskPoints(c.CostPerTaskPoints), c.CostPerTaskPoints, d.CostPerTaskPoints)
	}
	out.BaselinePer24h = num("baseline_per_24h", ValidateUsageCalculatorBaselinePer24h(c.BaselinePer24h), c.BaselinePer24h, d.BaselinePer24h)
	if c.MaxLanes == 0 {
		out.MaxLanes = d.MaxLanes
	} else if err := ValidateUsageCalculatorMaxLanes(c.MaxLanes); err != nil {
		say("[warn] usage.calculator.max_lanes: %d — using %d", c.MaxLanes, d.MaxLanes)
		out.MaxLanes = d.MaxLanes
	}

	out.ResetTime = strings.TrimSpace(c.ResetTime)
	if out.ResetTime == "" {
		out.ResetTime = d.ResetTime
	} else if err := ValidateUsageCalculatorResetTime(out.ResetTime); err != nil {
		say("[warn] usage.calculator.reset_time: %q is not a 24-hour HH:MM time — using %q", c.ResetTime, d.ResetTime)
		out.ResetTime = d.ResetTime
	}
	// An empty zone stays empty in the stored config: it means "follow the
	// server" and resolves lazily through EffectiveUsageResetZone, so an
	// invalid host TZ never raises a warning at load time. A set-but-invalid
	// zone degrades to the server zone with the usual warning.
	out.ResetZone = strings.TrimSpace(c.ResetZone)
	if out.ResetZone != "" {
		if err := ValidateUsageCalculatorResetZone(out.ResetZone); err != nil {
			say("[warn] usage.calculator.reset_zone: %q is not an IANA zone name — falling back to the server zone", c.ResetZone)
			out.ResetZone = ""
		}
	}
	out.Keeper = strings.TrimSpace(c.Keeper)
	if err := ValidateUsageCalculatorKeeper(out.Keeper); err != nil {
		say("[warn] usage.calculator.keeper: %q is not a peer name — keeping the readings on this daemon", c.Keeper)
		out.Keeper = ""
	}
	return out
}

// EffectiveUsageCalculator returns the settings a reader should act on,
// applying the same defaults and fallbacks as config load without logging.
// An unset ResetZone is materialised to the server zone (UTC when no server
// zone resolves); the choice and its flags come from EffectiveUsageResetZone.
func EffectiveUsageCalculator(c UsageCalculatorConfig) UsageCalculatorConfig {
	e := normalizeUsageCalculator(c, nil)
	if e.ResetZone == "" {
		e.ResetZone, _, _ = EffectiveUsageResetZone(e)
	}
	return e
}
