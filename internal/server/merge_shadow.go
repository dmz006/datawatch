package server

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// mergeShadowKeyPrefix is the config path every autonomous.merge_shadow.*
// key shares.
const mergeShadowKeyPrefix = "autonomous.merge_shadow."

// ValidateMergeShadowValue checks one autonomous.merge_shadow.<field> value.
// enabled must be a boolean (or a "true"/"false" string, matching how comm
// channel scalars land here); gate_timeout must parse as a positive Go
// duration; plan_keep must be a whole number >= 1; agreement_streak must be
// a whole number >= 1. A field this does not know is an error, so a
// rejected PUT names the bad key. The daemon API and
// the CLI (`datawatch config set`) both call this one function, so a value
// refused on one surface is refused on the other.
func ValidateMergeShadowValue(field string, v any) error {
	switch field {
	case "enabled":
		if _, ok := v.(bool); ok {
			return nil
		}
		if s, ok := v.(string); ok {
			if _, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
				return nil
			}
		}
		return fmt.Errorf("%senabled: must be true or false", mergeShadowKeyPrefix)
	case "gate_timeout":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%sgate_timeout: must be a duration string such as \"20m\"", mergeShadowKeyPrefix)
		}
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil || d <= 0 {
			return fmt.Errorf("%sgate_timeout: must parse as a positive duration (e.g. \"20m\")", mergeShadowKeyPrefix)
		}
	case "plan_keep":
		if !isWholePositive(v) {
			return fmt.Errorf("%splan_keep: must be a positive whole number", mergeShadowKeyPrefix)
		}
	case "agreement_streak":
		if !isWholePositive(v) {
			return fmt.Errorf("%sagreement_streak: must be a positive whole number", mergeShadowKeyPrefix)
		}
	default:
		return fmt.Errorf("%s%s: unknown key (use enabled, gate_timeout, plan_keep or agreement_streak)", mergeShadowKeyPrefix, field)
	}
	return nil
}

// mergeShadowFields is the closed set of autonomous.merge_shadow.* fields.
var mergeShadowFields = map[string]bool{
	"enabled":          true,
	"gate_timeout":     true,
	"plan_keep":        true,
	"agreement_streak": true,
}

// IsMergeShadowConfigKey reports whether key is one of the
// autonomous.merge_shadow.* settings and returns its field name.
func IsMergeShadowConfigKey(key string) (field string, ok bool) {
	field, ok = strings.CutPrefix(key, mergeShadowKeyPrefix)
	return field, ok && mergeShadowFields[field]
}

// isWholePositive reports whether a decoded JSON number is a whole number
// >= 1.
func isWholePositive(v any) bool {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	return ok && f == math.Trunc(f) && f >= 1
}

// applyMergeShadowValue stores one autonomous.merge_shadow.<field> value in
// the config block. A value that fails the check is left unchanged; PUT
// /api/config has already refused it by then.
func applyMergeShadowValue(c *config.MergeShadowConfig, field string, v any) {
	switch field {
	case "enabled":
		if b, ok := v.(bool); ok {
			c.Enabled = &b
			return
		}
		if s, ok := v.(string); ok {
			if b, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
				c.Enabled = &b
			}
		}
	case "gate_timeout":
		s, ok := v.(string)
		if !ok {
			return
		}
		s = strings.TrimSpace(s)
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			c.GateTimeout = &s
		}
	case "plan_keep":
		if n, ok := toInt(v); ok && n >= 1 {
			c.PlanKeep = &n
		}
	case "agreement_streak":
		if n, ok := toInt(v); ok && n >= 1 {
			c.AgreementStreak = &n
		}
	}
}

// mergeShadowConfigSurface is the autonomous.merge_shadow block GET
// /api/config serves: the values in effect, so an unset key shows its
// default.
func mergeShadowConfigSurface(c config.MergeShadowConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":          c.EffectiveEnabled(),
		"gate_timeout":     c.EffectiveGateTimeout().String(),
		"plan_keep":        c.EffectivePlanKeep(),
		"agreement_streak": c.EffectiveAgreementStreak(),
	}
}

// mergeShadowSizeCounts is the matched and different tally for one release
// size. A comparison counts as different when it found at least one
// difference, and as matched otherwise.
type mergeShadowSizeCounts struct {
	Matched   int `json:"matched"`
	Different int `json:"different"`
}

