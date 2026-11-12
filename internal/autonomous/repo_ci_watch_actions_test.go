// The repo CI watch action phase, verified through the pass with every
// write behind a fake: cancel of a hung run (once), rerun of an
// infra-failing run (once), the record-first/ green-second deletion
// order, the record-only classes, shadow mode's zero writes, and the
// alert gate.
package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

// actionCounters tallies the GitHub writes the pass attempts.
type actionCounters struct {
	mu      sync.Mutex
	cancels int
	reruns  int
	deletes int
}

func (a *actionCounters) wire(m *Manager) {
	m.repoCIAncestorFn = func(ctx context.Context, repoDir, older, newer string) bool {
		return older == "s1" && newer == "s2"
	}
	m.repoCICancelFn = func(ctx context.Context, ownerRepo string, runID int64) error {
		a.mu.Lock()
		a.cancels++
		a.mu.Unlock()
		return nil
	}
	m.repoCIRerunFn = func(ctx context.Context, ownerRepo string, runID int64) error {
		a.mu.Lock()
		a.reruns++
		a.mu.Unlock()
		return nil
	}
	m.repoCIDeleteFn = func(ctx context.Context, ownerRepo string, runID int64) error {
		a.mu.Lock()
		a.deletes++
		a.mu.Unlock()
		return nil
	}
}

func (a *actionCounters) snapshot() (cancels, reruns, deletes int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancels, a.reruns, a.deletes
}

// alertSink captures the Coordinator's alerts.
type alertSinkCapture struct {
	mu     sync.Mutex
	alerts []string
	body   string
}

func (s *alertSinkCapture) fn() func(level, title, body string) (id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return func(level, title, body string) (id string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.alerts = append(s.alerts, level+"|"+title)
		s.body = body
		return "al-1"
	}
}

func hasLog(rec *RepoCIWatchRecord, action, runID string) bool {
	for _, e := range rec.RerunLog {
		if e.Action == action && e.RunID == runID {
			return true
		}
	}
	return false
}

func hasWouldDo(rec *RepoCIWatchRecord, action, runID string) bool {
	for _, e := range rec.WouldDo {
		if e.Action == action && e.RunID == runID {
			return true
		}
	}
	return false
}

// newActionHarness is the poll harness plus an action write counter.
func newActionHarness(t *testing.T, repo string, cfg RepoCIWatchConfig) (m *Manager, c *Coordinator, ac *actionCounters) {
	t.Helper()
	m, c, _ = newPollHarness(t, repo, cfg)
	ac = &actionCounters{}
	ac.wire(m)
	return m, c, ac
}

// TestRepoCIWatchActionHungCancelRerun verifies a hung run is
// cancelled exactly once (a later poll finding it already cancelled or
// completed cancels nothing), with the cancel recorded in the record's
// rerun_log and no rerun of its own.
func TestRepoCIWatchActionHungCancelRerun(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60, AlertMinutes: ptrInt(0),
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{ongoing(fakeRun(200, "CI"), "in_progress", t0.Add(-3*time.Hour))}, nil
	}
	c.repoCIWatchPass()
	cancels, reruns, _ := ac.snapshot()
	if cancels != 1 {
		t.Errorf("pass 1: cancels = %d, want 1 (hung past the threshold)", cancels)
	}
	if reruns != 0 {
		t.Errorf("pass 1: reruns = %d, want 0 (a hung run is cancelled, not rerun)", reruns)
	}
	rec := recordByRunID(t, m)["200"]
	if rec == nil {
		t.Fatal("no record for hung run 200")
	}
	if !hasLog(rec, "cancel", "200") {
		t.Error("the cancel must be recorded in the record's rerun_log")
	}
	if hasLog(rec, "rerun", "200") {
		t.Error("a hung run is never rerun; the rerun_log carries the cancel only")
	}

	nc.advance(time.Minute)
	c.repoCIWatchPass() // still in_progress — the second poll cancels nothing
	if cancels, _, _ := ac.snapshot(); cancels != 1 {
		t.Errorf("pass 2: cancels = %d, want 1 (the run is already cancelled, nothing more to cancel)", cancels)
	}
}

