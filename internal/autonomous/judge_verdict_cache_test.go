package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newJudgeVerdictCacheTestManager builds a Manager (with the judge verdict
// cache opened under a temp data dir) where the PRD carries one task on
// commit sha "sha-a1b2c3". enabled nil = the key is unset (= on, per the
// normalizer); *enabled = an explicit operator value.
func newJudgeVerdictCacheTestManager(t *testing.T, dataDir string, enabled *bool) (*Manager, *PRD, string, GuardrailInvocation) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.JudgeVerdictCacheEnabled = enabled
	m, err := NewManager(dataDir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.store.CreatePRD("spec", t.TempDir(), "ollama", "llama-3", EffortLow)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	story, err := m.store.AddStory(prd.ID, "the story", "story spec")
	if err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	task, err := m.store.AddTask(prd.ID, story.ID, "the task", "task spec", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	// Record the anchor commit through the store's PRD and persist it, so a
	// manager reopened on the same data dir sees the same sha.
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after setup")
	}
	for i := range fresh.Story {
		for j := range fresh.Story[i].Tasks {
			if fresh.Story[i].Tasks[j].ID == task.ID {
				fresh.Story[i].Tasks[j].RecordAttemptCommit("sha-a1b2c3")
			}
		}
	}
	if err := m.store.SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	inv := GuardrailInvocation{
		PRDID:      fresh.ID,
		Level:      "task",
		UnitID:     task.ID,
		UnitTitle:  "the task",
		UnitSpec:   "task spec",
		Guardrail:  "rules",
		ProjectDir: fresh.ProjectDir,
	}
	return m, fresh, task.ID, inv
}

// fixedClock returns a now function pinned to ts and reports whether the
// cache's entries were stamped with it.
func fixedClock(ts time.Time) func() time.Time {
	return func() time.Time { return ts }
}

func TestJudgeVerdictCacheHit(t *testing.T) {
	dataDir := t.TempDir()
	fakeNow := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	m, prd, taskID, inv := newJudgeVerdictCacheTestManager(t, dataDir, nil /* unset = on */)
	c := m.judgeVerdictCacheRef()
	if c == nil {
		t.Fatal("manager has no judge verdict cache")
	}
	c.now = fixedClock(fakeNow)

	judged := GuardrailVerdict{
		Guardrail: "rules",
		Outcome:   "block",
		Category:  "content",
		Summary:   "AGENTS.md violated",
		VerdictAt: fakeNow,
	}
	calls := 0
	fn := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return judged, nil
	}
	v1, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn)
	if err != nil || v1.Outcome != "block" {
		t.Fatalf("first judge run: outcome=%q err=%v", v1.Outcome, err)
	}
	if calls != 1 {
		t.Fatalf("first run calls=%d want 1", calls)
	}

	// The completed verdict is persistable and replayed without a new judge.
	v2, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn)
	if err != nil {
		t.Fatalf("second judge run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("second run calls=%d want still 1 (cache hit)", calls)
	}
	if v2.Summary != judged.Summary || v2.Outcome != judged.Outcome {
		t.Fatalf("cached verdict mismatch: %+v", v2)
	}

	// Fake clock honored for the stored entry.
	c.mu.Lock()
	var stamped bool
	for _, e := range c.entries {
		if e.StoredAt.Equal(fakeNow) {
			stamped = true
		}
	}
	c.mu.Unlock()
	if !stamped {
		t.Fatalf("stored entry not stamped with the injected clock: %+v", c.entries)
	}

	// Persistence: a freshly opened manager on the same data dir replays
	// the same verdict from the cache file.
	m2, prd2, _, inv2 := newJudgeVerdictCacheTestManager(t, dataDir, nil)
	if m2.judgeVerdictCacheRef() == nil {
		t.Fatal("second manager has no judge verdict cache")
	}
	calls2 := 0
	fn2 := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		calls2++
		return GuardrailVerdict{Outcome: "pass"}, nil
	}
	v3, err := m2.runGuardrailWithInfraRetry(context.Background(), prd2, inv2, fn2)
	if calls2 != 0 {
		t.Fatalf("replay after restart calls=%d want 0", calls2)
	}
	if err != nil || v3.Outcome != judged.Outcome || v3.Summary != judged.Summary {
		t.Fatalf("replayed verdict: outcome=%q summary=%q err=%v", v3.Outcome, v3.Summary, err)
	}
	_ = taskID
	// The cache file lives inside the PRD store directory, nowhere else.
	want := filepath.Join(dataDir, "autonomous", JudgeVerdictCacheFileName)
	if got := m2.judgeVerdictCacheRef().Path(); got != want {
		t.Fatalf("cache path %q want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("cache file missing at %s: %v", want, err)
	}
}

