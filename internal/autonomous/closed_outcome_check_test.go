package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// ---- deploy record ----

func coShippedIn(t *testing.T, m *Manager, status PRDStatus, release string) *PRD {
	t.Helper()
	p := coPRD(t, m, status)
	m.Store().MutatePRD(p.ID, func(p *PRD) bool {
		p.MergedInRelease, p.MergeCommit = release, "c0ffee"+p.ID
		at := time.Now().Add(-3 * time.Hour)
		p.MergedAt = &at
		return true
	})
	return coGet(t, m, p.ID)
}

// The deploy of a release is written onto every Automaton that shipped in it,
// and only those, and a repeat does not rewrite it.
func TestDeployRecordStampsEveryAutomatonOfTheRelease(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	a := coShippedIn(t, m, PRDCompleted, "v9.1.0")
	b := coShippedIn(t, m, PRDArchived, "v9.1.0")
	other := coShippedIn(t, m, PRDCompleted, "v9.2.0")
	plain := coPRD(t, m, PRDCompleted)
	at := time.Now().UTC().Truncate(time.Second)
	ids, err := m.RecordReleaseDeployed(DeployRecord{Release: "v9.1.0", Version: "v9.1.0", At: at, Host: "h1", Health: DeployHealthPassed, Detail: "200 ok"})
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids=%v err=%v, want the two Automata of v9.1.0", ids, err)
	}
	for _, id := range []string{a.ID, b.ID} {
		got := coGet(t, m, id)
		if got.Deploy == nil || got.Deploy.Host != "h1" || got.Deploy.Health != DeployHealthPassed || got.Deploy.Version != "v9.1.0" || !got.Deploy.At.Equal(at) {
			t.Fatalf("%s deploy record = %+v", id, got.Deploy)
		}
	}
	if coGet(t, m, other.ID).Deploy != nil || coGet(t, m, plain.ID).Deploy != nil {
		t.Fatal("an Automaton of another release, or one that never shipped, must not get the record")
	}
	if ids, _ := m.RecordReleaseDeployed(DeployRecord{Release: "v9.1.0", Version: "v9.1.0", At: at, Host: "h1", Health: DeployHealthPassed}); len(ids) != 0 {
		t.Fatalf("a repeat of the same deploy changes nothing, changed %v", ids)
	}
	for _, bad := range []DeployRecord{
		{Version: "v9", Host: "h", Health: DeployHealthPassed},
		{Release: "v9.1.0", Host: "h", Health: DeployHealthPassed},
		{Release: "v9.1.0", Version: "v9.1.0", Host: "h", Health: "great"},
	} {
		if _, err := m.RecordReleaseDeployed(bad); err == nil {
			t.Fatalf("deploy record %+v must be refused", bad)
		}
	}
}

// The deploy-after-ship step writes the record: version, time, host, health.
func TestDeployAfterShipWritesDeployRecord(t *testing.T) {
	cases := []struct {
		name       string
		deployErr  error
		health     func(context.Context) (string, string)
		wantHealth string
	}{
		{"no health check", nil, nil, DeployHealthUnchecked},
		{"health passed", nil, func(context.Context) (string, string) { return DeployHealthPassed, "ready" }, DeployHealthPassed},
		{"deploy failed", errors.New("install failed"), nil, DeployHealthFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hooks := SupervisorLoopHooks{
				Settings:     func() supervisorloop.Settings { return supervisorloop.DefaultSettings() },
				Deploy:       func(context.Context) (string, error) { return "restarted", tc.deployErr },
				DeployHealth: tc.health,
			}
			h, steps, _ := newStepsRig(t, hooks)
			steps.hostName = func() string { return "build-host" }
			p := coShippedIn(t, h.m, PRDCompleted, "v9.5.0")
			steps.RunPass(context.Background())
			got := coGet(t, h.m, p.ID)
			if got.Deploy == nil {
				t.Fatal("the deploy step must write a deploy record onto the shipped Automaton")
			}
			if got.Deploy.Release != "v9.5.0" || got.Deploy.Version != "v9.5.0" || got.Deploy.Host != "build-host" || got.Deploy.Health != tc.wantHealth || got.Deploy.At.IsZero() {
				t.Fatalf("deploy record = %+v", got.Deploy)
			}
		})
	}
}

