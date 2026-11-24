package session

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func admissionManager(t *testing.T) *Manager {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, _, _ string) error {
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})
	return mgr
}

type admitRecorder struct {
	got      []StartAdmission
	released int
	bound    []string
	refuse   error
}

func (a *admitRecorder) fn() StartAdmitFn {
	return func(_ context.Context, s StartAdmission) (func(), func(string), error) {
		a.got = append(a.got, s)
		if a.refuse != nil {
			return nil, nil, a.refuse
		}
		return func() { a.released++ }, func(id string) { a.bound = append(a.bound, id) }, nil
	}
}

// With an admitter set, a start nobody admitted upstream is admitted at the
// manager before it launches, and the lease is bound to the new session.
func TestManagerStart_AdmitsUnadmittedStart(t *testing.T) {
	mgr := admissionManager(t)
	rec := &admitRecorder{}
	mgr.SetStartAdmitter(rec.fn())
	sess, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), &StartOptions{Backend: "claude-code", Model: "sonnet", AdmitClass: "supervisor", SpendRole: "supervisor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].Class != "supervisor" || rec.got[0].Backend != "claude-code" || rec.got[0].Model != "sonnet" || rec.got[0].Role != "supervisor" {
		t.Fatalf("admission = %+v", rec.got)
	}
	if len(rec.bound) != 1 || rec.bound[0] != sess.FullID {
		t.Fatalf("bound = %v, want the started session %s", rec.bound, sess.FullID)
	}
	if rec.released != 0 {
		t.Fatal("a successful start keeps its lease")
	}
}

// No options at all is still admitted: nothing is exempt for lack of a flag.
func TestManagerStart_NilOptionsStillAdmitted(t *testing.T) {
	mgr := admissionManager(t)
	rec := &admitRecorder{}
	mgr.SetStartAdmitter(rec.fn())
	if _, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].Class != "" {
		t.Fatalf("admission = %+v, want one call with an empty class (operator)", rec.got)
	}
}

// PreAdmitted skips the manager's admission: the caller already took the lease.
func TestManagerStart_PreAdmittedSkips(t *testing.T) {
	mgr := admissionManager(t)
	rec := &admitRecorder{}
	mgr.SetStartAdmitter(rec.fn())
	if _, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), &StartOptions{PreAdmitted: true}); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 0 {
		t.Fatalf("admission calls = %d, want 0 for a pre-admitted start", len(rec.got))
	}
}

// A refused admission starts nothing; a failed launch gives the lease back.
func TestManagerStart_RefusalAndLaunchFailureRelease(t *testing.T) {
	mgr := admissionManager(t)
	rec := &admitRecorder{refuse: errors.New("pool full")}
	mgr.SetStartAdmitter(rec.fn())
	before := len(mgr.ListSessions())
	if _, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), nil); err == nil {
		t.Fatal("a refused admission must refuse the start")
	}
	if len(mgr.ListSessions()) != before {
		t.Fatal("a refused start must not create a session")
	}
	// A launch that fails after admission gives the lease back.
	rec = &admitRecorder{}
	mgr.SetStartAdmitter(rec.fn())
	mgr.SetLLMBackend("test", func(_ context.Context, _, _, _, _ string) error { return errors.New("launch failed") })
	if _, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), nil); err == nil {
		t.Fatal("a failing launch must fail the start")
	}
	if len(rec.got) != 1 || rec.released != 1 || len(rec.bound) != 0 {
		t.Fatalf("admitted %d, released %d, bound %d: want the lease given back, nothing bound", len(rec.got), rec.released, len(rec.bound))
	}
}

