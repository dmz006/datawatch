package burnguard

import (
	"fmt"
	"strings"
	"time"
)

// Rung names. They match the values the usage.burn_guard.ladder setting accepts.
const (
	RungLowerInflight = "lower_inflight"
	RungRoutePiPrompt = "route_pi_prompt"
	RungHoldClaude    = "hold_claude"
)

// ClaudeBackend and PiBackend are the backends the default rungs act on.
const (
	ClaudeBackend = "claude-code"
	PiBackend     = "pi-prompt"
)

// ActionKind is what the daemon is asked to change. Every kind acts on admission only (how
// many sessions may start, where new work goes, whether new work waits). There is no kind
// that ends, kills or restarts a running session, and none may be added: a running session
// is never touched by the ladder.
type ActionKind string

const (
	ActionSetInflight  ActionKind = "set_inflight"
	ActionRouteBackend ActionKind = "route_backend"
	ActionHoldNewWork  ActionKind = "hold_new_work"
)

// ActionKinds lists every kind a Decision may carry. A release is the same kind with
// Release set.
func ActionKinds() []ActionKind {
	return []ActionKind{ActionSetInflight, ActionRouteBackend, ActionHoldNewWork}
}

// Action is one admission-side change.
type Action struct {
	Kind ActionKind `json:"kind"`
	// Release is true when the action undoes an earlier one: the inflight cap goes back to
	// Value, the route or the hold is lifted.
	Release bool `json:"release,omitempty"`
	// Backend is the backend whose admission changes; To is the backend new work is routed
	// to (route_backend only).
	Backend string `json:"backend,omitempty"`
	To      string `json:"to,omitempty"`
	// Value is the inflight cap to set (set_inflight only).
	Value int `json:"value,omitempty"`
}

// Direction is what the tick did to the ladder.
type Direction string

const (
	DirectionHold     Direction = "hold"
	DirectionClimb    Direction = "climb"
	DirectionStepBack Direction = "step_back"
	DirectionDisabled Direction = "disabled"
)

// LadderSettings is what the ladder reads from usage.burn_guard.
type LadderSettings struct {
	Enabled bool
	// Ladder is the ordered rungs; empty means the default ladder.
	Ladder []string
	// StepBack lets the ladder release rungs once the projection is under.
	StepBack bool
	// MarginPoints is the band around the target inside which the ladder holds.
	MarginPoints float64
}

// DefaultLadder is the ladder used when none is configured.
func DefaultLadder() []string {
	return []string{RungLowerInflight, RungRoutePiPrompt, RungHoldClaude}
}

// LadderState is what survives a restart.
type LadderState struct {
	// Applied lists the rungs now in force, in the order they were applied.
	Applied []string `json:"applied,omitempty"`
	// LastChange is when a rung was last applied or released.
	LastChange time.Time `json:"last_change,omitempty"`
	// OriginalInflight is the claude-code inflight cap before lower_inflight was applied;
	// it is what releasing the rung restores. Zero when that rung is not in force.
	OriginalInflight int `json:"original_inflight,omitempty"`
}

// StateStore is how the daemon saves the state. Load returns the zero state when nothing
// has been saved.
type StateStore interface {
	Load() (LadderState, error)
	Save(LadderState) error
}

// TickInput is what one tick reads.
type TickInput struct {
	Projection Projection
	Settings   LadderSettings
	Now        time.Time
	// InflightCap is the claude-code inflight cap now in force. It is read only when
	// lower_inflight is applied, to record the original.
	InflightCap int
}

