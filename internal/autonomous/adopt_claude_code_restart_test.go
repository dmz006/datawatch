// Boot-resume of claude-code one-shot workers after a daemon restart:
//
//   - a live one-shot session with a persisted TaskDeliveredAt is adopted
//     (same session id, no second session spawned, task not reset to pending);
//   - a session that already ended with a real completion goes to verification
//     and is never re-spawned.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestAdoptClaudeCode
package autonomous

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// claudeCodeEchoedTask is a pane capture of the typed task followed by a real
// summary line, as a finished claude-code worker leaves it.
const claudeCodeEchoedTask = "> Implement the widget.\n" +
	"  Finish with a line: DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>\n" +
	"\n● Added the widget.\n" +
	"DATAWATCH_COMPLETE: added the widget and its tests\n"

// setupClaudeCodeBooted is setupBootedAdoptState with the task pinned to the
// claude-code backend.
func setupClaudeCodeBooted(t *testing.T, m *Manager, sessionID string) *PRD {
	t.Helper()
	cur := setupBootedAdoptState(t, m, sessionID)
	tk := &cur.Story[0].Tasks[0]
	tk.Backend = "claude-code"
	if err := m.Store().SaveTask(tk); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	cur, _ = m.Store().GetPRD(cur.ID)
	return cur
}

func TestAdoptClaudeCode_LiveOneShotAdopted(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	fs := newFakeSessions("cc-live") // live one-shot; its TaskDeliveredAt is persisted session-side
	fs.wire(m)

	cur := setupClaudeCodeBooted(t, m, "cc-live")
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress || tk.SessionID != "cc-live" || !tk.AdoptedSession {
		t.Fatalf("live claude-code task not adopted: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("live claude-code session killed on boot-resume: %v", fs.killed())
	}

	var spawnN int32
	noSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawnN, 1)
		return SpawnResult{}, fmt.Errorf("must not spawn a second session for an adopted claude-code task")
	}
	out := runToTerminal(t, m, api, cur.ID, noSpawn, verify)
	if out.Status != PRDCompleted {
		t.Fatalf("want PRDCompleted, got %s (task error: %q)", out.Status, out.Story[0].Tasks[0].Error)
	}
	if n := atomic.LoadInt32(&spawnN); n != 0 {
		t.Fatalf("spawned %d second session(s) for an adopted task, want 0", n)
	}
	if n := fs.waitCount(); n != 1 {
		t.Fatalf("executor must wait once on the adopted session, waited %d", n)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("adopted session killed %d time(s): %v", n, fs.killed())
	}
}

func TestAdoptClaudeCode_EndedWithRealCompletionGoesToVerification(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	fs := newFakeSessions() // session already ended
	fs.recordOutput("cc-done", claudeCodeEchoedTask)
	fs.wire(m)

	cur := setupClaudeCodeBooted(t, m, "cc-done")
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskVerifying || tk.SessionID != "cc-done" {
		t.Fatalf("ended claude-code task must go to verification: status=%s session=%q", tk.Status, tk.SessionID)
	}

	var spawnN, verifyN int32
	noSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawnN, 1)
		return SpawnResult{}, fmt.Errorf("must not spawn: task finished while the daemon was down")
	}
	counting := func(ctx context.Context, p *PRD, k *Task) (VerificationResult, error) {
		atomic.AddInt32(&verifyN, 1)
		return verify(ctx, p, k)
	}
	out := runToTerminal(t, m, api, cur.ID, noSpawn, counting)
	if out.Status != PRDCompleted {
		t.Fatalf("want PRDCompleted, got %s (task error: %q)", out.Status, out.Story[0].Tasks[0].Error)
	}
	if n := atomic.LoadInt32(&spawnN); n != 0 {
		t.Fatalf("spawned %d worker(s) for finished work, want 0", n)
	}
	if n := atomic.LoadInt32(&verifyN); n != 1 {
		t.Fatalf("verify called %d times, want 1", n)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("finished session killed: %v", fs.killed())
	}
}

// An ended claude-code session whose pane holds only the echoed task with the
// instruction placeholder is NOT finished work: it is reset, not verified.
func TestAdoptClaudeCode_EchoOnlyIsNotCompletion(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	fs := newFakeSessions()
	fs.recordOutput("cc-echo", "> Implement the widget.\n  Finish with a line: DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>\n")
	fs.wire(m)

	cur := setupClaudeCodeBooted(t, m, "cc-echo")
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	if st := got.Story[0].Tasks[0].Status; st != TaskPending {
		t.Fatalf("echo-only output must not count as completion; status=%s, want pending", st)
	}
}
