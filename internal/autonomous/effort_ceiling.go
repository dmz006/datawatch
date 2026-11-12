package autonomous

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Effort ceiling values. The first three cap a task's effort; cheaper_backend
// moves the task to the cheaper backend instead of lowering its effort.
const (
	CeilingLow          = "low"
	CeilingMedium       = "medium"
	CeilingHigh         = "high"
	CeilingCheapBackend = "cheaper_backend"
)

// BurnGuardLimits is the daemon-wide burn guard setting this package reads:
// usage.burn_guard.effort_ceiling and usage.burn_guard.retry_limit_claude.
// Enabled false makes every limit a no-op.
type BurnGuardLimits struct {
	Enabled bool
	// EffortCeiling maps a task kind (docs, parity, live_check, final_gate) to
	// its ceiling. An empty map means no ceiling.
	EffortCeiling map[string]string
	// RetryLimitClaude is the most retries of one task on Claude. 0 or less
	// means no limit.
	RetryLimitClaude int
}

var burnGuardLimits atomic.Pointer[BurnGuardLimits]

// SetBurnGuardLimits installs the daemon-wide limits. Nil clears them.
func SetBurnGuardLimits(l *BurnGuardLimits) {
	if l == nil {
		burnGuardLimits.Store(nil)
		return
	}
	c := *l
	c.EffortCeiling = copyCeilingMap(l.EffortCeiling)
	burnGuardLimits.Store(&c)
}

func currentBurnGuardLimits() BurnGuardLimits {
	if l := burnGuardLimits.Load(); l != nil {
		return *l
	}
	return BurnGuardLimits{}
}

func copyCeilingMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// NormalizeCeilingKind folds a kind to its configured spelling: lower case,
// with spaces and dashes as underscores.
func NormalizeCeilingKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	k = strings.ReplaceAll(k, "-", "_")
	return strings.ReplaceAll(k, " ", "_")
}

// ValidCeiling reports whether v is an accepted ceiling value.
func ValidCeiling(v string) bool {
	switch v {
	case CeilingLow, CeilingMedium, CeilingHigh, CeilingCheapBackend:
		return true
	}
	return false
}

// ceilingTitleKeywords maps a task kind to the title keywords that identify it
// when the task's Kind field does not.
var ceilingTitleKeywords = map[string][]string{
	"docs":       {"docs", "documentation", "readme", "changelog", "howto"},
	"parity":     {"parity"},
	"live_check": {"live check", "live-check", "live_check"},
	"final_gate": {"final gate", "final-gate", "final_gate"},
}

// TaskCeilingKind returns the configured kind a task belongs to, or "". The
// task's Kind field wins; a title keyword is the fallback. Only kinds present
// in kinds are returned.
func TaskCeilingKind(t *Task, kinds map[string]string) string {
	if t == nil || len(kinds) == 0 {
		return ""
	}
	configured := make(map[string]string, len(kinds))
	for k := range kinds {
		configured[NormalizeCeilingKind(k)] = k
	}
	if nk := NormalizeCeilingKind(t.Kind); nk != "" {
		if orig, ok := configured[nk]; ok {
			return orig
		}
	}
	title := strings.ToLower(t.Title)
	for nk, orig := range configured {
		words := ceilingTitleKeywords[nk]
		if len(words) == 0 {
			words = []string{strings.ReplaceAll(nk, "_", " ")}
		}
		for _, w := range words {
			if strings.Contains(title, w) {
				return orig
			}
		}
	}
	return ""
}

func effortRank(e string) int {
	switch strings.ToLower(e) {
	case "off", "minimal", "quick", "low":
		return 1
	case "medium", "normal":
		return 2
	case "high", "thorough":
		return 3
	case "xhigh":
		return 4
	case "max":
		return 5
	}
	return 0
}

// LowerEffort applies a ceiling to an effort: an effort above the ceiling comes
// down to it, an effort at or under it stays, an empty (unset) effort takes the ceiling.
// It never raises. cheaper_backend is not an effort and returns cur unchanged.
func LowerEffort(cur Effort, ceiling string) Effort {
	if ceiling != CeilingLow && ceiling != CeilingMedium && ceiling != CeilingHigh {
		return cur
	}
	if cur == "" {
		// Unset means the backend default, which is the case the ceiling
		// targets: the task runs at the ceiling.
		return Effort(ceiling)
	}
	if r := effortRank(string(cur)); r > effortRank(ceiling) {
		return Effort(ceiling)
	}
	return cur
}

