package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type fakeSessions struct {
	ids    []string
	leases int
}

func (f *fakeSessions) RunningSessionIDs(context.Context) ([]string, error) { return f.ids, nil }
func (f *fakeSessions) HeldLeaseCount(context.Context) (int, error)         { return f.leases, nil }

type fakeQuestions struct{ got []autonomous.Question }

func (f *fakeQuestions) RaiseUnlessOpenSourceLine(q autonomous.Question) (autonomous.Question, bool, error) {
	f.got = append(f.got, q)
	return q, true, nil
}

type fakeOutcomes struct{ recs []OutcomeRecord }

func (f *fakeOutcomes) Append(r OutcomeRecord) error { f.recs = append(f.recs, r); return nil }

const (
	tHealth  = "check-health"
	tRestart = "restart-daemon"
)

type resumeEnv struct {
	r    *Resumer
	cmd  *fakeCmd
	st   *memStore
	q    *fakeQuestions
	out  *fakeOutcomes
	bin  string
	snap string
}

func newResumeEnv(t *testing.T, sess *fakeSessions) *resumeEnv {
	t.Helper()
	data := t.TempDir()
	bin := filepath.Join(t.TempDir(), "daemon")
	if err := os.WriteFile(bin, []byte("new-broken-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(data, "deploy", "binary.snapshot")
	if err := os.MkdirAll(filepath.Dir(snap), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, []byte("old-good-binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := config.DeployConfig{BinaryPath: bin, RestartCommand: tRestart, HealthCheck: tHealth, HealthTimeout: "4s"}
	cmd := &fakeCmd{codes: map[string]int{}}
	st := &memStore{}
	_ = st.Save(State{Phase: PhaseRestarting, SnapshotPath: snap,
		PreRestart: PreRestart{SessionIDs: []string{"s1", "s2"}, LeaseCount: 2}})
	q, out := &fakeQuestions{}, &fakeOutcomes{}
	return &resumeEnv{cmd: cmd, st: st, q: q, out: out, bin: bin, snap: snap,
		r: &Resumer{Config: cfg, Dir: t.TempDir(), DataDir: data, Cmd: cmd,
			Clock: &fakeClock{t: time.Unix(1000, 0)}, Store: st, Sessions: sess,
			Questions: q, Outcomes: out}}
}

func TestDeployResumeHealthyIsDoneAndIdempotent(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{ids: []string{"s1", "s2", "s3"}, leases: 3})
	res, err := e.r.Resume(context.Background())
	if err != nil || res.Phase != PhaseDone || !res.Acted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(e.out.recs) != 1 || e.out.recs[0].Outcome != OutcomeOK {
		t.Fatalf("records=%+v", e.out.recs)
	}
	calls, saves := len(e.cmd.calls), e.st.saves
	res, err = e.r.Resume(context.Background())
	if err != nil || res.Acted || len(e.cmd.calls) != calls || e.st.saves != saves || len(e.out.recs) != 1 {
		t.Fatalf("second resume acted: res=%+v err=%v", res, err)
	}
	if len(e.q.got) != 0 {
		t.Fatal("a healthy deploy raised a question")
	}
}

func TestDeployResumeMissingHealthCheckPasses(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{ids: []string{"s1", "s2"}, leases: 2})
	e.r.Config.HealthCheck = ""
	res, _ := e.r.Resume(context.Background())
	if res.Phase != PhaseDone || len(e.cmd.calls) != 0 {
		t.Fatalf("res=%+v calls=%v", res, e.cmd.calls)
	}
}

func TestDeployRollbackRestoresExactSnapshotBytes(t *testing.T) {
	// s2 never came back: a failed check, so the snapshot goes back.
	e := newResumeEnv(t, &fakeSessions{ids: []string{"s1"}, leases: 2})
	res, err := e.r.Resume(context.Background())
	if err != nil || res.Phase != PhaseRolledBack {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	got, _ := os.ReadFile(e.bin)
	want, _ := os.ReadFile(e.snap)
	if string(got) != string(want) || string(got) != "old-good-binary" {
		t.Fatalf("binary = %q", got)
	}
	fi, _ := os.Stat(e.bin)
	if fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	if n := countOf(e.cmd.calls, tRestart); n != 1 {
		t.Fatalf("restart ran %d times: %v", n, e.cmd.calls)
	}
	if len(e.q.got) != 1 || !strings.Contains(e.q.got[0].Text, "s2") ||
		!strings.Contains(e.q.got[0].Text, "restored: yes") || !strings.Contains(e.q.got[0].Text, "healthy") {
		t.Fatalf("question = %+v", e.q.got)
	}
	if len(e.out.recs) != 1 || e.out.recs[0].Outcome != OutcomeRolledBack || !e.out.recs[0].Restored {
		t.Fatalf("records = %+v", e.out.recs)
	}
	if s, _, _ := e.st.Load(); s.Phase != PhaseRolledBack {
		t.Fatalf("phase = %s", s.Phase)
	}
	if again, _ := e.r.Resume(context.Background()); again.Acted {
		t.Fatal("rolled_back state acted again")
	}
}

func TestDeployRollbackOnShortLeaseCount(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{ids: []string{"s1", "s2"}, leases: 1})
	res, _ := e.r.Resume(context.Background())
	if res.Phase != PhaseRolledBack || !strings.Contains(res.Reason, "leases") {
		t.Fatalf("res=%+v", res)
	}
}

func TestDeployRollbackOnHealthFailure(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{ids: []string{"s1", "s2"}, leases: 2})
	e.cmd.codes[tHealth] = 1
	res, _ := e.r.Resume(context.Background())
	if res.Phase != PhaseRolledBack || !strings.Contains(res.Reason, "health check") {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(e.q.got[0].Text, "failing") {
		t.Fatalf("final health not reported: %s", e.q.got[0].Text)
	}
}

func TestDeployRollbackRestoreFailureGivesRollbackFailed(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{ids: nil, leases: 0})
	if err := os.Remove(e.snap); err != nil {
		t.Fatal(err)
	}
	res, err := e.r.Resume(context.Background())
	if err != nil || res.Phase != PhaseRollbackFailed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got, _ := os.ReadFile(e.bin); string(got) != "new-broken-binary" {
		t.Fatalf("binary changed: %q", got)
	}
	if countOf(e.cmd.calls, tRestart) != 0 {
		t.Fatal("restart ran after a failed restore")
	}
	if len(e.q.got) != 1 || !strings.Contains(e.q.got[0].Text, "No further automatic action") {
		t.Fatalf("question = %+v", e.q.got)
	}
	if e.out.recs[0].Outcome != OutcomeRollbackFailed {
		t.Fatalf("records = %+v", e.out.recs)
	}
	if again, _ := e.r.Resume(context.Background()); again.Acted {
		t.Fatal("rollback_failed state acted again")
	}
}

func TestDeployRollbackSecondRestartFailure(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{ids: nil})
	e.cmd.codes[tRestart] = 1
	res, _ := e.r.Resume(context.Background())
	if res.Phase != PhaseRollbackFailed {
		t.Fatalf("res=%+v", res)
	}
	if got, _ := os.ReadFile(e.bin); string(got) != "old-good-binary" {
		t.Fatalf("binary not restored: %q", got)
	}
}

