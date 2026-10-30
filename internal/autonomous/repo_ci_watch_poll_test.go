package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// pollCounters tallies the pass's external calls under a mutex.
type pollCounters struct {
	mu       sync.Mutex
	fetches  int
	peels    int
	runLists int
}

func (p *pollCounters) bump(fetch bool, peel bool, runs bool) {
	p.mu.Lock()
	if fetch {
		p.fetches++
	}
	if peel {
		p.peels++
	}
	if runs {
		p.runLists++
	}
	p.mu.Unlock()
}

func (p *pollCounters) snapshot() (fetches, peels, runLists int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetches, p.peels, p.runLists
}

const (
	fakeHeadSHA = "aaaa18bc26b80b0be05f9c7d2e4a3b2c1d0e9f8a7b6c"
	fakeTagSHA  = "bbbb18bc26b80b0be05f9c7d2e4a3b2c1d0e9f8a7b6c"
)

var judgeErr = errors.New("judge unavailable")

// newPollHarness builds a Manager whose repo CI watch deps, judge and
// store are under test control: fetch/origin/head/tags/peel are fakes
// (deterministic head "aaaa...", tag v1.1.0 → "bbbb...", github
// origin), and the caller supplies the run list and the judge.
func newPollHarness(t *testing.T, repo string, cfg RepoCIWatchConfig) (m *Manager, c *Coordinator, pc *pollCounters) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	base := m.Config()
	base.Automation.RepoCIWatch = cfg
	m.SetConfig(base)
	c = NewCoordinator(m, &fakeStarter{m: m})
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	pc = &pollCounters{}
	m.repoCIFetchFn = func(ctx context.Context, repoDir string) error {
		pc.bump(true, false, false)
		return nil
	}
	m.repoCIOriginFn = func(ctx context.Context, repoDir string) (string, error) {
		return "https://github.com/dmz006/datawatch.git", nil
	}
	m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) {
		return fakeHeadSHA, nil
	}
	m.repoCITagsFn = func(ctx context.Context, repoDir string) ([]string, error) {
		return []string{"v1.1.0", "v1.0.9", "v0.9.0"}, nil
	}
	m.repoCIPeelFn = func(ctx context.Context, repoDir, tag string) (string, error) {
		pc.bump(false, true, false)
		if tag == "v1.1.0" {
			return fakeTagSHA, nil
		}
		return "", fmt.Errorf("tag %s not found", tag)
	}
	return m, c, pc
}

// recordByRunID indexes the store's records by run ID.
func recordByRunID(t *testing.T, m *Manager) map[string]*RepoCIWatchRecord {
	t.Helper()
	out := map[string]*RepoCIWatchRecord{}
	for _, r := range m.ListRepoCIWatchRecords() {
		out[r.RunID] = r
	}
	return out
}

// fakeRun builds a run list entry with the identity fields fixed.
func fakeRun(id int64, workflow string) RepoCIWatchRun {
	return RepoCIWatchRun{
		ID:           id,
		Name:         workflow + " run " + fmt.Sprint(id),
		WorkflowName: workflow,
		URL:          fmt.Sprintf("https://github.com/dmz006/datawatch/actions/runs/%d", id),
		Event:        "push",
		HeadBranch:   "main",
	}
}

