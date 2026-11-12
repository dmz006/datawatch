package autonomous

import (
	"strings"
	"testing"
	"time"
)

// testTitlesSnap is the shared fixture: PRD aaaaaaaa with task bbbbbbbb.
func testTitlesSnap() TitleSnapshot {
	return TitleSnapshot{
		PRDs: map[string]string{"aaaaaaaa": "Automate the release pipeline"},
		Tasks: map[string]TaskRef{
			"bbbbbbbb": {PRDID: "aaaaaaaa", Title: "Wire up the retry path"},
		},
	}
}

func TestHolderNameTask(t *testing.T) {
	r := NewTitleResolver(TitleResolverOptions{Lookup: testTitlesSnap})
	h := r.Resolve("bbbbbbbb")
	if h.ID != "bbbbbbbb" {
		t.Errorf("ID = %q, want raw id unchanged", h.ID)
	}
	if h.Kind != HolderKindTask {
		t.Errorf("Kind = %q, want %q", h.Kind, HolderKindTask)
	}
	if h.TaskTitle != "Wire up the retry path" {
		t.Errorf("TaskTitle = %q", h.TaskTitle)
	}
	if h.PRDID != "aaaaaaaa" || h.PRDTitle != "Automate the release pipeline" {
		t.Errorf("PRD = %q / %q", h.PRDID, h.PRDTitle)
	}
	if h.ShortID != "bbbbbbbb" {
		t.Errorf("ShortID = %q", h.ShortID)
	}
	want := "Automate the release pipeline / Wire up the retry path (bbbbbbbb)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
	if h.Full != want {
		t.Errorf("Full = %q, want %q", h.Full, want)
	}
}

func TestHolderNameVerifier(t *testing.T) {
	r := NewTitleResolver(TitleResolverOptions{Lookup: testTitlesSnap})
	h := r.Resolve(VerifyHolder("bbbbbbbb"))
	if h.ID != "verify:bbbbbbbb" {
		t.Errorf("ID = %q, want the raw id", h.ID)
	}
	if h.Kind != HolderKindVerifier {
		t.Errorf("Kind = %q, want %q", h.Kind, HolderKindVerifier)
	}
	if h.ShortID != "bbbbbbbb" || h.PRDID != "aaaaaaaa" {
		t.Errorf("ShortID/PRDID = %q / %q", h.ShortID, h.PRDID)
	}
	want := "Verifying: Wire up the retry path (bbbbbbbb)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
}

func TestHolderNameDecompose(t *testing.T) {
	r := NewTitleResolver(TitleResolverOptions{Lookup: testTitlesSnap})
	for _, holder := range []string{"plan:aaaaaaaa", "decompose:aaaaaaaa"} {
		h := r.Resolve(holder)
		if h.Kind != HolderKindDecompose {
			t.Errorf("%s: Kind = %q, want %q", holder, h.Kind, HolderKindDecompose)
		}
		if h.PRDID != "aaaaaaaa" || h.ShortID != "aaaaaaaa" {
			t.Errorf("%s: PRDID/ShortID = %q / %q", holder, h.PRDID, h.ShortID)
		}
		want := "Planning: Automate the release pipeline (aaaaaaaa)"
		if h.Readable != want {
			t.Errorf("%s: Readable = %q, want %q", holder, h.Readable, want)
		}
	}
}

func TestHolderNameMissingTitle(t *testing.T) {
	snap := testTitlesSnap()
	snap.Tasks["cccccccc"] = TaskRef{PRDID: "99999999", Title: "An orphan task"} // PRD gone
	r := NewTitleResolver(TitleResolverOptions{Lookup: func() TitleSnapshot { return snap }})

	// Unknown task id: falls back to a session showing the short id.
	h := r.Resolve("ffffffff")
	if h.Kind != HolderKindSession {
		t.Errorf("Kind = %q, want session", h.Kind)
	}
	if h.Readable != "ffffffff" || h.ShortID != "ffffffff" {
		t.Errorf("Readable/ShortID = %q / %q", h.Readable, h.ShortID)
	}

	// Task whose PRD is deleted: the PRD-title slot is "(untitled <short id>)".
	h = r.Resolve("cccccccc")
	want := "(untitled 99999999) / An orphan task (cccccccc)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}

	// Verifier of an unknown task, and planner of a deleted PRD.
	if got := r.Resolve("verify:ffffffff").Readable; got != "Verifying: (untitled ffffffff)" {
		t.Errorf("verifier missing = %q", got)
	}
	if got := r.Resolve("plan:eeeeeeee").Readable; got != "Planning: (untitled eeeeeeee)" {
		t.Errorf("decompose missing = %q", got)
	}

	// Session with a known name shows the name.
	rn := NewTitleResolver(TitleResolverOptions{
		Lookup:      testTitlesSnap,
		SessionName: func(id string) string { return "release-bot" },
	})
	if got := rn.Resolve("sess-id-123456").Readable; got != "release-bot" {
		t.Errorf("named session = %q, want release-bot", got)
	}
}