// LowerThinkingLevel is LowerEffort for the pi --thinking level.
func LowerThinkingLevel(cur, ceiling string) string {
	return string(LowerEffort(Effort(cur), ceiling))
}

// EffectiveCeiling resolves the ceiling for a task. Most specific wins: the
// task's own ceiling, then the Automaton's per-kind map, then the daemon-wide
// map. Disabled limits and tasks outside every kind return "".
func EffectiveCeiling(t *Task, auto *Automation) string {
	g := currentBurnGuardLimits()
	if !g.Enabled || t == nil {
		return ""
	}
	if ValidCeiling(t.EffortCeiling) {
		return t.EffortCeiling
	}
	if auto != nil && len(auto.EffortCeiling) > 0 {
		if kind := TaskCeilingKind(t, auto.EffortCeiling); kind != "" && ValidCeiling(auto.EffortCeiling[kind]) {
			return auto.EffortCeiling[kind]
		}
	}
	if kind := TaskCeilingKind(t, g.EffortCeiling); kind != "" && ValidCeiling(g.EffortCeiling[kind]) {
		return g.EffortCeiling[kind]
	}
	return ""
}

// ApplyEffortCeiling lowers the resolved effort and thinking level of a task to
// its ceiling. cheaper is true when the ceiling asks for the cheaper backend,
// in which case effort and thinking are returned unchanged. An explicit setting
// at or under the ceiling is kept as set.
func ApplyEffortCeiling(t *Task, auto *Automation, effort Effort, thinking string) (Effort, string, bool) {
	c := EffectiveCeiling(t, auto)
	switch c {
	case "":
		return effort, thinking, false
	case CeilingCheapBackend:
		return effort, thinking, true
	}
	return LowerEffort(effort, c), LowerThinkingLevel(thinking, c), false
}

// cheaperBackendFor is the backend a cheaper_backend ceiling or a Claude retry
// limit routes to.
const cheaperBackendFor = "pi-prompt"

// SetAutomationEffortCeiling replaces the Automaton's per-kind ceiling map. An
// empty map clears it. Every value must be a valid ceiling.
func (m *Manager) SetAutomationEffortCeiling(id string, ceiling map[string]string, actor string, expectedVersion int64) (*Automation, error) {
	norm := make(map[string]string, len(ceiling))
	for k, v := range ceiling {
		v = strings.ToLower(strings.TrimSpace(v))
		if !ValidCeiling(v) {
			return nil, fmt.Errorf("invalid effort ceiling %q for kind %q — valid values: low, medium, high, cheaper_backend", v, k)
		}
		nk := NormalizeCeilingKind(k)
		if nk == "" {
			return nil, fmt.Errorf("empty task kind in effort ceiling")
		}
		norm[nk] = v
	}
	return m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}); err != nil {
			return err
		}
		if len(norm) == 0 {
			a.EffortCeiling = nil
		} else {
			a.EffortCeiling = norm
		}
		controlDecision(a, "set_effort_ceiling", actor, fmt.Sprintf("effort_ceiling=%v", norm))
		return nil
	})
}

// SetTaskEffortCeiling sets one task's ceiling. Empty clears it.
func (m *Manager) SetTaskEffortCeiling(prdID, taskID, ceiling, actor string) (*PRD, error) {
	ceiling = strings.ToLower(strings.TrimSpace(ceiling))
	if ceiling != "" && !ValidCeiling(ceiling) {
		return nil, fmt.Errorf("invalid effort ceiling %q — valid values: low, medium, high, cheaper_backend (empty clears it)", ceiling)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	found := false
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if prd.Story[si].Tasks[ti].ID == taskID {
					prd.Story[si].Tasks[ti].EffortCeiling = ceiling
					prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
					found = true
				}
			}
		}
		if !found {
			return
		}
		prd.UpdatedAt = time.Now()
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: "set_task_effort_ceiling", Actor: actor,
			Note: fmt.Sprintf("task=%s effort_ceiling=%s", taskID, ceiling),
		})
	})
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}
