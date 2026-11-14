// The held group read: the Automata an operator put on hold, shown as their
// own group beside the failed and blocked ones. The supervisor watch, the
// triage read and the dashboard all read the group from here so each shows
// the same reason and the same look-again wording.
//
// Expiry of the look-again time never changes the status. An expired hold
// stays held; the view marks it expired so the operator sees it first.

package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Look-again wording an operator reads on a held Automaton.
const (
	heldNextLookNoDate  = "no date set"
	heldNextLookExpired = "hold expired, ready to review"
	heldNextLookPrefix  = "looked at again on "
	heldNextLookDateFmt = "2006-01-02"
)

// HeldView is one held Automaton as the operator reads it.
type HeldView struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Project      string `json:"project,omitempty"`
	AutomationID string `json:"automation_id,omitempty"`
	Reason       string `json:"reason"`
	Condition    string `json:"condition,omitempty"`
	// Until is the look-again time the operator set, when one was set.
	Until *time.Time `json:"until,omitempty"`
	// HeldAt is when the hold was placed.
	HeldAt *time.Time `json:"held_at,omitempty"`
	HeldBy string     `json:"held_by,omitempty"`
	// From is the status the Automaton had when it was held; a release
	// restores it.
	From string `json:"from,omitempty"`
	// Expired is true once the look-again time has passed (or the hold
	// carries its expiry mark). The Automaton is still held.
	Expired bool `json:"expired"`
	// NextLook is Until in RFC3339, empty when no date was set.
	NextLook string `json:"next_look"`
	// NextLookText is the look-again line: "looked at again on <date>",
	// "no date set" or "hold expired, ready to review".
	NextLookText string `json:"next_look_text"`
}

// HeldGroup is the held group read: every held Automaton, expired first,
// with the total and the expired count. Held is never nil.
type HeldGroup struct {
	Held    []HeldView `json:"held"`
	Total   int        `json:"total"`
	Expired int        `json:"expired"`
}

// emptyHeldGroup is a group with no held Automata and a non-nil list.
func emptyHeldGroup() HeldGroup {
	return HeldGroup{Held: []HeldView{}}
}

// NewHeldView builds the view of one held Automaton as of now. An Automaton
// that carries no hold record still builds a view, with an empty reason, so
// a damaged record is visible rather than dropped.
func NewHeldView(p *PRD, now time.Time) HeldView {
	v := HeldView{}
	if p == nil {
		return v
	}
	v.ID = p.ID
	v.Title = p.Title
	v.Project = p.Project
	if v.Project == "" {
		v.Project = p.ProjectDir
	}
	v.AutomationID = p.AutomationID
	h := p.Hold
	if h != nil {
		v.Reason = h.Reason
		v.Condition = h.Condition
		v.HeldBy = h.HeldBy
		v.From = string(h.From)
		if !h.HeldAt.IsZero() {
			t := h.HeldAt
			v.HeldAt = &t
		}
		if h.Until != nil {
			u := *h.Until
			v.Until = &u
			v.NextLook = u.UTC().Format(time.RFC3339)
		}
		v.Expired = h.ExpiredAt != nil || (h.Until != nil && !h.Until.After(now))
	}
	switch {
	case v.Expired:
		v.NextLookText = heldNextLookExpired
	case v.Until != nil:
		v.NextLookText = heldNextLookPrefix + v.Until.UTC().Format(heldNextLookDateFmt)
	default:
		v.NextLookText = heldNextLookNoDate
	}
	return v
}

// BuildHeldGroup builds the held group from a list of Automata as of now.
// Automata whose status is not held are skipped. The order is expired first,
// then the soonest look-again time (an undated hold after the dated ones),
// then the oldest hold, then the id so the order never wobbles.
func BuildHeldGroup(prds []*PRD, now time.Time) HeldGroup {
	g := emptyHeldGroup()
	for _, p := range prds {
		if p == nil || p.Status != PRDHeld {
			continue
		}
		g.Held = append(g.Held, NewHeldView(p, now))
	}
	sort.SliceStable(g.Held, func(i, j int) bool { return heldViewBefore(g.Held[i], g.Held[j]) })
	g.Total = len(g.Held)
	for _, v := range g.Held {
		if v.Expired {
			g.Expired++
		}
	}
	return g
}

// heldViewBefore is the held group order.
func heldViewBefore(a, b HeldView) bool {
	if a.Expired != b.Expired {
		return a.Expired
	}
	switch {
	case a.Until != nil && b.Until == nil:
		return true
	case a.Until == nil && b.Until != nil:
		return false
	case a.Until != nil && b.Until != nil && !a.Until.Equal(*b.Until):
		return a.Until.Before(*b.Until)
	}
	ah, bh := heldAtOrZero(a), heldAtOrZero(b)
	if !ah.Equal(bh) {
		return ah.Before(bh)
	}
	return a.ID < b.ID
}

func heldAtOrZero(v HeldView) time.Time {
	if v.HeldAt == nil {
		return time.Time{}
	}
	return *v.HeldAt
}

// HeldGroup reads the held group from the store as of now.
func (m *Manager) HeldGroup(now time.Time) HeldGroup {
	if m == nil || m.store == nil {
		return emptyHeldGroup()
	}
	return BuildHeldGroup(m.store.ListPRDs(), now)
}

// HeldAutomata returns the held group: every held Automaton with its reason
// and look-again line, expired first. The lists are never nil.
func (a *API) HeldAutomata() HeldGroup {
	return a.M.HeldGroup(a.M.holdNow())
}

// renderHeldGroupLines words the held group for the supervisor watch and the
// triage read: a heading with the counts, then one line per held Automaton
// with its reason and look-again line. It returns nothing for an empty group.
func renderHeldGroupLines(g HeldGroup) string {
	if len(g.Held) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Held by the operator: %d Automat%s", len(g.Held), pluralN(len(g.Held)))
	if g.Expired > 0 {
		fmt.Fprintf(&b, " (%d expired)", g.Expired)
	}
	b.WriteString("\n")
	for _, v := range g.Held {
		name := v.Title
		if name == "" {
			name = v.ID
		}
		fmt.Fprintf(&b, "  %s (id %s): %s — %s\n", name, v.ID, oneLine(v.Reason), v.NextLookText)
		if v.Condition != "" {
			fmt.Fprintf(&b, "    Condition: %s\n", oneLine(v.Condition))
		}
	}
	return b.String()
}