// TestRepoCIWatchActionInfraOnceNotTwice verifies an infra-failing run
// is rerun at most once while the infra retry budget has reruns left
// (the budget is per run, not per poll): three passes of the same
// failing run take exactly one rerun.
func TestRepoCIWatchActionInfraOnceNotTwice(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60, AlertMinutes: ptrInt(0),
	})
	base := m.Config()
	base.RetryPolicy = map[string]int{"infra": 4} // the pass's budget
	m.SetConfig(base)
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(100, "CI"), "cancelled")}, nil
	}

	for i := 0; i < 3; i++ {
		if i > 0 {
			nc.advance(time.Minute)
		}
		c.repoCIWatchPass()
	}
	cancels, reruns, deletes := ac.snapshot()
	if reruns != 1 {
		t.Errorf("reruns = %d, want 1 (an infra run reruns at most once — the budget counts its reruns, not each poll)", reruns)
	}
	if cancels != 0 || deletes != 0 {
		t.Errorf("cancels = %d, deletes = %d, want 0 and 0", cancels, deletes)
	}
	rec := recordByRunID(t, m)["100"]
	if rec == nil {
		t.Fatal("no record for infra run 100")
	}
	n := 0
	for _, e := range rec.RerunLog {
		if e.Action == "rerun" && e.RunID == "100" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("rerun_log rerun entries for run 100 = %d, want 1", n)
	}
}

// TestRepoCIWatchActionCodeTestNeverRerun verifies a code-or-test
// failing run is record-only: no rerun (or other write), no rerun_log,
// no would_do.
func TestRepoCIWatchActionCodeTestNeverRerun(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60, AlertMinutes: ptrInt(0),
	})
	nc := &nowControl{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(250, "CI"), "failure")}, nil
	}
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "fail", Category: "test"}, nil
	}
	c.repoCIWatchPass()
	cancels, reruns, deletes := ac.snapshot()
	if cancels != 0 || reruns != 0 || deletes != 0 {
		t.Errorf("code_or_test: cancels = %d, reruns = %d, deletes = %d, want 0 (record-only)", cancels, reruns, deletes)
	}
	rec := recordByRunID(t, m)["250"]
	if rec == nil {
		t.Fatal("no record for run 250")
	}
	if len(rec.RerunLog) != 0 {
		t.Errorf("code_or_test: rerun_log = %v, want empty", rec.RerunLog)
	}
	if hasWouldDo(rec, "rerun", "250") {
		t.Error("code_or_test: a would_do rerun row must not exist — a code/test failure is never rerun")
	}
}

// TestRepoCIWatchActionSecurityNeverRerunDelete verifies a failing
// run of a security workflow is record-only even in shadow: no
// cancel, rerun, or delete, and no would_do row for any of them.
func TestRepoCIWatchActionSecurityNeverRerunDelete(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode:              "shadow",
		Repos:             []string{dir},
		PollSeconds:       60,
		AlertMinutes:      ptrInt(0),
		SecurityWorkflows: []string{"CodeQL"},
	})
	nc := &nowControl{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(260, "CodeQL"), "failure")}, nil
	}
	c.repoCIWatchPass()
	cancels, reruns, deletes := ac.snapshot()
	if cancels != 0 || reruns != 0 || deletes != 0 {
		t.Errorf("security: cancels = %d, reruns = %d, deletes = %d, want 0 (a security run is never acted on)", cancels, reruns, deletes)
	}
	rec := recordByRunID(t, m)["260"]
	if rec == nil {
		t.Fatal("no record for security run 260")
	}
	if rec.Classification != "security" {
		t.Errorf("security run 260: classification = %q, want security", rec.Classification)
	}
	if len(rec.RerunLog) != 0 {
		t.Errorf("security: rerun_log = %v, want empty", rec.RerunLog)
	}
	if len(rec.WouldDo) != 0 {
		t.Errorf("security: would_do = %v, want empty (record-only is literal)", rec.WouldDo)
	}
}