func TestJudgeVerdictCacheMissNewCommit(t *testing.T) {
	dataDir := t.TempDir()
	m, prd, taskID, inv := newJudgeVerdictCacheTestManager(t, dataDir, nil)
	c := m.judgeVerdictCacheRef()

	judged := GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Category: "", Summary: "ok", VerdictAt: time.Now()}
	calls := 0
	fn := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return judged, nil
	}
	if _, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn); err != nil || calls != 1 {
		t.Fatalf("first run: calls=%d err=%v", calls, err)
	}

	// A new commit sha anchors a different key: the judge must run again.
	fresh, _ := m.store.GetPRD(prd.ID)
	for i := range fresh.Story {
		for j := range fresh.Story[i].Tasks {
			tk := &fresh.Story[i].Tasks[j]
			if tk.ID == taskID {
				tk.RecordAttemptCommit("sha-DEF456")
			}
		}
	}
	if err := m.store.SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	again, _ := m.store.GetPRD(prd.ID)

	if _, err := m.runGuardrailWithInfraRetry(context.Background(), again, inv, fn); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("new-commit calls=%d want 2 (cache miss on new commit)", calls)
	}
	if c.Len() != 2 {
		t.Fatalf("cache len=%d want 2 (both commits keep their verdicts)", c.Len())
	}
}

func TestJudgeVerdictCacheMissOnPolicyOrRulesChange(t *testing.T) {
	dataDir := t.TempDir()
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, dataDir, nil)
	c := m.judgeVerdictCacheRef()

	judged := GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Category: "", Summary: "ok", VerdictAt: time.Now()}
	calls := 0
	fn := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return judged, nil
	}
	if _, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn); err != nil || calls != 1 {
		t.Fatalf("first run: calls=%d err=%v", calls, err)
	}

	// Policy change: a different judge backend list is a different key.
	cfg := DefaultConfig()
	cfg.JudgePolicy = []string{"openwebui"}
	m.SetConfig(cfg)
	again, _ := m.store.GetPRD(prd.ID)
	if _, err := m.runGuardrailWithInfraRetry(context.Background(), again, inv, fn); err != nil {
		t.Fatalf("policy-changed run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("policy change calls=%d want 2 (cache miss on policy change)", calls)
	}

	// Rules change: the judged spec text went, so the rules hash goes.
	invChanged := inv
	invChanged.UnitSpec = "task spec v2"
	if _, err := m.runGuardrailWithInfraRetry(context.Background(), again, invChanged, fn); err != nil {
		t.Fatalf("rules-changed run: %v", err)
	}
	if calls != 3 {
		t.Fatalf("rules change calls=%d want 3 (cache miss on rules change)", calls)
	}
	if c.Len() != 3 {
		t.Fatalf("cache len=%d want 3 (each distinct (commit, policy, rules) triple keeps its verdict)", c.Len())
	}
}

func TestJudgeVerdictCacheTimedOutNeverCached(t *testing.T) {
	dataDir := t.TempDir()
	m, prd, _, inv := newJudgeVerdictCacheTestManager(t, dataDir, nil)
	c := m.judgeVerdictCacheRef()

	timeoutVerdict := GuardrailVerdict{
		Guardrail: "rules",
		Outcome:   "block",
		Category:  JudgeTimeoutCategory,
		Summary:   "judge timed out",
		VerdictAt: time.Now(),
	}
	calls := 0
	fn := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return timeoutVerdict, nil
	}
	if _, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn); err != nil {
		t.Fatalf("timed-out run: %v", err)
	}
	// Nothing may be stored for the timed-out verdict.
	if c.Len() != 0 {
		t.Fatalf("cache len=%d want 0 — a timed-out judge is never cached", c.Len())
	}
	if v, ok := c.Lookup("sha-a1b2c3", "judge:", JudgeVerdictRulesHash(inv.Guardrail, inv.Prompt, inv.UnitTitle, inv.UnitSpec, inv.GuardrailModel)); ok {
		t.Fatalf("timed-out verdict is present in the cache: %+v", v)
	}

	// A hard error from the judge is also never cached.
	calls = 0
	fnErr := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return GuardrailVerdict{}, context.DeadlineExceeded
	}
	_, _ = m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fnErr)
	if c.Len() != 0 {
		t.Fatalf("cache len=%d want 0 after a hard error", c.Len())
	}

	// infra-category block (the other unfinished-judge class) is likewise
	// excluded even though it is also a block.
	infraVerdict := GuardrailVerdict{Guardrail: "rules", Outcome: "block", Category: "infra", Summary: "crash", VerdictAt: time.Now()}
	fnInfra := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
		return infraVerdict, nil
	}
	_, _ = m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fnInfra)
	if c.Len() != 0 {
		t.Fatalf("cache len=%d want 0 after an infra block", c.Len())
	}

	// JudgeVerdictCacheable is the single storage-boundary gate: assert its
	// contract directly for the classes that must be excluded.
	if JudgeVerdictCacheable(timeoutVerdict) {
		t.Fatal("timed-out verdict reports cacheable")
	}
	if JudgeVerdictCacheable(infraVerdict) {
		t.Fatal("infra block reports cacheable")
	}
	if !JudgeVerdictCacheable(GuardrailVerdict{Outcome: "pass"}) {
		t.Fatal("pass verdict reports not cacheable")
	}
	if !JudgeVerdictCacheable(GuardrailVerdict{Outcome: "block", Category: "content"}) {
		t.Fatal("content block reports not cacheable")
	}
	if !JudgeVerdictCacheable(GuardrailVerdict{Outcome: "block", Category: ""}) {
		t.Fatal("empty-category block (pre-BL410 content) reports not cacheable")
	}
}