// The supervisor class may start above max_sessions by the host buffer; every
// other class stops at the cap.
func TestManagerStart_SupervisorSlotsAboveSessionCap(t *testing.T) {
	mgr := admissionManager(t)
	mgr.SetMaxSessions(1)
	mgr.SetSupervisorSlots(1)
	dir := t.TempDir()
	if _, err := mgr.Start(context.Background(), "echo a", "", dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Start(context.Background(), "echo b", "", dir, nil); err == nil {
		t.Fatal("an operator start must stop at max_sessions")
	}
	if _, err := mgr.Start(context.Background(), "echo c", "", dir, &StartOptions{AdmitClass: "supervisor"}); err != nil {
		t.Fatalf("a supervisor start must use the host buffer: %v", err)
	}
	if _, err := mgr.Start(context.Background(), "echo d", "", dir, &StartOptions{AdmitClass: "supervisor"}); err == nil {
		t.Fatal("the host buffer holds one: a second supervisor must stop")
	}
}

func TestAdmitClassForLaunchRole(t *testing.T) {
	if AdmitClassForLaunchRole("supervisor") != "supervisor" || AdmitClassForLaunchRole("") != "" || AdmitClassForLaunchRole("worker") != "" {
		t.Fatal("only the supervisor role maps to the supervisor class")
	}
}

// A restart of a worker session keeps the worker class and re-enters the queue
// at the head; an operator session's restart stays an operator start.
func TestRestartAdmissionKeepsWorkerClass(t *testing.T) {
	class, resume := RestartAdmission(&Session{SpendRole: "worker", TaskID: "t1", PRDID: "p1"})
	if class != "worker" || !resume {
		t.Fatalf("worker restart = %q, %v, want worker, true", class, resume)
	}
	class, resume = RestartAdmission(&Session{})
	if class != "" || resume {
		t.Fatalf("operator session restart = %q, %v, want empty class, false", class, resume)
	}
}

// The manager tells the admitter a start is a restart.
func TestManagerStart_PassesRestartFlagToAdmitter(t *testing.T) {
	mgr := admissionManager(t)
	rec := &admitRecorder{}
	mgr.SetStartAdmitter(rec.fn())
	if _, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), &StartOptions{AdmitClass: "worker", AdmitRestart: true}); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].Class != "worker" || !rec.got[0].Restart {
		t.Fatalf("admission = %+v, want worker class with Restart", rec.got)
	}
}

// An operator start at max_sessions succeeds into the operator allowance and
// records how far over the cap it is; past the allowance it is refused with a
// reason that names the setting; a worker never uses the allowance.
func TestManagerStart_OperatorAllowanceAboveSessionCap(t *testing.T) {
	mgr := admissionManager(t)
	mgr.SetMaxSessions(1)
	mgr.SetOperatorSlots(2)
	var over []int
	mgr.SetOverSessionCapHook(func(class string, overBy int) { over = append(over, overBy) })
	dir := t.TempDir()
	if _, err := mgr.Start(context.Background(), "echo a", "", dir, nil); err != nil {
		t.Fatal(err)
	}
	if len(over) != 0 {
		t.Fatalf("a start within the cap reported over-cap: %v", over)
	}
	// A worker is refused at the cap: the allowance is for operators.
	_, err := mgr.Start(context.Background(), "echo w", "", dir, &StartOptions{AdmitClass: "worker"})
	if err == nil || strings.Contains(err.Error(), "allowance") {
		t.Fatalf("worker err = %v, want the plain cap refusal", err)
	}
	// Operators go into the allowance (no options, or class operator).
	s2, err := mgr.Start(context.Background(), "echo b", "", dir, &StartOptions{AdmitClass: "operator"})
	if err != nil {
		t.Fatalf("operator start at the cap: %v", err)
	}
	if s2.OverSessionCap != 1 {
		t.Fatalf("OverSessionCap = %d, want 1 (one over max_sessions)", s2.OverSessionCap)
	}
	if _, err := mgr.Start(context.Background(), "echo c", "", dir, nil); err != nil {
		t.Fatalf("second operator start into the allowance: %v", err)
	}
	if len(over) != 2 || over[0] != 1 || over[1] != 2 {
		t.Fatalf("over-cap reports = %v, want [1 2]", over)
	}
	_, err = mgr.Start(context.Background(), "echo d", "", dir, nil)
	if err == nil || !strings.Contains(err.Error(), "autonomous.capacity_operator_reserve_host") || !strings.Contains(err.Error(), "max sessions (1)") {
		t.Fatalf("past the allowance err = %v, want a plain reason naming autonomous.capacity_operator_reserve_host", err)
	}
	// Supervisors keep their own host slots, separate from the allowance.
	mgr.SetSupervisorSlots(1)
	if _, err := mgr.Start(context.Background(), "echo s", "", dir, &StartOptions{AdmitClass: "supervisor"}); err == nil {
		t.Fatal("the supervisor limit is cap plus supervisor slots; with 3 running (cap 1 + 2 operator) it must be full")
	}
}

func TestManagerSessionCapStatus(t *testing.T) {
	mgr := admissionManager(t)
	mgr.SetMaxSessions(1)
	mgr.SetOperatorSlots(2)
	mgr.SetSupervisorSlots(1)
	st := mgr.SessionCap()
	if st.Max != 1 || st.OperatorSlots != 2 || st.SupervisorSlots != 1 || st.Active != 0 || st.Over != 0 {
		t.Fatalf("status = %+v", st)
	}
}
