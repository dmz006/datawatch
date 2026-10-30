// supervisor_watch_compare_test.go — the supervisor watch
// cross-compare: the record file the oracle script publishes, its
// freshness rule (unavailable is not a disagreement), the set
// comparison in both disagreement directions, the comparison history
// cap, the per-day agreement summary, and the persist + reload
// round-trip. Run with:
//
//	env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestSupervisorWatchCompare
package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// swcFindings renders kind/subject pairs into the watch-side findings.
func swcFindings(pairs ...[2]string) []SupervisorWatchFinding {
	out := make([]SupervisorWatchFinding, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, SupervisorWatchFinding{Kind: SupervisorWatchKind(p[0]), Subject: p[1]})
	}
	return out
}

// swcScriptFindings renders kind/subject pairs into the script-side record findings.
func swcScriptFindings(pairs ...[2]string) []supervisorWatchScriptFinding {
	out := make([]supervisorWatchScriptFinding, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, supervisorWatchScriptFinding{Kind: p[0], Subject: p[1], Text: "line"})
	}
	return out
}

// swcWriteRecord writes an oracle-script record file (the --record shape)
// at dir/record.json stamped at at and returns its path.
func swcWriteRecord(t *testing.T, dir string, at time.Time, pairs ...[2]string) string {
	t.Helper()
	rec := supervisorWatchScriptRecord{At: at.Unix(), Findings: swcScriptFindings(pairs...)}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	p := filepath.Join(dir, "record.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	return p
}

// swcSetScriptPath points the manager's supervisor watch config at p.
func swcSetScriptPath(t *testing.T, m *Manager, p string) SupervisorWatchConfig {
	t.Helper()
	m.mu.Lock()
	cfg := m.cfg.Automation.SupervisorWatch
	cfg.ScriptOutputPath = p
	m.cfg.Automation.SupervisorWatch = cfg
	m.mu.Unlock()
	return cfg
}

// swcList returns the manager's comparison history (nil store reads as
// empty).
func swcList(t *testing.T, m *Manager) []SupervisorWatchComparison {
	t.Helper()
	st := m.supervisorWatchStore()
	if st == nil {
		return nil
	}
	return st.ListComparisons()
}

// TestSupervisorWatchCompareFullAgreement pins the happy path: the
// daemon-side findings and the oracle script's record file raise the
// same kind+subject set, the comparison records agreed, and the day
// shows up as a full-agreement day.
func TestSupervisorWatchCompareFullAgreement(t *testing.T) {
	m := swNewManager(t)
	now := time.Now()

	path := swcWriteRecord(t, t.TempDir(), now,
		[2]string{"tmux_restart", "local"},
		[2]string{"task_failed", "prd-1"},
		[2]string{"task_review_needed", "prd-2"},
	)
	cfg := swcSetScriptPath(t, m, path)

	cmp := m.supervisorWatchCompare(cfg, swcFindings(
		[2]string{"task_failed", "prd-1"},  // order and duplicates must not matter —
		[2]string{"tmux_restart", "local"}, // it is a set comparison
		[2]string{"tmux_restart", "local"},
		[2]string{"task_review_needed", "prd-2"},
	), now)
	if cmp == nil {
		t.Fatal("comparison not recorded")
	}
	if !cmp.Agreed {
		t.Errorf("Agreed = false, want true; only_watch=%v only_script=%v", cmp.OnlyWatch, cmp.OnlyScript)
	}
	if len(cmp.WatchKinds) != 3 || len(cmp.ScriptKinds) != 3 {
		t.Errorf("watch=%v script=%v, want 3 entries each", cmp.WatchKinds, cmp.ScriptKinds)
	}
	if len(cmp.OnlyWatch) != 0 || len(cmp.OnlyScript) != 0 {
		t.Errorf("non-empty diffs on a full agreement: %v / %v", cmp.OnlyWatch, cmp.OnlyScript)
	}
	if got := swcList(t, m); len(got) != 1 || !got[0].Agreed {
		t.Errorf("store history = %+v, want one agreed comparison", got)
	}
	day := m.supervisorWatchStore().DayAgreement()
	if day.FullAgreementDays() != 1 {
		t.Errorf("FullAgreementDays() = %d, want 1 (summary %+v)", day.FullAgreementDays(), day.Days)
	}
	if d, ok := day.Days[compareDayKey(now)]; !ok || !d.Full() {
		t.Errorf("day %s = %+v (present=%v), want a full-agreement day", compareDayKey(now), d, ok)
	}
}

// TestSupervisorWatchCompareOnlyWatch pins the one-sided difference
// where the daemon-side watch raised a finding the oracle script did
// not: not agreed, named in OnlyWatch, and the day stops being a
// full-agreement day.
func TestSupervisorWatchCompareOnlyWatch(t *testing.T) {
	m := swNewManager(t)
	now := time.Now()

	path := swcWriteRecord(t, t.TempDir(), now,
		[2]string{"tmux_restart", "local"},
	)
	cfg := swcSetScriptPath(t, m, path)

	cmp := m.supervisorWatchCompare(cfg, swcFindings(
		[2]string{"tmux_restart", "local"},
		[2]string{"task_failed", "prd-2"},
	), now)
	if cmp == nil {
		t.Fatal("comparison not recorded")
	}
	if cmp.Agreed {
		t.Fatal("Agreed = true, want false (the watch raised task_failed the script did not)")
	}
	if len(cmp.OnlyWatch) != 1 || cmp.OnlyWatch[0] != "task_failed/prd-2" {
		t.Errorf("OnlyWatch = %v, want [task_failed/prd-2]", cmp.OnlyWatch)
	}
	if len(cmp.OnlyScript) != 0 {
		t.Errorf("OnlyScript = %v, want empty", cmp.OnlyScript)
	}
	day := m.supervisorWatchStore().DayAgreement()
	if day.FullAgreementDays() != 0 {
		t.Errorf("FullAgreementDays() = %d, want 0 (summary %+v)", day.FullAgreementDays(), day.Days)
	}
	if d := day.Days[compareDayKey(now)]; d.Total != 1 || d.Disagreed != 1 {
		t.Errorf("day tally = %+v, want {total 1, disagreed 1}", d)
	}
}

// TestSupervisorWatchCompareOnlyScript pins the other one-sided
// difference: a finding the oracle script raised that the daemon-side
// watch did not.
func TestSupervisorWatchCompareOnlyScript(t *testing.T) {
	m := swNewManager(t)
	now := time.Now()

	path := swcWriteRecord(t, t.TempDir(), now,
		[2]string{"ci_red", "401234"},
		[2]string{"tmux_restart", "local"},
	)
	cfg := swcSetScriptPath(t, m, path)

	cmp := m.supervisorWatchCompare(cfg, swcFindings(
		[2]string{"tmux_restart", "local"},
	), now)
	if cmp == nil {
		t.Fatal("comparison not recorded")
	}
	if cmp.Agreed {
		t.Fatal("Agreed = true, want false (the script raised ci_red the watch did not)")
	}
	if len(cmp.OnlyScript) != 1 || cmp.OnlyScript[0] != "ci_red/401234" {
		t.Errorf("OnlyScript = %v, want [ci_red/401234]", cmp.OnlyScript)
	}
	if len(cmp.OnlyWatch) != 0 {
		t.Errorf("OnlyWatch = %v, want empty", cmp.OnlyWatch)
	}
}

// TestSupervisorWatchCompareMissingFile pins that "script output
// unavailable" is not a disagreement: a record path that does not
// exist records no comparison at all, and the store's history stays
// empty for it.
func TestSupervisorWatchCompareMissingFile(t *testing.T) {
	m := swNewManager(t)
	cfg := swcSetScriptPath(t, m, filepath.Join(t.TempDir(), "does-not-exist.json"))

	cmp := m.supervisorWatchCompare(cfg, swcFindings([2]string{"tmux_restart", "local"}), time.Now())
	if cmp != nil {
		t.Fatalf("comparison recorded for a missing file: %+v", cmp)
	}
	if got := swcList(t, m); len(got) != 0 {
		t.Errorf("store history = %+v, want empty (missing file must not count as a disagreement)", got)
	}
	// Same for an unparseable file.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("this is not json"), 0o644); err != nil {
		t.Fatalf("write bad record: %v", err)
	}
	cfg = swcSetScriptPath(t, m, bad)
	if cmp := m.supervisorWatchCompare(cfg, nil, time.Now()); cmp != nil {
		t.Fatalf("comparison recorded for an unparseable file: %+v", cmp)
	}
}

