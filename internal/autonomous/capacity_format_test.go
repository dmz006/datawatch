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

// TestCapacityText_BackoffRungAndReason asserts a back-off flag that the burn
// guard set prints its rung and its reason, a rung-only flag prints just the
// rung, and a flag without a rung prints its reason exactly as before —
// with the reset time shown in the offset the daemon sent it with.
func TestCapacityText_BackoffRungAndReason(t *testing.T) {
	offset := time.FixedZone("EDT", -4*3600)
	CapacityTextClock = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, offset) }
	t.Cleanup(func() { CapacityTextClock = time.Now })

	out, err := FormatCapacityText([]byte(`{
"pools":[],"leases":[],"waiting":[],
"backoff":[
{"backend":"claude-code","until":"2026-11-01T14:00:00-04:00","reason":"usage cap hit","rung":"hold_claude","reset_known":true},
{"backend":"pi","until":"2026-11-01T14:30:00-04:00","rung":"lower_inflight","reset_known":true},
{"backend":"ollama","until":"2026-11-01T15:00:00Z","reason":"rate limited","reset_known":true}],
"budgets":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// Rung + reason: the rung names the guard's hold, the reason follows
		// it, and the time keeps the daemon's -04:00 offset (no UTC rewrite).
		"claude-code: resets 2026-11-01T14:00:00-04:00 (burn guard rung hold_claude: usage cap hit)",
		// Rung with no reason: the rung is still printed.
		"pi: resets 2026-11-01T14:30:00-04:00 (burn guard rung lower_inflight)",
		// No rung: the old form, reason in plain parentheses.
		"ollama: resets 2026-11-01T15:00:00Z (rate limited)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The -04:00 offset must not be rewritten to UTC or a different zone.
	for _, gone := range []string{"18:00:00Z", "10:00:00-04:00"} {
		if strings.Contains(out, gone) {
			t.Fatalf("offset rewritten to %q:\n%s", gone, out)
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

// A body with tasks in verification prints a Verifying section: each task by
// its readable name (short id as the secondary reference), its state, and the
// wait reason for a task queued for a verifier slot. A body with no
// verifying tasks (older daemon) prints no section at all.
func TestCapacityText_VerifyingSection(t *testing.T) {
	CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { CapacityTextClock = time.Now })

	out, err := FormatCapacityText([]byte(`{
"pools":[],"leases":[],"waiting":[],
"tasks":{
"waiting":[],"running":[],
"verifying":[
{"task_id":"taaaaaaaaaaaaaa","prd_id":"pa","prd_title":"Ship billing","task_title":"Add exports",
 "readable":"Ship billing / Add exports (taaaaaaa)","short_id":"taaaaaaa",
 "node":"b","since":"2026-10-22T16:55:00Z","state":"verifying"},
{"task_id":"tbccccccccccccc","prd_id":"pb","prd_title":"Plan be","task_title":"Wire the retry path",
 "readable":"Plan be / Wire the retry path (tbcccccc)","short_id":"tbcccccc",
 "since":"2026-10-22T16:50:00Z","state":"waiting_for_verifier","wait_reason":"pool llm:ollama full"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Verifying (2):",
		// Readable name, then the state: the running verifier's line.
		"Ship billing / Add exports (taaaaaaa): verifying",
		// Queued line: state plus the wait reason.
		"Plan be / Wire the retry path (tbcccccc): waiting_for_verifier - pool llm:ollama full",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// A body with no verifying key (older daemon) prints no section and no
	// state wording at all.
	out, err = FormatCapacityText([]byte(`{"pools":[],"tasks":{"waiting":[],"running":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Verifying") {
		t.Fatalf("older body must not print a Verifying section:\n%s", out)
	}
}

// A body whose adoption hold has ended carries the node-engine reason in the
// ledger waiter and the queued task alike; the text prints it and nothing of
// the stale adoption wording.
func TestCapacityText_PrintsCurrentNodeEngineReason(t *testing.T) {
	CapacityTextClock = func() time.Time { return time.Date(2026, 11, 4, 12, 5, 0, 0, time.UTC) }
	t.Cleanup(func() { CapacityTextClock = time.Now })
	const node = "waiting for node alpha: 1/1 engines in use, held by owner"
	for name, body := range map[string]string{
		"tasks.waiting": `{"pools":[],"waiting":[{"holder":"t1","prd_id":"prd-1","reason":"` + node + `","since":"2026-11-04T12:00:00Z"}],
"tasks":{"waiting":[{"task_id":"t1","prd_id":"prd-1","reason":"` + node + `","readable_reason":"` + node + `","since":"2026-11-04T12:00:00Z"}],"running":[]}}`,
		"top-level waiting": `{"pools":[],"waiting":[{"holder":"t1","prd_id":"prd-1","reason":"` + node + `","since":"2026-11-04T12:00:00Z"}],"tasks":{"waiting":[],"running":[]}}`,
	} {
		out, err := FormatCapacityText([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(out, node) {
			t.Errorf("%s: missing node-engine reason in:\n%s", name, out)
		}
		if strings.Contains(out, "adopted") {
			t.Errorf("%s: stale adoption text in:\n%s", name, out)
		}
	}
}

func TestFormatCapacityText_ShowsOperatorOverageAndUnmapped(t *testing.T) {
	body := []byte(`{"pools":[],"leases":[{"holder":"start-1","class":"operator","overage":[{"pool":"llm:claude-code","limit":1,"over":1}]}],"unmapped":[{"backend":"claude-code","count":3}],"waiting":[],"tasks":{"waiting":[],"running":[],"verifying":[]}}`)
	out, err := FormatCapacityText(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Operator sessions over a limit (1)", "start-1 (operator): llm:claude-code is 1 over its limit of 1", "No LLM entry for backend claude-code (3 starts)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text missing %q:\n%s", want, out)
		}
	}
}

func TestFormatCapacityText_ShowsHostSessionCapOver(t *testing.T) {
	body := []byte(`{"pools":[],"leases":[],"session_cap":{"max":20,"active":22,"operator_slots":5,"over":2},"waiting":[],"tasks":{"waiting":[],"running":[],"verifying":[]}}`)
	out, err := FormatCapacityText(body)
	if err != nil || !strings.Contains(out, "Host session cap: 22 running against 20, 2 over (operator allowance 5)") {
		t.Fatalf("text %q err %v", out, err)
	}
	body = []byte(`{"pools":[],"leases":[],"session_cap":{"max":20,"active":3,"operator_slots":5},"waiting":[],"tasks":{"waiting":[],"running":[],"verifying":[]}}`)
	out, _ = FormatCapacityText(body)
	if strings.Contains(out, "Host session cap") {
		t.Fatalf("within the cap nothing is shown: %q", out)
	}
}

func TestFormatCapacityText_ShowsClusterHostsAndSharedAccount(t *testing.T) {
	body := []byte(`{"pools":[],"leases":[],"waiting":[],"tasks":{"waiting":[],"running":[],"verifying":[]},
"federation":{"hosts":[
 {"name":"alpha","self":true,"reachable":true,"llm_limit":1,"llm_held":1,"node_limit":2,"node_held":0,"shared_account":"self"},
 {"name":"beta","reachable":true,"llm_limit":5,"llm_held":3,"shared_account":"confirmed","counted_held":1},
 {"name":"gamma","reachable":false,"shared_account":"pending","note":"possible shared account, awaiting your confirmation"}],
 "cluster":{"llm_limit":6,"llm_held":4,"node_limit":2,"node_held":0},
 "account":{"limit":2,"local_held":1,"peer_held":1,"total":2}}}`)
	out, err := FormatCapacityText(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Cluster, 3 hosts, each at its own limits: LLM 4/6 in use", "Claude account 2/2 (this host 1, peers 1)", "alpha (this host): LLM 1/1", "beta: LLM 3/5, nodes 0/no limit; shares the Claude account (confirmed), 1 counted here", "gamma (not reachable, counted as 0)", "possible shared account, awaiting your confirmation"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