// TestRepoCIWatchPollGates verifies the pass's three gates: mode off
// (nothing runs, nothing recorded), no configured repos (fetch is
// never called), and poll_seconds gating (a second pass that comes in
// before the interval is skipped; one at the interval is not).
func TestRepoCIWatchPollGates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("mode off", func(t *testing.T) {
		m, c, pc := newPollHarness(t, dir, RepoCIWatchConfig{
			Mode: "off", Repos: []string{dir}, PollSeconds: 60,
		})
		c.repoCIWatchPass()
		if f, _, _ := pc.snapshot(); f != 0 {
			t.Errorf("mode off: fetches = %d, want 0 (the pass must not run at all)", f)
		}
		if recs := m.ListRepoCIWatchRecords(); len(recs) != 0 {
			t.Errorf("mode off: records = %d, want 0", len(recs))
		}
	})

	t.Run("empty repos", func(t *testing.T) {
		m, c, pc := newPollHarness(t, dir, RepoCIWatchConfig{
			Mode: "shadow", Repos: []string{}, PollSeconds: 60,
		})
		c.repoCIWatchPass()
		if f, _, _ := pc.snapshot(); f != 0 {
			t.Errorf("empty repos: fetches = %d, want 0", f)
		}
		if recs := m.ListRepoCIWatchRecords(); len(recs) != 0 {
			t.Errorf("empty repos: records = %d, want 0", len(recs))
		}
	})

	t.Run("poll_seconds gating", func(t *testing.T) {
		m, c, pc := newPollHarness(t, dir, RepoCIWatchConfig{
			Mode: "shadow", Repos: []string{dir}, PollSeconds: 60, TagCount: ptrInt(1),
		})
		m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
			pc.bump(false, false, true)
			return []RepoCIWatchRun{greenRun(42)}, nil
		}
		t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
		nc := &nowControl{now: t0}
		c.now = func() time.Time { return nc.now }

		c.repoCIWatchPass()
		f, peels, lists := pc.snapshot()
		if f != 1 {
			t.Fatalf("first pass: fetches = %d, want 1", f)
		}
		if peels != 1 {
			t.Errorf("first pass: tag peels = %d, want 1 (v1.1.0)", peels)
		}
		if lists != 2 {
			t.Errorf("first pass: run lists = %d, want 2 (main + one tag)", lists)
		}

		nc.advance(59 * time.Second)
		c.repoCIWatchPass()
		if f, _, _ := pc.snapshot(); f != 1 {
			t.Errorf("pass at +59s: fetches = %d, want 1 (poll_seconds=60 has not elapsed)", f)
		}

		nc.advance(time.Second) // +60s since the first pass
		c.repoCIWatchPass()
		if f, _, _ := pc.snapshot(); f != 2 {
			t.Errorf("pass at +60s: fetches = %d, want 2 (the interval has elapsed)", f)
		}
		if recs := m.ListRepoCIWatchRecords(); len(recs) != 2 {
			t.Errorf("records = %d, want 2 (the main run and the tag run)", len(recs))
		}
	})
}

func greenRun(id int64) RepoCIWatchRun {
	r := fakeRun(id, "CI")
	r.Status, r.Conclusion = "completed", "success"
	return r
}

func ptrInt(v int) *int { return &v }

