// BL387 Phase 3 — auto-report on PRD completion + MemoryReport field.
//
// TC-1: memoryReportFn called after all tasks complete when MemorySeed.Enabled
// TC-2: memoryReportFn NOT called when MemorySeed.Enabled=false
// TC-3: memoryReportFn NOT called when fn is nil
// TC-4: MemoryReport + MemoryReportAt stored on PRD after successful call
// TC-5: reportFn error does not abort PRD completion (PRDCompleted still set)
// TC-6: empty report string from fn does not write MemoryReport

package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

// bl387Phase3Setup builds a manager + approved PRD with MemorySeed enabled,
// wired with no-op spawn/verify so Run() completes all tasks cleanly.
func bl387Phase3Setup(t *testing.T) (*Manager, *PRD, SpawnFn, VerifyFn) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("phase3 spec", "/proj387p3", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			Title: "P3 Story",
			Tasks: []Task{
				{Title: "P3 Task", Spec: "do the work"},
			},
		},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	spawn, verify := nopSpawnVerify()
	return m, prd, spawn, verify
}

// waitForMemoryReport polls PRD.MemoryReport until non-empty or timeout.
func waitForMemoryReport(t *testing.T, m *Manager, prdID string) *PRD {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, ok := m.Store().GetPRD(prdID)
		if ok && p.MemoryReport != "" {
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	p, _ := m.Store().GetPRD(prdID)
	return p
}

// TestBL387_AutoReport_CalledOnCompletion verifies that memoryReportFn fires
// after Run() when all tasks succeed and MemorySeed.Enabled=true.
func TestBL387_AutoReport_CalledOnCompletion(t *testing.T) {
	m, prd, spawn, verify := bl387Phase3Setup(t)

	var mu sync.Mutex
	var reportCalls []string
	m.SetMemoryReportFn(func(_ context.Context, prdID, _ string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		reportCalls = append(reportCalls, prdID)
		return "## Memory Report\n- decision A\n- decision B", nil
	})

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Report fires in a goroutine — wait briefly.
	waitForMemoryReport(t, m, prd.ID)

	mu.Lock()
	n := len(reportCalls)
	mu.Unlock()
	if n == 0 {
		t.Error("expected memoryReportFn to be called on completion, got 0 calls")
	}
	if n > 0 && reportCalls[0] != prd.ID {
		t.Errorf("reportCalls[0]=%q, want %q", reportCalls[0], prd.ID)
	}
}

// TestBL387_AutoReport_NotCalledWhenSeedDisabled verifies that memoryReportFn
// is NOT called when MemorySeed.Enabled=false.
func TestBL387_AutoReport_NotCalledWhenSeedDisabled(t *testing.T) {
	m, prd, spawn, verify := bl387Phase3Setup(t)
	prd.MemorySeed.Enabled = false
	_ = m.Store().SavePRD(prd)

	called := false
	m.SetMemoryReportFn(func(_ context.Context, _, _ string) (string, error) {
		called = true
		return "report", nil
	})

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let goroutine fire if it was going to

	if called {
		t.Error("memoryReportFn must NOT be called when MemorySeed.Enabled=false")
	}
}

// TestBL387_AutoReport_NotCalledWhenFnNil verifies that a nil memoryReportFn
// does not panic and PRD still reaches PRDCompleted.
func TestBL387_AutoReport_NotCalledWhenFnNil(t *testing.T) {
	m, prd, spawn, verify := bl387Phase3Setup(t)
	// No SetMemoryReportFn — must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Run panicked with nil memoryReportFn: %v", r)
		}
	}()
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	if p.Status != PRDCompleted {
		t.Errorf("PRD status=%q, want PRDCompleted", p.Status)
	}
}

// TestBL387_AutoReport_StoredOnPRD verifies that MemoryReport and MemoryReportAt
// are written to the PRD store after a successful report call.
func TestBL387_AutoReport_StoredOnPRD(t *testing.T) {
	m, prd, spawn, verify := bl387Phase3Setup(t)
	before := time.Now().UTC()

	m.SetMemoryReportFn(func(_ context.Context, _, _ string) (string, error) {
		return "## stored report", nil
	})

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p := waitForMemoryReport(t, m, prd.ID)

	if p.MemoryReport == "" {
		t.Error("expected PRD.MemoryReport to be populated, got empty string")
	}
	if p.MemoryReportAt == nil {
		t.Error("expected PRD.MemoryReportAt to be set")
	} else if p.MemoryReportAt.Before(before) {
		t.Errorf("MemoryReportAt=%v is before test started at %v", p.MemoryReportAt, before)
	}
}

// TestBL387_AutoReport_ErrorDoesNotAbortCompletion verifies that when
// memoryReportFn returns an error, the PRD still reaches PRDCompleted with no panic.
func TestBL387_AutoReport_ErrorDoesNotAbortCompletion(t *testing.T) {
	m, prd, spawn, verify := bl387Phase3Setup(t)

	m.SetMemoryReportFn(func(_ context.Context, _, _ string) (string, error) {
		return "", context.DeadlineExceeded
	})

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let goroutine settle

	p, _ := m.Store().GetPRD(prd.ID)
	if p.Status != PRDCompleted {
		t.Errorf("PRD status=%q after report error, want PRDCompleted", p.Status)
	}
	if p.MemoryReport != "" {
		t.Error("MemoryReport should be empty when reportFn returns error")
	}
}

// TestBL387_AutoReport_EmptyStringNotStored verifies that an empty string
// returned by memoryReportFn does not overwrite MemoryReport.
func TestBL387_AutoReport_EmptyStringNotStored(t *testing.T) {
	m, prd, spawn, verify := bl387Phase3Setup(t)

	m.SetMemoryReportFn(func(_ context.Context, _, _ string) (string, error) {
		return "", nil // empty — nothing to store
	})

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let goroutine settle

	p, _ := m.Store().GetPRD(prd.ID)
	if p.MemoryReport != "" {
		t.Errorf("MemoryReport=%q should be empty for empty-string return", p.MemoryReport)
	}
}
