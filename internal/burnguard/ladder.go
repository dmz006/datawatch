package burnguard

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Rung names. They are the values the usage.burn_guard.ladder setting accepts. The
// names are entry-generic: a rung acts on the ladder's entry (EntryTarget), not on any
// fixed backend. route_entry routes the entry's new work to the entry's route target,
// and hold_entry holds new work on the entry (or on its held models). The first version
// of the guard named the same two rungs route_pi_prompt and hold_claude; both legacy
// names are still accepted in saved configs and saved state and normalize to the
// entry-generic names (NormalizeRung).
const (
	RungLowerInflight     = "lower_inflight"
	RungRouteEntry        = "route_entry"
	RungRouteCheaperModel = "route_cheaper_model"
	RungHoldEntry         = "hold_entry"

	// Legacy first-version rung names. They normalize to the entry-generic names above
	// (NormalizeRung) and are kept for saved configs and saved state.
	RungRoutePiPrompt = "route_pi_prompt"
	RungHoldClaude    = "hold_claude"
)

// legacyRungNames maps a legacy rung name to its entry-generic name.
var legacyRungNames = map[string]string{
	RungRoutePiPrompt: RungRouteEntry,
	RungHoldClaude:    RungHoldEntry,
}

// KnownRungs lists the entry-generic rung names.
func KnownRungs() []string {
	return []string{RungLowerInflight, RungRouteEntry, RungRouteCheaperModel, RungHoldEntry, RungRoutePiPrompt, RungHoldClaude}
}

// NormalizeRung resolves one rung name: an entry-generic name passes through, a legacy
// first-version name maps to its entry-generic one, and an unknown name is returned as
// given with ok false.
func NormalizeRung(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if n, ok := legacyRungNames[name]; ok {
		return n, true
	}
	for _, k := range KnownRungs() {
		if name == k {
			return name, true
		}
	}
	return name, false
}

// IsHoldRung reports whether rung is the hold rung in either naming: the entry-generic
// name or the legacy first-version name. Callers that branch on "is this the hold rung?"
// should use this so a saved state written under the legacy name is recognised too.
func IsHoldRung(rung string) bool {
	n, ok := NormalizeRung(rung)
	return ok && n == RungHoldEntry
}

// IsRouteRung reports whether rung is the route rung in either naming: the entry-generic
// name or the legacy first-version name.
func IsRouteRung(rung string) bool {
	n, ok := NormalizeRung(rung)
	return ok && n == RungRouteEntry
}

// IsRouteCheaperModelRung reports whether rung names the route_cheaper_model rung, which
// sends the entry's new work to its own cheaper model (the same backend, a different
// model). It has no legacy first-version name.
func IsRouteCheaperModelRung(rung string) bool {
	n, ok := NormalizeRung(rung)
	return ok && n == RungRouteCheaperModel
}

// IsLowerInflightRung reports whether rung is the lower-inflight rung.
func IsLowerInflightRung(rung string) bool {
	n, ok := NormalizeRung(rung)
	return ok && n == RungLowerInflight
}

