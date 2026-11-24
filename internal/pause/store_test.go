package pause

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func openTemp(t *testing.T, startPaused bool) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pause.json")
	s, err := Open(p, startPaused, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestNothingPausedByDefault(t *testing.T) {
	s, _ := openTemp(t, false)
	if _, blocked := s.Blocked("/work/a"); blocked {
		t.Fatal("a fresh store must not block anything")
	}
	if snap := s.Snapshot(); snap.Paused {
		t.Fatalf("fresh snapshot reports paused: %+v", snap)
	}
}

func TestGlobalPauseBlocksEveryProject(t *testing.T) {
	s, _ := openTemp(t, false)
	if _, err := s.Pause(ScopeGlobal, "", "release freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/work/a", "/work/b", ""} {
		b, blocked := s.Blocked(dir)
		if !blocked {
			t.Fatalf("dir %q not blocked by a global pause", dir)
		}
		if b.Scope != ScopeGlobal || b.Reason != "release freeze" || b.By != "operator" {
			t.Fatalf("block = %+v", b)
		}
		if !strings.Contains(b.Message(), "release freeze") || !strings.Contains(b.Message(), "paused by operator") {
			t.Fatalf("message lacks the reason: %q", b.Message())
		}
	}
}

func TestProjectPauseBlocksOnlyThatProject(t *testing.T) {
	s, _ := openTemp(t, false)
	if _, err := s.Pause(ScopeProject, "/work/a", "reviewing", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, blocked := s.Blocked("/work/a"); !blocked {
		t.Fatal("paused project must block")
	}
	if _, blocked := s.Blocked("/work/a/"); !blocked {
		t.Fatal("a trailing slash names the same project")
	}
	if _, blocked := s.Blocked("/work/a/sub/dir"); !blocked {
		t.Fatal("a directory inside the paused project must block")
	}
	if _, blocked := s.Blocked("/work/ab"); blocked {
		t.Fatal("a sibling that shares a name prefix must not block")
	}
	if _, blocked := s.Blocked("/work/b"); blocked {
		t.Fatal("another project must not block")
	}
}

func TestPauseNeedsReasonAndScope(t *testing.T) {
	s, _ := openTemp(t, false)
	if _, err := s.Pause(ScopeGlobal, "", "  ", "operator"); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("empty reason err = %v", err)
	}
	if _, err := s.Pause(ScopeProject, "", "why", "operator"); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("project scope without project err = %v", err)
	}
	if _, err := s.Pause("planet", "", "why", "operator"); !errors.Is(err, ErrBadScope) {
		t.Fatalf("bad scope err = %v", err)
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	s, path := openTemp(t, false)
	if _, err := s.Pause(ScopeGlobal, "", "freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pause(ScopeProject, "/work/a", "review", "operator"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, false, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	snap := s2.Snapshot()
	if snap.Global == nil || snap.Global.Reason != "freeze" || len(snap.Projects) != 1 {
		t.Fatalf("restored snapshot = %+v", snap)
	}
	if _, blocked := s2.Blocked("/work/zzz"); !blocked {
		t.Fatal("restored global pause must block")
	}
}

func TestStartPausedAppliesOnlyWithoutSavedState(t *testing.T) {
	s, path := openTemp(t, true)
	b, blocked := s.Blocked("/x")
	if !blocked || b.By != ActorConfig {
		t.Fatalf("start_paused must pause globally by config: %+v %v", b, blocked)
	}
	// The operator resumes; a restart with start_paused still on must not re-pause.
	if _, err := s.ResumeAll("operator", ""); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, true, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	if _, blocked := s2.Blocked("/x"); blocked {
		t.Fatal("a saved resume must survive a restart even with start_paused on")
	}
}

func TestResumeScopes(t *testing.T) {
	s, _ := openTemp(t, false)
	_, _ = s.Pause(ScopeGlobal, "", "g", "operator")
	_, _ = s.Pause(ScopeProject, "/work/a", "p", "operator")
	if ok, err := s.Resume(ScopeProject, "/work/a", "operator", ""); err != nil || !ok {
		t.Fatalf("resume project = %v %v", ok, err)
	}
	if _, blocked := s.Blocked("/work/a"); !blocked {
		t.Fatal("the global pause still holds after the project resume")
	}
	if ok, err := s.Resume(ScopeProject, "/work/a", "operator", ""); err != nil || ok {
		t.Fatalf("resuming a project that is not paused must report false, got %v %v", ok, err)
	}
	if ok, err := s.Resume(ScopeGlobal, "", "operator", ""); err != nil || !ok {
		t.Fatalf("resume global = %v %v", ok, err)
	}
	if _, blocked := s.Blocked("/work/a"); blocked {
		t.Fatal("all cleared")
	}
}

func TestPauseAllAndResumeAllAreSingleActions(t *testing.T) {
	s, _ := openTemp(t, false)
	_, _ = s.Pause(ScopeProject, "/work/a", "p", "operator")
	_, _ = s.Pause(ScopeProject, "/work/b", "p", "operator")
	if _, err := s.Pause(ScopeGlobal, "", "g", "operator"); err != nil {
		t.Fatal(err)
	}
	n, err := s.ResumeAll("operator", "done")
	if err != nil || n != 3 {
		t.Fatalf("ResumeAll cleared %d err %v, want 3", n, err)
	}
	if snap := s.Snapshot(); snap.Paused {
		t.Fatalf("still paused: %+v", snap)
	}
}

func TestOnlyTheOperatorCanResume(t *testing.T) {
	s, _ := openTemp(t, false)
	_, _ = s.Pause(ScopeGlobal, "", "g", "operator")
	for _, actor := range []string{"doer", "watcher", "supervisor", "supervisor:detector", "llm", "agent:abc", "session:xyz", "autonomous", "claude-code", ""} {
		if _, err := s.Resume(ScopeGlobal, "", actor, ""); !errors.Is(err, ErrNotOperator) {
			t.Fatalf("actor %q resume err = %v, want ErrNotOperator", actor, err)
		}
		if _, err := s.ResumeAll(actor, ""); !errors.Is(err, ErrNotOperator) {
			t.Fatalf("actor %q resume-all err = %v, want ErrNotOperator", actor, err)
		}
	}
	if _, blocked := s.Blocked("/x"); !blocked {
		t.Fatal("the pause must still hold after refused resumes")
	}
	for _, actor := range []string{"operator", "cli", "web", "channel:signal", "peer:laptop", "api"} {
		_, _ = s.Pause(ScopeGlobal, "", "again", "operator")
		if ok, err := s.Resume(ScopeGlobal, "", actor, ""); err != nil || !ok {
			t.Fatalf("surface actor %q must be allowed: %v %v", actor, ok, err)
		}
	}
}

func TestAuditRecordsWhoWhyWhen(t *testing.T) {
	s, _ := openTemp(t, false)
	var evs []Event
	s.SetAudit(func(e Event) { evs = append(evs, e) })
	_, _ = s.Pause(ScopeProject, "/work/a", "review", "cli")
	_, _ = s.Resume(ScopeProject, "/work/a", "web", "ok now")
	_, _ = s.Resume(ScopeGlobal, "", "doer", "") // refused: also audited
	if len(evs) != 3 {
		t.Fatalf("events = %+v", evs)
	}
	if e := evs[0]; e.Action != ActionPause || e.Actor != "cli" || e.Reason != "review" || e.Project != "/work/a" || e.At.IsZero() {
		t.Fatalf("pause event = %+v", e)
	}
	if e := evs[1]; e.Action != ActionResume || e.Actor != "web" || e.Reason != "ok now" {
		t.Fatalf("resume event = %+v", e)
	}
	if e := evs[2]; e.Action != ActionResumeRefused || e.Actor != "doer" {
		t.Fatalf("refused event = %+v", e)
	}
}

func TestCorruptFileIsAnErrorNotSilentlyUnpaused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pause.json")
	if err := writeFile(p, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p, false, fixedClock()); err == nil {
		t.Fatal("an unreadable state file must fail Open, never read as unpaused")
	}
}

func TestPausedAfterPauseIsRepeatedKeepsFirstAuthor(t *testing.T) {
	s, _ := openTemp(t, false)
	_, _ = s.Pause(ScopeGlobal, "", "first", "operator")
	e, err := s.Pause(ScopeGlobal, "", "second", "cli")
	if err != nil || e.Reason != "second" {
		t.Fatalf("a repeat pause replaces the reason: %+v %v", e, err)
	}
}
