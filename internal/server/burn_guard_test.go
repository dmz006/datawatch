package server

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// failingSessions is a fake session manager. It counts RunningCount calls (so a test can
// prove the runner really consulted it) and records every call that would end a session.
// The runner is handed it as a BurnGuardSessions; TestBurnGuardCannotReachAnEndCall proves
// by static analysis that no end call can be made through anything the runner holds, and
// the behaviour tests assert endCalls stays zero.
type failingSessions struct {
	mu       sync.Mutex
	running  int
	counted  int
	endCalls []string
}

func (f *failingSessions) RunningCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counted++
	return f.running
}
func (f *failingSessions) end(what string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endCalls = append(f.endCalls, what)
}
func (f *failingSessions) Kill(string)    { f.end("kill") }
func (f *failingSessions) Stop(string)    { f.end("stop") }
func (f *failingSessions) Restart(string) { f.end("restart") }
func (f *failingSessions) End(string)     { f.end("end") }
func (f *failingSessions) KillAll()       { f.end("kill-all") }

// assertNoEnd fails the test if any end call reached the fake, or the runner never asked it.
func (f *failingSessions) assertNoEnd(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.endCalls) > 0 {
		t.Fatalf("burn guard ended a session: %v", f.endCalls)
	}
	if f.counted == 0 {
		t.Fatal("the runner never consulted the session view; the test proves nothing")
	}
}

type fakeAdmission struct {
	mu     sync.Mutex
	cap    int
	routes map[string]string
	held   map[string]bool
	calls  []string
}

func newFakeAdmission(cap int) *fakeAdmission {
	return &fakeAdmission{cap: cap, routes: map[string]string{}, held: map[string]bool{}}
}
func (a *fakeAdmission) InflightCap(string) int { a.mu.Lock(); defer a.mu.Unlock(); return a.cap }
func (a *fakeAdmission) SetInflight(b string, n int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cap = n
	a.calls = append(a.calls, fmt.Sprintf("inflight %s %d", b, n))
	return nil
}
func (a *fakeAdmission) RouteBackend(from, to string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if on {
		a.routes[from] = to
	} else {
		delete(a.routes, from)
	}
	a.calls = append(a.calls, fmt.Sprintf("route %s %s %v", from, to, on))
	return nil
}
func (a *fakeAdmission) HoldNewWork(b string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held[b] = on
	a.calls = append(a.calls, fmt.Sprintf("hold %s %v", b, on))
	return nil
}

type fakeNotifier struct {
	alerts    []string
	questions []string
}

func (n *fakeNotifier) Alert(_ alerts.Level, title, body string) {
	n.alerts = append(n.alerts, title+" | "+body)
}
func (n *fakeNotifier) Question(text string) error {
	n.questions = append(n.questions, text)
	return nil
}

