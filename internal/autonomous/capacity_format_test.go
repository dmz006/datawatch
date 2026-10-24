package autonomous

import (
	"strings"
	"testing"
	"time"
)

// readableCapacityFixture is a fresh-daemon /api/capacity body: the node pool
// carries holder_names, and the task views carry readable / short_id /
// readable_reason. The host pool and the queued line's raw reason field stay
// raw: the readable fields only override them where present.
const readableCapacityFixture = `{
"pools":[
{"name":"host","limit":9,"external":1,"held":2,"holders":["t1","t2"]},
{"name":"node:a","limit":1,"held":1,"holders":["t7abc1234def5678"],
 "holder_names":[{"id":"t7abc1234def5678","kind":"task","readable":"Ship billing / Add exports","short_id":"t7abc123"}]}],
"leases":[],"waiting":[],
"tasks":{
"waiting":[{"task_id":"t9fedcba87654","prd_id":"p9","readable":"Ship billing (untitled 9fedcba8)","short_id":"9fedcba8",
 "readable_reason":"waiting for node:a, held by Ship billing / Add exports (t7abc123)",
 "reason":"waiting for node:a, held by t7abc1234def5678","since":"2026-10-22T16:00:00Z"}],
"running":[{"task_id":"t7abc1234def5678","prd_id":"p7","readable":"Ship billing / Add exports (t7abc123)","short_id":"t7abc123",
 "pools":["host","llm:claude","node:a"],"node":"a","session_id":"s-1","since":"2026-10-22T16:55:00Z"}]}}`

