package autonomous

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// holdMigrationPRD builds an Automaton with one task, then forces its fields.
func holdMigrationPRD(t *testing.T, m *Manager, title, spec string, status PRDStatus, blockedReason string, taskStatus TaskStatus, decisions ...Decision) *PRD {
	t.Helper()
	prd := newEmitPRD(t, m, PRDDraft)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Title = title
	prd.Spec = spec
	prd.Project = "datawatch"
	prd.Status = status
	prd.BlockedReason = blockedReason
	prd.Story[0].Tasks[0].Status = taskStatus
	prd.Decisions = append(prd.Decisions, decisions...)
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return got
}

func holdMigrationCandidateIDs(c []HoldMigrationCandidate) []string {
	ids := []string{}
	for _, x := range c {
		ids = append(ids, x.ID)
	}
	return ids
}

func holdMigrationFind(c []HoldMigrationCandidate, id string) *HoldMigrationCandidate {
	for i := range c {
		if c[i].ID == id {
			return &c[i]
		}
	}
	return nil
}

// holdMigrationFixture adds the three look-alikes and one plain failure.
func holdMigrationFixture(t *testing.T) (m *Manager, clk *holdClock, evalP, designP, noteP, plainP *PRD) {
	t.Helper()
	m, clk = newHoldManager(t)
	evalP = holdMigrationPRD(t, m, "Model tiering matrix run", "measure the tiers", PRDFailed, "", TaskFailed)
	designP = holdMigrationPRD(t, m, "Dashboard mockup", "draw the page", PRDBlocked, "waiting for an answer", TaskBlocked,
		Decision{At: holdClockStart, Kind: operatorDecisionKind, Note: "where does the card go?"})
	noteP = holdMigrationPRD(t, m, "Plain feature work", "add a column", PRDBlocked, "parked until after the release", TaskBlocked)
	plainP = holdMigrationPRD(t, m, "Add a CLI flag", "wire the flag through", PRDFailed, "worker exited", TaskFailed)
	return
}

func TestHoldMigrationListsCandidatesWithWhy(t *testing.T) {
	m, _, evalP, designP, noteP, plainP := holdMigrationFixture(t)
	cands := m.HoldMigrationCandidates()

	for _, c := range []struct {
		p    *PRD
		code string
	}{
		{evalP, "eval_measurement"},
		{designP, "design_parked"},
		{noteP, "operator_hold_note"},
	} {
		got := holdMigrationFind(cands, c.p.ID)
		if got == nil {
			t.Fatalf("%s not listed; got %v", c.code, holdMigrationCandidateIDs(cands))
		}
		if len(got.Why) != 1 || !strings.HasPrefix(got.Why[0], c.code+": ") || len(got.Why[0]) <= len(c.code)+2 {
			t.Errorf("%s why = %v, want one %q entry with a sentence", c.p.ID, got.Why, c.code)
		}
		if got.Title != c.p.Title || got.Project != "datawatch" || got.Status != string(c.p.Status) || got.BlockedReason != c.p.BlockedReason {
			t.Errorf("candidate fields = %+v", got)
		}
	}
	if holdMigrationFind(cands, plainP.ID) != nil {
		t.Errorf("plain failed feature work listed: %v", holdMigrationCandidateIDs(cands))
	}
	if len(cands) != 3 {
		t.Errorf("candidates = %d, want 3", len(cands))
	}
}