// burnGuardRig builds a guard whose usage runs well over the pace line.
func burnGuardRig(t *testing.T, cfg *config.BurnGuardConfig) (*BurnGuard, *fakeAdmission, *fakeNotifier, *failingSessions) {
	t.Helper()
	now := time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	proj := filepath.Join(dir, "-example-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// 40M cache-write tokens (about 15 points) in the last hour, shortly before now.
	line := fmt.Sprintf(`{"type":"assistant","timestamp":"%s","requestId":"r1","message":{"id":"m1","usage":{"cache_creation_input_tokens":40000000,"cache_read_input_tokens":0}}}`+"\n",
		now.Add(-5*time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(proj, "s.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	pct := func(v float64) *float64 { return &v }
	readings := []usagecalc.Reading{
		{At: now.Add(-6 * time.Hour), Percent: pct(55)},
		{At: now.Add(-90 * time.Minute), Percent: pct(60)},
	}
	adm := newFakeAdmission(3)
	nt := &fakeNotifier{}
	ses := &failingSessions{running: 4}
	in := BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return *cfg },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			return readings, 0, usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}, nil
		},
		TranscriptDir: dir,
		Now:           func() time.Time { return now },
	}
	g, err := NewBurnGuard(in, adm, ses, nt, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g, adm, nt, ses
}

func TestBurnGuardClimbsAndNeverEndsASession(t *testing.T) {
	cfg := rungFixtureConfig()
	// ask_on_change is off by default (alerts only); turn it on to exercise the question path.
	cfg.AskOnChange = true
	g, adm, nt, ses := burnGuardRig(t, &cfg)
	ctx := context.Background()

	d := g.Tick(ctx)
	if d.Direction != burnguard.DirectionClimb || d.Rung != burnguard.RungLowerInflight {
		t.Fatalf("first tick = %+v, want a climb to lower_inflight", d)
	}
	if adm.cap != 2 {
		t.Fatalf("inflight cap = %d, want 2", adm.cap)
	}
	g.Tick(ctx)
	g.Tick(ctx)
	if adm.routes["claude-code"] != "pi-prompt" || !adm.held["claude-code"] {
		t.Fatalf("after three ticks routes=%v held=%v", adm.routes, adm.held)
	}
	if len(nt.alerts) != 3 || len(nt.questions) != 3 {
		t.Fatalf("alerts=%d questions=%d, want 3 each", len(nt.alerts), len(nt.questions))
	}
	for _, s := range append(append([]string{}, nt.alerts...), nt.questions...) {
		if !strings.Contains(s, "guard ") {
			t.Fatalf("notice carries no numbers: %q", s)
		}
	}
	if !strings.Contains(nt.alerts[0], "4 running session(s) were left alone") {
		t.Fatalf("alert does not say sessions were left alone: %q", nt.alerts[0])
	}
	st := g.Status()
	if st.State != "guarding" || st.Level != 3 || st.LastProjection == nil || st.LastDecision == nil {
		t.Fatalf("status = %+v", st)
	}
	if got := g.Stats(); got.Level != 3 || got.Counters.Climbs != 3 || got.Counters.Ticks != 3 {
		t.Fatalf("stats = %+v", got)
	}

	// Turning the guard off releases every rung and restores the original cap.
	cfg.Enabled = false
	d = g.Tick(ctx)
	if d.Direction != burnguard.DirectionDisabled || adm.cap != 3 || adm.held["claude-code"] || len(adm.routes) != 0 {
		t.Fatalf("disable: %+v cap=%d held=%v routes=%v", d, adm.cap, adm.held, adm.routes)
	}
	if g.Status().State != "off" {
		t.Fatalf("state = %q, want off", g.Status().State)
	}
	ses.assertNoEnd(t)
}

// TestBurnGuardRenewsHoldWhileAppliedAndClearsOnStepBack: while the hold rung stays applied and
// nothing changes, every tick renews the hold flag; when the projection returns under the pace
// line and the rung steps back, the release clears it.
func TestBurnGuardRenewsHoldWhileAppliedAndClearsOnStepBack(t *testing.T) {
	cfg := rungFixtureConfig()
	cfg.Enabled = true
	cfg.Ladder = []string{burnguard.RungHoldClaude}

	ctx := context.Background()
	fixedNow := time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC)
	proj := filepath.Join(t.TempDir(), "-example-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// 40M cache-write tokens (about 15 points) in the last hour, shortly before now —
	// well over the pace line, like the shared rig.
	line := fmt.Sprintf(`{"type":"assistant","timestamp":"%s","requestId":"r1","message":{"id":"m1","usage":{"cache_creation_input_tokens":40000000,"cache_read_input_tokens":0}}}`+"\n",
		fixedNow.Add(-5*time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(proj, "s.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	pct := func(v float64) *float64 { return &v }
	// Usage well over the pace line.
	readings := []usagecalc.Reading{
		{At: fixedNow.Add(-6 * time.Hour), Percent: pct(55)},
		{At: fixedNow.Add(-90 * time.Minute), Percent: pct(60)},
	}
	adm := newFakeAdmission(3)
	notif := &fakeNotifier{}
	ses := &failingSessions{running: 1}
	in := BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return cfg },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			return readings, 0, usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}, nil
		},
		TranscriptDir: filepath.Dir(proj),
		Now:           func() time.Time { return fixedNow },
	}
	g, err := NewBurnGuard(in, adm, ses, notif, nil)
	if err != nil {
		t.Fatal(err)
	}
	holdCalls := func(on bool) int {
		adm.mu.Lock()
		defer adm.mu.Unlock()
		want := fmt.Sprintf("hold claude-code %v", on)
		n := 0
		for _, c := range adm.calls {
			if c == want {
				n++
			}
		}
		return n
	}
	heldNow := func() bool {
		adm.mu.Lock()
		defer adm.mu.Unlock()
		return adm.held["claude-code"]
	}

	// First tick: the projection is over the line, the hold rung climbs in and sets the flag once.
	d := g.Tick(ctx)
	if d.Direction != burnguard.DirectionClimb || d.Rung != burnguard.RungHoldClaude {
		t.Fatalf("first tick = %+v, want a climb to hold_claude", d)
	}
	if got := holdCalls(true); got != 1 {
		t.Fatalf("climb set the hold %d times, want 1", got)
	}

	// Second tick: nothing changes and the rung stays applied. The flag must be renewed.
	d = g.Tick(ctx)
	if d.Changed {
		t.Fatalf("second tick changed: %+v", d)
	}
	if got := holdCalls(true); got != 2 {
		t.Fatalf("no-change tick: hold(true) calls = %d, want 2 (the held tick renews the flag)", got)
	}
	if !heldNow() {
		t.Fatal("the hold is not in force after the renewal ticks")
	}

	// Step back: burn has quieted (the transcript line is now outside the 3h rate window)
	// and the readings have dropped under the pace line, so the release clears the flag.
	line = fmt.Sprintf(`{"type":"assistant","timestamp":"%s","requestId":"r1","message":{"id":"m1","usage":{"cache_creation_input_tokens":40000000,"cache_read_input_tokens":0}}}`+"\n",
		fixedNow.Add(-40*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(proj, "s.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	readings = []usagecalc.Reading{
		{At: fixedNow.Add(-6 * time.Hour), Percent: pct(10)},
		{At: fixedNow.Add(-90 * time.Minute), Percent: pct(20)},
	}
	d = g.Tick(ctx)
	if d.Direction != burnguard.DirectionStepBack {
		t.Fatalf("after the readings drop, tick = %+v, want a step back", d)
	}
	if heldNow() {
		t.Fatal("the step back left the hold in force; the release must clear it")
	}
	if got := holdCalls(false); got != 1 {
		t.Fatalf("step back: hold(false) calls = %d, want 1", got)
	}
	ses.assertNoEnd(t)
}

func TestBurnGuardDisabledDoesNothing(t *testing.T) {
	cfg := config.DefaultBurnGuard()
	cfg.Enabled = false
	g, adm, nt, _ := burnGuardRig(t, &cfg)
	d := g.Tick(context.Background())
	if d.Changed || len(adm.calls) != 0 || len(nt.alerts) != 0 {
		t.Fatalf("disabled guard acted: %+v calls=%v", d, adm.calls)
	}
}

func TestBurnGuardRefitWakesOnReading(t *testing.T) {
	cfg := config.DefaultBurnGuard()
	g, _, _, _ := burnGuardRig(t, &cfg)
	g.OnReading()
	select {
	case <-g.refitWake:
	default:
		t.Fatal("OnReading did not wake the runner")
	}
	cfg.RefitOnReading = false
	g.OnReading()
	select {
	case <-g.refitWake:
		t.Fatal("OnReading woke the runner with refit_on_reading off")
	default:
	}
	var nilGuard *BurnGuard
	nilGuard.OnReading() // an unwired guard is a no-op
}

func TestBurnGuardAskOnChangeOffAlertsOnly(t *testing.T) {
	cfg := rungFixtureConfig()
	if cfg.AskOnChange {
		t.Fatal("default ask_on_change = true, want false")
	}
	g, _, nt, _ := burnGuardRig(t, &cfg)
	d := g.Tick(context.Background())
	if d.Direction != burnguard.DirectionClimb || len(nt.alerts) != 1 || len(nt.questions) != 0 {
		t.Fatalf("decision=%+v alerts=%v questions=%v", d, nt.alerts, nt.questions)
	}
	if !strings.Contains(nt.alerts[0], "ask_on_change") {
		t.Fatalf("alert does not name the setting to turn questions on: %q", nt.alerts[0])
	}
}

// The seams the runner holds have no method that could end a session.
func TestBurnGuardSeamsCannotEndSessions(t *testing.T) {
	for _, ty := range []reflect.Type{
		reflect.TypeOf((*BurnGuardSessions)(nil)).Elem(),
		reflect.TypeOf((*BurnGuardAdmission)(nil)).Elem(),
	} {
		for i := 0; i < ty.NumMethod(); i++ {
			n := strings.ToLower(ty.Method(i).Name)
			for _, bad := range []string{"kill", "stop", "restart", "end", "terminate", "delete", "cancel"} {
				if strings.HasPrefix(n, bad) {
					t.Fatalf("%s has method %s", ty.Name(), ty.Method(i).Name)
				}
			}
		}
	}
}

// TestBurnGuardCannotReachAnEndCall parses the runner's source and fails if it calls anything
// that ends a session, and checks the session view it holds declares no such method.
func TestBurnGuardCannotReachAnEndCall(t *testing.T) {
	typ := reflect.TypeOf((*BurnGuardSessions)(nil)).Elem()
	if typ.NumMethod() != 1 || typ.Method(0).Name != "RunningCount" {
		t.Fatalf("BurnGuardSessions must be read-only (RunningCount only), has %d methods", typ.NumMethod())
	}
	banned := map[string]bool{"Kill": true, "KillAll": true, "Stop": true, "StopAll": true, "Restart": true,
		"End": true, "Delete": true, "Terminate": true, "Cancel": true, "KillSession": true, "StopSession": true}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "burn_guard.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && banned[sel.Sel.Name] {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "t" {
					return true // the runner's own time.Timer
				}
				t.Errorf("burn_guard.go:%d calls %s, which could end a session", fset.Position(c.Pos()).Line, sel.Sel.Name)
			}
		}
		return true
	})
}