// TestRepoCIWatchActionPolicyNeverRerun verifies a policy-violation
// failing run is record-only: no write, no would_do rerun row.
func TestRepoCIWatchActionPolicyNeverRerun(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60, AlertMinutes: ptrInt(0),
	})
	nc := &nowControl{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(270, "CI"), "failure")}, nil
	}
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "fail", Category: "policy"}, nil
	}
	c.repoCIWatchPass()
	cancels, reruns, deletes := ac.snapshot()
	if cancels != 0 || reruns != 0 || deletes != 0 {
		t.Errorf("policy: cancels = %d, reruns = %d, deletes = %d, want 0 (a policy violation is record-only)", cancels, reruns, deletes)
	}
	rec := recordByRunID(t, m)["270"]
	if rec == nil {
		t.Fatal("no record for policy run 270")
	}
	if rec.Classification != "policy" {
		t.Errorf("policy run 270: classification = %q, want policy", rec.Classification)
	}
	if hasWouldDo(rec, "rerun", "270") {
		t.Error("policy: no would_do rerun row — the run is never rerun")
	}
}

// TestRepoCIWatchActionShadowNoWrite verifies shadow mode records the
// intended actions as would_do stand-ins and writes nothing to GitHub.
func TestRepoCIWatchActionShadowNoWrite(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60, AlertMinutes: ptrInt(0),
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{
			finished(fakeRun(100, "CI"), "cancelled"),                        // infra → would rerun
			ongoing(fakeRun(200, "CI"), "in_progress", t0.Add(-3*time.Hour)), // hung → would cancel
		}, nil
	}
	c.repoCIWatchPass()
	cancels, reruns, deletes := ac.snapshot()
	if cancels != 0 || reruns != 0 || deletes != 0 {
		t.Fatalf("shadow: cancels = %d, reruns = %d, deletes = %d, want 0 (shadow never writes)", cancels, reruns, deletes)
	}
	byRun := recordByRunID(t, m)
	if rec := byRun["100"]; rec == nil || !hasWouldDo(rec, "rerun", "100") {
		t.Errorf("shadow: infra run 100 must carry a would_do rerun row (got %+v)", byRun["100"])
	}
	if rec := byRun["200"]; rec == nil || !hasWouldDo(rec, "cancel", "200") {
		t.Errorf("shadow: hung run 200 must carry a would_do cancel row (got %+v)", byRun["200"])
	}
}

// TestRepoCIWatchActionSupersededReportDefault verifies the default
// (report) superseded policy: the earlier failing run is named on the
// green record that superseded it, a would_do stand-in is written for
// it, and nothing is deleted — in either mode.
func TestRepoCIWatchActionSupersededReportDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60, AlertMinutes: ptrInt(0),
	}
	nc := &nowControl{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}

	t.Run("mode on, report", func(t *testing.T) {
		m, c, ac := newActionHarness(t, dir, cfg)
		c.now = func() time.Time { return nc.now }
		judge := func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
			return GuardrailVerdict{Outcome: "fail", Category: "test"}, nil
		}
		m.repoCIJudgeFn = judge
		runsFor := func(sha string) []RepoCIWatchRun {
			switch sha {
			case "s1":
				return []RepoCIWatchRun{finished(fakeRun(301, "CI"), "failure")}
			case "s2":
				return []RepoCIWatchRun{greenRun(302)}
			default:
				return []RepoCIWatchRun{}
			}
		}

		m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) { return "s1", nil }
		m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
			return runsFor(sha), nil
		}
		c.repoCIWatchPass() // the failing run at s1 is recorded

		nc.advance(time.Minute)
		m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) { return "s2", nil }
		c.repoCIWatchPass() // the green run at s2 names it; report mode

		if _, _, deletes := ac.snapshot(); deletes != 0 {
			t.Errorf("report mode: deletes = %d, want 0 (deletion requires delete_superseded=delete)", deletes)
		}
		green := recordByRunID(t, m)["302"]
		if green == nil {
			t.Fatal("no record for green run 302")
		}
		named := false
		for _, sr := range green.SupersededRunIDs {
			if sr.RunID == "301" {
				named = true
			}
		}
		if !named {
			t.Error("the green record must name superseded run 301 in its superseded_run_ids")
		}
		if !hasWouldDo(green, "delete", "301") {
			t.Error("report mode: the green record must stand in with a would_do delete row for run 301")
		}
	})

	t.Run("shadow", func(t *testing.T) {
		cfg.Mode = "shadow"
		m, c, ac := newActionHarness(t, dir, cfg)
		nc2 := &nowControl{now: nc.now.Add(-30 * time.Minute)}
		c.now = func() time.Time { return nc2.now }
		m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
			return GuardrailVerdict{Outcome: "fail", Category: "test"}, nil
		}
		runsFor := func(sha string) []RepoCIWatchRun {
			switch sha {
			case "s1":
				return []RepoCIWatchRun{finished(fakeRun(301, "CI"), "failure")}
			case "s2":
				return []RepoCIWatchRun{greenRun(302)}
			default:
				return []RepoCIWatchRun{}
			}
		}
		m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) { return "s1", nil }
		m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
			return runsFor(sha), nil
		}
		c.repoCIWatchPass()

		nc2.advance(time.Minute)
		m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) { return "s2", nil }
		c.repoCIWatchPass()

		if _, _, deletes := ac.snapshot(); deletes != 0 {
			t.Errorf("shadow: deletes = %d, want 0 (shadow never writes)", deletes)
		}
		green := recordByRunID(t, m)["302"]
		if green == nil {
			t.Fatal("no record for green run 302")
		}
		if !hasWouldDo(green, "delete", "301") {
			t.Error("shadow: the green record must stand in with a would_do delete row for run 301")
		}
	})
}