func TestHolderNameLongTitle(t *testing.T) {
	longPRD := strings.Repeat("a", 120)
	longTask := strings.Repeat("b", 120)
	snap := TitleSnapshot{
		PRDs:  map[string]string{"aaaaaaaa": longPRD},
		Tasks: map[string]TaskRef{"bbbbbbbb": {PRDID: "aaaaaaaa", Title: longTask}},
	}
	r := NewTitleResolver(TitleResolverOptions{Lookup: func() TitleSnapshot { return snap }})
	h := r.Resolve("bbbbbbbb")

	truncPRD := shortTitle(longPRD)
	truncTask := shortTitle(longTask)
	want := truncPRD + " / " + truncTask + " (bbbbbbbb)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}

	// Middle ellipsis: each per-title truncation is exactly the limit long,
	// keeps the start and end, and contains exactly one ellipsis.
	for _, tr := range []string{truncPRD, truncTask} {
		if got := len([]rune(tr)); got != titleLimit {
			t.Errorf("truncated title %d runes, want %d", got, titleLimit)
		}
		if !strings.Contains(tr, "…") || strings.Count(tr, "…") != 1 {
			t.Errorf("truncated title missing a single middle ellipsis: %q", tr)
		}
	}
	if !strings.HasPrefix(truncPRD, "aaa") || !strings.HasSuffix(truncPRD, "aaa") {
		t.Errorf("truncated PRD title should keep both ends: %q", truncPRD)
	}

	// Full keeps the whole text.
	if !strings.Contains(h.Full, longPRD) || !strings.Contains(h.Full, longTask) {
		t.Errorf("Full must keep the full titles: %q", h.Full)
	}
	wantFull := longPRD + " / " + longTask + " (bbbbbbbb)"
	if h.Full != wantFull {
		t.Errorf("Full = %q, want %q", h.Full, wantFull)
	}
}

// titleTestClock is a hand-advanced clock for the TTL tests.
type titleTestClock struct{ now time.Time }

func (f *titleTestClock) Now() time.Time                       { return f.now }
func (f *titleTestClock) After(time.Duration) <-chan time.Time { return nil }

// countingLookup records how many snapshots the resolver requested.
type countingLookup struct {
	n    int
	snap TitleSnapshot
}

func (c *countingLookup) fn() TitleLookupFn {
	return func() TitleSnapshot { c.n++; return c.snap }
}

func TestJudgeHolderNamePRD(t *testing.T) {
	r := NewTitleResolver(TitleResolverOptions{Lookup: testTitlesSnap})
	h := r.Resolve(JudgeHolder("aaaaaaaa"))
	if h.ID != "judge:aaaaaaaa" {
		t.Errorf("ID = %q, want the raw id", h.ID)
	}
	if h.Kind != HolderKindJudge {
		t.Errorf("Kind = %q, want %q", h.Kind, HolderKindJudge)
	}
	if h.PRDID != "aaaaaaaa" || h.PRDTitle != "Automate the release pipeline" {
		t.Errorf("PRD = %q / %q", h.PRDID, h.PRDTitle)
	}
	if h.ShortID != "aaaaaaaa" {
		t.Errorf("ShortID = %q", h.ShortID)
	}
	want := "Judging rules: Automate the release pipeline (aaaaaaaa)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
	if h.Full != want {
		t.Errorf("Full = %q, want %q", h.Full, want)
	}
}