func TestCapacityText_ReadableHoldersTasksAndReason(t *testing.T) {
	CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { CapacityTextClock = time.Now })

	out, err := FormatCapacityText([]byte(readableCapacityFixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// New fields: readable holder form, short id in parentheses as the
		// secondary reference.
		"node:a: 1/1 held by Ship billing / Add exports (t7abc123)",
		"Ship billing (untitled 9fedcba8): waiting for node:a, held by Ship billing / Add exports (t7abc123) - waiting 1h0m0s",
		"Ship billing / Add exports (t7abc123): holds host, llm:claude, node:a, node a, session s-1 - running 5m0s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The host pool has no holder_names (old-style entry): raw ids, unchanged.
	if !strings.Contains(out, "host: 3/9 (1 external) held by t1,t2") {
		t.Fatalf("missing raw fallback line in:\n%s", out)
	}
	// The readable form replaces the raw full holder id everywhere the new
	// fields exist.
	if strings.Contains(out, "t7abc1234def5678") {
		t.Fatalf("raw full holder id should be replaced by the readable form:\n%s", out)
	}
}

func TestCapacityText_OldBodyFallsBackToRawIDs(t *testing.T) {
	out, err := FormatCapacityText([]byte(`{
"pools":[{"name":"host","limit":2,"held":1,"holders":["t1"]}],
"tasks":{"waiting":[{"task_id":"t9","prd_id":"p9","task_title":"Write parser","prd_title":"Plan nine","reason":"waiting for node:a","since":"2026-10-22T16:00:00Z"}],
"running":[{"task_id":"t1","prd_id":"p7","task_title":"Build index","pools":["host"],"session_id":"s-1","since":"2026-10-22T16:55:00Z"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"held by t1",
		"Write parser (PRD Plan nine): waiting for node:a",
		"Build index (PRD p7): holds host, session s-1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing raw fallback %q in:\n%s", want, out)
		}
	}
	// A body with only ledger waiters (no tasks array) prints them raw, as before.
	out, err = FormatCapacityText([]byte(`{
"pools":[],
"waiting":[{"holder":"t3","prd_id":"p9","pools":["node:a"],"since":"2026-10-22T16:00:00Z","reason":"pool node:a full"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"t3 (PRD p9)", "pool node:a full", "Waiting (1):"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing raw fallback %q in:\n%s", want, out)
		}
	}
}

func TestHolderNameDisplay(t *testing.T) {
	cases := []struct {
		v    HolderNameView
		want string
	}{
		{HolderNameView{ID: "s-1", Readable: "ship-billing", ShortID: "s1abc2de"}, "ship-billing (s1abc2de)"},
		// Short id already in the sentence: not appended again.
		{HolderNameView{ID: "t7", Readable: "Ship billing / Add exports (t7abc123)", ShortID: "t7abc123"}, "Ship billing / Add exports (t7abc123)"},
		// Unknown session: readable IS the short id; no duplication.
		{HolderNameView{ID: "s-9", Readable: "f00dbabe", ShortID: "f00dbabe"}, "f00dbabe"},
		// Resolver produced no readable form (older daemon fields): raw id.
		{HolderNameView{ID: "t-34567890"}, "t-34567890"},
	}
	for _, c := range cases {
		if got := holderNameDisplay(c.v); got != c.want {
			t.Errorf("holderNameDisplay(%+v) = %q, want %q", c.v, got, c.want)
		}
	}
	// The public wrapper used by the in-process text path agrees.
	n := HolderName{ID: "s-1", Readable: "ship-billing", ShortID: "s1abc2de"}
	if got := DisplayHolderName(n); got != "ship-billing (s1abc2de)" {
		t.Errorf("DisplayHolderName = %q", got)
	}
}

// Resolve-side guarantees behind the "(untitled <short id>)" form and the
// 60-rune middle truncation of long titles.
func TestResolveHolder_UntitledAndTruncation(t *testing.T) {
	untitled := TitleSnapshot{
		PRDs:  map[string]string{"7890abcdef": "Ship billing"},
		Tasks: map[string]TaskRef{"9fedcba87654321": {PRDID: "7890abcdef", Title: ""}},
	}
	r := NewTitleResolver(TitleResolverOptions{Lookup: func() TitleSnapshot { return untitled }})

	// A known task with an empty title: "(untitled <short id>)".
	n := r.Resolve("9fedcba87654321")
	if !strings.Contains(n.Readable, "(untitled 9fedcba8)") {
		t.Fatalf("untitled form wrong: %q", n.Readable)
	}
	if n.ShortID != "9fedcba8" {
		t.Fatalf("ShortID = %q", n.ShortID)
	}
	// An unknown task's verifier holds under "verify:<id>": same untitled form.
	if got := r.Resolve("verify:ffffffffffffffff").Readable; got != "Verifying: (untitled ffffffff)" {
		t.Fatalf("verify untitled = %q", got)
	}

	// Both titles longer than 60 runes: the readable keeps them middle-truncated
	// to exactly the limit, one middle ellipsis, starts and ends kept.
	longPRD := strings.Repeat("p", 70)
	longTask := strings.Repeat("t", 70)
	snap := TitleSnapshot{
		PRDs:  map[string]string{"7890abcdef": longPRD},
		Tasks: map[string]TaskRef{"abcdefab123456": {PRDID: "7890abcdef", Title: longTask}},
	}
	r2 := NewTitleResolver(TitleResolverOptions{Lookup: func() TitleSnapshot { return snap }})
	m := r2.Resolve("abcdefab123456")
	want := shortTitle(longPRD) + " / " + shortTitle(longTask) + " (abcdefab)"
	if m.Readable != want {
		t.Fatalf("Readable = %q, want %q", m.Readable, want)
	}
	for _, got := range []string{shortTitle(longPRD), shortTitle(longTask)} {
		if len([]rune(got)) != titleLimit {
			t.Fatalf("truncated title = %d runes, want %d: %q", len([]rune(got)), titleLimit, got)
		}
		if strings.Count(got, "…") != 1 {
			t.Fatalf("want exactly one middle ellipsis: %q", got)
		}
	}
	if display := DisplayHolderName(m); !strings.Contains(display, "(abcdefab)") {
		t.Fatalf("display missing short id: %q", display)
	}
}

func TestDisplayHolderName(t *testing.T) {
	cases := []struct {
		n    HolderName
		want string
	}{
		{HolderName{ID: "s-1", Readable: "ship-billing", ShortID: "s1abc2de"}, "ship-billing (s1abc2de)"},
		{HolderName{ID: "t7", Readable: "Ship / Add exports (t7abc123)", ShortID: "t7abc123"}, "Ship / Add exports (t7abc123)"},
		{HolderName{ID: "t-34567890"}, "t-34567890"},
	}
	for _, c := range cases {
		if got := DisplayHolderName(c.n); got != c.want {
			t.Errorf("DisplayHolderName(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
}

func judgeFormatFixtureBody() string {
	return `{
"pools":[
{"name":"node:a","limit":1,"held":1,"holders":["judge:aaaaaaaa"],
 "holder_names":[{"id":"judge:aaaaaaaa","kind":"judge","readable":"Judging rules: Automate the release pipeline (aaaaaaaa)","short_id":"aaaaaaaa"}]},
{"name":"host","limit":2,"held":1,"holders":["t1"]}],
"leases":[],
"waiting":[{"holder":"judge:bbbbbbbb","prd_id":"aaaaaaaa","pools":["node:a"],
 "holder_name":{"id":"judge:bbbbbbbb","kind":"judge","readable":"Judging: Wire up the retry path (bbbbbbbb)","short_id":"bbbbbbbb"},
 "reason":"waiting for node a: 1/1 engines in use, held by judge:aaaaaaaa","since":"2026-10-22T16:00:00Z"}],
"tasks":{"waiting":[],"running":[]}}`
}

// TestJudgeCapacityText asserts the CLI text renderer prints a RUNNING judge
// on its pool line (holder_names form) and a QUEUED judge on its waiting line
// (holder_name form), both with readable names: no raw "judge:" holder id in
// either holder position, and the queued judge's wait reason still names the
// pool and the running holder.
func TestJudgeCapacityText(t *testing.T) {
	CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { CapacityTextClock = time.Now })

	out, err := FormatCapacityText([]byte(judgeFormatFixtureBody()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// Running judge: readable pool-hold line, short id already in the sentence.
		"node:a: 1/1 held by Judging rules: Automate the release pipeline (aaaaaaaa)",
		// Queued judge: holder column readable, reason names the pool + holder.
		"Judging: Wire up the retry path (bbbbbbbb) (PRD aaaaaaaa) 1h0m0s: waiting for node a: 1/1 engines in use, held by judge:aaaaaaaa",
		// Unresolved pools keep their raw form.
		"host: 1/2 held by t1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The queued judge's raw id appears nowhere in the holder position — only
	// the readable form.
	if strings.Contains(out, "judge:bbbbbbbb") {
		t.Fatalf("raw queued judge id leaked into the text:\n%s", out)
	}
}
