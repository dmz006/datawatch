package autonomous

// Boot resume after a restart with a persisted back-off flag: the flag is
// reloaded from disk into a fresh registry before the pass runs, new session
// starts for the flagged backend wait in the capacity queue while another
// backend still dispatches, and a session already alive is never killed.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// bootBackoffFixture builds a Manager with a ledger that gates "llm:<backend>"
// pools on a registry restored from path, as the daemon wires it.
func bootBackoffFixture(t *testing.T, path string) (*Manager, *API, *Backoff, []Entry) {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	bo := NewBackoffPersisted(path)
	restored, err := bo.Load(time.Now())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	led := capacity.New(capacity.Options{
		PollInterval: 10 * time.Millisecond,
		AutonomousBackpressure: func(pool string) (bool, string) {
			backend, ok := strings.CutPrefix(pool, "llm:")
			if !ok {
				return false, ""
			}
			if blocked, why := bo.Blocked(backend); blocked {
				return true, "backoff: " + why
			}
			return false, ""
		},
	})
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:" + backend}, ""
	})
	m.SetBackoff(bo)
	return m, api, bo, restored
}

func bootPRD(t *testing.T, m *Manager, backend string, task Task) *PRD {
	t.Helper()
	proj := t.TempDir()
	prd, err := m.CreatePRD("spec "+backend, proj, backend, "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{task}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return prd
}

type spawnLog struct {
	mu       sync.Mutex
	backends []string
}

func (s *spawnLog) add(b string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backends = append(s.backends, b)
}

func (s *spawnLog) count(b string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, x := range s.backends {
		if x == b {
			n++
		}
	}
	return n
}

func TestBootResumeBackoff_PersistedFlagHoldsBackendOthersDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autonomous", "backoff.json")

	// First "daemon": set a flag; every mutation is written to the path.
	first := NewBackoffPersisted(path)
	first.Set("opencode", time.Now().Add(time.Hour), "rate-limit", BackoffSourceRateLimit)
	if err := first.SaveErr(); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Second "daemon": fresh Manager and registry, Load before boot resume.
	m, api, bo, restored := bootBackoffFixture(t, path)
	if len(restored) != 1 || restored[0].Backend != "opencode" {
		t.Fatalf("restored = %+v, want the opencode flag", restored)
	}
	if blocked, _ := bo.Blocked("opencode"); !blocked {
		t.Fatal("restored flag must block opencode")
	}
	var alerts []string
	m.SetAlertFn(func(title, body string) { alerts = append(alerts, title+": "+body) })
	m.RecordBackoffRestored(restored)
	if len(alerts) != 1 || !strings.Contains(alerts[0], "opencode") {
		t.Errorf("alerts = %v, want one naming opencode", alerts)
	}
	var logged bool
	for _, e := range m.BootResumeReport() {
		if e.Outcome == "backoff_restored" && strings.Contains(e.Reason, "opencode") {
			logged = true
		}
	}
	if !logged {
		t.Error("no backoff_restored decision-log entry for opencode")
	}

	blockedPRD := bootPRD(t, m, "opencode", Task{Title: "held", Spec: "x"})
	freePRD := bootPRD(t, m, "pi-prompt", Task{Title: "free", Spec: "y"})

	var spawns spawnLog
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawns.add(req.Backend)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, verify)

	pollPRDTerminal(t, m, freePRD.ID, 5*time.Second)
	if spawns.count("pi-prompt") == 0 {
		t.Error("the unflagged backend never dispatched")
	}
	// The flagged backend's task is waiting in the queue, not started.
	pollUntil(t, 5*time.Second, "flagged task waiting on back-off", func() bool {
		cur, _ := m.Store().GetPRD(blockedPRD.ID)
		tk := cur.Story[0].Tasks[0]
		return tk.Status == TaskWaitingCapacity && strings.Contains(tk.WaitReason, "backoff")
	})
	if n := spawns.count("opencode"); n != 0 {
		t.Errorf("opencode session starts = %d while flagged, want 0", n)
	}

	// Clearing the flag admits it: it was held, not lost.
	bo.Clear("opencode")
	pollPRDTerminal(t, m, blockedPRD.ID, 5*time.Second)
	if spawns.count("opencode") == 0 {
		t.Error("opencode never dispatched after the flag cleared")
	}
}

func TestBootResumeBackoff_LiveSessionOnFlaggedBackendNotKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backoff.json")
	NewBackoffPersisted(path).Set("opencode", time.Now().Add(time.Hour), "rate-limit", BackoffSourceRateLimit)

	m, api, bo, _ := bootBackoffFixture(t, path)
	if blocked, _ := bo.Blocked("opencode"); !blocked {
		t.Fatal("restored flag must block opencode")
	}
	now := time.Now()
	prd := bootPRD(t, m, "opencode", Task{Title: "live", Spec: "x", Status: TaskInProgress, SessionID: "sess-live", StartedAt: &now})

	var kills, spawns atomic.Int32
	m.SetSessionAliveFn(func(id string) bool { return id == "sess-live" })
	m.SetSessionKillerFn(func(_, _, _ string) error { kills.Add(1); return nil })
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawns.Add(1)
		return SpawnResult{SessionID: "sess-new"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, verify)

	// Wait for the boot pass to have decided about the PRD.
	pollUntil(t, 5*time.Second, "boot-resume decision", func() bool {
		for _, e := range m.BootResumeReport() {
			if e.PRDID == prd.ID {
				return true
			}
		}
		return false
	})
	time.Sleep(200 * time.Millisecond)
	if n := kills.Load(); n != 0 {
		t.Errorf("live session kills = %d, want 0", n)
	}
	if n := spawns.Load(); n != 0 {
		t.Errorf("new session starts = %d for the live task, want 0", n)
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	if got := cur.Story[0].Tasks[0].SessionID; got != "sess-live" {
		t.Errorf("task session = %q, want the live one kept", got)
	}
}

func TestBootResumeBackoff_ExpiredFlagNotRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backoff.json")
	NewBackoffPersisted(path).Set("opencode", time.Now().Add(-time.Minute), "rate-limit", BackoffSourceRateLimit)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persist file missing: %v", err)
	}
	_, _, bo, restored := bootBackoffFixture(t, path)
	if len(restored) != 0 {
		t.Errorf("restored = %+v, want none", restored)
	}
	if blocked, _ := bo.Blocked("opencode"); blocked {
		t.Error("an expired flag must not block")
	}
}