func TestDeployResumeIgnoresOtherPhases(t *testing.T) {
	e := newResumeEnv(t, &fakeSessions{})
	_ = e.st.Save(State{Phase: PhaseInstallFailed})
	if res, _ := e.r.Resume(context.Background()); res.Acted || len(e.cmd.calls) != 0 {
		t.Fatalf("res=%+v", res)
	}
}

func TestDeployDeferredStateIsRetried(t *testing.T) {
	st := &memStore{}
	_ = st.Save(State{Phase: PhaseDeferred})
	clock := &fakeClock{t: time.Unix(0, 0)}
	calls := 0
	retry := func(context.Context) (Result, error) {
		calls++
		if calls == 1 {
			return Result{Status: StatusDeferred}, nil
		}
		_ = st.Save(State{Phase: PhaseRestarting})
		return Result{Status: StatusRestarting}, nil
	}
	res, err := RetryDeferred(context.Background(), st, clock, retry)
	if err != nil || res.Status != StatusRestarting || calls != 2 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, calls)
	}
	res, _ = RetryDeferred(context.Background(), st, clock, retry)
	if res.Status != StatusSkipped || calls != 2 {
		t.Fatalf("non-deferred state was retried: %+v calls=%d", res, calls)
	}
}

func TestDeployRetryEntryPointRunsDeferredRunner(t *testing.T) {
	r, cmd, drain, st := setup(t, config.DeployConfig{RestartCommand: tRestart})
	_ = st.Save(State{Phase: PhaseDeferred})
	drain.n = 0
	res, err := RetryDeferred(context.Background(), st, r.Clock, r.Retry)
	if err != nil || res.Status != StatusRestarting || countOf(cmd.calls, tRestart) != 1 {
		t.Fatalf("res=%+v err=%v calls=%v", res, err, cmd.calls)
	}
}

func TestDeployResumeStoreErrorSurfaces(t *testing.T) {
	r := &Resumer{Store: errStore{}, Clock: &fakeClock{}}
	if _, err := r.Resume(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

type errStore struct{}

func (errStore) Save(State) error           { return errors.New("x") }
func (errStore) Load() (State, bool, error) { return State{}, false, errors.New("load") }

func countOf(l []string, s string) int {
	n := 0
	for _, x := range l {
		if x == s {
			n++
		}
	}
	return n
}
