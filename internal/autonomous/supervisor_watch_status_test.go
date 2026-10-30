package autonomous

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// Tests for the read view the supervisor watch assembles from the
// manager (SupervisorWatchStatus), its operator rendering
// (RenderSupervisorWatch, the one text both the CLI and the comm verb
// print) and the manager's wiring seams.

func TestSupervisorWatchStatusNilManager(t *testing.T) {
	var m *Manager
	got := m.SupervisorWatchStatus()
	if got.Mode != "" || got.Findings != nil || !got.LastPollAt.IsZero() {
		t.Errorf("a nil manager reads as an empty view, got %+v", got)
	}
}

func TestSupervisorWatchStatusAssemblesTheView(t *testing.T) {
	m := swNewManager(t)

	// fresh manager: the release default mode, no poll, no comparison
	fresh := m.SupervisorWatchStatus()
	if fresh.Mode != SupervisorWatchModeShadow {
		t.Errorf("fresh mode = %q, want the shadow default", fresh.Mode)
	}
	if !fresh.LastPollAt.IsZero() || fresh.Comparison.Last != nil || fresh.Comparison.AgreedDays != 0 {
		t.Errorf("fresh view must have no poll and no comparison, got %+v", fresh)
	}
	if fresh.Comparison.OnlyWatch == nil || fresh.Comparison.OnlyScript == nil {
		t.Errorf("the one-sided lists must be empty lists, not nil: %+v", fresh.Comparison)
	}

	seed := swFinding(SupervisorWatchTaskFailed, "p1", "Automaton p1 task failed")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatal(err)
	}
	st := m.supervisorWatchStore()
	if _, err := st.RecordComparison(SupervisorWatchComparison{
		At: time.Now(), OnlyWatch: []string{"task_failed/p1"}, Agreed: false,
	}); err != nil {
		t.Fatal(err)
	}

	c := NewCoordinator(m, swFakeStarter{})
	c.supervisorWatchLastPoll = time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	m.mu.Lock()
	m.coordinator = c
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeOn
	m.mu.Unlock()

	got := m.SupervisorWatchStatus()
	if got.Mode != SupervisorWatchModeOn {
		t.Errorf("mode = %q, want on", got.Mode)
	}
	if !got.LastPollAt.Equal(c.supervisorWatchLastPoll) {
		t.Errorf("last poll = %v, want the coordinator's", got.LastPollAt)
	}
	if len(got.Findings) != 1 || got.Findings[0].Subject != "p1" {
		t.Errorf("findings = %+v", got.Findings)
	}
	if got.Comparison.Last == nil || got.Comparison.Last.Agreed {
		t.Errorf("last comparison = %+v, want the disagreeing one", got.Comparison.Last)
	}
	if len(got.Comparison.OnlyWatch) != 1 || got.Comparison.OnlyScript == nil || len(got.Comparison.OnlyScript) != 0 {
		t.Errorf("one-sided lists = %v / %v", got.Comparison.OnlyWatch, got.Comparison.OnlyScript)
	}
	if got.Comparison.AgreedDays != 0 {
		t.Errorf("a disagreeing day must not count: AgreedDays = %d", got.Comparison.AgreedDays)
	}

	// an agreeing comparison on a later pass makes today count only if
	// every comparison of the day agreed — today already has a miss
	if _, err := st.RecordComparison(SupervisorWatchComparison{At: time.Now(), Agreed: true}); err != nil {
		t.Fatal(err)
	}
	if again := m.SupervisorWatchStatus(); again.Comparison.AgreedDays != 0 {
		t.Errorf("a day with a disagreement is not a full-agreement day, got %d", again.Comparison.AgreedDays)
	}
}

func TestSupervisorWatchStatusAgreedDay(t *testing.T) {
	m := swNewManager(t)
	st := m.supervisorWatchStore()
	if _, err := st.RecordComparison(SupervisorWatchComparison{At: time.Now(), Agreed: true}); err != nil {
		t.Fatal(err)
	}
	got := m.SupervisorWatchStatus()
	if got.Comparison.AgreedDays != 1 || got.Comparison.Last == nil || !got.Comparison.Last.Agreed {
		t.Errorf("one agreeing comparison = one agreed day, got %+v", got.Comparison)
	}
}

