package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Lane preferences an Automaton can carry. An empty PRD.LanePreference means
// any lane.
const (
	LanePreferenceGPU    = "gpu"
	LanePreferenceClaude = "claude"
	LanePreferenceAny    = "any"
)

// NormalizeLanePreference trims and lower-cases s. ok is true for "gpu" and
// "claude", and for "any" and the empty string, which both mean any lane and
// come back as the empty string (the stored form).
func NormalizeLanePreference(s string) (string, bool) {
	switch p := strings.ToLower(strings.TrimSpace(s)); p {
	case "", LanePreferenceAny:
		return "", true
	case LanePreferenceGPU, LanePreferenceClaude:
		return p, true
	}
	return strings.ToLower(strings.TrimSpace(s)), false
}

// effectiveLanePreference names the stored preference, with the empty value
// shown as "any".
func effectiveLanePreference(p string) string {
	if p == "" {
		return LanePreferenceAny
	}
	return p
}

// SetPRDLanePreference sets which kind of lane suits the Automaton: gpu,
// claude, or any (also the empty string, which clears the preference). An
// unknown value is refused. The change is committed only if the record kept
// the revision it was read at, retrying a bounded number of times, and is
// logged on the decision timeline.
func (m *Manager) SetPRDLanePreference(prdID, pref string) error {
	p, ok := NormalizeLanePreference(pref)
	if !ok {
		return fmt.Errorf("unknown lane preference %q: use gpu, claude or any", strings.TrimSpace(pref))
	}
	for attempt := 0; attempt < kindSetAttempts; attempt++ {
		rev, found := m.store.PRDRev(prdID)
		if !found {
			return fmt.Errorf("prd %q not found", prdID)
		}
		err := m.store.CommitPRDIfRev(prdID, rev, func(prd *PRD) bool {
			from := effectiveLanePreference(prd.LanePreference)
			prd.LanePreference = p
			prd.Decisions = append(prd.Decisions, Decision{
				At: time.Now(), Kind: "set_lane_preference", Actor: "operator",
				Note: fmt.Sprintf("lane preference %s -> %s", from, effectiveLanePreference(p)),
			})
			return true
		})
		if !errors.Is(err, ErrPRDStale) {
			return err
		}
	}
	return ErrPRDStale
}
