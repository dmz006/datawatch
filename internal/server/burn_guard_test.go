package server

import (
	"context"
	"fmt"
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

// failingSessions is a fake session manager: it counts, and fails the test if any call that
// would end a session is made.
type failingSessions struct {
	t       *testing.T
	running int
}

func (f *failingSessions) RunningCount() int { return f.running }
func (f *failingSessions) Kill(string)       { f.t.Fatal("burn guard killed a session") }
func (f *failingSessions) Stop(string)       { f.t.Fatal("burn guard stopped a session") }
func (f *failingSessions) Restart(string)    { f.t.Fatal("burn guard restarted a session") }
func (f *failingSessions) End(string)        { f.t.Fatal("burn guard ended a session") }
func (f *failingSessions) KillAll()          { f.t.Fatal("burn guard killed all sessions") }

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
	proj := filepath.Join(dir, "-home-dmz-workspace-datawatch")
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
	ses := &failingSessions{t: t, running: 4}
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
	cfg := config.DefaultBurnGuard()
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
		if !strings.Contains(s, "projected") && !strings.Contains(s, "projects to") {
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
	_ = ses // any end call would have failed the test already
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