// TestRepoCIWatchPollClasses verifies one pass ends every run it lists
// in exactly one classification: green (success), infra (cancelled),
// hung (in_progress older than hung_after_minutes), pending
// (in_progress not yet there), security (a failed security-workflow
// run, never judged) and policy (a failed CI-workflow run the judge
// ruled a policy violation). The judge is called exactly once in the
// pass — the security run is never judged.
func TestRepoCIWatchPollClasses(t *testing.T) {
	dir := t.TempDir()
	m, c, pc := newPollHarness(t, dir, RepoCIWatchConfig{
		Mode:              "shadow",
		Repos:             []string{dir},
		PollSeconds:       60,
		SecurityWorkflows: []string{"CodeQL"},
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }

	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		pc.bump(false, false, true)
		if sha == fakeHeadSHA {
			runs := []RepoCIWatchRun{
				finished(fakeRun(1, "CI"), "success"),                          // green
				finished(fakeRun(2, "CI"), "cancelled"),                        // infra
				ongoing(fakeRun(3, "CI"), "in_progress", t0.Add(-2*time.Hour)), // hung (past 60m)
				ongoing(fakeRun(4, "CI"), "queued", t0.Add(-5*time.Minute)),    // pending
				finished(fakeRun(5, "CodeQL"), "failure"),                      // security
				finished(fakeRun(6, "CI"), "failure"),                          // policy (judged)
			}
			return runs, nil
		}
		return []RepoCIWatchRun{}, nil // the tag carries no runs
	}

	judgeCalls := 0
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		judgeCalls++
		return GuardrailVerdict{Outcome: "fail", Category: "policy"}, nil
	}

	c.repoCIWatchPass()

	if judgeCalls != 1 {
		t.Fatalf("judge calls = %d, want 1 (one per pass; the security run is never judged)", judgeCalls)
	}
	byRun := recordByRunID(t, m)
	assertClass := func(runID, status, class string) {
		t.Helper()
		rec, ok := byRun[runID]
		if !ok {
			t.Errorf("no record for run %s", runID)
			return
		}
		if rec.Status != status || rec.Classification != class {
			t.Errorf("run %s: status=%q class=%q, want status=%q class=%q",
				runID, rec.Status, rec.Classification, status, class)
		}
	}
	assertClass("1", "green", "none")
	assertClass("2", "failing", "infra")
	assertClass("3", "hung", "hung")
	assertClass("4", "pending", "none")
	assertClass("5", "failing", "security")
	assertClass("6", "failing", "policy")

	if rec := byRun["5"]; byRun["5"] != nil {
		if rec.JudgeVerdict != nil {
			t.Errorf("security run 5 carries a judge verdict — it must never be judged")
		}
	}
	if rec := byRun["6"]; rec != nil && rec.JudgeVerdict == nil {
		t.Error("judged policy run 6 must carry its verdict on the record")
	}
	if byRun["6"] == nil {
		t.Error("policy run 6 has no record")
	}
}

func finished(r RepoCIWatchRun, conclusion string) RepoCIWatchRun {
	r.Status, r.Conclusion = "completed", conclusion
	return r
}

func ongoing(r RepoCIWatchRun, status string, at time.Time) RepoCIWatchRun {
	r.Status, r.Conclusion = status, ""
	r.CreatedAt, r.StartedAt = at, at
	return r
}

// nowControl is the tests' clock.
type nowControl struct {
	now time.Time
}

func (n *nowControl) advance(d time.Duration) { n.now = n.now.Add(d) }

// TestRepoCIWatchPollJudgeOnce verifies judge-once-per-run: a failed
// run with no cached verdict is judged on the pass that reaches it,
// and later passes reuse the cached verdict — the judge is called
// exactly once across three passes of the same failing run, and the
// record keeps its verdict throughout.
func TestRepoCIWatchPollJudgeOnce(t *testing.T) {
	dir := t.TempDir()
	m, c, _ := newPollHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60,
	})
	nc := &nowControl{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }

	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil // the run is only listed at the head
		}
		return []RepoCIWatchRun{finished(fakeRun(42, "CI"), "failure")}, nil
	}

	judgeCalls := 0
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		if judgeCalls >= 1 {
			t.Error("the judge was called a second time for the same run — cached verdicts must be reused")
		}
		judgeCalls++
		return GuardrailVerdict{Outcome: "fail", Category: "policy"}, nil
	}

	for i := 0; i < 3; i++ {
		c.repoCIWatchPass()
		nc.advance(61 * time.Second) // clear the poll gate for the next pass
	}
	if judgeCalls != 1 {
		t.Errorf("judge calls across 3 passes = %d, want exactly 1", judgeCalls)
	}
	recs := m.ListRepoCIWatchRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].Classification != "policy" || recs[0].JudgeVerdict == nil || recs[0].JudgeVerdict.RunID != "42" {
		t.Errorf("run 42: class=%q verdict=%#v, want policy with the cached verdict", recs[0].Classification, recs[0].JudgeVerdict)
	}
}

