package burnguard

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

type memStore struct {
	s     LadderState
	saves int
	err   error
}

func (m *memStore) Load() (LadderState, error) { return m.s, nil }
func (m *memStore) Save(s LadderState) error {
	if m.err != nil {
		return m.err
	}
	m.s, m.saves = s, m.saves+1
	return nil
}

func settings() LadderSettings {
	return LadderSettings{Enabled: true, StepBack: true, MarginPoints: 2, AskOnChange: true}
}

func proj(overBy float64) Projection {
	return Projection{Projected: 98 + overBy, Target: 98, Ceiling: 100, Margin: 2, OverBy: overBy,
		Over: overBy > 0, PointsPerHour: 1.7, AllowedPerHour: 0.6}
}

func tick(t *testing.T, l *Ladder, p Projection, s LadderSettings, at time.Time) Decision {
	t.Helper()
	d, err := l.Tick(TickInput{Projection: p, Settings: s, Now: at, InflightCap: 3})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBurnGuardAskOnChangeFalseRaisesNoQuestion(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	s := settings()
	s.AskOnChange = false
	at := time.Now()
	for i := 0; i < 3; i++ {
		d := tick(t, l, proj(20), s, at)
		if d.Direction != DirectionClimb || d.QuestionText != "" || d.AlertText == "" {
			t.Fatalf("climb %d: %+v", i, d)
		}
	}
	want := []string{RungHoldClaude, RungRoutePiPrompt, RungLowerInflight}
	for i, r := range want {
		d := tick(t, l, proj(-10), s, at)
		if d.Direction != DirectionStepBack || d.Rung != r || d.QuestionText != "" || d.AlertText == "" {
			t.Fatalf("step back %d: %+v", i, d)
		}
	}
}

func TestBurnGuardAskOnChangeTrueQuestionsEachClimbAndStepBack(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	at := time.Now()
	for i := 0; i < 3; i++ {
		d := tick(t, l, proj(20), settings(), at)
		if d.Direction != DirectionClimb || d.QuestionText == "" || !strings.Contains(d.QuestionText, "I did this") {
			t.Fatalf("climb %d: %+v", i, d)
		}
	}
	d := tick(t, l, proj(-10), settings(), at)
	if d.Direction != DirectionStepBack || d.QuestionText == "" || d.QuestionText == d.AlertText {
		t.Fatalf("step back: %+v", d)
	}
	// a disabled release raises no question even with ask_on_change on
	s := settings()
	s.Enabled = false
	d = tick(t, l, proj(50), s, at)
	if d.Direction != DirectionDisabled || d.QuestionText != "" || d.AlertText == "" {
		t.Fatalf("disabled: %+v", d)
	}
}

func TestBurnGuardAskOnChangeFalseOnNoOpStepAndOnRelease(t *testing.T) {
	// a climb that cannot lower the cap (already 1, so 1 to 1) changes no setting
	l, _ := NewLadder(&memStore{})
	at := time.Now()
	s := settings()
	s.Ladder = []string{RungLowerInflight}
	d, err := l.Tick(TickInput{Projection: proj(20), Settings: s, Now: at, InflightCap: 1})
	if err != nil {
		t.Fatal(err)
	}
	if d.Direction != DirectionClimb || !d.Changed || d.QuestionText != "" || d.AlertText == "" {
		t.Fatalf("no-op step: %+v", d)
	}
	// its release restores nothing either
	d, err = l.Tick(TickInput{Projection: proj(-10), Settings: s, Now: at, InflightCap: 1})
	if err != nil {
		t.Fatal(err)
	}
	if d.Direction != DirectionStepBack || !d.Changed || d.QuestionText != "" || d.AlertText == "" {
		t.Fatalf("no-op release: %+v", d)
	}
	// a disabled release never questions, even with ask_on_change on
	l, _ = NewLadder(&memStore{})
	tick(t, l, proj(20), settings(), at)
	s = settings()
	s.Enabled = false
	if d := tick(t, l, proj(50), s, at); d.Direction != DirectionDisabled || d.QuestionText != "" || d.AlertText == "" {
		t.Fatalf("disabled release: %+v", d)
	}
}

func TestBurnGuardAskOnChangeWithdrawOnEveryChangedStep(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	at := time.Now()
	if d := tick(t, l, proj(20), settings(), at); d.Withdraw != d.Changed || !d.Withdraw {
		t.Fatalf("climb: %+v", d)
	}
	// holds never withdraw
	if d := tick(t, l, proj(0), settings(), at); d.Withdraw || d.Changed || d.Direction != DirectionHold {
		t.Fatalf("hold: %+v", d)
	}
	t0 := tick(t, l, proj(20), settings(), at)
	t1 := tick(t, l, proj(20), settings(), at)
	if t0.Level != 2 || t1.Level != 3 {
		t.Fatalf("levels: %+v / %+v", t0, t1)
	}
	if d := tick(t, l, proj(20), settings(), at); d.Withdraw || d.Changed {
		t.Fatalf("hold at the last rung: %+v", d)
	}
	if d := tick(t, l, proj(-10), settings(), at); d.Withdraw != d.Changed || !d.Withdraw {
		t.Fatalf("step back: %+v", d)
	}
	s := settings()
	s.Enabled = false
	if d := tick(t, l, proj(50), s, at); d.Withdraw != d.Changed || !d.Withdraw {
		t.Fatalf("disabled release: %+v", d)
	}
}

func TestBurnGuardAskOnChangeAlertNamesProjectionTargetAndSetting(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	s := settings()
	s.AskOnChange = false
	d := tick(t, l, proj(20), s, time.Now())
	for _, want := range []string{"guard 118.0%", "target 98.0%", "usage.burn_guard.ask_on_change"} {
		if !strings.Contains(d.AlertText, want) {
			t.Fatalf("climb alert %q missing %q", d.AlertText, want)
		}
	}
	d = tick(t, l, proj(-10), s, time.Now())
	for _, want := range []string{"guard 88.0%", "target 98.0%", "usage.burn_guard.ask_on_change"} {
		if !strings.Contains(d.AlertText, want) {
			t.Fatalf("step back alert %q missing %q", d.AlertText, want)
		}
	}
	// with ask_on_change on the alert stops carrying the hint
	l, _ = NewLadder(&memStore{})
	if d := tick(t, l, proj(20), settings(), time.Now()); strings.Contains(d.AlertText, "ask_on_change") {
		t.Fatalf("alert should not mention the setting while questions are on: %q", d.AlertText)
	}
}

func TestActionKindsAreAdmissionOnly(t *testing.T) {
	got := ActionKinds()
	want := []ActionKind{"set_inflight", "route_backend", "hold_new_work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	// No ActionKind constant outside the three may be declared in the package source.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "ladder.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		vs, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if id, ok := vs.Type.(*ast.Ident); ok && id.Name == "ActionKind" {
			n += len(vs.Names)
		}
		return true
	})
	if n != 3 {
		t.Fatalf("%d ActionKind constants declared, want 3", n)
	}
	for _, k := range got {
		if s := string(k); strings.Contains(s, "kill") || strings.HasPrefix(s, "end") || strings.Contains(s, "stop") {
			t.Fatalf("kind %q acts on running sessions", s)
		}
	}
}