func TestJudgeVerdictCacheDisabled(t *testing.T) {
	dataDir := t.TempDir()

	t.Run("explicit false ignores even a preexisting entry", func(t *testing.T) {
		// Seed a completed verdict for the exact key while the cache is on…
		on := true
		mOn, prdOn, _, invOn := newJudgeVerdictCacheTestManager(t, dataDir, &on)
		judged := GuardrailVerdict{Guardrail: "rules", Outcome: "pass", Category: "", Summary: "seeded", VerdictAt: time.Now()}
		calls := 0
		fn := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
			calls++
			return judged, nil
		}
		if _, err := mOn.runGuardrailWithInfraRetry(context.Background(), prdOn, invOn, fn); err != nil || calls != 1 {
			t.Fatalf("seed run: calls=%d err=%v", calls, err)
		}
		wantHash := JudgeVerdictRulesHash(invOn.Guardrail, invOn.Prompt, invOn.UnitTitle, invOn.UnitSpec, invOn.GuardrailModel)
		if v, ok := mOn.judgeVerdictCacheRef().Lookup("sha-a1b2c3", "judge:", wantHash); !ok || v.Summary != "seeded" {
			t.Fatalf("seeded entry not visible: %+v ok=%v", v, ok)
		}

		// …then flip the flag off: the identical key must NOT replay.
		off := false
		mOff, prdOff, _, invOff := newJudgeVerdictCacheTestManager(t, dataDir, &off)
		callsOff := 0
		fnOff := func(ctx context.Context, i GuardrailInvocation) (GuardrailVerdict, error) {
			callsOff++
			return GuardrailVerdict{Guardrail: "rules", Outcome: "block", Category: "content", Summary: "fresh", VerdictAt: time.Now()}, nil
		}
		v, err := mOff.runGuardrailWithInfraRetry(context.Background(), prdOff, invOff, fnOff)
		if err != nil || callsOff != 1 {
			t.Fatalf("disabled run: calls=%d (want 1 — no cache hit) err=%v", callsOff, err)
		}
		if v.Summary != "fresh" {
			t.Fatalf("disabled run returned the cached verdict %+v", v)
		}
		// And the fresh verdict must not have been stored.
		if got := mOff.judgeVerdictCacheRef().Len(); got != 1 {
			t.Fatalf("cache len=%d want the still-existing seed entry only (no new store while disabled)", got)
		}
		// The file exists from the seed store, but carries exactly one entry.
		if _, err := os.Stat(filepath.Join(dataDir, "autonomous", JudgeVerdictCacheFileName)); err != nil {
			t.Fatalf("seed cache file: %v", err)
		}
	})
}

// TestJudgeVerdictCacheStoresEntryForNewHead: a rejudge on a moved head
// stores its fresh verdict under the new head's key, and the entry for the
// old head is left in place.
func TestJudgeVerdictCacheStoresEntryForNewHead(t *testing.T) {
	m, id, _, got := runRejudgeHeadChange(t)
	inv, _ := m.prdRulesReviewInvocation(context.Background(), got)
	c := m.judgeVerdictCacheRef()
	if c == nil {
		t.Fatal("manager has no judge verdict cache")
	}

	fresh, ok := c.Lookup("sha-head-2", judgeVerdictPolicyKey(m, inv), judgeVerdictRulesHash(inv))
	if !ok {
		t.Fatalf("no cache entry stored for the new head sha-head-2 (prd %s)", id)
	}
	if fresh.JudgeSessionID != "judge-head-b" {
		t.Errorf("new-head cache entry judge session = %q, want %q", fresh.JudgeSessionID, "judge-head-b")
	}
	old, ok := c.Lookup("sha-head-1", judgeVerdictPolicyKey(m, inv), judgeVerdictRulesHash(inv))
	if !ok || old.JudgeSessionID != "judge-head-a" {
		t.Errorf("old-head cache entry = %+v ok=%v, want the untouched judge-head-a verdict", old, ok)
	}
}
