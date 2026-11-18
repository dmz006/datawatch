package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeChainOps struct {
	tasks    []ChainTask
	paths    map[string]ChainPathState
	pathErr  error
	resets   []string
	syncs    []string
	cleans   []string
	syncErr  error
	cleanErr error
	resetErr error
}

func (f *fakeChainOps) ChainTasks(context.Context) []ChainTask { return f.tasks }
func (f *fakeChainOps) ResetTask(_ context.Context, a, t string) error {
	f.resets = append(f.resets, a+"/"+t)
	return f.resetErr
}
func (f *fakeChainOps) PathState(_ context.Context, wt, rel string) (ChainPathState, error) {
	return f.paths[wt+":"+rel], f.pathErr
}
func (f *fakeChainOps) SyncDocs(_ context.Context, wt string) (string, error) {
	f.syncs = append(f.syncs, wt)
	return "", f.syncErr
}
func (f *fakeChainOps) Cleanup(_ context.Context, wt, rel string) (string, error) {
	f.cleans = append(f.cleans, wt+":"+rel)
	return "", f.cleanErr
}

type chainHarness struct {
	r      *Repairer
	ops    *fakeChainOps
	logs   []string
	alerts []string
	cfg    config.HousekeepingRepairsConfig
}

func newChainHarness(t *testing.T, mode string, ops *fakeChainOps) *chainHarness {
	t.Helper()
	zero := 0
	h := &chainHarness{ops: ops, cfg: config.HousekeepingRepairsConfig{TaskChain: mode, GraceSeconds: &zero}}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return h.cfg },
		Log:      func(l string) { h.logs = append(h.logs, l) },
		Alert:    func(_, title, _ string) { h.alerts = append(h.alerts, title) },
		Now:      func() time.Time { return time.Unix(1000, 0) },
	}, NewTaskChainRepairs(ops)...)
	return h
}

func depPair(depStatus TaskStatus) []ChainTask {
	return []ChainTask{
		{AutomatonID: "a1", TaskID: "t1", Status: depStatus},
		{AutomatonID: "a1", TaskID: "t2", Title: "second", Status: TaskFailed, DependsOn: []string{"t1"},
			FailureSource: dependencyInfraSource, FailureCategory: "infra"},
	}
}

func TestNewTaskChainRepairsNil(t *testing.T) {
	if NewTaskChainRepairs(nil) != nil {
		t.Fatal("nil ops should give none")
	}
	if got := NewTaskChainRepairs(&fakeChainOps{}); len(got) != 3 {
		t.Fatalf("want 3 repairs, got %d", len(got))
	}
}

func TestDependentResetOnceDependencyFixed(t *testing.T) {
	ops := &fakeChainOps{tasks: depPair(TaskCompleted)}
	h := newChainHarness(t, "on", ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairDid || len(ops.resets) != 1 || ops.resets[0] != "a1/t2" {
		t.Fatalf("recs=%v resets=%v", recs, ops.resets)
	}
	if len(h.logs) != 1 || !strings.Contains(h.logs[0], "undo:") || len(h.alerts) != 0 {
		t.Fatalf("logs=%v alerts=%v", h.logs, h.alerts)
	}
}

func TestDependentResetNotBeforeDependencyFixed(t *testing.T) {
	for _, st := range []TaskStatus{TaskFailed, TaskInProgress, TaskPending, TaskBlocked} {
		ops := &fakeChainOps{tasks: depPair(st)}
		h := newChainHarness(t, "on", ops)
		if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.resets) != 0 {
			t.Fatalf("dependency %s: acted: %v %v", st, recs, ops.resets)
		}
	}
	// A missing dependency is not fixed either.
	ops := &fakeChainOps{tasks: depPair(TaskCompleted)[1:]}
	if recs := newChainHarness(t, "on", ops).r.Run(context.Background()); len(recs) != 0 {
		t.Fatalf("missing dependency acted: %v", recs)
	}
}

func TestDependentResetIgnoresOtherFailures(t *testing.T) {
	ts := depPair(TaskCompleted)
	ts[1].FailureSource = "verifier-ask"
	ops := &fakeChainOps{tasks: ts}
	if recs := newChainHarness(t, "on", ops).r.Run(context.Background()); len(recs) != 0 {
		t.Fatalf("acted on a non-dependency failure: %v", recs)
	}
}