func TestClimbsOneRungPerTickThenHolds(t *testing.T) {
	st := &memStore{}
	l, _ := NewLadder(st)
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	d := tick(t, l, proj(20), settings(), at)
	if d.Direction != DirectionClimb || d.Rung != RungLowerInflight || d.Level != 1 {
		t.Fatalf("tick 1: %+v", d)
	}
	if a := d.Actions[0]; a.Kind != ActionSetInflight || a.Value != 2 || a.Release {
		t.Fatalf("action %+v", a)
	}
	if !strings.Contains(d.AlertText, "from 3 to 2") || !strings.Contains(d.QuestionText, "I did this") {
		t.Fatalf("texts: %q / %q", d.AlertText, d.QuestionText)
	}
	d = tick(t, l, proj(20), settings(), at.Add(time.Hour))
	if d.Rung != RungRoutePiPrompt || d.Actions[0].Kind != ActionRouteBackend || d.Actions[0].To != PiBackend {
		t.Fatalf("tick 2: %+v", d)
	}
	d = tick(t, l, proj(20), settings(), at.Add(2*time.Hour))
	if d.Rung != RungHoldClaude || d.Actions[0].Kind != ActionHoldNewWork {
		t.Fatalf("tick 3: %+v", d)
	}
	d = tick(t, l, proj(20), settings(), at.Add(3*time.Hour))
	if d.Changed || d.Level != 3 || d.Direction != DirectionHold || d.AlertText != "" {
		t.Fatalf("tick 4 should hold: %+v", d)
	}
	if st.saves != 3 || st.s.OriginalInflight != 3 || !st.s.LastChange.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("saved %d times, state %+v", st.saves, st.s)
	}
}

