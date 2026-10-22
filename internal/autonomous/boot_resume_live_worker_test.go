package autonomous

import (
	"strings"
	"testing"
	"time"
)

func liveWorkerPRD(dir string, now time.Time, age time.Duration) *PRD {
	ts := now.Add(-age)
	p := &PRD{ProjectDir: dir, CreatedAt: ts, UpdatedAt: ts}
	p.Story = []Story{{Tasks: []Task{{ID: "t1", Status: TaskInProgress, SessionID: "sess-w", UpdatedAt: ts, StartedAt: &ts}}}}
	return p
}

func TestBootResumeLiveWorkerAliveNeverStale(t *testing.T) {
	now := time.Now()
	prd := liveWorkerPRD(testProjectDir(t), now, 3946*time.Second)
	h := bootResumeHooks{alive: func(id string) bool { return id == "sess-w" }}
	dec := decideBootResume(prd, now, 3600, h)
	if !dec.resume {
		t.Fatalf("resume = false (%q), want true", dec.reason)
	}
	if !strings.Contains(dec.reason, "alive") || !strings.Contains(dec.reason, "sess-w") {
		t.Errorf("reason %q should say the worker is alive and name sess-w", dec.reason)
	}
	// never-resume rule still wins
	if d := decideBootResume(prd, now, 0, h); d.resume {
		t.Error("resume_max_age_seconds=0 must still park")
	}
}

func TestBootResumeLiveWorkerDeadIsParked(t *testing.T) {
	now := time.Now()
	prd := liveWorkerPRD(testProjectDir(t), now, 3946*time.Second)
	h := bootResumeHooks{
		alive:      func(string) bool { return false },
		lastOutput: func(string) (time.Time, bool) { return time.Time{}, false },
	}
	if dec := decideBootResume(prd, now, 3600, h); dec.resume {
		t.Fatalf("resume = true (%q), want parked", dec.reason)
	}
}

func TestBootResumeLiveWorkerRecentOutputCounts(t *testing.T) {
	now := time.Now()
	prd := liveWorkerPRD(testProjectDir(t), now, 3946*time.Second)
	h := bootResumeHooks{
		alive:      func(string) bool { return false },
		lastOutput: func(string) (time.Time, bool) { return now.Add(-30 * time.Second), true },
	}
	dec := decideBootResume(prd, now, 3600, h)
	if !dec.resume {
		t.Fatalf("resume = false (%q), want true", dec.reason)
	}
	if dec.ageSec > 60 {
		t.Errorf("ageSec = %d, want ~30", dec.ageSec)
	}
}

func TestBootResumeLiveWorkerNewestActivityMax(t *testing.T) {
	now := time.Now()
	rec := now.Add(-3 * time.Hour)
	taskUpd := now.Add(-2 * time.Hour)
	out := now.Add(-time.Minute)
	prd := &PRD{CreatedAt: rec, UpdatedAt: rec}
	prd.Story = []Story{{Tasks: []Task{{ID: "t1", Status: TaskInProgress, SessionID: "s", UpdatedAt: taskUpd}}}}
	withOut := bootResumeHooks{lastOutput: func(string) (time.Time, bool) { return out, true }}
	if got := newestActivity(prd, withOut); !got.Equal(out) {
		t.Errorf("with session output: got %v want %v", got, out)
	}
	if got := newestActivity(prd, bootResumeHooks{}); !got.Equal(taskUpd) {
		t.Errorf("without hook: got %v want task update %v", got, taskUpd)
	}
	old := bootResumeHooks{lastOutput: func(string) (time.Time, bool) { return now.Add(-5 * time.Hour), true }}
	if got := newestActivity(prd, old); !got.Equal(taskUpd) {
		t.Errorf("old session output must not lower the max: got %v", got)
	}
}

func verifyingWorkerPRD(dir string, now time.Time, age time.Duration) *PRD {
	p := liveWorkerPRD(dir, now, age)
	p.Story[0].Tasks[0].Status = TaskVerifying
	return p
}

func TestBootResumeVerifyingLiveWorkerAliveNeverStale(t *testing.T) {
	now := time.Now()
	prd := verifyingWorkerPRD(testProjectDir(t), now, 3946*time.Second)
	h := bootResumeHooks{alive: func(id string) bool { return id == "sess-w" }}
	dec := decideBootResume(prd, now, 3600, h)
	if !dec.resume {
		t.Fatalf("resume = false (%q), want true", dec.reason)
	}
	if !strings.Contains(dec.reason, "alive") || !strings.Contains(dec.reason, "sess-w") {
		t.Errorf("reason %q should say the worker is alive and name sess-w", dec.reason)
	}
	if got := liveWorkerSessions(prd, h); len(got) != 1 || got[0] != "sess-w" {
		t.Errorf("liveWorkerSessions = %v, want [sess-w]", got)
	}
	// never-resume rule still wins
	if d := decideBootResume(prd, now, 0, h); d.resume {
		t.Error("resume_max_age_seconds=0 must still park")
	}
}

func TestBootResumeVerifyingLiveWorkerDeadIsParked(t *testing.T) {
	now := time.Now()
	prd := verifyingWorkerPRD(testProjectDir(t), now, 3946*time.Second)
	h := bootResumeHooks{
		alive:      func(string) bool { return false },
		lastOutput: func(string) (time.Time, bool) { return time.Time{}, false },
	}
	if dec := decideBootResume(prd, now, 3600, h); dec.resume {
		t.Fatalf("resume = true (%q), want parked", dec.reason)
	}
}

func TestBootResumeVerifyingLiveWorkerRecentOutputCounts(t *testing.T) {
	now := time.Now()
	prd := verifyingWorkerPRD(testProjectDir(t), now, 3946*time.Second)
	h := bootResumeHooks{
		lastOutput: func(string) (time.Time, bool) { return now.Add(-30 * time.Second), true },
	}
	dec := decideBootResume(prd, now, 3600, h)
	if !dec.resume {
		t.Fatalf("resume = false (%q), want true", dec.reason)
	}
	if dec.ageSec > 60 {
		t.Errorf("ageSec = %d, want ~30", dec.ageSec)
	}
	if got := newestActivity(prd, h); now.Sub(got) > time.Minute {
		t.Errorf("newestActivity = %v, want the recent session output", got)
	}
}