// ---- standing check ----

type proofGit struct {
	files []string
	syms  map[string]bool
	mu    sync.Mutex
	calls int
}

func (g *proofGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	switch args[0] {
	case "symbolic-ref":
		return "refs/remotes/origin/main\n", nil
	case "ls-tree":
		return strings.Join(g.files, "\n"), nil
	case "grep":
		for i, a := range args {
			if a == "-e" && g.syms[args[i+1]] {
				return "internal/x.go\n", nil
			}
		}
		return "", errors.New("exit status 1")
	}
	return "", nil
}

func coCheckMgr(t *testing.T, check string) (*Manager, *proofGit, *[]string) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ClosedOutcomeArchive = ClosedOutcomeOn
	cfg.ClosedOutcomeCheck = check
	cfg.WorktreeBaseRepo = "/repo"
	m := newEmitManager(t, cfg, nil)
	g := &proofGit{files: []string{"internal/foo/bar.go", "docs/a.md"}, syms: map[string]bool{"FooBar": true}}
	m.archivedUnmergedGit = g
	var alerts []string
	var mu sync.Mutex
	m.SetAlertSink(func(level, title, body string) string {
		mu.Lock()
		alerts = append(alerts, title+"|"+body)
		mu.Unlock()
		return ""
	})
	m.SetOutcomeCheckEnv(OutcomeCheckEnv{DeploysAfterShip: func() bool { return true }, RunningVersion: func() string { return "v9.1.0" }})
	return m, g, &alerts
}

func findingKindsOf(r OutcomeCheckReport, id string) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Automaton == id {
			out = append(out, f.Kind)
		}
	}
	return out
}

func hasFindingKind(r OutcomeCheckReport, id, kind string) bool {
	for _, k := range findingKindsOf(r, id) {
		if k == kind {
			return true
		}
	}
	return false
}

func TestOutcomeCheckFlagsEachUnlabelledOrUndeployedState(t *testing.T) {
	m, _, _ := coCheckMgr(t, ClosedOutcomeShadow)
	// completed, never shipped, no outcome
	noShip := coPRD(t, m, PRDCompleted)
	// completed and labelled dropped: fine
	labelled := coPRD(t, m, PRDCompleted)
	m.Store().MutatePRD(labelled.ID, func(p *PRD) bool { p.ClosedOutcome = OutcomeDropped; return true })
	// shipped, no deploy record, on a host that deploys after ship
	noDeploy := coShippedIn(t, m, PRDCompleted, "v9.1.0")
	// shipped and deployed at the shipped release: fine
	deployed := coShippedIn(t, m, PRDCompleted, "v9.1.0")
	m.Store().MutatePRD(deployed.ID, func(p *PRD) bool {
		p.Deploy = &DeployRecord{Release: "v9.1.0", Version: "9.1.0", At: time.Now(), Host: "h", Health: DeployHealthPassed}
		return true
	})
	// deployed version older than the shipped one
	older := coShippedIn(t, m, PRDCompleted, "v9.1.0")
	m.Store().MutatePRD(older.ID, func(p *PRD) bool {
		p.Deploy = &DeployRecord{Release: "v9.0.0", Version: "v9.0.9", At: time.Now(), Host: "h", Health: DeployHealthPassed}
		return true
	})
	// archived with no outcome (legacy sweep)
	legacy := coPRD(t, m, PRDArchived)
	// running work is never flagged
	running := coPRD(t, m, PRDRunning)

	r := m.OutcomeCheck()
	want := map[string]string{
		noShip.ID: FindingCompletedNoShipped, noDeploy.ID: FindingShippedNoDeploy,
		older.ID: FindingDeployedOlder, legacy.ID: FindingClosedNoOutcome,
	}
	for id, kind := range want {
		if !hasFindingKind(r, id, kind) {
			t.Errorf("automaton %s: want finding %s, got %v", id, kind, findingKindsOf(r, id))
		}
	}
	for _, id := range []string{labelled.ID, deployed.ID, running.ID} {
		if ks := findingKindsOf(r, id); len(ks) != 0 {
			t.Errorf("automaton %s must not be flagged, got %v", id, ks)
		}
	}
	if r.Mode != ClosedOutcomeShadow {
		t.Errorf("mode = %q", r.Mode)
	}
}