func TestHoldMigrationRuleBoundaries(t *testing.T) {
	m, _ := newHoldManager(t)
	// each case: a record that must NOT match
	cases := []struct {
		name string
		p    *PRD
	}{
		{"eval words but no failed task", holdMigrationPRD(t, m, "Benchmark the cache", "x", PRDFailed, "", TaskCompleted)},
		{"eval words but wrong status", holdMigrationPRD(t, m, "Benchmark the cache", "x", PRDRunning, "", TaskFailed)},
		{"design words but failed not blocked", holdMigrationPRD(t, m, "Design the page", "HOLD", PRDFailed, "", TaskFailed)},
		{"design words with no decision or HOLD line", holdMigrationPRD(t, m, "Design the page", "just build it", PRDBlocked, "waiting on worker", TaskBlocked)},
		{"substring is not a word", holdMigrationPRD(t, m, "Evaluator refactor", "holder of the tiers", PRDFailed, "placeholder", TaskFailed)},
		{"held automaton is excluded", holdMigrationPRD(t, m, "Matrix run", "x", PRDHeld, "", TaskFailed)},
	}
	for _, c := range cases {
		if got := holdMigrationFind(m.HoldMigrationCandidates(), c.p.ID); got != nil {
			t.Errorf("%s: listed with %v", c.name, got.Why)
		}
	}

	// positive variants
	holdLine := holdMigrationPRD(t, m, "Proposal for the router", "intro\nHOLD until the operator picks\nmore", PRDBlocked, "", TaskBlocked)
	answered := holdMigrationPRD(t, m, "Proposal for the router", "x", PRDBlocked, "", TaskBlocked,
		Decision{At: holdClockStart, Kind: operatorAnswerKind, Note: "chose b"})
	gate := holdMigrationPRD(t, m, "Proposal for the router", "x", PRDBlocked, "", TaskBlocked,
		Decision{At: holdClockStart, Kind: "decision_gate_hold", Note: "gate"})
	newestNote := holdMigrationPRD(t, m, "Ordinary", "x", PRDFailed, "worker exited", TaskFailed,
		Decision{At: holdClockStart, Kind: "run", Note: "old note says parked"},
		Decision{At: holdClockStart.Add(time.Hour), Kind: "run", Note: "wait for after the release"})
	oldNoteOnly := holdMigrationPRD(t, m, "Ordinary", "x", PRDFailed, "worker exited", TaskFailed,
		Decision{At: holdClockStart, Kind: "run", Note: "this was parked"},
		Decision{At: holdClockStart.Add(time.Hour), Kind: "run", Note: "retrying"})
	tagged := holdMigrationPRD(t, m, "Ordinary", "x", PRDFailed, "worker exited", TaskFailed)
	tag, err := m.CreateTag("eval-suite", "", "", "")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := m.SetPRDTags(tagged.ID, []string{tag.ID}); err != nil {
		t.Fatal(err)
	}
	cands := m.HoldMigrationCandidates()
	for name, p := range map[string]*PRD{"HOLD line": holdLine, "operator_answer": answered, "decision_gate_hold": gate, "newest note": newestNote, "tag name": tagged} {
		if holdMigrationFind(cands, p.ID) == nil {
			t.Errorf("%s: not listed", name)
		}
	}
	if holdMigrationFind(cands, oldNoteOnly.ID) != nil {
		t.Error("an old note (not the newest) matched operator_hold_note")
	}
}

func TestHoldMigrationDryRunWritesNothing(t *testing.T) {
	m, _, evalP, designP, noteP, plainP := holdMigrationFixture(t)
	before := map[string]PRDStatus{}
	for _, p := range []*PRD{evalP, designP, noteP, plainP} {
		before[p.ID] = p.Status
	}
	decisions := len(evalP.Decisions)

	for _, opts := range []HoldMigrationOpts{
		{DryRun: true, IDs: []string{evalP.ID}, Reason: "waiting"},
		{IDs: nil, Reason: "waiting"},
		{},
	} {
		rep, err := m.MigrateToHold(opts)
		if err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
		if len(rep.Candidates) != 3 || rep.Considered != 4 || len(rep.Held) != 0 || len(rep.Skipped) != 0 {
			t.Errorf("%+v: report = %+v", opts, rep)
		}
	}
	for id, st := range before {
		got, _ := m.Store().GetPRD(id)
		if got.Status != st || got.Hold != nil {
			t.Errorf("%s changed: %s hold=%v", id, got.Status, got.Hold)
		}
	}
	got, _ := m.Store().GetPRD(evalP.ID)
	if len(got.Decisions) != decisions {
		t.Error("a decision was written")
	}
}