// mergeShadowStreak is one project's agreement streak as served on
// GET /api/autonomous/merge/shadow: the run of most-recent comparisons
// with no major difference, the merges it is measured over, and the
// threshold the operator set.
type mergeShadowStreak struct {
	Length    int  `json:"length"`
	Total     int  `json:"total"`
	Threshold int  `json:"threshold"`
	Met       bool `json:"met"`
}

// mergeShadowProject is one project's entry in GET
// /api/autonomous/merge/shadow.
type mergeShadowProject struct {
	Project     string                           `json:"project"`
	Plans       []*mergeshadow.MergePlan         `json:"plans"`
	Comparisons []*mergeshadow.Comparison        `json:"comparisons"`
	Counts      map[string]mergeShadowSizeCounts `json:"counts"`
	Streak      *mergeShadowStreak               `json:"streak,omitempty"`
}

// mergeShadowUnknownProject groups comparisons whose Automaton has no stored
// plan to name a project.
const mergeShadowUnknownProject = "unknown"

// buildMergeShadowView groups stored plans and comparisons by project. A
// comparison joins its plan by Automaton id to find the project. Every entry
// carries all four release sizes in counts, and empty lists, never null.
func buildMergeShadowView(plans []*mergeshadow.MergePlan, comps []*mergeshadow.Comparison, streakThreshold int) []mergeShadowProject {
	byProject := map[string]*mergeShadowProject{}
	get := func(name string) *mergeShadowProject {
		p, ok := byProject[name]
		if !ok {
			p = &mergeShadowProject{
				Project:     name,
				Plans:       []*mergeshadow.MergePlan{},
				Comparisons: []*mergeshadow.Comparison{},
				Counts:      map[string]mergeShadowSizeCounts{},
			}
			for _, sz := range []mergeshadow.ReleaseSize{
				mergeshadow.ReleaseSizePatch, mergeshadow.ReleaseSizeMinor,
				mergeshadow.ReleaseSizeMajor, mergeshadow.ReleaseSizeNone,
			} {
				p.Counts[string(sz)] = mergeShadowSizeCounts{}
			}
			byProject[name] = p
		}
		return p
	}
	owner := map[string]string{}
	for _, pl := range plans {
		name := pl.Project
		if name == "" {
			name = mergeShadowUnknownProject
		}
		owner[pl.AutomatonID] = name
		p := get(name)
		p.Plans = append(p.Plans, pl)
	}
	for _, c := range comps {
		name, ok := owner[c.AutomatonID]
		if !ok {
			name = mergeShadowUnknownProject
		}
		p := get(name)
		p.Comparisons = append(p.Comparisons, c)
		cnt := p.Counts[string(c.ReleaseSize)]
		if c.Different > 0 {
			cnt.Different++
		} else {
			cnt.Matched++
		}
		p.Counts[string(c.ReleaseSize)] = cnt
	}
	out := make([]mergeShadowProject, 0, len(byProject))
	for _, p := range byProject {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	for i := range out {
		s := mergeshadow.AgreementStreakOf(out[i].Comparisons)
		met := s.Meets(streakThreshold)
		out[i].Streak = &mergeShadowStreak{Length: s.Length, Total: s.Total, Threshold: streakThreshold, Met: met}
	}
	return out
}

// handleAutonomousMergeShadow serves GET /api/autonomous/merge/shadow: per
// project, the stored shadow merge plans, the stored comparisons, the
// matched and different comparison counts per release size (patch, minor,
// major, none), and the agreement streak against the configured
// autonomous.merge_shadow.agreement_streak threshold. With nothing stored,
// "projects" is an empty list.
func (s *Server) handleAutonomousMergeShadow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	dir := s.statePath()
	plans, err := mergeshadow.List(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	comps, err := mergeshadow.ListComparisons(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	threshold := config.DefaultMergeShadowAgreementStreak
	if s.cfg != nil {
		threshold = s.cfg.Autonomous.MergeShadow.EffectiveAgreementStreak()
	}
	out := map[string]any{"projects": buildMergeShadowView(plans, comps, threshold)}
	if rep := s.mergeTurnOnReport(r.Context(), threshold); rep != nil {
		out["turn_on"] = rep
	}
	writeJSONOK(w, out)
}