func TestJudgeHolderNameTask(t *testing.T) {
	r := NewTitleResolver(TitleResolverOptions{Lookup: testTitlesSnap})
	h := r.Resolve(JudgeHolder("bbbbbbbb"))
	if h.Kind != HolderKindJudge {
		t.Errorf("Kind = %q, want %q", h.Kind, HolderKindJudge)
	}
	if h.TaskTitle != "Wire up the retry path" || h.PRDID != "aaaaaaaa" {
		t.Errorf("task = %q / PRD = %q", h.TaskTitle, h.PRDID)
	}
	want := "Judging: Wire up the retry path (bbbbbbbb)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
	if h.Full != want {
		t.Errorf("Full = %q, want %q", h.Full, want)
	}
}

func TestJudgeHolderNameUnknown(t *testing.T) {
	r := NewTitleResolver(TitleResolverOptions{Lookup: testTitlesSnap})
	h := r.Resolve(JudgeHolder("ffffffff"))
	if h.Kind != HolderKindJudge {
		t.Errorf("Kind = %q, want %q", h.Kind, HolderKindJudge)
	}
	if h.ShortID != "ffffffff" {
		t.Errorf("ShortID = %q", h.ShortID)
	}
	want := "Judging: (untitled ffffffff)"
	if h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
	if h.Full != want {
		t.Errorf("Full = %q, want %q", h.Full, want)
	}
}

func TestJudgeHolderNameLongTitle(t *testing.T) {
	longPRD := strings.Repeat("p", 120)
	longTask := strings.Repeat("q", 120)
	snap := TitleSnapshot{
		PRDs:  map[string]string{"aaaaaaaa": longPRD},
		Tasks: map[string]TaskRef{"bbbbbbbb": {PRDID: "aaaaaaaa", Title: longTask}},
	}
	r := NewTitleResolver(TitleResolverOptions{Lookup: func() TitleSnapshot { return snap }})

	// The PRD form truncates under the per-title limit in Readable only; Full
	// keeps the whole title.
	h := r.Resolve(JudgeHolder("aaaaaaaa"))
	if want := "Judging rules: " + shortTitle(longPRD) + " (aaaaaaaa)"; h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
	if want := "Judging rules: " + longPRD + " (aaaaaaaa)"; h.Full != want {
		t.Errorf("Full = %q, must keep the full PRD title", h.Full)
	}
	if tr := shortTitle(longPRD); len([]rune(tr)) != titleLimit || strings.Count(tr, "…") != 1 {
		t.Errorf("truncated title %q: want %d runes with one middle ellipsis", tr, titleLimit)
	}

	// The task form does the same for the task title.
	h = r.Resolve(JudgeHolder("bbbbbbbb"))
	if want := "Judging: " + shortTitle(longTask) + " (bbbbbbbb)"; h.Readable != want {
		t.Errorf("Readable = %q, want %q", h.Readable, want)
	}
	if want := "Judging: " + longTask + " (bbbbbbbb)"; h.Full != want {
		t.Errorf("Full must keep the full task title: %q", h.Full)
	}
}

func TestTitleResolverCacheWithinTTL(t *testing.T) {
	tclk := &titleTestClock{now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	lookup := &countingLookup{snap: testTitlesSnap()}
	r := NewTitleResolver(TitleResolverOptions{Lookup: lookup.fn(), Clock: tclk, TTL: 5 * time.Second})

	if h := r.Resolve("bbbbbbbb"); h.Kind != HolderKindTask {
		t.Fatalf("first resolve: Kind = %q", h.Kind)
	}
	if lookup.n != 1 {
		t.Fatalf("lookups after first resolve = %d, want 1", lookup.n)
	}

	// Inside the TTL: neither a repeated holder nor a different holder
	// consults the lookup again — the snapshot covers both.
	tclk.now = tclk.now.Add(1 * time.Second)
	if h := r.Resolve("bbbbbbbb"); h.Readable != "Automate the release pipeline / Wire up the retry path (bbbbbbbb)" {
		t.Errorf("repeat resolve = %q", h.Readable)
	}
	if h := r.Resolve("plan:aaaaaaaa"); h.Kind != HolderKindDecompose {
		t.Errorf("second holder = %q", h.Kind)
	}
	if lookup.n != 1 {
		t.Errorf("lookups inside TTL = %d, want 1", lookup.n)
	}

	// After the TTL expires the store is consulted again.
	tclk.now = tclk.now.Add(10 * time.Second)
	_ = r.Resolve("bbbbbbbb")
	if lookup.n != 2 {
		t.Errorf("lookups after TTL expiry = %d, want 2", lookup.n)
	}
}
