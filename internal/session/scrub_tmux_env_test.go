package session

import "testing"

func scrubCalls(tm *FakeTmux) int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	n := 0
	for _, c := range tm.Calls {
		if c.Op == "send-keys" && len(c.Args) > 0 && c.Args[0] == scrubTmuxEnvCommand {
			n++
		}
	}
	return n
}

func TestScrubPaneTmuxEnv_OnlyWhenRequested(t *testing.T) {
	tm := NewFakeTmux()
	m := &Manager{tmux: tm}

	m.scrubPaneTmuxEnv(&Session{TmuxSession: "s1"})
	if got := scrubCalls(tm); got != 0 {
		t.Fatalf("operator session must not be scrubbed, got %d calls", got)
	}

	m.scrubPaneTmuxEnv(&Session{TmuxSession: "s1", ScrubTmuxEnv: true})
	if got := scrubCalls(tm); got != 1 {
		t.Fatalf("PRD-owned session must be scrubbed once, got %d calls", got)
	}

	m.scrubPaneTmuxEnv(nil)
	m.scrubPaneTmuxEnv(&Session{ScrubTmuxEnv: true})
	if got := scrubCalls(tm); got != 1 {
		t.Fatalf("nil/no-tmux sessions must be skipped, got %d calls", got)
	}
}