// Decision is the result of a tick.
type Decision struct {
	Direction Direction `json:"direction"`
	// Rung is the rung applied or released; empty on a hold.
	Rung string `json:"rung,omitempty"`
	// Level is the number of rungs in force after the tick; Previous before it.
	Level    int `json:"level"`
	Previous int `json:"previous"`
	// Applied lists the rungs in force after the tick.
	Applied []string `json:"applied,omitempty"`
	Actions []Action `json:"actions,omitempty"`

	Projected      float64 `json:"projected"`
	Line           float64 `json:"line"`
	OverBy         float64 `json:"over_by"`
	PointsPerHour  float64 `json:"points_per_hour"`
	AllowedPerHour float64 `json:"allowed_per_hour"`

	// Changed is true when the tick applied or released a rung.
	Changed bool `json:"changed"`
	// AlertText and QuestionText state what was done; both are empty on a hold that changed
	// nothing, and Reason says why the ladder held.
	AlertText    string `json:"alert_text,omitempty"`
	QuestionText string `json:"question_text,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// Ladder is the state machine. It is not safe for concurrent use; the daemon calls Tick from
// one goroutine.
type Ladder struct {
	store StateStore
	state LadderState
}

// NewLadder loads the saved state from store (which may be nil for no persistence).
func NewLadder(store StateStore) (*Ladder, error) {
	l := &Ladder{store: store}
	if store != nil {
		s, err := store.Load()
		if err != nil {
			return nil, fmt.Errorf("burnguard: load ladder state: %w", err)
		}
		l.state = s
	}
	return l, nil
}

// State returns a copy of the current state.
func (l *Ladder) State() LadderState {
	s := l.state
	s.Applied = append([]string(nil), s.Applied...)
	return s
}

// Tick advances the ladder by at most one rung. The projection is compared with its target
// (the ceiling less the projection margin): more than MarginPoints above climbs one rung,
// more than MarginPoints below steps back one rung when StepBack is on, anything between
// holds. A projection with no usable readings holds. A disabled ladder releases whatever is
// in force at once (so admission is never left restricted by a guard that is off) and then
// does nothing. The returned error is a failure to save; the decision is still valid and the
// in-memory state has moved.
func (l *Ladder) Tick(in TickInput) (Decision, error) {
	prev := len(l.state.Applied)
	d := Decision{Direction: DirectionHold, Previous: prev, Level: prev}
	p := in.Projection
	d.Projected, d.Line, d.OverBy = p.Projected, p.Target, p.OverBy
	d.PointsPerHour, d.AllowedPerHour = p.PointsPerHour, p.AllowedPerHour

	finish := func() (Decision, error) {
		d.Level = len(l.state.Applied)
		d.Applied = append([]string(nil), l.state.Applied...)
		if !d.Changed {
			return d, nil
		}
		l.state.LastChange = in.Now
		if l.store != nil {
			if err := l.store.Save(l.State()); err != nil {
				return d, fmt.Errorf("burnguard: save ladder state: %w", err)
			}
		}
		return d, nil
	}

	if !in.Settings.Enabled {
		d.Direction = DirectionDisabled
		if prev == 0 {
			d.Reason = "ladder disabled"
			return finish()
		}
		released := append([]string(nil), l.state.Applied...)
		for len(l.state.Applied) > 0 {
			d.Actions = append(d.Actions, l.release()...)
		}
		d.Changed, d.Rung = true, released[len(released)-1]
		d.Reason = "ladder disabled"
		d.AlertText = fmt.Sprintf("Burn guard turned off: released %s.", strings.Join(reverse(released), ", "))
		d.QuestionText = d.AlertText + " New work is admitted as before."
		return finish()
	}
	if p.Needs != "" {
		d.Reason = "no usable projection: " + p.Needs
		return finish()
	}

	ladder := in.Settings.Ladder
	if len(ladder) == 0 {
		ladder = DefaultLadder()
	}
	margin := in.Settings.MarginPoints
	if margin < 0 {
		margin = 0
	}
	switch {
	case p.OverBy > margin:
		rung, ok := nextRung(ladder, l.state.Applied)
		if !ok {
			d.Reason = "over the line, already at the last rung"
			return finish()
		}
		acts := l.apply(rung, in.InflightCap)
		d.Direction, d.Rung, d.Actions, d.Changed = DirectionClimb, rung, acts, true
		what := describe(acts, l.state.OriginalInflight)
		d.AlertText = fmt.Sprintf("Burn guard stepped up to rung %d of %d (%s): %s %s",
			len(l.state.Applied), len(ladder), rung, what, numbers(p))
		d.QuestionText = fmt.Sprintf("The weekly burn projects to %.1f%% at the reset against a target of %.1f%%. I did this: %s Release it, or leave it?",
			p.Projected, p.Target, what)
	case p.OverBy < -margin && in.Settings.StepBack && prev > 0:
		rung := l.state.Applied[prev-1]
		acts := l.release()
		d.Direction, d.Rung, d.Actions, d.Changed = DirectionStepBack, rung, acts, true
		what := describe(acts, 0)
		d.AlertText = fmt.Sprintf("Burn guard stepped back from %s: %s %s", rung, what, numbers(p))
		d.QuestionText = fmt.Sprintf("The weekly burn now projects to %.1f%% against a target of %.1f%%. I did this: %s", p.Projected, p.Target, what)
	case p.OverBy < -margin && prev > 0:
		d.Reason = "under the line, step back is off"
	case p.OverBy > margin:
		d.Reason = "over the line"
	default:
		d.Reason = "within the margin"
	}
	return finish()
}

// nextRung is the first rung of the ladder not yet applied, preferring the one right after a
// prefix of what is applied.
func nextRung(ladder, applied []string) (string, bool) {
	in := map[string]bool{}
	for _, a := range applied {
		in[a] = true
	}
	if n := len(applied); n < len(ladder) && !in[ladder[n]] {
		return ladder[n], true
	}
	for _, r := range ladder {
		if !in[r] {
			return r, true
		}
	}
	return "", false
}

// apply puts a rung in force and returns its admission actions.
func (l *Ladder) apply(rung string, inflight int) []Action {
	l.state.Applied = append(l.state.Applied, rung)
	switch rung {
	case RungLowerInflight:
		l.state.OriginalInflight = inflight
		return []Action{{Kind: ActionSetInflight, Backend: ClaudeBackend, Value: lowered(inflight)}}
	case RungRoutePiPrompt:
		return []Action{{Kind: ActionRouteBackend, Backend: ClaudeBackend, To: PiBackend}}
	case RungHoldClaude:
		return []Action{{Kind: ActionHoldNewWork, Backend: ClaudeBackend}}
	}
	return nil
}

// release lifts the newest rung and returns the actions that undo it.
func (l *Ladder) release() []Action {
	n := len(l.state.Applied)
	rung := l.state.Applied[n-1]
	l.state.Applied = l.state.Applied[:n-1]
	switch rung {
	case RungLowerInflight:
		orig := l.state.OriginalInflight
		l.state.OriginalInflight = 0
		return []Action{{Kind: ActionSetInflight, Release: true, Backend: ClaudeBackend, Value: orig}}
	case RungRoutePiPrompt:
		return []Action{{Kind: ActionRouteBackend, Release: true, Backend: ClaudeBackend, To: PiBackend}}
	case RungHoldClaude:
		return []Action{{Kind: ActionHoldNewWork, Release: true, Backend: ClaudeBackend}}
	}
	return nil
}

// lowered is the inflight cap one below cap, never below one.
func lowered(cap int) int {
	if cap <= 1 {
		return 1
	}
	return cap - 1
}

func describe(acts []Action, original int) string {
	var parts []string
	for _, a := range acts {
		switch {
		case a.Kind == ActionSetInflight && !a.Release:
			parts = append(parts, fmt.Sprintf("lowered the %s inflight cap from %d to %d.", a.Backend, original, a.Value))
		case a.Kind == ActionSetInflight:
			parts = append(parts, fmt.Sprintf("restored the %s inflight cap to %d.", a.Backend, a.Value))
		case a.Kind == ActionRouteBackend && !a.Release:
			parts = append(parts, fmt.Sprintf("routed new %s work to %s.", a.Backend, a.To))
		case a.Kind == ActionRouteBackend:
			parts = append(parts, fmt.Sprintf("stopped routing new %s work to %s.", a.Backend, a.To))
		case a.Kind == ActionHoldNewWork && !a.Release:
			parts = append(parts, fmt.Sprintf("holding new %s work; running sessions continue.", a.Backend))
		case a.Kind == ActionHoldNewWork:
			parts = append(parts, fmt.Sprintf("released the hold on new %s work.", a.Backend))
		}
	}
	return strings.Join(parts, " ")
}

func numbers(p Projection) string {
	return fmt.Sprintf("(projected %.1f%% at the reset, target %.1f%%, burning %.2f points/hour against %.2f allowed)",
		p.Projected, p.Target, p.PointsPerHour, p.AllowedPerHour)
}

func reverse(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}