// On a host that does not deploy after ship, a missing deploy record is not a
// finding; a running daemon older than the newest shipped release is.
func TestOutcomeCheckDeployRulesFollowTheHost(t *testing.T) {
	m, _, _ := coCheckMgr(t, ClosedOutcomeShadow)
	a := coShippedIn(t, m, PRDCompleted, "v9.3.0")
	r := m.OutcomeCheck()
	if !hasFindingKind(r, a.ID, FindingShippedNoDeploy) {
		t.Fatalf("deploying host must flag %s", a.ID)
	}
	if !hasFindingKind(r, "", FindingDaemonOlder) {
		t.Fatalf("running v9.1.0 is older than shipped v9.3.0: want %s, got %+v", FindingDaemonOlder, r.Findings)
	}
	m.SetOutcomeCheckEnv(OutcomeCheckEnv{DeploysAfterShip: func() bool { return false }, RunningVersion: func() string { return "v9.1.0" }})
	r = m.OutcomeCheck()
	if hasFindingKind(r, a.ID, FindingShippedNoDeploy) || hasFindingKind(r, "", FindingDaemonOlder) {
		t.Fatalf("a host that does not deploy after ship has no deploy findings, got %+v", r.Findings)
	}
}

// The intent proof: named evidence on main plus task completion.
func TestOutcomeCheckIntentProof(t *testing.T) {
	m, g, _ := coCheckMgr(t, ClosedOutcomeShadow)
	mk := func(spec string, tasksDone bool) *PRD {
		p := coPRD(t, m, PRDArchived)
		m.Store().MutatePRD(p.ID, func(p *PRD) bool {
			p.Spec = spec
			p.ClosedOutcome = OutcomeDelivered
			p.ClosedOutcomeNote = "main already had it"
			if tasksDone {
				p.Story[0].Tasks[0].Status = TaskCompleted
			}
			return true
		})
		return coGet(t, m, p.ID)
	}
	proven := mk("Adds `FooBar` in internal/foo/bar.go.", true)
	partial := mk("Adds `FooBar` and `GoneSym` in internal/foo/bar.go.", true)
	undone := mk("Adds `FooBar` in internal/foo/bar.go.", false)
	unnamed := mk("Make it nicer.", true)
	viaTwin := mk("Adds `Missing` in nowhere/x.go.", true)
	twin := coShippedIn(t, m, PRDCompleted, "v9.1.0")
	m.Store().MutatePRD(viaTwin.ID, func(p *PRD) bool { p.DeliveredVia = twin.ID; return true })
	_ = g

	r := m.RunOutcomeCheck(context.Background())
	if hasFindingKind(r, proven.ID, FindingIntentUnproven) {
		t.Errorf("fully evidenced work must pass, got %v", findingKindsOf(r, proven.ID))
	}
	for name, id := range map[string]string{"partial": partial.ID, "tasks not done": undone.ID, "no named evidence": unnamed.ID} {
		if !hasFindingKind(r, id, FindingIntentUnproven) {
			t.Errorf("%s: want %s, got %v", name, FindingIntentUnproven, findingKindsOf(r, id))
		}
	}
	if hasFindingKind(r, viaTwin.ID, FindingIntentUnproven) {
		t.Errorf("work delivered by a shipped twin is proven by the twin's record, not re-proved from its spec")
	}
	if r.Proof.Checked != 4 || r.Proof.Proven != 1 {
		t.Errorf("proof summary = %+v, want 4 checked, 1 proven", r.Proof)
	}
	for _, f := range r.Findings {
		if f.Automaton == partial.ID && !strings.Contains(f.Detail, "GoneSym") {
			t.Errorf("a partial proof names what is missing, got %q", f.Detail)
		}
	}
}

