package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// infraRetryFixture is a Manager with one Automaton holding one task.
func infraRetryFixture(t *testing.T, adjust func(*Config)) (*Manager, *PRD, *Task) {
	t.Helper()
	cfg := DefaultConfig()
	if adjust != nil {
		adjust(&cfg)
	}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.store.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = []Story{{ID: "s1", PRDID: prd.ID, Title: "Story", Tasks: []Task{{
		ID: "t1", StoryID: "s1", PRDID: prd.ID, Title: "Parse the input", Status: TaskInProgress,
	}}}}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("prd not stored")
	}
	return m, stored, &stored.Story[0].Tasks[0]
}

func storageErr() error { return fmt.Errorf("write state: %w", syscall.ENOSPC) }

func planFor(t *testing.T, m *Manager, prd *PRD, task *Task, err error) infraRetryPlan {
	t.Helper()
	plan, ok := m.planInfraRetry(prd, task, err)
	if !ok {
		t.Fatalf("planInfraRetry refused %v", err)
	}
	return plan
}

func TestInfraRetryRecord_AppendsRecordWithReasonAndClass(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	plan := planFor(t, m, prd, task, storageErr())
	before := time.Now()
	m.recordInfraRetry(prd, task, plan)

	if len(task.InfraRetries) != 1 {
		t.Fatalf("records = %d, want 1", len(task.InfraRetries))
	}
	rec := task.InfraRetries[0]
	if rec.Number != 1 || rec.Class != config.InfraRetryClassStorageWrite {
		t.Errorf("record = %+v", rec)
	}
	if !strings.Contains(rec.Reason, "no space left on device") {
		t.Errorf("reason %q lacks the error text", rec.Reason)
	}
	if rec.DelaySeconds != 60 {
		t.Errorf("delay = %d, want 60", rec.DelaySeconds)
	}
	if rec.At.Before(before) || !rec.NextAt.Equal(rec.At.Add(60*time.Second)) {
		t.Errorf("at %v next_at %v", rec.At, rec.NextAt)
	}
	if rec.Source == "" {
		t.Error("source is empty")
	}
}

func TestInfraRetryRecord_ReasonIsTruncated(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	long := strings.Repeat("x", 5000)
	plan := planFor(t, m, prd, task, &InfraSpawnError{Cause: errors.New(long)})
	m.recordInfraRetry(prd, task, plan)
	if got := len([]rune(task.InfraRetries[0].Reason)); got > maxInfraRetryReasonChars+1 {
		t.Errorf("reason length %d exceeds the bound", got)
	}
}

func TestInfraRetryRecord_KeepsNewest50(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	for i := 1; i <= 60; i++ {
		m.recordInfraRetry(prd, task, infraRetryPlan{
			Class: config.InfraRetryClassSpawnError, Reason: "r", Number: i, MaxTries: 99,
		})
	}
	if len(task.InfraRetries) != maxInfraRetryRecords {
		t.Fatalf("records = %d, want %d", len(task.InfraRetries), maxInfraRetryRecords)
	}
	if first := task.InfraRetries[0].Number; first != 11 {
		t.Errorf("oldest kept record is %d, want 11", first)
	}
	if last := task.InfraRetries[len(task.InfraRetries)-1].Number; last != 60 {
		t.Errorf("newest record is %d, want 60", last)
	}
}

func TestInfraRetryRecord_WritesDecision(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	plan := planFor(t, m, prd, task, storageErr())
	m.recordInfraRetry(prd, task, plan)

	var found *Decision
	for i := range prd.Decisions {
		if prd.Decisions[i].Kind == "infra_retry" {
			found = &prd.Decisions[i]
		}
	}
	if found == nil {
		t.Fatal("no infra_retry decision")
	}
	if found.Actor != "autonomous" || found.FailureCategory != "infra" {
		t.Errorf("decision = %+v", *found)
	}
	for _, want := range []string{"Parse the input", config.InfraRetryClassStorageWrite, "retry 1 of 3", "1m0s"} {
		if !strings.Contains(found.Note, want) {
			t.Errorf("note %q lacks %q", found.Note, want)
		}
	}
	if strings.Contains(found.Note, "PRD") {
		t.Errorf("note %q says PRD", found.Note)
	}
}

func TestInfraRetryRecord_CountIncrements(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	for want := 1; want <= 3; want++ {
		plan := planFor(t, m, prd, task, storageErr())
		if plan.Number != want {
			t.Fatalf("plan number = %d, want %d", plan.Number, want)
		}
		m.recordInfraRetry(prd, task, plan)
		if task.InfraRetryCount != want {
			t.Fatalf("count = %d, want %d", task.InfraRetryCount, want)
		}
	}
}

func TestInfraRetryRecord_ClearKeepsHistory(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	m.recordInfraRetry(prd, task, planFor(t, m, prd, task, storageErr()))
	clearInfraRetryCount(task)
	if task.InfraRetryCount != 0 {
		t.Errorf("count = %d after clear", task.InfraRetryCount)
	}
	if len(task.InfraRetries) != 1 {
		t.Errorf("records = %d after clear, want 1", len(task.InfraRetries))
	}
	clearInfraRetryCount(nil)
}