// NormalizeRungs maps a ladder (or an applied list) to entry-generic names, keeping
// first-occurrence order and dropping unknown rungs.
func NormalizeRungs(rungs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rungs {
		n, ok := NormalizeRung(r)
		if !ok || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// EntryTarget is the paid LLM entry a ladder acts on. Every backend name an action
// carries comes from here; the ladder has no backend name of its own.
type EntryTarget struct {
	// Backend is the entry (backend) whose admission the rungs change.
	Backend string `json:"backend"`
	// RouteTo is where the route rung sends new work; empty makes the route rung a
	// rung that changes nothing.
	RouteTo string `json:"route_to,omitempty"`
	// CheaperModel is the model the route_cheaper_model rung sends the entry's new work
	// to (the same backend's cheaper model, e.g. Haiku for a Claude entry); empty makes
	// the rung one that changes nothing.
	CheaperModel string `json:"cheaper_model,omitempty"`
	// HeldModels limits the hold rung to these models; empty holds the whole entry.
	HeldModels []string `json:"held_models,omitempty"`
}

// ActionKind is what the daemon is asked to change. Every kind acts on admission only (how
// many sessions may start, where new work goes, whether new work waits). There is no kind
// that ends, kills or restarts a running session, and none may be added: a running session
// is never touched by the ladder.
type ActionKind string

const (
	ActionSetInflight  ActionKind = "set_inflight"
	ActionRouteBackend ActionKind = "route_backend"
	ActionRouteModel   ActionKind = "route_model"
	ActionHoldNewWork  ActionKind = "hold_new_work"
)

// ActionKinds lists every kind a Decision may carry. A release is the same kind with
// Release set.
func ActionKinds() []ActionKind {
	return []ActionKind{ActionSetInflight, ActionRouteBackend, ActionRouteModel, ActionHoldNewWork}
}

// Action is one admission-side change.
type Action struct {
	Kind ActionKind `json:"kind"`
	// Rung is the rung whose name the action carries: the entry-generic rung name the
	// ladder recorded it under. It is how a rung that is in force reaches the admission
	// seam (the back-off flag of a hold) with its own name, not an assumed one.
	Rung string `json:"rung,omitempty"`
	// Release is true when the action undoes an earlier one: the inflight cap goes back to
	// Value, the route or the hold is lifted.
	Release bool `json:"release,omitempty"`
	// Backend is the backend whose admission changes; To is the backend new work is routed
	// to (route_backend only); Model is the model new work runs on (route_model only,
	// the same backend).
	Backend string `json:"backend,omitempty"`
	To      string `json:"to,omitempty"`
	Model   string `json:"model,omitempty"`
	// Value is the inflight cap to set (set_inflight only).
	Value int `json:"value,omitempty"`
	// Models limits a hold to these models of the backend (hold_new_work only); empty
	// holds the whole backend.
	Models []string `json:"models,omitempty"`
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
	// MaxProjectionGapPoints is the largest allowed gap between the guard projection and
	// the advice projection. When the gap is larger, the tick decides on the lower of the
	// two; 0 means never compare (the guard projection alone decides).
	MaxProjectionGapPoints float64
	// AskOnChange gates the per-step question: true raises a question for each
	// climb or step back that changed a setting, false alerts only (the alerts
	// then carry how to turn the questions on).
	AskOnChange bool
}

// askOnOffHint is the tail of an alert when AskOnChange is off, naming the
// setting whose value would raise a question per change.
const askOnOffHint = "Set usage.burn_guard.ask_on_change to true to get a question per change."

// DefaultLadder is the ladder used when none is configured. route_cheaper_model is not
// in the default: it is a no-op until an entry resolves a cheaper model, so it is
// opted in by the ladder setting where it sits before hold_entry.
func DefaultLadder() []string {
	return []string{RungLowerInflight, RungRouteEntry, RungHoldEntry}
}

// LadderState is what survives a restart.
type LadderState struct {
	// Applied lists the rungs now in force, in the order they were applied.
	Applied []string `json:"applied,omitempty"`
	// LastChange is when a rung was last applied or released.
	LastChange time.Time `json:"last_change,omitempty"`
	// OriginalInflight is the entry's inflight cap before lower_inflight was applied;
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
	// InflightCap is the entry's inflight cap now in force. It is read only when
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

	// Projected is the guard projection (the token-priced one); GuardProjected repeats it
	// beside AdviceProjected, the readings-only projection, and Gap is their absolute
	// difference. OverBy is the excess the tick decided on: the guard's own, or the lower
	// projection's when the gap was over the limit.
	Projected       float64 `json:"projected"`
	GuardProjected  float64 `json:"guard_projected"`
	AdviceProjected float64 `json:"advice_projected"`
	Gap             float64 `json:"gap"`
	Line            float64 `json:"line"`
	OverBy          float64 `json:"over_by"`
	PointsPerHour   float64 `json:"points_per_hour"`
	AllowedPerHour  float64 `json:"allowed_per_hour"`

	// Changed is true when the tick applied or released a rung.
	Changed bool `json:"changed"`
	// Withdraw is true on every changed step, even one that raised no question:
	// the caller must withdraw the open guard question before raising a new one.
	Withdraw bool `json:"withdraw,omitempty"`
	// AlertText states what the guard did and where the projection stands against the
	// target; with AskOnChange off it carries how to turn on the per-change question.
	// QuestionText is set only when AskOnChange is on and the changed step actually
	// changed a setting; both are empty on a hold that changed nothing, and a disabled
	// release raises no question at all. Reason says why the ladder held.
	AlertText    string `json:"alert_text,omitempty"`
	QuestionText string `json:"question_text,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// changesSetting reports whether a step's actions changed an admission setting, as
// opposed to a no-op that left it where it was: a lower_inflight climb that cannot
// lower the cap (it is already at one), or its release restoring a cap that was
// never lowered. Other rungs always change the setting.
func changesSetting(acts []Action, applyCap int) bool {
	for _, a := range acts {
		if a.Kind == ActionSetInflight {
			if (a.Release && a.Value > 1) || (!a.Release && applyCap > 1) {
				return true
			}
			continue
		}
		return true
	}
	return false
}

// Ladder is the state machine. It is not safe for concurrent use; the daemon calls Tick from
// one goroutine.
type Ladder struct {
	store  StateStore
	state  LadderState
	target EntryTarget
}

// NewEntryLadder is NewLadder for a ladder that acts on target.
func NewEntryLadder(store StateStore, target EntryTarget) (*Ladder, error) {
	l, err := NewLadder(store)
	if err != nil {
		return nil, err
	}
	l.target = target
	return l, nil
}

// Target is the entry the ladder acts on.
func (l *Ladder) Target() EntryTarget { return l.target }

// SetTarget changes the entry the ladder acts on (a config change). Rungs already in
// force keep their state; their release acts on the new target.
func (l *Ladder) SetTarget(t EntryTarget) { l.target = t }

// NewLadder loads the saved state from store (which may be nil for no persistence)
// and normalizes any legacy first-version rung names in it to their entry-generic
// names, so saved state from before the rename behaves identically.
func NewLadder(store StateStore) (*Ladder, error) {
	l := &Ladder{store: store}
	if store != nil {
		s, err := store.Load()
		if err != nil {
			return nil, fmt.Errorf("burnguard: load ladder state: %w", err)
		}
		s.Applied = NormalizeRungs(s.Applied)
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
// does nothing. A question is raised only when AskOnChange is on and the changed step
// actually changed a setting; when it is off the alerts carry how to turn questions on.
// The returned error is a failure to save; the decision is still valid and the
// in-memory state has moved.
func (l *Ladder) Tick(in TickInput) (Decision, error) {
	prev := len(l.state.Applied)
	d := Decision{Direction: DirectionHold, Previous: prev, Level: prev}
	p := in.Projection
	d.Projected, d.Line, d.OverBy = p.Projected, p.Target, p.OverBy
	d.PointsPerHour, d.AllowedPerHour = p.PointsPerHour, p.AllowedPerHour
	d.GuardProjected, d.AdviceProjected = p.Projected, p.AdviceProjected
	d.Gap = math.Abs(p.Projected - p.AdviceProjected)

	finish := func() (Decision, error) {
		d.Level = len(l.state.Applied)
		d.Applied = append([]string(nil), l.state.Applied...)
		d.Withdraw = d.Changed
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
			acts, _ := l.release(in.InflightCap)
			d.Actions = append(d.Actions, acts...)
		}
		d.Changed, d.Rung = true, released[len(released)-1]
		d.Reason = "ladder disabled"
		alert := "Burn guard turned off: released " + strings.Join(reverse(released), ", ") + "."
		if p.Needs == "" {
			alert += " " + numbers(p, false)
		}
		if !in.Settings.AskOnChange {
			alert += " " + askOnOffHint
		}
		d.AlertText = alert
		return finish()
	}
	if p.Needs != "" {
		d.Reason = "no usable projection: " + p.Needs
		return finish()
	}

	ladder := NormalizeRungs(in.Settings.Ladder)
	if len(ladder) == 0 {
		ladder = DefaultLadder()
	}
	margin := in.Settings.MarginPoints
	if margin < 0 {
		margin = 0
	}
	// The gap check: when the guard and advice projections differ by more than the limit,
	// the lower of the two decides. The reading check: when the guard projection is over
	// and the newest reading is already above the pace line, the ladder still climbs one
	// rung, so the lower value can never skip a rung the guard calls for.
	overBy, lower := p.OverBy, false
	if limit := in.Settings.MaxProjectionGapPoints; limit > 0 && d.Gap > limit {
		lower = true
		overBy = math.Min(p.Projected, p.AdviceProjected) - p.Target
	}
	d.OverBy = overBy
	forced := p.OverBy > margin && p.PercentAtReading > p.Line

	switch {
	case overBy > margin || forced:
		rung, ok := nextRung(ladder, l.state.Applied)
		if !ok {
			d.Reason = "over the line, already at the last rung " + numbers(p, lower)
			return finish()
		}
		acts := l.apply(rung, in.InflightCap)
		d.Direction, d.Rung, d.Actions, d.Changed = DirectionClimb, rung, acts, true
		what := describe(acts, l.state.OriginalInflight)
		d.AlertText = fmt.Sprintf("Burn guard stepped up to rung %d of %d (%s): %s %s",
			len(l.state.Applied), len(ladder), rung, what, numbers(p, lower))
		if in.Settings.AskOnChange && changesSetting(acts, in.InflightCap) {
			d.QuestionText = fmt.Sprintf("The weekly burn projects at the reset (%s; target %.1f%%). I did this: %s Release it, or leave it?",
				projections(p, lower), p.Target, what)
		}
		if !in.Settings.AskOnChange {
			d.AlertText += " " + askOnOffHint
		}
	case overBy < -margin && in.Settings.StepBack && prev > 0:
		rung := l.state.Applied[prev-1]
		acts, kept := l.release(in.InflightCap)
		d.Direction, d.Rung, d.Actions, d.Changed = DirectionStepBack, rung, acts, true
		what := strings.TrimSpace(describe(acts, 0) + " " + kept)
		d.AlertText = fmt.Sprintf("Burn guard stepped back from %s: %s %s", rung, what, numbers(p, lower))
		if in.Settings.AskOnChange && changesSetting(acts, 0) {
			d.QuestionText = fmt.Sprintf("The weekly burn now projects at the reset (%s; target %.1f%%). I did this: %s",
				projections(p, lower), p.Target, what)
		}
		if !in.Settings.AskOnChange {
			d.AlertText += " " + askOnOffHint
		}
	case overBy < -margin && prev > 0:
		d.Reason = "under the line, step back is off " + numbers(p, lower)
	case overBy < -margin:
		d.Reason = "under the line " + numbers(p, lower)
	default:
		d.Reason = "within the margin " + numbers(p, lower)
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

// apply puts a rung in force and returns its admission actions, all on the ladder's
// entry. A route rung with no route target changes nothing.
func (l *Ladder) apply(rung string, inflight int) []Action {
	l.state.Applied = append(l.state.Applied, rung)
	t := l.target
	switch rung {
	case RungLowerInflight:
		l.state.OriginalInflight = inflight
		return []Action{{Kind: ActionSetInflight, Rung: rung, Backend: t.Backend, Value: lowered(inflight)}}
	case RungRouteEntry, RungRoutePiPrompt: // the legacy name: the same route rung
		if t.RouteTo == "" {
			return nil
		}
		return []Action{{Kind: ActionRouteBackend, Rung: rung, Backend: t.Backend, To: t.RouteTo}}
	case RungRouteCheaperModel: // no legacy name; routes to the entry's own cheaper model
		if t.CheaperModel == "" {
			return nil
		}
		return []Action{{Kind: ActionRouteModel, Rung: rung, Backend: t.Backend, Model: t.CheaperModel}}
	case RungHoldEntry, RungHoldClaude: // the legacy name: the same hold rung
		return []Action{{Kind: ActionHoldNewWork, Rung: rung, Backend: t.Backend, Models: append([]string(nil), t.HeldModels...)}}
	}
	return nil
}

// release lifts the newest rung and returns the actions that undo it. now is the
// entry's inflight cap in force at the release (zero when unknown). A
// lower_inflight release restores only what the guard itself lowered: the cap is
// put back to the original when it is still where the guard left it (one below the
// original) or already back at the original. Any other value is the operator's,
// set after the lowering, and is kept; kept then says so.
func (l *Ladder) release(now int) (acts []Action, kept string) {
	n := len(l.state.Applied)
	rung := l.state.Applied[n-1]
	l.state.Applied = l.state.Applied[:n-1]
	t := l.target
	switch rung {
	case RungLowerInflight:
		orig := l.state.OriginalInflight
		l.state.OriginalInflight = 0
		if now > 0 && now != orig && now != lowered(orig) {
			return nil, fmt.Sprintf("kept the %s inflight cap at %d: it was changed after the guard lowered it, so the guard leaves it as set.", t.Backend, now)
		}
		return []Action{{Kind: ActionSetInflight, Rung: rung, Release: true, Backend: t.Backend, Value: orig}}, ""
	case RungRouteEntry, RungRoutePiPrompt: // the legacy name: the same route rung
		if t.RouteTo == "" {
			return nil, ""
		}
		return []Action{{Kind: ActionRouteBackend, Rung: rung, Release: true, Backend: t.Backend, To: t.RouteTo}}, ""
	case RungRouteCheaperModel: // no legacy name; releases the cheaper-model route
		if t.CheaperModel == "" {
			return nil, ""
		}
		return []Action{{Kind: ActionRouteModel, Rung: rung, Release: true, Backend: t.Backend, Model: t.CheaperModel}}, ""
	case RungHoldEntry, RungHoldClaude: // the legacy name: the same hold rung
		return []Action{{Kind: ActionHoldNewWork, Rung: rung, Release: true, Backend: t.Backend, Models: append([]string(nil), t.HeldModels...)}}, ""
	}
	return nil, ""
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
		case a.Kind == ActionRouteModel && !a.Release:
			parts = append(parts, fmt.Sprintf("routing new %s work to the cheaper model %s; running sessions continue.", a.Backend, a.Model))
		case a.Kind == ActionRouteModel:
			parts = append(parts, fmt.Sprintf("stopped routing new %s work to the cheaper model %s; the original model routing is restored.", a.Backend, a.Model))
		case a.Kind == ActionHoldNewWork && !a.Release && len(a.Models) > 0:
			parts = append(parts, fmt.Sprintf("holding new %s work on models %s; running sessions continue.", a.Backend, strings.Join(a.Models, ", ")))
		case a.Kind == ActionHoldNewWork && !a.Release:
			parts = append(parts, fmt.Sprintf("holding new %s work; running sessions continue.", a.Backend))
		case a.Kind == ActionHoldNewWork:
			parts = append(parts, fmt.Sprintf("released the hold on new %s work.", a.Backend))
		}
	}
	return strings.Join(parts, " ")
}

// numbers is the parenthetical every alert and reason carries: both projections, the target
// and the burn. lower is true when the tick decided on the lower of the two projections.
func numbers(p Projection, lower bool) string {
	note := ""
	if p.Estimated {
		note = "; estimated from tokens since the weekly reset, enter a reading to correct it"
	}
	return fmt.Sprintf("(%s; target %.1f%%, burning %.2f points/hour against %.2f allowed%s)",
		projections(p, lower), p.Target, p.PointsPerHour, p.AllowedPerHour, note)
}

// projections names the guard and advice projections and which one the tick used, for
// example "guard 173.7%, advice 57.0%, using the lower".
func projections(p Projection, lower bool) string {
	using := "guard"
	if lower {
		using = "lower"
	}
	return fmt.Sprintf("guard %.1f%%, advice %.1f%%, using the %s", p.Projected, p.AdviceProjected, using)
}

func reverse(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}
