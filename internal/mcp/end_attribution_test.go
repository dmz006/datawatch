// Tests for the end reason the MCP kill tools record: the journal and the
// session timeline name the tool (by=) and the caller (reason=).

package mcp

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

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

// newEndAttrServer returns an admin-mode MCP server whose journal is captured.
func newEndAttrServer(t *testing.T) (*Server, *endAttrJournal) {
	t.Helper()
	s := bl91Server(t)
	j := &endAttrJournal{}
	s.manager.SetJournal(j)
	return s, j
}

// endAttrSession saves a running session with a timeline tracker, so its end
// reaches timeline.md the way a live session's does.
func endAttrSession(t *testing.T, s *Server, id string) *session.Session {
	t.Helper()
	sess := mustSaveSession(t, s, id, "")
	if _, err := session.NewTracker(s.dataDir, sess); err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return sess
}

// assertEndAttrRecorded checks that the end reached the journal and the
// timeline, each naming the caller and the reason.
func assertEndAttrRecorded(t *testing.T, s *Server, j *endAttrJournal, sess *session.Session, caller, reason string) {
	t.Helper()
	lines, err := session.ResumeTracker(s.dataDir, sess).ReadTimeline()
	if err != nil {
		t.Fatalf("ReadTimeline: %v", err)
	}
	var ended []string
	for _, line := range lines {
		if strings.Contains(line, "session ended") {
			ended = append(ended, line)
		}
	}
	by := "by=" + caller
	why := "reason=" + reason
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

func TestEndAttributionMCPKillSession(t *testing.T) {
	s, j := newEndAttrServer(t)
	sess := endAttrSession(t, s, "mk01")

	out, err := s.handleKillSession(context.Background(), call(map[string]any{"session_id": sess.FullID}))
	if res := resultText(t, out, err); !strings.Contains(res, "killed") {
		t.Fatalf("kill_session reply = %q, want a killed reply", res)
	}

	assertEndAttrRecorded(t, s, j, sess, endCallerMCPKillSession, "MCP kill_session requested by operator")
}

func TestEndAttributionMCPStopAllSessions(t *testing.T) {
	s, j := newEndAttrServer(t)
	sess := endAttrSession(t, s, "sa01")

	out, err := s.handleStopAllSessions(context.Background(), call(nil))
	if res := resultText(t, out, err); !strings.Contains(res, "Stopped 1 session(s)") {
		t.Fatalf("stop_all_sessions reply = %q, want one session stopped", res)
	}

	assertEndAttrRecorded(t, s, j, sess, endCallerMCPStopAll, "MCP stop_all_sessions requested by operator")
}