func TestSupervisorWatchManagerSeamsAndGuards(t *testing.T) {
	// every wiring seam is a no-op on a nil manager
	var nilM *Manager
	nilM.SetSupervisorWatchProbe(nil)
	nilM.SetSupervisorWatchNodeView(nil)
	nilM.SetSupervisorWatchSessionLookup(nil)
	nilM.SetSupervisorWatchFindingsFn(nil)
	nilM.SupervisorWatchResetAlerts()
	if nilM.supervisorWatchStore() != nil || nilM.supervisorWatchList() != nil {
		t.Error("a nil manager has no store and no list")
	}
	if _, err := nilM.SaveSupervisorWatchFinding(&SupervisorWatchFinding{}); err == nil {
		t.Error("saving without a store must be an error")
	}
	if err := nilM.ResolveSupervisorWatchFinding("k"); err == nil {
		t.Error("resolving without a store must be an error")
	}

	m := swNewManager(t)
	m.SetSupervisorWatchSessionLookup(func(id string) (*session.Session, bool) {
		return &session.Session{LogFile: "logs/" + id + ".log"}, true
	})
	m.mu.Lock()
	sess := m.supervisorWatchSess
	m.mu.Unlock()
	if p, ok := sess.SessionLogPath("s1"); !ok || p != "logs/s1.log" {
		t.Errorf("the lookup was not wired: %q %v", p, ok)
	}
	m.SetSupervisorWatchSessionLookup(nil)
	m.mu.Lock()
	cleared := m.supervisorWatchSess
	m.mu.Unlock()
	if cleared != nil {
		t.Error("a nil lookup must clear the seam")
	}

	m.SetSupervisorWatchNodeView(nil)
	m.SetSupervisorWatchProbe(nil)

	// alert dedup: the first claim wins, a reset lets it be claimed again
	if !m.supervisorWatchTakeAlert("k") || m.supervisorWatchTakeAlert("k") {
		t.Error("the episode's alert must be claimable exactly once")
	}
	m.SupervisorWatchResetAlerts()
	if !m.supervisorWatchTakeAlert("k") {
		t.Error("after a reset the alert is claimable again")
	}

	// resolve closes a stored finding
	seed := swFinding(SupervisorWatchCIRed, "9", "CI run 9 is red")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatal(err)
	}
	if err := m.ResolveSupervisorWatchFinding(SupervisorWatchKey(SupervisorWatchCIRed, "9")); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if f := swGet(t, m, SupervisorWatchKey(SupervisorWatchCIRed, "9")); f == nil || !f.Resolved {
		t.Errorf("the finding must be resolved, got %+v", f)
	}
}

func TestSupervisorWatchViewAdapters(t *testing.T) {
	// the lane fill view reads unavailable for a nil manager
	if _, ok := (supervisorWatchLaneFill{}).LaneFillStat(); ok {
		t.Error("a nil manager must read as no lane fill status")
	}
	m := swNewManager(t)
	if _, ok := (supervisorWatchLaneFill{m: m}).LaneFillStat(); !ok {
		t.Error("a manager must serve its lane fill status")
	}

	if got := (supervisorWatchPRDLister{prds: []*PRD{{ID: "a"}}}).PRDs(); len(got) != 1 {
		t.Errorf("PRD lister = %v", got)
	}
	if got := (supervisorWatchCISnapshot{recs: []*RepoCIWatchRecord{{}}}).CIWatchRecords(); len(got) != 1 {
		t.Errorf("CI snapshot = %v", got)
	}
	var nilM *Manager
	if nilM.supervisorWatchCIRecords() != nil || nilM.supervisorWatchPRDs() != nil {
		t.Error("a nil manager has no CI records and no Automata")
	}
	if got := m.supervisorWatchCIRecords(); len(got) != 0 {
		t.Errorf("no CI records stored, got %d", len(got))
	}
	if got := m.supervisorWatchPRDs(); len(got) != 0 {
		t.Errorf("no Automata stored, got %d", len(got))
	}
}

func renderToString(st SupervisorWatchStatus) string {
	var b bytes.Buffer
	RenderSupervisorWatch(&b, &st)
	return b.String()
}

func TestRenderSupervisorWatchQuiet(t *testing.T) {
	out := renderToString(SupervisorWatchStatus{Mode: SupervisorWatchModeShadow})
	for _, want := range []string{
		"Supervisor watch mode: shadow",
		"Last poll: never",
		"No supervisor watch findings.",
		"Comparison: none recorded yet (days of full agreement: 0)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("quiet render lacks %q:\n%s", want, out)
		}
	}
}

