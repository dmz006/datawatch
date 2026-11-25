package autonomous

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// Every process started from a session's pane now carries the session id. That
// alone must not make the session-end reap end it: before the id reached panes
// such a process was never matched. Only Find is called; nothing is signalled.
func TestOrphanReaperPaneSessionIDAloneIsNoEvidence(t *testing.T) {
	if _, err := os.Stat("/proc/self/environ"); err != nil {
		t.Skip("no /proc")
	}
	id := fmt.Sprintf("testhost-paneenv-%d", os.Getpid())
	marker := session.PaneSessionMarkerEnv + "=" + session.PaneSessionMarker(id)
	start := func(env ...string) int {
		t.Helper()
		c := exec.Command("sleep", "60")
		c.Env = env
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() }) // our own child
		return c.Process.Pid
	}
	cases := []struct {
		name string
		pid  int
		want bool
	}{
		{"started from this session's pane", start("DATAWATCH_SESSION_ID="+id, marker), false},
		{"subprocess session task", start("DATAWATCH_SESSION_ID=" + id), true},
		{"session bridge started from the pane", start("DATAWATCH_SESSION_ID="+id, marker, "CLAUDE_SESSION_ID="+id), true},
		// A daemon run from another session's pane hands that pane's marker
		// down to its own subprocess sessions.
		{"inherited marker of another session", start("DATAWATCH_SESSION_ID="+id,
			session.PaneSessionMarkerEnv+"="+session.PaneSessionMarker("testhost-other")), true},
	}
	r := &OrphanReaper{}
	deadline := time.Now().Add(5 * time.Second)
	var got map[int]bool
	for {
		got = map[int]bool{}
		for _, p := range r.Find(OrphanEvidence{SessionID: id}) {
			got[p.PID] = true
		}
		if (got[cases[1].pid] && got[cases[2].pid] && got[cases[3].pid]) || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, c := range cases {
		if got[c.pid] != c.want {
			t.Errorf("%s: matched=%v, want %v", c.name, got[c.pid], c.want)
		}
	}
}