func TestBurnGuardStatusEndpoint(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleBurnGuardStatus(rec, httptest.NewRequest(http.MethodGet, "/api/usage/burn-guard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("unwired status = %d", rec.Code)
	}
	var st BurnGuardStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.State != "off" {
		t.Fatalf("unwired body = %s (%v)", rec.Body.String(), err)
	}

	cfg := rungFixtureConfig()
	g, _, _, _ := burnGuardRig(t, &cfg)
	g.Tick(context.Background())
	s.burnGuard = g
	rec = httptest.NewRecorder()
	s.handleBurnGuardStatus(rec, httptest.NewRequest(http.MethodGet, "/api/usage/burn-guard", nil))
	st = BurnGuardStatus{}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.State != "guarding" || st.LastProjection == nil || st.LastDecision == nil {
		t.Fatalf("status = %+v", st)
	}
	rec = httptest.NewRecorder()
	s.handleBurnGuardStatus(rec, httptest.NewRequest(http.MethodPost, "/api/usage/burn-guard", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rec.Code)
	}
}

// rungFixtureConfig is the default config with the projection-gap check off. The rung
// fixtures pin the ladder's behaviour on the guard projection; the gap check has its own
// tests in internal/burnguard and in TestBurnGuardGapLimitComesFromConfig.
func rungFixtureConfig() config.BurnGuardConfig {
	cfg := config.DefaultBurnGuard()
	cfg.MaxProjectionGapPoints = 0
	return cfg
}

// TestBurnGuardGapLimitComesFromConfig checks the configured max_projection_gap_points
// reaches the ladder: the default limit (25) decides on the lower projection, and a zero
// limit decides on the guard projection alone.
func TestBurnGuardGapLimitComesFromConfig(t *testing.T) {
	cfg := config.DefaultBurnGuard()
	g, _, _, _ := burnGuardRig(t, &cfg)
	d := g.Tick(context.Background())
	if d.Direction != burnguard.DirectionHold || !strings.Contains(d.Reason, "using the lower") {
		t.Fatalf("default gap limit: decision = %+v, want a hold that uses the lower projection", d)
	}
	if d.Gap <= cfg.MaxProjectionGapPoints {
		t.Fatalf("fixture gap %v is not over the default limit %v", d.Gap, cfg.MaxProjectionGapPoints)
	}

	cfg = config.DefaultBurnGuard()
	cfg.MaxProjectionGapPoints = 0
	g, _, _, _ = burnGuardRig(t, &cfg)
	if d := g.Tick(context.Background()); d.Direction != burnguard.DirectionClimb || !strings.Contains(d.AlertText, "using the guard") {
		t.Fatalf("zero gap limit: decision = %+v, want a climb on the guard projection", d)
	}
}

// TestBurnGuardProjectionGapRESTRefusesInvalidAndAcceptsZeroAndDefault proves the REST
// surface refuses an invalid max_projection_gap_points with 400 and stores nothing, and
// accepts 0 (never compare) and 25 (the default), storing exactly the value sent.
func TestBurnGuardProjectionGapRESTRefusesInvalidAndAcceptsZeroAndDefault(t *testing.T) {
	for _, body := range []string{
		`{"max_projection_gap_points": -1}`,
		`{"max_projection_gap_points": 100.5}`,
		`{"max_projection_gap_points": 101}`,
		`{"max_projection_gap_points": "NaN"}`,
		`{"max_projection_gap_points": "Inf"}`,
	} {
		s := burnGuardConfigServer(t)
		before := s.cfg.Usage.BurnGuard.MaxProjectionGapPoints
		rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", body, s.handleBurnGuardConfig)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: PUT = %d (%s), want 400", body, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "max_projection_gap_points") {
			t.Errorf("%s: refusal does not name the key: %s", body, rr.Body.String())
		}
		if s.cfg.Usage.BurnGuard.MaxProjectionGapPoints != before {
			t.Errorf("%s: a refused PUT changed the stored value", body)
		}
	}
	for _, v := range []float64{0, 25} {
		s := burnGuardConfigServer(t)
		if v == 25 {
			s.cfg.Usage.BurnGuard.MaxProjectionGapPoints = 0 // so 25 is a real change
		}
		body := fmt.Sprintf(`{"max_projection_gap_points": %v}`, v)
		rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", body, s.handleBurnGuardConfig)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v = %d: %s", v, rr.Code, rr.Body.String())
		}
		if got := s.cfg.Usage.BurnGuard.MaxProjectionGapPoints; got != v {
			t.Fatalf("stored max_projection_gap_points = %v, want %v", got, v)
		}
		rr = doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/config", "", s.handleBurnGuardConfig)
		var got config.BurnGuardConfig
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.MaxProjectionGapPoints != v {
			t.Fatalf("GET after PUT %v = %v", v, got.MaxProjectionGapPoints)
		}
	}
}