// TestSupervisorWatchCompareStaleFile pins the freshness rule: a
// record file older than three poll intervals is "unavailable" — the
// pass records no comparison and no disagreement — while a record
// written this pass is comparable.
func TestSupervisorWatchCompareStaleFile(t *testing.T) {
	m := swNewManager(t)
	now := time.Now()

	stale := swcWriteRecord(t, t.TempDir(), now.Add(-4*time.Minute),
		[2]string{"tmux_restart", "local"},
	)
	cfg := swcSetScriptPath(t, m, stale)
	if cmp := m.supervisorWatchCompare(cfg, swcFindings([2]string{"tmux_restart", "local"}), now); cmp != nil {
		t.Fatalf("stale record compared: %+v (want: recorded nothing)", cmp)
	}
	if got := swcList(t, m); len(got) != 0 {
		t.Errorf("store history = %+v, want empty (stale file must not count)", got)
	}

	// A record written within the window is comparable; both directions of
	// the explicit fresh-vs-stale verdict hold either way.
	fresh := swcWriteRecord(t, t.TempDir(), now,
		[2]string{"tmux_restart", "local"},
	)
	if !supervisorWatchScriptFresh(swcRecord(t, fresh), now) {
		t.Error("a record written this pass reads stale, want fresh")
	}
	if supervisorWatchScriptFresh(supervisorWatchScriptRecord{At: now.Add(-4 * time.Minute).Unix()}, now) {
		t.Error("a 4-minute-old record (past 3 poll intervals) reads fresh, want stale")
	}
	cfg = swcSetScriptPath(t, m, fresh)
	if cmp := m.supervisorWatchCompare(cfg, swcFindings([2]string{"tmux_restart", "local"}), now); cmp == nil || !cmp.Agreed {
		t.Errorf("fresh record not compared: %+v", cmp)
	}
}