func TestInfraRetryRecord_WaitSetsAndClearsReason(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	plan := planFor(t, m, prd, task, storageErr())

	var slept time.Duration
	var during string
	orig := infraSleep
	infraSleep = func(_ context.Context, d time.Duration) error {
		slept = d
		during = task.WaitReason
		return nil
	}
	t.Cleanup(func() { infraSleep = orig })

	if err := m.waitInfraRetry(context.Background(), prd, task, plan); err != nil {
		t.Fatalf("waitInfraRetry: %v", err)
	}
	if slept != 60*time.Second {
		t.Errorf("slept %v, want 1m", slept)
	}
	if !strings.HasPrefix(during, "infrastructure retry 1 of 3 at ") || !strings.HasSuffix(during, "(storage_write)") {
		t.Errorf("wait reason during sleep = %q", during)
	}
	if task.WaitReason != "" {
		t.Errorf("wait reason after sleep = %q", task.WaitReason)
	}
}

func TestInfraRetryRecord_WaitReturnsContextError(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	plan := planFor(t, m, prd, task, storageErr())
	orig := infraSleep
	infraSleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { infraSleep = orig })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.waitInfraRetry(ctx, prd, task, plan); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if task.WaitReason != "" {
		t.Errorf("wait reason = %q after cancelled wait", task.WaitReason)
	}
}

func TestInfraBackoff_DefaultDelaysSumTo900(t *testing.T) {
	var cfg config.InfraRetryConfig
	var total time.Duration
	want := []time.Duration{60 * time.Second, 300 * time.Second, 540 * time.Second}
	for n := 1; n <= 3; n++ {
		d := infraBackoffDelay(cfg, n)
		if d != want[n-1] {
			t.Errorf("delay %d = %v, want %v", n, d, want[n-1])
		}
		total += d
	}
	if total != 900*time.Second {
		t.Errorf("total = %v, want 900s", total)
	}
}

func TestInfraBackoff_ShortListRepeatsLastEntry(t *testing.T) {
	cfg := config.InfraRetryConfig{MaxTries: 5, BackoffSeconds: []int{10, 20}}
	want := []time.Duration{10, 20, 20, 20, 20}
	for n := 1; n <= 5; n++ {
		if d := infraBackoffDelay(cfg, n); d != want[n-1]*time.Second {
			t.Errorf("delay %d = %v, want %vs", n, d, want[n-1])
		}
	}
	if d := infraBackoffDelay(cfg, 9); d != 20*time.Second {
		t.Errorf("delay past the limit = %v, want 20s", d)
	}
}

func TestInfraBackoff_ZeroEntriesAllowed(t *testing.T) {
	cfg := config.InfraRetryConfig{BackoffSeconds: []int{0}}
	if d := infraBackoffDelay(cfg, 2); d != 0 {
		t.Errorf("delay = %v, want 0", d)
	}
	empty := config.InfraRetryConfig{BackoffSeconds: []int{}}
	if d := infraBackoffDelay(empty, 1); d != 0 {
		t.Errorf("empty list delay = %v, want 0", d)
	}
}

func TestInfraBackoff_PlanRefusesWhenDisabled(t *testing.T) {
	off := false
	m, prd, task := infraRetryFixture(t, func(c *Config) { c.InfraRetry.Enabled = &off })
	if _, ok := m.planInfraRetry(prd, task, storageErr()); ok {
		t.Error("plan accepted with the feature disabled")
	}
}

func TestInfraBackoff_PlanRefusesUnlistedClass(t *testing.T) {
	m, prd, task := infraRetryFixture(t, func(c *Config) {
		c.InfraRetry.Categories = []string{config.InfraRetryClassSpawnError}
	})
	if _, ok := m.planInfraRetry(prd, task, storageErr()); ok {
		t.Error("plan accepted a class that is not listed")
	}
	if _, ok := m.planInfraRetry(prd, task, &InfraSpawnError{Cause: errors.New("boom")}); !ok {
		t.Error("plan refused a listed class")
	}
	m2, prd2, task2 := infraRetryFixture(t, func(c *Config) { c.InfraRetry.Categories = []string{} })
	if _, ok := m2.planInfraRetry(prd2, task2, storageErr()); ok {
		t.Error("plan accepted a class with an empty category list")
	}
}

func TestInfraBackoff_PlanRefusesWhenCountReachedMax(t *testing.T) {
	m, prd, task := infraRetryFixture(t, func(c *Config) { c.InfraRetry.MaxTries = 2 })
	for i := 0; i < 2; i++ {
		m.recordInfraRetry(prd, task, planFor(t, m, prd, task, storageErr()))
	}
	if _, ok := m.planInfraRetry(prd, task, storageErr()); ok {
		t.Error("plan accepted after max_tries retries")
	}
	clearInfraRetryCount(task)
	if _, ok := m.planInfraRetry(prd, task, storageErr()); !ok {
		t.Error("plan refused after the count was cleared")
	}
}

func TestInfraBackoff_PlanRefusesNonInfraError(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	for _, err := range []error{nil, errors.New("tests failed: 2 assertions"), context.Canceled} {
		if _, ok := m.planInfraRetry(prd, task, err); ok {
			t.Errorf("plan accepted %v", err)
		}
	}
}

func TestInfraBackoff_PlanCarriesDelayAndLimit(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	m.recordInfraRetry(prd, task, planFor(t, m, prd, task, storageErr()))
	plan := planFor(t, m, prd, task, storageErr())
	if plan.Number != 2 || plan.MaxTries != 3 || plan.Delay != 300*time.Second {
		t.Errorf("plan = %+v", plan)
	}
}
