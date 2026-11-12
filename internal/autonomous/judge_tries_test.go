package autonomous

import (
	"path/filepath"
	"testing"
	"time"
)

func judgeTryTestManager(t *testing.T, storePath string) *Manager {
	t.Helper()
	st, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &Manager{cfg: Config{}, store: st}
}

func TestJudgeTriesKeepTimedOutAttemptsPerJudgeCall(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prds.json")
	m := judgeTryTestManager(t, storePath)
	if err := m.store.SavePRD(&PRD{ID: "prd-tries", Title: "tries"}); err != nil {
		t.Fatalf("save PRD: %v", err)
	}
	started := time.Now().Truncate(time.Second)
	rec := JudgeAttemptRecord{Backend: "claude-code", Started: started, Duration: 30 * time.Minute, Outcome: JudgeOutcomeTimedOut}
	if err := m.RecordJudgeTry("prd-tries", "unit-1|rules", rec); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := m.RecordJudgeTry("prd-tries", "unit-1|rules", JudgeAttemptRecord{Backend: "qwen", Started: started, Outcome: JudgeOutcomeTimedOut}); err != nil {
		t.Fatalf("record second: %v", err)
	}
	if err := m.RecordJudgeTry("prd-tries", "unit-2|rules", JudgeAttemptRecord{Backend: "ollama", Outcome: JudgeOutcomeTimedOut}); err != nil {
		t.Fatalf("record other call: %v", err)
	}

	got := m.JudgeTries("prd-tries", "unit-1|rules")
	if len(got) != 2 || got[0].Backend != "claude-code" || got[1].Backend != "qwen" {
		t.Fatalf("tries for unit-1 = %+v, want claude-code then qwen", got)
	}
	if got[0].Duration != 30*time.Minute || !got[0].Started.Equal(started) {
		t.Fatalf("first try = %+v, want its duration and start kept", got[0])
	}
	if other := m.JudgeTries("prd-tries", "unit-3|rules"); len(other) != 0 {
		t.Fatalf("an untouched judge call has tries %+v, want none", other)
	}
}

func TestJudgeTriesSurviveDaemonRestart(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prds.json")
	before := judgeTryTestManager(t, storePath)
	if err := before.store.SavePRD(&PRD{ID: "prd-restart", Title: "restart"}); err != nil {
		t.Fatalf("save PRD: %v", err)
	}
	if err := before.RecordJudgeTry("prd-restart", "unit-1|rules", JudgeAttemptRecord{Backend: "claude-code", Outcome: JudgeOutcomeTimedOut}); err != nil {
		t.Fatalf("record: %v", err)
	}

	// A new store and manager read the same file, as a restarted daemon does.
	after := judgeTryTestManager(t, storePath)
	got := after.JudgeTries("prd-restart", "unit-1|rules")
	if len(got) != 1 || got[0].Backend != "claude-code" {
		t.Fatalf("tries after restart = %+v, want the claude-code timeout", got)
	}
}

func TestClearJudgeTriesRemovesOnlyThatCallAndSkipsTheSaveWhenEmpty(t *testing.T) {
	m := judgeTryTestManager(t, filepath.Join(t.TempDir(), "prds.json"))
	if err := m.store.SavePRD(&PRD{ID: "prd-clear", Title: "clear"}); err != nil {
		t.Fatalf("save PRD: %v", err)
	}
	for _, key := range []string{"unit-1|rules", "unit-2|rules"} {
		if err := m.RecordJudgeTry("prd-clear", key, JudgeAttemptRecord{Backend: "qwen", Outcome: JudgeOutcomeTimedOut}); err != nil {
			t.Fatalf("record %s: %v", key, err)
		}
	}
	if err := m.ClearJudgeTries("prd-clear", "unit-1|rules"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if left := m.JudgeTries("prd-clear", "unit-1|rules"); len(left) != 0 {
		t.Fatalf("cleared call still has %+v", left)
	}
	if kept := m.JudgeTries("prd-clear", "unit-2|rules"); len(kept) != 1 {
		t.Fatalf("other call tries = %+v, want its one record kept", kept)
	}
	if err := m.ClearJudgeTries("prd-clear", "unit-1|rules"); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

func TestJudgeTriesForUnknownPRDAreEmptyAndRecordingFails(t *testing.T) {
	m := judgeTryTestManager(t, filepath.Join(t.TempDir(), "prds.json"))
	if got := m.JudgeTries("no-such-prd", "unit-1|rules"); len(got) != 0 {
		t.Fatalf("unknown PRD has tries %+v", got)
	}
	if err := m.RecordJudgeTry("no-such-prd", "unit-1|rules", JudgeAttemptRecord{Backend: "qwen"}); err == nil {
		t.Fatal("recording a try on an unknown PRD must fail, not drop it silently")
	}
}
