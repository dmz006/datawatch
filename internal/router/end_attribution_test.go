// Tests for the end reason the comm channel kill command records: the journal
// and the session timeline name the command (by=) and the channel (reason=).

package router

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// endAttrJournal is a journal writer that is safe for the goroutines a
// session end can start.
type endAttrJournal struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (j *endAttrJournal) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.b.Write(p)
}

func (j *endAttrJournal) String() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.b.String()
}

func TestEndAttributionCommChannelKill(t *testing.T) {
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	sm.WithFakeTmux()
	j := &endAttrJournal{}
	sm.SetJournal(j)
	t.Cleanup(sm.WaitBackground)

	now := time.Now()
	sess := &session.Session{
		ID: "rk01", FullID: "h-rk01", Hostname: "h", TmuxSession: "cs-rk01",
		Task: "end attribution", State: session.StateRunning, CreatedAt: now, UpdatedAt: now,
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if _, err := session.NewTracker(dir, sess); err != nil {
		t.Fatalf("NewTracker: %v", err)
	}

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
		manager:  sm,
	}
	r.handleKill(Command{Type: CmdKill, SessionID: sess.FullID})

	lines, err := session.ResumeTracker(dir, sess).ReadTimeline()
	if err != nil {
		t.Fatalf("ReadTimeline: %v", err)
	}
	var ended []string
	for _, line := range lines {
		if strings.Contains(line, "session ended") {
			ended = append(ended, line)
		}
	}
	by := "by=" + endCallerCommChannelKill
	why := "reason=comm channel kill from test"
	if out := j.String(); !strings.Contains(out, by) || !strings.Contains(out, why) {
		t.Errorf("journal lacks %q and %q:\n%s", by, why, out)
	}
	if len(ended) != 1 {
		t.Fatalf("want one session-ended timeline line, got %d: %q", len(ended), ended)
	}
	if !strings.Contains(ended[0], by) || !strings.Contains(ended[0], why) {
		t.Errorf("timeline line %q lacks %q and %q", ended[0], by, why)
	}
}