// TestRepoCIWatchActionSupersededNotDeletedBeforeRecord verifies the
// order: a superseded run is never deleted in the same poll that
// first named it on the green record's superseded_run_ids.
func TestRepoCIWatchActionSupersededNotDeletedBeforeRecord(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode:             "on",
		Repos:            []string{dir},
		PollSeconds:      60,
		AlertMinutes:     ptrInt(0),
		DeleteSuperseded: "delete",
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "fail", Category: "test"}, nil
	}
	runsFor := func(sha string) []RepoCIWatchRun {
		switch sha {
		case "s1":
			return []RepoCIWatchRun{finished(fakeRun(301, "CI"), "failure")}
		case "s2":
			return []RepoCIWatchRun{greenRun(302)}
		default:
			return []RepoCIWatchRun{}
		}
	}
	m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) { return "s1", nil }
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		return runsFor(sha), nil
	}
	c.repoCIWatchPass() // pass 1: the failing run at s1 is recorded

	nc.advance(time.Minute)
	m.repoCIHeadFn = func(ctx context.Context, repoDir string) (string, error) { return "s2", nil }
	c.repoCIWatchPass() // pass 2: the green run at s2 names it — the same poll must not delete

	if _, _, deletes := ac.snapshot(); deletes != 0 {
		t.Errorf("the naming poll deletes = %d, want 0 — the record must name the run first, in the previous poll at the latest", deletes)
	}
	green := recordByRunID(t, m)["302"]
	if green == nil {
		t.Fatal("no record for green run 302")
	}
	named := false
	for _, sr := range green.SupersededRunIDs {
		if sr.RunID == "301" {
			named = true
		}
	}
	if !named {
		t.Error("the green record must carry run 301 in superseded_run_ids (that is the record the deletion is gated on)")
	}
	if hasLog(green, "delete", "301") {
		t.Error("the deletion log entry must not appear on the naming poll")
	}
}

// TestRepoCIWatchActionSupersededNotDeletedWithoutGreen verifies a
// superseded run is only deleted by a later green commit of the same
// workflow: failed runs on top of failed runs never delete, no matter
// the mode.
func TestRepoCIWatchActionSupersededNotDeletedWithoutGreen(t *testing.T) {
	dir := t.TempDir()
	m, c, ac := newActionHarness(t, dir, RepoCIWatchConfig{
		Mode:             "on",
		Repos:            []string{dir},
		PollSeconds:      60,
		AlertMinutes:     ptrInt(0),
		DeleteSuperseded: "delete",
	})
	nc := &nowControl{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }
	m.repoCIJudgeFn = func(ctx context.Context, repoDir, label string) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "fail", Category: "test"}, nil
	}
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(301, "CI"), "failure"), finished(fakeRun(303, "CI"), "failure")}, nil
	}
	for i := 0; i < 2; i++ {
		if i > 0 {
			nc.advance(time.Minute)
		}
		c.repoCIWatchPass()
	}
	if _, _, deletes := ac.snapshot(); deletes != 0 {
		t.Errorf("deletes = %d, want 0 — without a green commit nothing supersedes anything", deletes)
	}
}