// TestRepoCIWatchPollJudgeErrorsStaysJudging verifies a failed judge
// call leaves the run "judging" (not a false class, and NO cached
// verdict — it must be retried), and the next pass re-judges it and
// lands it in its final class.
func TestRepoCIWatchPollJudgeErrorsStaysJudging(t *testing.T) {
	dir := t.TempDir()
	m, c, _ := newPollHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60,
	})
	nc := &nowControl{now: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }

	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(42, "CI"), "failure")}, nil
	}

	judgeCalls := 0
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		judgeCalls++
		if judgeCalls == 1 {
			return GuardrailVerdict{}, judgeErr
		}
		return GuardrailVerdict{Outcome: "fail", Category: "policy"}, nil
	}

	c.repoCIWatchPass()
	recs := m.ListRepoCIWatchRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].Status != "failing" || recs[0].Classification != "judging" {
		t.Errorf("after a failed judge: status=%q class=%q, want failing/judging", recs[0].Status, recs[0].Classification)
	}
	if recs[0].JudgeVerdict != nil {
		t.Error("a failed judge call must not leave a cached verdict on the record")
	}

	nc.advance(61 * time.Second)
	c.repoCIWatchPass()
	if judgeCalls != 2 {
		t.Errorf("judge calls = %d, want 2 (failed, then the retry)", judgeCalls)
	}
	recs = m.ListRepoCIWatchRecords()
	if recs[0].Classification != "policy" {
		t.Errorf("after a successful retry: class=%q, want policy", recs[0].Classification)
	}
}

// TestRepoCIWatchPollPerPollJudgeBudget verifies the at-most-one-judge
// per poll pass discipline: with two failed, non-security runs in the
// same pass, exactly ONE is judged this pass and the other stays
// "judging" (never guessed, never double-judged); the next pass budgets
// the second judge call and lands both in their final class.
func TestRepoCIWatchPollPerPollJudgeBudget(t *testing.T) {
	dir := t.TempDir()
	m, c, _ := newPollHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60,
	})
	nc := &nowControl{now: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }

	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil // both runs exist only at the head
		}
		return []RepoCIWatchRun{
			finished(fakeRun(50, "CI"), "failure"),
			finished(fakeRun(51, "CI"), "failure"),
		}, nil
	}

	judgeCalls := 0
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		judgeCalls++
		return GuardrailVerdict{Outcome: "fail", Category: "policy"}, nil
	}

	c.repoCIWatchPass()
	if judgeCalls != 1 {
		t.Fatalf("pass 1: judge calls = %d, want exactly 1 (one per poll pass, not per run)", judgeCalls)
	}
	byRun := recordByRunID(t, m)
	if len(byRun) != 2 {
		t.Fatalf("records = %d, want 2", len(byRun))
	}
	judged, pending := 0, 0
	for id, rec := range byRun {
		switch rec.Classification {
		case "policy":
			judged++
			if rec.JudgeVerdict == nil {
				t.Errorf("judged run %s has no cached verdict", id)
			}
		case "judging":
			pending++
			if rec.JudgeVerdict != nil {
				t.Errorf("unjudged run %s must not carry a verdict", id)
			}
		default:
			t.Errorf("run %s: class=%q, want policy (judged) or judging (budget not reached)", id, rec.Classification)
		}
	}
	if judged != 1 || pending != 1 {
		t.Fatalf("after pass 1: judged=%d pending=%d, want 1/1 (one run gets the pass's only judge call)", judged, pending)
	}

	nc.advance(61 * time.Second)
	c.repoCIWatchPass()
	if judgeCalls != 2 {
		t.Errorf("pass 2: judge calls = %d, want 2 (the judge budgets are per pass)", judgeCalls)
	}
	byRun = recordByRunID(t, m)
	for id, rec := range byRun {
		if rec.Classification != "policy" || rec.JudgeVerdict == nil {
			t.Errorf("run %s after pass 2: class=%q verdict=%#v, want policy with the cached verdict", id, rec.Classification, rec.JudgeVerdict)
		}
	}
}

