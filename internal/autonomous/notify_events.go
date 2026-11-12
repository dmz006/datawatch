// notify_events.go — notify_events lists for Automaton and guardrail
// profiles. Each Automaton (the PRD struct in models.go) and each
// GuardrailProfile carries a NotifyEvents list of lifecycle event kinds
// that produce notifications. An empty list means "inherit from the
// next tier" (Automaton > profile > global default), so the
// DefaultNotifyEvents set below is the final fallback.

package autonomous

import (
	"fmt"
	"sort"
	"strings"
)

// Notification source tiers, from most specific to least. Used by
// callers that resolve which tier supplied a resolved event kind so
// they can report the source to operators.
const (
	// NotifySourceAutomaton marks an event kind supplied by the
	// Automaton's own notify_events list.
	NotifySourceAutomaton = "automaton"
	// NotifySourceProfile marks an event kind inherited from the
	// assigned guardrail profile.
	NotifySourceProfile = "profile"
	// NotifySourceGlobal marks an event kind from the global default
	// set.
	NotifySourceGlobal = "global"
)

// ValidNotifyEventKinds returns a fresh sorted slice of every
// subscription event kind except KindPRDUpdated: prd.updated is the
// internal refresh signal, not a notification, so it cannot be named
// in a notify_events list.
func ValidNotifyEventKinds() []string {
	valid := make([]string, 0, len(KnownEventKinds))
	for k := range KnownEventKinds {
		if k == KindPRDUpdated {
			continue
		}
		valid = append(valid, k)
	}
	sort.Strings(valid)
	return valid
}

// OptInNotifyEvents returns a fresh sorted slice of every notify event
// kind that is off by default and must be listed explicitly (in an
// Automaton's or guardrail profile's notify_events, or in the global
// default_events) to produce notifications.
func OptInNotifyEvents() []string {
	opt := []string{
		KindTaskSessionStarted,
		KindTaskSessionCleared,
		KindScopeWarning,
		KindGuardrailWarn,
	}
	sort.Strings(opt)
	return opt
}

// DefaultNotifyEvents returns a fresh copy of the default notification
// set, in display order (terminal Automaton states first). The opt-in
// kinds (see OptInNotifyEvents: task session start and end, scope
// warning, guardrail warn) are deliberately absent.
func DefaultNotifyEvents() []string {
	return []string{
		PRDStatusKind(PRDNeedsReview),
		PRDStatusKind(PRDBlocked),
		PRDStatusKind(PRDFailed),
		PRDStatusKind(PRDCompleted),
		PRDStatusKind(PRDCancelled),
	}
}

// ValidateNotifyEvents checks a notify_events list. nil or an empty
// list is valid (it means "inherit"). It returns the first problem:
// an empty/whitespace-only entry, or a kind outside
// ValidNotifyEventKinds (including prd.updated).
func ValidateNotifyEvents(kinds []string) error {
	all := ValidNotifyEventKinds()
	valid := make(map[string]bool, len(all))
	for _, k := range all {
		valid[k] = true
	}
	validList := strings.Join(all, ", ")
	for i, k := range kinds {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("notify event kind %d is empty", i)
		}
		if !valid[k] {
			return fmt.Errorf("unknown notify event kind %q; valid kinds: %s", k, validList)
		}
	}
	return nil
}

// NormalizeNotifyEvents trims each entry, drops duplicates keeping
// first-seen order, and returns nil when nothing is left. Callers
// validate first (ValidateNotifyEvents), then normalize.
func NormalizeNotifyEvents(kinds []string) []string {
	seen := make(map[string]bool, len(kinds))
	out := []string{}
	for _, k := range kinds {
		t := strings.TrimSpace(k)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