func TestMarginHolds(t *testing.T) {
	l, _ := NewLadder(nil)
	at := time.Now()
	for _, o := range []float64{2, 1, 0, -1, -2} {
		if d := tick(t, l, proj(o), settings(), at); d.Changed || !strings.HasPrefix(d.Reason, "within the margin") {
			t.Fatalf("overBy %v: %+v", o, d)
		}
	}
	tick(t, l, proj(5), settings(), at)
	for _, o := range []float64{2, 0, -2} {
		if d := tick(t, l, proj(o), settings(), at); d.Changed {
			t.Fatalf("overBy %v changed: %+v", o, d)
		}
	}
}

func TestStepsBackOneRungPerTick(t *testing.T) {
	l, _ := NewLadder(nil)
	at := time.Now()
	for i := 0; i < 3; i++ {
		tick(t, l, proj(20), settings(), at)
	}
	want := []string{RungHoldClaude, RungRoutePiPrompt, RungLowerInflight}
	for i, r := range want {
		d := tick(t, l, proj(-10), settings(), at)
		if d.Direction != DirectionStepBack || d.Rung != r || d.Level != 2-i || !d.Actions[0].Release {
			t.Fatalf("step %d: %+v", i, d)
		}
	}
	// the last release restores the original inflight cap
	if l.State().OriginalInflight != 0 || len(l.State().Applied) != 0 {
		t.Fatalf("state %+v", l.State())
	}
	if d := tick(t, l, proj(-10), settings(), at); d.Changed {
		t.Fatalf("empty ladder changed: %+v", d)
	}
}

func TestInflightRestoreValue(t *testing.T) {
	l, _ := NewLadder(nil)
	tick(t, l, proj(20), settings(), time.Now())
	d := tick(t, l, proj(-10), settings(), time.Now())
	if a := d.Actions[0]; a.Kind != ActionSetInflight || !a.Release || a.Value != 3 {
		t.Fatalf("%+v", a)
	}
}

func TestStepBackOff(t *testing.T) {
	l, _ := NewLadder(nil)
	s := settings()
	s.StepBack = false
	tick(t, l, proj(20), s, time.Now())
	d := tick(t, l, proj(-20), s, time.Now())
	if d.Changed || d.Level != 1 {
		t.Fatalf("%+v", d)
	}
}

func TestDisabledLadder(t *testing.T) {
	l, _ := NewLadder(nil)
	s := settings()
	s.Enabled = false
	if d := tick(t, l, proj(50), s, time.Now()); d.Changed || d.Direction != DirectionDisabled || d.Level != 0 {
		t.Fatalf("%+v", d)
	}
	// turned off while rungs are in force: they are all released
	l, _ = NewLadder(nil)
	tick(t, l, proj(20), settings(), time.Now())
	tick(t, l, proj(20), settings(), time.Now())
	d := tick(t, l, proj(50), s, time.Now())
	if !d.Changed || d.Level != 0 || len(d.Actions) != 2 || !d.Actions[0].Release || !d.Actions[1].Release {
		t.Fatalf("%+v", d)
	}
	if d.Actions[0].Kind != ActionRouteBackend || d.Actions[1].Value != 3 {
		t.Fatalf("release order: %+v", d.Actions)
	}
}

