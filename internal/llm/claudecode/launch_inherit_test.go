// The claude-code backend is one object shared by every session. The
// --permission-mode and --model flags of a launch or restart come from that
// session alone (or the daemon-start configuration), never from whatever the
// previous launch left on the backend.

package claudecode

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/session"
)

type inheritRig struct {
	t    *testing.T
	b    *Backend
	mgr  *session.Manager
	argv string
}

// newInheritRig wires the real backend to a manager with a fake tmux.
// configured is the permission mode the daemon sets at start ("" = none).
func newInheritRig(t *testing.T, configured string) *inheritRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	argv := startFakeTmux(t, false)
	b := backendOf()
	if configured != "" {
		b.SetPermissionMode(configured) // as cmd/datawatch does at start
	}
	prev, prevErr := llm.Get(b.Name())
	llm.Register(b)
	t.Cleanup(func() {
		if prevErr == nil {
			llm.Register(prev)
		}
	})
	mgr, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.AllowProjectRoot(os.TempDir())
	mgr.WithFakeTmux()
	return &inheritRig{t: t, b: b, mgr: mgr, argv: argv}
}

func (r *inheritRig) start(mode, model string) (*session.Session, string) {
	r.t.Helper()
	sess, err := r.mgr.Start(context.Background(), "task", "", r.t.TempDir(), &session.StartOptions{
		Backend: r.b.Name(), BackendObj: r.b, LaunchFn: r.b.Launch,
		PermissionMode: mode, Model: model,
	})
	if err != nil {
		r.t.Fatalf("Start: %v", err)
	}
	return sess, recordedCmd(r.t, r.argv)
}

func (r *inheritRig) restart(sess *session.Session) string {
	r.t.Helper()
	cur, ok := r.mgr.GetSession(sess.FullID)
	if !ok {
		r.t.Fatalf("session %s gone", sess.FullID)
	}
	cur.State = session.StateKilled
	if err := r.mgr.SaveSession(cur); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.mgr.Restart(context.Background(), sess.FullID); err != nil {
		r.t.Fatalf("Restart: %v", err)
	}
	return recordedCmd(r.t, r.argv)
}

func wantFlag(t *testing.T, what, cmd, flag, value string) {
	t.Helper()
	want := flag + " " + shellQuote(value)
	if value == "" {
		if strings.Contains(cmd, flag+" ") {
			t.Errorf("%s: command must carry no %s, got:\n%s", what, flag, cmd)
		}
		return
	}
	if !strings.Contains(cmd, want) {
		t.Errorf("%s: command must carry %s, got:\n%s", what, want, cmd)
	}
}

func TestStartAndRestartDoNotInheritPermissionMode(t *testing.T) {
	for _, configured := range []string{"", "acceptEdits"} {
		t.Run("configured="+configured, func(t *testing.T) {
			r := newInheritRig(t, configured)
			c, _ := r.start("", "haiku")
			_, aCmd := r.start("plan", "sonnet")
			wantFlag(t, "A names plan", aCmd, "--permission-mode", "plan")
			_, bCmd := r.start("", "sonnet")
			wantFlag(t, "B names none", bCmd, "--permission-mode", configured)
			r.start("plan", "sonnet")
			cCmd := r.restart(c)
			wantFlag(t, "restart of C, which named none", cCmd, "--permission-mode", configured)
			wantFlag(t, "restart of C keeps its model", cCmd, "--model", "haiku")
		})
	}
}

func TestRestartKeepsTheSessionsOwnPermissionMode(t *testing.T) {
	r := newInheritRig(t, "acceptEdits")
	a, _ := r.start("plan", "sonnet")
	r.start("bypassPermissions", "sonnet")
	wantFlag(t, "restart of A", r.restart(a), "--permission-mode", "plan")
}

func TestRestartOfRecordWithoutModelDoesNotInheritOne(t *testing.T) {
	r := newInheritRig(t, "")
	old, _ := r.start("", "haiku")
	cur, _ := r.mgr.GetSession(old.FullID)
	cur.Model = "" // a record from before the model was stored
	r.start("", "opus")
	wantFlag(t, "restart of a record with no model", r.restart(old), "--model", "")
}