func TestDependentResetReportMode(t *testing.T) {
	ops := &fakeChainOps{tasks: depPair(TaskCompleted)}
	h := newChainHarness(t, "report", ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairWould || len(ops.resets) != 0 {
		t.Fatalf("recs=%v resets=%v", recs, ops.resets)
	}
}

func TestDependentResetSecondRetryRefused(t *testing.T) {
	ts := depPair(TaskCompleted)
	ts[1].PriorAttempts = 1
	ops := &fakeChainOps{tasks: ts}
	h := newChainHarness(t, "on", ops)
	recs := h.r.Run(context.Background())
	h.r.Run(context.Background()) // second pass must not alert again
	if len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(ops.resets) != 0 || len(h.alerts) != 1 {
		t.Fatalf("recs=%v resets=%v alerts=%v", recs, ops.resets, h.alerts)
	}
}

func TestDependentResetNeverRepeatsInProcess(t *testing.T) {
	ops := &fakeChainOps{tasks: depPair(TaskCompleted)}
	h := newChainHarness(t, "on", ops)
	h.r.Run(context.Background())
	recs := h.r.Run(context.Background())
	if len(ops.resets) != 1 || len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe {
		t.Fatalf("resets=%v recs=%v", ops.resets, recs)
	}
}

func TestDependentResetFailureRecorded(t *testing.T) {
	ops := &fakeChainOps{tasks: depPair(TaskCompleted), resetErr: errors.New("boom")}
	recs := newChainHarness(t, "on", ops).r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed {
		t.Fatalf("recs=%v", recs)
	}
}

func gateTask(detail string) ChainTask {
	return ChainTask{AutomatonID: "a1", TaskID: "g", Title: "final gate", Kind: TaskKindFinalGate,
		Status: TaskFailed, FailureCategory: "content", FailureDetail: detail, Worktree: "/wt"}
}

const staleDetail = "make check failed: internal/server/web/docs is out of date with docs/"

func TestStaleDocsGateSyncsAndRetriesOnce(t *testing.T) {
	ops := &fakeChainOps{tasks: []ChainTask{gateTask(staleDetail)}}
	h := newChainHarness(t, "on", ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairDid || len(ops.syncs) != 1 || ops.syncs[0] != "/wt" || len(ops.resets) != 1 {
		t.Fatalf("recs=%v syncs=%v resets=%v", recs, ops.syncs, ops.resets)
	}
	if !strings.Contains(recs[0].Done, "make sync-docs") {
		t.Fatalf("done=%q", recs[0].Done)
	}
	// Same gate failing again in the same process: a second retry is refused.
	recs = h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(ops.syncs) != 1 || len(h.alerts) != 1 {
		t.Fatalf("second pass recs=%v syncs=%v alerts=%v", recs, ops.syncs, h.alerts)
	}
}

func TestStaleDocsGateReportModeActsOnNothing(t *testing.T) {
	ops := &fakeChainOps{tasks: []ChainTask{gateTask(staleDetail)}}
	recs := newChainHarness(t, "report", ops).r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairWould || len(ops.syncs)+len(ops.resets) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestStaleDocsGateRefusals(t *testing.T) {
	cases := map[string]ChainTask{
		"no stale word": gateTask("internal/server/web/docs check exited 1"),
		"also stray":    gateTask(staleDetail + "; stray file: notes.txt"),
		"no worktree":   func() ChainTask { g := gateTask(staleDetail); g.Worktree = ""; return g }(),
		"prior retry":   func() ChainTask { g := gateTask(staleDetail); g.PriorAttempts = 1; return g }(),
	}
	for name, g := range cases {
		ops := &fakeChainOps{tasks: []ChainTask{g}}
		h := newChainHarness(t, "on", ops)
		recs := h.r.Run(context.Background())
		var skipped int
		for _, r := range recs {
			if r.Group == RepairGroupTaskChain && r.Kind == RepairSkippedUnsafe {
				skipped++
			}
		}
		if skipped < 1 || len(ops.syncs) != 0 || len(ops.resets) != 0 || len(h.alerts) != 1 {
			t.Fatalf("%s: recs=%v syncs=%v resets=%v alerts=%v", name, recs, ops.syncs, ops.resets, h.alerts)
		}
	}
}

func TestStaleDocsGateIgnoresOrdinaryTasksAndOtherFailures(t *testing.T) {
	g := gateTask(staleDetail)
	g.Kind = ""
	other := gateTask("unit tests failed")
	ops := &fakeChainOps{tasks: []ChainTask{g, other}}
	if recs := newChainHarness(t, "on", ops).r.Run(context.Background()); len(recs) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestStaleDocsGateSyncFailureDoesNotRetry(t *testing.T) {
	ops := &fakeChainOps{tasks: []ChainTask{gateTask(staleDetail)}, syncErr: errors.New("rsync missing")}
	recs := newChainHarness(t, "on", ops).r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed || len(ops.resets) != 0 {
		t.Fatalf("recs=%v resets=%v", recs, ops.resets)
	}
}

func leakTask(detail string) ChainTask {
	return ChainTask{AutomatonID: "a1", TaskID: "w", Title: "worker", Status: TaskFailed,
		FailureCategory: "content", FailureDetail: detail, Worktree: "/wt"}
}

func TestLeakedFileCleanedWhenUntrackedInWorktree(t *testing.T) {
	ops := &fakeChainOps{
		tasks: []ChainTask{leakTask("finish blocked by stray file: CHECKPOINT.md")},
		paths: map[string]ChainPathState{"/wt:CHECKPOINT.md": {Exists: true}},
	}
	h := newChainHarness(t, "on", ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairDid || len(ops.cleans) != 1 || ops.cleans[0] != "/wt:CHECKPOINT.md" || len(ops.resets) != 1 {
		t.Fatalf("recs=%v cleans=%v resets=%v", recs, ops.cleans, ops.resets)
	}
	// Retry is once only.
	recs = h.r.Run(context.Background())
	if len(ops.cleans) != 1 || len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe {
		t.Fatalf("second pass recs=%v cleans=%v", recs, ops.cleans)
	}
}

func TestLeakedFileFromEvidenceAndReportMode(t *testing.T) {
	lt := leakTask("finish blocked")
	lt.FailureDetail = "finish blocked by a leaked marker"
	lt.FailureEvidence = []string{"leaked marker: .done-marker"}
	ops := &fakeChainOps{tasks: []ChainTask{lt}, paths: map[string]ChainPathState{"/wt:.done-marker": {Exists: true}}}
	recs := newChainHarness(t, "report", ops).r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairWould || len(ops.cleans)+len(ops.resets) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestLeakedFileRefusals(t *testing.T) {
	tracked := map[string]ChainPathState{"/wt:main.go": {Exists: true, Tracked: true}}
	cases := map[string]struct {
		task  ChainTask
		paths map[string]ChainPathState
		err   error
	}{
		"tracked":         {leakTask("stray file: main.go"), tracked, nil},
		"absolute":        {leakTask("stray file: /etc/passwd"), nil, nil},
		"climbs out":      {leakTask("stray file: ../outside.txt"), nil, nil},
		"two files":       {leakTask("stray file: a.txt and stray file: b.txt"), nil, nil},
		"no path":         {leakTask("there are stray files"), nil, nil},
		"gone":            {leakTask("stray file: gone.txt"), nil, nil},
		"state unknown":   {leakTask("stray file: x.txt"), nil, errors.New("git failed")},
		"prior retry":     {func() ChainTask { l := leakTask("stray file: x.txt"); l.PriorAttempts = 1; return l }(), map[string]ChainPathState{"/wt:x.txt": {Exists: true}}, nil},
		"no worktree":     {func() ChainTask { l := leakTask("stray file: x.txt"); l.Worktree = ""; return l }(), nil, nil},
		"also stale docs": {leakTask("stray file: x.txt and internal/server/web/docs is stale"), map[string]ChainPathState{"/wt:x.txt": {Exists: true}}, nil},
	}
	for name, c := range cases {
		ops := &fakeChainOps{tasks: []ChainTask{c.task}, paths: c.paths, pathErr: c.err}
		h := newChainHarness(t, "on", ops)
		recs := h.r.Run(context.Background())
		if len(recs) < 1 || recs[0].Kind != RepairSkippedUnsafe || len(ops.cleans) != 0 || len(ops.resets) != 0 || len(h.alerts) != 1 {
			t.Fatalf("%s: recs=%v cleans=%v resets=%v alerts=%v", name, recs, ops.cleans, ops.resets, h.alerts)
		}
	}
}

func TestLeakedFileCleanupFailureDoesNotRetry(t *testing.T) {
	ops := &fakeChainOps{
		tasks:    []ChainTask{leakTask("stray file: x.txt")},
		paths:    map[string]ChainPathState{"/wt:x.txt": {Exists: true}},
		cleanErr: errors.New("denied"),
	}
	recs := newChainHarness(t, "on", ops).r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed || len(ops.resets) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestTaskChainModeOffAndGroupName(t *testing.T) {
	ops := &fakeChainOps{tasks: depPair(TaskCompleted)}
	h := newChainHarness(t, "off", ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.resets) != 0 {
		t.Fatalf("off acted: %v", recs)
	}
	for _, r := range NewTaskChainRepairs(ops) {
		if r.Group() != RepairGroupTaskChain || r.UndoHint(RepairFinding{}) == "" {
			t.Fatalf("bad repair %T", r)
		}
	}
}

func TestChainApplyRejectsBadSubjectAndGoneTask(t *testing.T) {
	ops := &fakeChainOps{}
	for _, r := range NewTaskChainRepairs(ops) {
		if _, err := r.Apply(context.Background(), RepairFinding{Subject: "bogus"}); err == nil {
			t.Fatalf("%T accepted a bad subject", r)
		}
		if _, err := r.Apply(context.Background(), RepairFinding{Subject: "task a/b"}); err == nil {
			t.Fatalf("%T accepted a gone task", r)
		}
	}
}

func TestCleanRelPath(t *testing.T) {
	for in, want := range map[string]string{"a.txt": "a.txt", "./a/../b.txt": "b.txt", "d/e.txt": "d/e.txt"} {
		if got, ok := cleanRelPath(in); !ok || got != want {
			t.Fatalf("%q -> %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"", ".", "..", "../x", "/abs", "a/../../x"} {
		if _, ok := cleanRelPath(in); ok {
			t.Fatalf("%q accepted", in)
		}
	}
}

func TestCommandChainOps(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, dir string, argv []string) (string, error) {
		calls = append(calls, append([]string{dir}, argv...))
		if argv[1] == "ls-files" && argv[2] == "--others" {
			return "x.txt\n", nil
		}
		if argv[1] == "ls-files" {
			return "", nil
		}
		return "", nil
	}
	c := CommandChainOps{Run: run}
	st, err := c.PathState(context.Background(), "/wt", "x.txt")
	if err != nil || !st.Exists || st.Tracked {
		t.Fatalf("st=%v err=%v", st, err)
	}
	if _, err := c.SyncDocs(context.Background(), "/wt"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Cleanup(context.Background(), "/wt", "x.txt"); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if strings.Join(last, " ") != "/wt git clean -f -- x.txt" {
		t.Fatalf("cleanup argv %v", last)
	}
	if strings.Join(calls[2], " ") != "/wt make sync-docs" {
		t.Fatalf("sync argv %v", calls[2])
	}
	if _, err := c.Cleanup(context.Background(), "/wt", "../x"); err == nil {
		t.Fatal("cleanup accepted a path outside the worktree")
	}
	if _, err := c.PathState(context.Background(), "/wt", "/abs"); err == nil {
		t.Fatal("path state accepted an absolute path")
	}
	// A tracked file reports tracked.
	c.Run = func(_ context.Context, _ string, argv []string) (string, error) { return "main.go\n", nil }
	if st, _ := c.PathState(context.Background(), "/wt", "main.go"); !st.Tracked {
		t.Fatal("tracked file not reported tracked")
	}
	// A git failure is an error, never "untracked".
	c.Run = func(context.Context, string, []string) (string, error) { return "", errors.New("no git") }
	if _, err := c.PathState(context.Background(), "/wt", "x"); err == nil {
		t.Fatal("git failure read as a state")
	}
}

func TestExecChainCommand(t *testing.T) {
	if _, err := ExecChainCommand(context.Background(), t.TempDir(), nil); err == nil {
		t.Fatal("empty command accepted")
	}
	out, err := ExecChainCommand(context.Background(), t.TempDir(), []string{"echo", "hi"})
	if err != nil || strings.TrimSpace(out) != "hi" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestManagerChainOpsNilAPI(t *testing.T) {
	var o ManagerChainOps
	if o.ChainTasks(context.Background()) != nil {
		t.Fatal("nil API listed tasks")
	}
	if err := o.ResetTask(context.Background(), "a", "b"); err == nil {
		t.Fatal("nil API reset")
	}
}