// TestRepoCIWatchPollCodeOrTest verifies the last failure class: a
// failed run whose judge verdict is NOT a policy violation (here a
// content verdict) is recorded as "code_or_test" — a real code or test
// defect — with the verdict cached on the record.
func TestRepoCIWatchPollCodeOrTest(t *testing.T) {
	dir := t.TempDir()
	m, c, _ := newPollHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60,
	})
	nc := &nowControl{now: time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }

	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(7, "Lint"), "failure")}, nil
	}

	judgeCalls := 0
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		judgeCalls++
		return GuardrailVerdict{Outcome: "fail", Category: "content"}, nil
	}

	c.repoCIWatchPass()
	if judgeCalls != 1 {
		t.Fatalf("judge calls = %d, want 1", judgeCalls)
	}
	rec := recordByRunID(t, m)["7"]
	if rec == nil {
		t.Fatal("no record for the failed run")
	}
	if rec.Status != "failing" || rec.Classification != "code_or_test" {
		t.Errorf("status=%q class=%q, want failing/code_or_test (a non-policy verdict is a code/test defect)", rec.Status, rec.Classification)
	}
	if rec.JudgeVerdict == nil || rec.JudgeVerdict.Verdict != "content" {
		t.Errorf("cached verdict = %#v, want the stored \"content\" verdict", rec.JudgeVerdict)
	}
}

// TestRepoCIWatchPollTagPeelAndRecord verifies the tag path end to
// end: the newest tag in the tag list is peeled to its commit, a run
// listed at that commit records with ref kind "tag" — while the main's
// run records with ref kind "main" — and both are stored under their
// own (repo, ref-kind, sha, workflow, run-id) identity.
func TestRepoCIWatchPollTagPeelAndRecord(t *testing.T) {
	dir := t.TempDir()
	m, c, pc := newPollHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60, TagCount: ptrInt(2),
	})
	nc := &nowControl{now: time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }

	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		pc.bump(false, false, true)
		switch sha {
		case fakeHeadSHA:
			return []RepoCIWatchRun{greenRun(100)}, nil
		case fakeTagSHA:
			return []RepoCIWatchRun{greenRun(200)}, nil
		}
		return []RepoCIWatchRun{}, nil
	}
	// The only configured repository is this temp directory; the pass
	// must run it (fetch, head, tags, peel) rather than skip it.
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		return GuardrailVerdict{}, errors.New("the judge must never see a green run")
	}

	c.repoCIWatchPass()

	f, peels, lists := pc.snapshot()
	if f != 1 || peels != 2 || lists != 2 {
		t.Fatalf("calls: fetch=%d peels=%d lists=%d, want 1/2/2 (two tags in the tag_count window, one unreadable; main + one tag listed)", f, peels, lists)
	}
	recs := m.ListRepoCIWatchRecords()
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2 (one for main, one for the tag)", len(recs))
	}
	for _, rec := range recs {
		if rec.Repo != "dmz006/datawatch" {
			t.Errorf("record %s: repo=%q, want dmz006/datawatch (the origin's owner/repo)", rec.RunID, rec.Repo)
		}
	}
	var mainRec, tagRec *RepoCIWatchRecord
	for _, rec := range recs {
		switch rec.RunID {
		case "100":
			mainRec = rec
		case "200":
			tagRec = rec
		}
	}
	if mainRec == nil || tagRec == nil {
		t.Fatalf("expected the main run (100) and the tag run (200), got %#v", recs)
	}
	if mainRec.RefKind != "main" || mainRec.RefName != "main" || mainRec.SHA != fakeHeadSHA {
		t.Errorf("main record: kind=%q name=%q sha=%s, want main/main/%s", mainRec.RefKind, mainRec.RefName, mainRec.SHA, fakeHeadSHA)
	}
	if tagRec.RefKind != "tag" || tagRec.RefName != "v1.1.0" || tagRec.SHA != fakeTagSHA {
		t.Errorf("tag record: kind=%q name=%q sha=%s, want tag/v1.1.0/%s", tagRec.RefKind, tagRec.RefName, tagRec.SHA, fakeTagSHA)
	}
}