func TestHoldMigrationRealRunHoldsOnlyChosen(t *testing.T) {
	m, clk, evalP, designP, noteP, _ := holdMigrationFixture(t)
	until := clk.t.Add(48 * time.Hour)
	// the store hands out live pointers, so note the starting statuses now
	was := map[string]PRDStatus{evalP.ID: evalP.Status, noteP.ID: noteP.Status}
	rep, err := m.MigrateToHold(HoldMigrationOpts{
		IDs:    []string{evalP.ID, noteP.ID},
		Reason: "  evals wait until the release  ", Condition: "until after the release", Until: &until, Actor: "dmz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Held) != 2 || len(rep.Skipped) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	for _, p := range []*PRD{evalP, noteP} {
		got, _ := m.Store().GetPRD(p.ID)
		if got.Status != PRDHeld || got.Hold == nil {
			t.Fatalf("%s status %s", p.ID, got.Status)
		}
		h := got.Hold
		if h.Reason != "evals wait until the release" || h.Condition != "until after the release" ||
			h.Until == nil || !h.Until.Equal(until) || h.HeldBy != "dmz" || h.From != was[p.ID] {
			t.Errorf("%s hold = %+v", p.ID, h)
		}
		var d *Decision
		for i := range got.Decisions {
			if got.Decisions[i].Kind == "hold" {
				d = &got.Decisions[i]
			}
		}
		if d == nil || d.Actor != "dmz" {
			t.Errorf("%s hold decision = %+v", p.ID, d)
		}
	}
	if got, _ := m.Store().GetPRD(noteP.ID); got.Hold.PriorBlockedReason != "parked until after the release" {
		t.Errorf("prior blocked reason = %q", got.Hold.PriorBlockedReason)
	}
	// an unchosen candidate and an unchosen plain record stay as they were
	if got, _ := m.Store().GetPRD(designP.ID); got.Status != PRDBlocked || got.Hold != nil {
		t.Errorf("unchosen candidate changed: %s", got.Status)
	}
}

func TestHoldMigrationChosenNeedNotBeCandidate(t *testing.T) {
	m, _, _, _, _, plainP := holdMigrationFixture(t)
	rep, err := m.MigrateToHold(HoldMigrationOpts{IDs: []string{plainP.ID, plainP.ID}, Reason: "operator says wait"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Held) != 1 || rep.Held[0].ID != plainP.ID || rep.Held[0].From != "failed" {
		t.Fatalf("report = %+v", rep)
	}
	if got, _ := m.Store().GetPRD(plainP.ID); got.Status != PRDHeld || got.Hold.HeldBy != "operator" {
		t.Errorf("status %s hold %+v", got.Status, got.Hold)
	}
}

func TestHoldMigrationEmptyReasonRefused(t *testing.T) {
	m, _, evalP, _, _, _ := holdMigrationFixture(t)
	for _, reason := range []string{"", "   \n\t"} {
		_, err := m.MigrateToHold(HoldMigrationOpts{IDs: []string{evalP.ID}, Reason: reason})
		if err == nil || !strings.Contains(err.Error(), "reason") {
			t.Fatalf("reason %q: err = %v, want an error naming the reason", reason, err)
		}
	}
	if got, _ := m.Store().GetPRD(evalP.ID); got.Status != PRDFailed || got.Hold != nil {
		t.Errorf("status changed to %s", got.Status)
	}
	// a reason that is too long, or a past until, also refuses the whole run
	long := strings.Repeat("x", holdReasonMaxChars+1)
	if _, err := m.MigrateToHold(HoldMigrationOpts{IDs: []string{evalP.ID}, Reason: long}); err == nil {
		t.Error("over-long reason accepted")
	}
	past := holdClockStart.Add(-time.Hour)
	if _, err := m.MigrateToHold(HoldMigrationOpts{IDs: []string{evalP.ID}, Reason: "ok", Until: &past}); err == nil {
		t.Error("past until accepted")
	}
	if got, _ := m.Store().GetPRD(evalP.ID); got.Status != PRDFailed {
		t.Errorf("status changed to %s", got.Status)
	}
}

func TestHoldMigrationSkipsWithReason(t *testing.T) {
	m, _ := newHoldManager(t)
	running := holdMigrationPRD(t, m, "Running work", "x", PRDRunning, "", TaskInProgress)
	done := holdMigrationPRD(t, m, "Done work", "x", PRDCompleted, "", TaskCompleted)
	held := holdMigrationPRD(t, m, "Already held", "x", PRDFailed, "", TaskFailed)
	if _, err := m.Hold(held.ID, HoldRequest{Reason: "first"}, "operator"); err != nil {
		t.Fatal(err)
	}
	ok := holdMigrationPRD(t, m, "Failed work", "x", PRDFailed, "", TaskFailed)

	rep, err := m.MigrateToHold(HoldMigrationOpts{
		IDs:    []string{running.ID, done.ID, held.ID, "nope0000", ok.ID},
		Reason: "operator decision",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		running.ID: "not blocked or failed (status running)",
		done.ID:    "not blocked or failed (status completed)",
		held.ID:    "already held",
		"nope0000": "not found",
	}
	if len(rep.Skipped) != len(want) {
		t.Fatalf("skipped = %+v", rep.Skipped)
	}
	for _, s := range rep.Skipped {
		if want[s.ID] != s.Reason {
			t.Errorf("skip %s = %q, want %q", s.ID, s.Reason, want[s.ID])
		}
	}
	if len(rep.Held) != 1 || rep.Held[0].ID != ok.ID {
		t.Errorf("held = %+v", rep.Held)
	}
	if got, _ := m.Store().GetPRD(running.ID); got.Status != PRDRunning {
		t.Errorf("running changed to %s", got.Status)
	}
	if got, _ := m.Store().GetPRD(done.ID); got.Status != PRDCompleted {
		t.Errorf("completed changed to %s", got.Status)
	}
	if got, _ := m.Store().GetPRD(held.ID); got.Hold.Reason != "first" {
		t.Errorf("existing hold changed: %+v", got.Hold)
	}
}

func TestHoldMigrationBusyRefusal(t *testing.T) {
	m, _, evalP, _, _, _ := holdMigrationFixture(t)
	runner := holdMigrationPRD(t, m, "Gate runner", "x", PRDRunning, "", TaskVerifying)
	runner.Story[0].Tasks[0].Kind = TaskKindFinalGate
	if err := m.Store().SavePRD(runner); err != nil {
		t.Fatal(err)
	}

	rep, err := m.MigrateToHold(HoldMigrationOpts{IDs: []string{evalP.ID}, Reason: "operator decision"})
	if !errors.Is(err, ErrHoldMigrationBusy) {
		t.Fatalf("err = %v, want ErrHoldMigrationBusy", err)
	}
	if !rep.Busy || len(rep.Held) != 0 {
		t.Errorf("report = %+v", rep)
	}
	if got, _ := m.Store().GetPRD(evalP.ID); got.Status != PRDFailed || got.Hold != nil {
		t.Errorf("busy run changed status to %s", got.Status)
	}
	// a dry run still reports while busy
	rep, err = m.MigrateToHold(HoldMigrationOpts{DryRun: true, IDs: []string{evalP.ID}})
	if err != nil || !rep.Busy || len(rep.Candidates) == 0 {
		t.Errorf("dry run while busy: err %v report %+v", err, rep)
	}
}

func TestHoldMigrationAPIWrappers(t *testing.T) {
	m, _, evalP, _, _, _ := holdMigrationFixture(t)
	a := &API{M: m}
	if got := a.HoldMigrationCandidates(); len(got) != 3 {
		t.Fatalf("candidates = %d", len(got))
	}
	rep, err := a.MigrateToHold(HoldMigrationOpts{IDs: []string{evalP.ID}, Reason: "operator decision", Actor: "dmz"})
	if err != nil || len(rep.Held) != 1 {
		t.Fatalf("err %v report %+v", err, rep)
	}
	if got, _ := m.Store().GetPRD(evalP.ID); got.Status != PRDHeld || got.Hold.HeldBy != "dmz" {
		t.Errorf("status %s", got.Status)
	}
}

// TestHoldMigrationNeverCalledFromLoopOrPass proves, from the package source,
// that MigrateToHold is called from exactly one place outside tests: the API
// wrapper the operator's command goes through. No loop, pass, housekeeping
// step or startup path calls it.
func TestHoldMigrationNeverCalledFromLoopOrPass(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %d", err, len(files))
	}
	type site struct{ file, fn string }
	var sites []site
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				switch rt := fd.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					if id, ok := rt.X.(*ast.Ident); ok {
						name = id.Name + "." + name
					}
				case *ast.Ident:
					name = rt.Name + "." + name
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					if fun.Sel.Name == "MigrateToHold" {
						sites = append(sites, site{f, name})
					}
				case *ast.Ident:
					if fun.Name == "MigrateToHold" {
						sites = append(sites, site{f, name})
					}
				}
				return true
			})
		}
	}

	loopLike := regexp.MustCompile(`(?i)loop|pass|housekeep|startup|boot|tick|sweep|scan|watch|reconcile|expire|expiry|resume|repair|run(One|All|Once)?$`)
	cases := []struct {
		name   string
		check  func(s site) bool // true = this call site is a violation
		reason string
	}{
		{"no call from a loop, pass, housekeeping or startup function", func(s site) bool { return loopLike.MatchString(s.fn) }, "loop-like function"},
		{"no call outside the API wrapper", func(s site) bool { return s.fn != "API.MigrateToHold" }, "not the API wrapper"},
		{"no call from a file other than api.go", func(s site) bool { return s.file != "api.go" }, "wrong file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, s := range sites {
				if c.check(s) {
					t.Errorf("MigrateToHold called from %s in %s (%s)", s.fn, s.file, c.reason)
				}
			}
		})
	}
	if want := []site{{"api.go", "API.MigrateToHold"}}; !reflect.DeepEqual(sites, want) {
		t.Errorf("call sites = %v, want %v", sites, want)
	}
}