func TestRenderSupervisorWatchPopulated(t *testing.T) {
	polled := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	alertedAt := polled
	st := SupervisorWatchStatus{
		Mode:       SupervisorWatchModeOn,
		LastPollAt: polled,
		Findings: []*SupervisorWatchFinding{
			{Kind: SupervisorWatchTaskFailed, Subject: "p1", Text: "Automaton p1 task failed", LastSeenAt: polled},
			{Kind: SupervisorWatchCIRed, Subject: "42", WouldAlert: true},
			{Kind: SupervisorWatchCISTuck, Subject: "7", AlertedAt: &alertedAt, LastSeenAt: polled},
			{Kind: SupervisorWatchWorktreeSilent, Subject: "p9", Resolved: true, LastSeenAt: polled},
		},
		Comparison: SupervisorWatchComparisonView{
			Last:       &SupervisorWatchComparison{At: polled, Agreed: false},
			AgreedDays: 2,
			OnlyWatch:  []string{"task_failed/p1"},
			OnlyScript: []string{"ci_red/42"},
		},
	}
	out := renderToString(st)
	for _, want := range []string{
		"Supervisor watch mode: on",
		"Last poll: 2026-10-30 10:00:00",
		"Findings: 4",
		"task_failed", "open", "Automaton p1 task failed",
		"would alert", // the finding without text falls back to its subject
		"alerted",
		"resolved",
		"Comparison: differs at 2026-10-30 10:00:00 (days of full agreement: 2)",
		"only the watch raised: task_failed/p1",
		"only the script raised: ci_red/42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("populated render lacks %q:\n%s", want, out)
		}
	}
	// a finding that was never seen again shows a dash for its last-seen
	if !strings.Contains(out, "-") {
		t.Errorf("a finding without a last-seen instant must show a dash:\n%s", out)
	}

	agreed := renderToString(SupervisorWatchStatus{
		Mode: SupervisorWatchModeShadow,
		Comparison: SupervisorWatchComparisonView{
			Last: &SupervisorWatchComparison{At: polled, Agreed: true}, AgreedDays: 1,
		},
	})
	if !strings.Contains(agreed, "Comparison: agreed at 2026-10-30 10:00:00 (days of full agreement: 1)") {
		t.Errorf("agreed render:\n%s", agreed)
	}
	if strings.Contains(agreed, "only the") {
		t.Errorf("an agreeing comparison names no one-sided difference:\n%s", agreed)
	}
}

func TestSupervisorWatchFindingStateWording(t *testing.T) {
	at := time.Now()
	cases := []struct {
		f    SupervisorWatchFinding
		want string
	}{
		{SupervisorWatchFinding{Resolved: true, WouldAlert: true}, "resolved"},
		{SupervisorWatchFinding{AlertedAt: &at, WouldAlert: true}, "alerted"},
		{SupervisorWatchFinding{WouldAlert: true}, "would alert"},
		{SupervisorWatchFinding{}, "open"},
	}
	for _, c := range cases {
		f := c.f
		if got := SupervisorWatchFindingState(&f); got != c.want {
			t.Errorf("state of %+v = %q, want %q", c.f, got, c.want)
		}
	}
}

// TestSupervisorWatchStatusOneSidedListsNeverNull — a stored comparison
// that carries no one-sided lists at all (a history file written without
// them) still reads as two empty lists, never null.
func TestSupervisorWatchStatusOneSidedListsNeverNull(t *testing.T) {
	dir := t.TempDir()
	hist := `{"comparisons":[{"at":"2026-10-30T10:00:00Z","agreed":true}],"days":{}}`
	if err := os.WriteFile(filepath.Join(dir, SupervisorWatchComparisonFile), []byte(hist), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewSupervisorWatchStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := swNewManager(t)
	m.mu.Lock()
	m.supervisorWatch = st
	m.mu.Unlock()

	got := m.SupervisorWatchStatus()
	if got.Comparison.Last == nil {
		t.Fatal("the stored comparison was not read")
	}
	if got.Comparison.OnlyWatch == nil || got.Comparison.OnlyScript == nil {
		t.Errorf("one-sided lists must be empty, not null: %v / %v", got.Comparison.OnlyWatch, got.Comparison.OnlyScript)
	}
}