// Shadow records and reads; on also raises one alert per finding, once.
func TestOutcomeCheckShadowRaisesNoAlertOnRaisesOnce(t *testing.T) {
	for _, mode := range []string{ClosedOutcomeShadow, ClosedOutcomeOn} {
		m, _, alerts := coCheckMgr(t, mode)
		a := coPRD(t, m, PRDArchived)
		m.RunOutcomeCheck(context.Background())
		m.RunOutcomeCheck(context.Background())
		want := 0
		if mode == ClosedOutcomeOn {
			want = 1
		}
		n := 0
		for _, al := range *alerts {
			if strings.Contains(al, a.ID) {
				n++
			}
		}
		if n != want {
			t.Errorf("mode %s: %d alerts naming %s, want %d (%v)", mode, n, a.ID, want, *alerts)
		}
	}
}

func TestOutcomeCheckOffDoesNothing(t *testing.T) {
	m, g, _ := coCheckMgr(t, ClosedOutcomeOff)
	coPRD(t, m, PRDArchived)
	r := m.RunOutcomeCheck(context.Background())
	if r.Mode != ClosedOutcomeOff || len(r.Findings) != 0 || g.calls != 0 {
		t.Fatalf("off must scan nothing: mode=%s findings=%d git calls=%d", r.Mode, len(r.Findings), g.calls)
	}
}

// The scheduled run is daily, and its report survives a restart.
func TestOutcomeCheckScheduleAndPersistence(t *testing.T) {
	m, _, _ := coCheckMgr(t, ClosedOutcomeShadow)
	clk := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clk }
	a := coPRD(t, m, PRDArchived)
	if !m.OutcomeCheckIfDue(context.Background()) {
		t.Fatal("the first scheduled run is due at once")
	}
	if m.OutcomeCheckIfDue(context.Background()) {
		t.Fatal("a second run inside the interval must not happen")
	}
	clk = clk.Add(25 * time.Hour)
	if !m.OutcomeCheckIfDue(context.Background()) {
		t.Fatal("a run is due after the interval")
	}
	// A fresh Manager over the same data directory reads the last report.
	m2, err := NewManager(m.dataDir, m.Config(), nil)
	if err != nil {
		t.Fatal(err)
	}
	last := m2.LastOutcomeCheck()
	if last.RunAt.IsZero() || !hasFindingKind(last, a.ID, FindingClosedNoOutcome) {
		t.Fatalf("persisted report missing: %+v", last)
	}
}

// 500 records: the live read is bounded and fast.
func TestOutcomeCheckTimingAt500Automata(t *testing.T) {
	m, _, _ := coCheckMgr(t, ClosedOutcomeShadow)
	for i := 0; i < 500; i++ {
		p := coPRD(t, m, PRDArchived)
		if i%2 == 0 {
			m.Store().MutatePRD(p.ID, func(p *PRD) bool {
				p.ClosedOutcome = OutcomeDelivered
				p.Spec = fmt.Sprintf("Adds `FooBar` in internal/foo/bar.go (%d).", i)
				p.Story[0].Tasks[0].Status = TaskCompleted
				return true
			})
		}
	}
	start := time.Now()
	r := m.RunOutcomeCheck(context.Background())
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("check of 500 Automata took %s", d)
	}
	if r.Scanned < 500 || len(r.Findings) != 250 {
		t.Fatalf("scanned=%d findings=%d, want 500 scanned and 250 unlabelled", r.Scanned, len(r.Findings))
	}
}