// swcRecord loads a record file back through the same loader the pass
// uses (test-only helper for the boundary check above).
func swcRecord(t *testing.T, path string) supervisorWatchScriptRecord {
	t.Helper()
	rec, err := loadSupervisorWatchScriptRecord(path)
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	return rec
}

// TestSupervisorWatchCompareDaySummary pins the per-day agreement
// summary: days whose comparisons all agree count as full-agreement
// days, a single disagreement in a day does not, and the summary
// survives a store reload.
func TestSupervisorWatchCompareDaySummary(t *testing.T) {
	m := swNewManager(t)
	st := m.supervisorWatchStore()
	if st == nil {
		t.Fatal("no supervisor watch store")
	}

	dayA := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	dayB := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	dayC := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

	rec := func(at time.Time, agreed bool) {
		t.Helper()
		c := SupervisorWatchComparison{
			At:          at,
			WatchKinds:  []string{"tmux_restart/local"},
			ScriptKinds: []string{"tmux_restart/local"},
			Agreed:      agreed,
		}
		if !agreed {
			c.WatchKinds = append(c.WatchKinds, "task_failed/prd-1")
			c.OnlyWatch = []string{"task_failed/prd-1"}
		}
		if _, err := st.RecordComparison(c); err != nil {
			t.Fatalf("RecordComparison(%v): %v", at, err)
		}
	}
	rec(dayA, true)
	rec(dayA, true) // a full day keeps counting after a second passing comparison
	rec(dayB, true)
	rec(dayB, false) // one disagreement in the day loses it
	rec(dayC, true)

	day := st.DayAgreement()
	for name, want := range map[string]bool{
		compareDayKey(dayA): true,
		compareDayKey(dayB): false,
		compareDayKey(dayC): true,
	} {
		d, ok := day.Days[name]
		if !ok {
			t.Errorf("day %s missing from the summary %+v", name, day.Days)
			continue
		}
		if d.Full() != want {
			t.Errorf("day %s = %+v, want full-agreement = %v", name, d, want)
		}
	}
	if got := day.FullAgreementDays(); got != 2 {
		t.Errorf("FullAgreementDays() = %d, want 2 (summary %+v)", got, day.Days)
	}

	// The history and the per-day summary must survive a reload of the
	// same store directory.
	reopened, err := NewSupervisorWatchStore(st.dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if got := reopened.ListComparisons(); len(got) != 5 {
		t.Errorf("reloaded history has %d comparison(s), want 5", len(got))
	}
	day = reopened.DayAgreement()
	if got := day.FullAgreementDays(); got != 2 {
		t.Errorf("reloaded FullAgreementDays() = %d, want 2", got)
	}
}

// TestSupervisorWatchCompareHistoryCap pins the history bound: the
// store keeps the last SupervisorWatchComparisonLen (500) comparisons.
func TestSupervisorWatchCompareHistoryCap(t *testing.T) {
	st, err := NewSupervisorWatchStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSupervisorWatchStore: %v", err)
	}
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	want := SupervisorWatchComparisonLen
	for i := 0; i < want+50; i++ {
		if _, err := st.RecordComparison(SupervisorWatchComparison{
			At:          base.Add(time.Duration(i) * time.Minute),
			WatchKinds:  []string{},
			ScriptKinds: []string{},
			Agreed:      true,
		}); err != nil {
			t.Fatalf("RecordComparison #%d: %v", i, err)
		}
	}
	hist := st.ListComparisons()
	if len(hist) != want {
		t.Fatalf("history has %d comparisons, want %d (the cap)", len(hist), want)
	}
	// The oldest ones fell off; the newest one is the last recorded.
	if got := hist[len(hist)-1].At; !got.Equal(base.Add(time.Duration(want+49) * time.Minute)) {
		t.Errorf("newest kept comparison At = %v, want %v", got, base.Add(time.Duration(want+49)*time.Minute))
	}
}
