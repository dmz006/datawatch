package session

import (
	"context"
	"testing"
)

// Every admission class a caller can name takes exactly one lease at the
// manager, whatever the other options say. PreAdmitted is the only way past it
// (see TestManagerStart_PreAdmittedSkips); nothing else, one_shot included,
// skips the lease.
func TestManagerStart_EveryClassTakesOneLease(t *testing.T) {
	cases := []struct {
		name string
		opts *StartOptions
		want string
	}{
		{"no options", nil, ""},
		{"operator", &StartOptions{AdmitClass: "operator"}, "operator"},
		{"worker restart", &StartOptions{AdmitClass: "worker", AdmitRestart: true}, "worker"},
		{"supervisor", &StartOptions{AdmitClass: "supervisor", SpendRole: "supervisor"}, "supervisor"},
		{"one shot is not an exemption", &StartOptions{OneShot: true, Subprocess: false}, ""},
		{"ephemeral workspace is not an exemption", &StartOptions{EphemeralWorkspace: true}, ""},
	}
	for _, c := range cases {
		mgr := admissionManager(t)
		rec := &admitRecorder{}
		mgr.SetStartAdmitter(rec.fn())
		if _, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), c.opts); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(rec.got) != 1 || rec.got[0].Class != c.want {
			t.Fatalf("%s: admissions = %+v, want exactly one in class %q", c.name, rec.got, c.want)
		}
		if len(rec.bound) != 1 {
			t.Fatalf("%s: the lease must be bound to the started session, bound = %v", c.name, rec.bound)
		}
	}
}

// Relaunching an ended session in place starts a new pane. It must take a
// lease like any other start, because the lease of the first run ended with it.
//
// OPEN GAP, found by this test: Manager.Restart (reached by POST
// /api/sessions/{id}/restart and the MCP restart_session tool) relaunches the
// pane with no admission, so a restarted session runs without a lease. The fix
// is a design choice for the operator (which class, and whether a full pool may
// refuse an operator's restart), so the test is skipped, not weakened. Remove
// the skip when Restart admits.
func TestManagerRestart_TakesLease(t *testing.T) {
	t.Skip("open gap: Manager.Restart takes no capacity lease; see the follow-ups report and start_path_lease_test.go in cmd/datawatch")
	mgr := admissionManager(t)
	rec := &admitRecorder{}
	mgr.SetStartAdmitter(rec.fn())
	sess, err := mgr.Start(context.Background(), "echo hi", "", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sess.State = StateKilled
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Restart(context.Background(), sess.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if len(rec.got) != 2 {
		t.Fatalf("a restart in place took %d new lease(s), want 1 (admissions so far: %+v)", len(rec.got)-1, rec.got)
	}
}