func TestOneRungLadder(t *testing.T) {
	l, _ := NewLadder(nil)
	s := settings()
	s.Ladder = []string{RungHoldClaude}
	d := tick(t, l, proj(30), s, time.Now())
	if d.Rung != RungHoldClaude || d.Level != 1 {
		t.Fatalf("%+v", d)
	}
	d = tick(t, l, proj(30), s, time.Now())
	if d.Changed || d.Level != 1 || d.Reason == "" {
		t.Fatalf("%+v", d)
	}
	d = tick(t, l, proj(-30), s, time.Now())
	if d.Rung != RungHoldClaude || d.Level != 0 || !d.Actions[0].Release {
		t.Fatalf("%+v", d)
	}
}

func TestNeedsHolds(t *testing.T) {
	l, _ := NewLadder(nil)
	tick(t, l, proj(20), settings(), time.Now())
	d := tick(t, l, Projection{Needs: "reading"}, settings(), time.Now())
	if d.Changed || d.Level != 1 || !strings.Contains(d.Reason, "reading") {
		t.Fatalf("%+v", d)
	}
}

func TestRestartWithPersistedState(t *testing.T) {
	st := &memStore{}
	l, _ := NewLadder(st)
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tick(t, l, proj(20), settings(), at)
	tick(t, l, proj(20), settings(), at.Add(time.Hour))

	// the daemon restarts: a new ladder over the same store, with a different current cap
	l2, err := NewLadder(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := l2.State(); len(got.Applied) != 2 || got.OriginalInflight != 3 {
		t.Fatalf("loaded %+v", got)
	}
	d, _ := l2.Tick(TickInput{Projection: proj(20), Settings: settings(), Now: at.Add(2 * time.Hour), InflightCap: 2})
	if d.Rung != RungHoldClaude || d.Previous != 2 {
		t.Fatalf("%+v", d)
	}
	for i := 0; i < 2; i++ {
		d, _ = l2.Tick(TickInput{Projection: proj(-10), Settings: settings(), Now: at, InflightCap: 2})
	}
	if a := d.Actions[0]; a.Kind != ActionRouteBackend && a.Kind != ActionSetInflight {
		t.Fatalf("%+v", a)
	}
	d, _ = l2.Tick(TickInput{Projection: proj(-10), Settings: settings(), Now: at, InflightCap: 2})
	if a := d.Actions[0]; a.Kind != ActionSetInflight || a.Value != 3 {
		t.Fatalf("original cap not restored from persisted state: %+v", a)
	}
}

func TestSaveErrorStillDecides(t *testing.T) {
	st := &memStore{err: errors.New("disk full")}
	l, _ := NewLadder(st)
	d, err := l.Tick(TickInput{Projection: proj(20), Settings: settings(), Now: time.Now(), InflightCap: 3})
	if err == nil || !d.Changed || d.Level != 1 {
		t.Fatalf("err=%v d=%+v", err, d)
	}
}

func TestFridayNightReplayThroughLadder(t *testing.T) {
	f := loadFixture(t)
	loc := ny(t)
	limitHit := time.Date(2026, 10, 31, 5, 30, 0, 0, loc)
	l, _ := NewLadder(&memStore{})

	var climbs []time.Time
	levelAtLimit := 0
	for now := time.Date(2026, 10, 29, 10, 0, 0, 0, loc); now.Before(limitHit); now = now.Add(time.Hour) {
		p := Project(ProjectInput{Readings: readingsUpTo(f.Readings, now), Reset: f.Reset, Now: now,
			Buckets: f.Buckets, Coef: f.Coefficients})
		d, _ := l.Tick(TickInput{Projection: p, Settings: settings(), Now: now, InflightCap: 3})
		if d.Direction == DirectionClimb {
			climbs = append(climbs, now)
		}
		levelAtLimit = d.Level
	}
	if len(climbs) < 3 || levelAtLimit != 3 {
		t.Fatalf("climbs %v, level at the limit %d", climbs, levelAtLimit)
	}
	// the ladder reached its top rung well before the limit and was holding there at the end
	if lead := limitHit.Sub(climbs[len(climbs)-1]); lead < 2*time.Hour {
		t.Fatalf("the last climb came only %v before the limit", lead)
	}

	// The burn eases: the same replay's projection with the burn removed steps back, one a tick.
	now := limitHit
	under := Projection{Projected: 60, Target: 98, OverBy: -38, Ceiling: 100, Margin: 2}
	for i, want := range []string{RungHoldClaude, RungRoutePiPrompt, RungLowerInflight} {
		d, _ := l.Tick(TickInput{Projection: under, Settings: settings(), Now: now.Add(time.Duration(i) * time.Hour), InflightCap: 2})
		if d.Direction != DirectionStepBack || d.Rung != want {
			t.Fatalf("step back %d: %+v", i, d)
		}
	}
	if len(l.State().Applied) != 0 {
		t.Fatalf("state %+v", l.State())
	}
}

// gapProj is a projection whose guard projection (173.7%) is far above its advice
// projection (57.0%), with the newest reading at readingPct against the line.
func gapProj(readingPct, line float64) Projection {
	return Projection{Projected: 173.7, AdviceProjected: 57.0, Target: 98, Ceiling: 100, Margin: 2,
		OverBy: 75.7, Over: true, Line: line, PercentAtReading: readingPct,
		PointsPerHour: 1.7, AllowedPerHour: 0.6}
}

func gapSettings(limit float64) LadderSettings {
	s := settings()
	s.MaxProjectionGapPoints = limit
	return s
}

func TestGapUsesLowerProjectionWhenGapOverLimit(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	d := tick(t, l, gapProj(50, 90), gapSettings(25), time.Now())
	if d.Changed || d.Direction != DirectionHold {
		t.Fatalf("lower projection (57%%) is under target; no rung should apply: %+v", d)
	}
	if d.GuardProjected != 173.7 || d.AdviceProjected != 57.0 || math.Abs(d.Gap-116.7) > 1e-9 {
		t.Fatalf("decision projections: guard %v advice %v gap %v", d.GuardProjected, d.AdviceProjected, d.Gap)
	}
	if d.OverBy != 57.0-98 {
		t.Fatalf("OverBy must be recomputed from the lower value, got %v", d.OverBy)
	}
	if !strings.Contains(d.Reason, "guard 173.7%, advice 57.0%, using the lower") {
		t.Fatalf("reason does not name both projections: %q", d.Reason)
	}
}

func TestGapForcesClimbWhenReadingOverLine(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	d := tick(t, l, gapProj(99, 90), gapSettings(25), time.Now())
	if d.Direction != DirectionClimb || d.Rung != RungLowerInflight || d.Changed == false {
		t.Fatalf("reading over the line must still climb one rung: %+v", d)
	}
	for _, want := range []string{"guard 173.7%, advice 57.0%, using the lower", "target 98.0%"} {
		if !strings.Contains(d.AlertText, want) {
			t.Fatalf("alert %q missing %q", d.AlertText, want)
		}
	}
	if d.QuestionText == "" {
		t.Fatalf("ask_on_change is on in settings(); a changed step must raise a question: %+v", d)
	}
}

func TestGapNamesBothProjectionsInQuestion(t *testing.T) {
	l, _ := NewLadder(&memStore{})
	d := tick(t, l, gapProj(99, 90), gapSettings(25), time.Now())
	if !strings.Contains(d.QuestionText, "guard 173.7%, advice 57.0%, using the lower") {
		t.Fatalf("question %q does not name both projections", d.QuestionText)
	}
}

func TestGapLimitZeroOrWithinLimitUsesGuard(t *testing.T) {
	for _, limit := range []float64{0, 200} {
		l, _ := NewLadder(&memStore{})
		d := tick(t, l, gapProj(50, 90), gapSettings(limit), time.Now())
		if d.Direction != DirectionClimb || d.OverBy != 75.7 {
			t.Fatalf("limit %v: guard projection should decide: %+v", limit, d)
		}
		if !strings.Contains(d.AlertText, "using the guard") {
			t.Fatalf("limit %v: alert %q should say the guard was used", limit, d.AlertText)
		}
	}
}

func TestGapLowerNeverSkipsRungWhenOverLine(t *testing.T) {
	// Lower value is over the target too: the normal climb applies, one rung per tick.
	p := gapProj(99, 90)
	p.AdviceProjected = 150
	p.Projected = 173.7
	l, _ := NewLadder(&memStore{})
	d := tick(t, l, p, gapSettings(10), time.Now())
	if d.Direction != DirectionClimb || len(d.Applied) != 1 {
		t.Fatalf("expected a single climb: %+v", d)
	}
}
