package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const readableCLIFixture = `{"pools":[{"name":"node:a","limit":1,"held":1,"holders":["t7abc1234def5678"],
"holder_names":[{"id":"t7abc1234def5678","kind":"task","readable":"Ship billing / Add exports","short_id":"t7abc123"}]},
{"name":"host","limit":9,"held":2,"holders":["t1","t2"]}],
"leases":[],"waiting":[],
"tasks":{"waiting":[{"task_id":"t9fedcba87654","prd_id":"p9","readable":"Ship billing (untitled 9fedcba8)","short_id":"9fedcba8","readable_reason":"waiting for node:a, held by Ship billing / Add exports (t7abc123)","reason":"waiting for node:a, held by t7abc1234def5678","since":"2026-10-22T16:00:00Z"}],
"running":[]}}`

// The CLI (and, via the shared FormatCapacityText, the comm reply) print the
// readable holder forms with the short id in parentheses, and fall back to the
// raw ids for entries without the new fields — same body, both surfaces.
func TestCapacityText_CLIReadableWithFallback(t *testing.T) {
	var cli bytes.Buffer
	if err := renderCapacity(&cli, []byte(readableCLIFixture), false); err != nil {
		t.Fatal(err)
	}
	out := cli.String()
	for _, want := range []string{
		"node:a: 1/1 held by Ship billing / Add exports (t7abc123)",
		"host: 2/9 held by t1,t2",
		"Ship billing (untitled 9fedcba8): waiting for node:a, held by Ship billing / Add exports (t7abc123)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "t7abc1234def5678") {
		t.Fatalf("raw full holder id should be replaced:\n%s", out)
	}
}

func TestJudgeCapacityTextCLI(t *testing.T) {
	autonomous.CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autonomous.CapacityTextClock = time.Now })

	// A running judge holds the node pool; a queued judge waits for it. The
	// CLI must print the readable judge forms in both places and never the
	// raw "judge:" id in a holder position.
	body := `{
"pools":[
{"name":"node:a","limit":1,"held":1,"holders":["judge:aaaaaaaa"],
 "holder_names":[{"id":"judge:aaaaaaaa","kind":"judge","readable":"Judging rules: Automate the release pipeline (aaaaaaaa)","short_id":"aaaaaaaa"}]},
{"name":"host","limit":2,"held":1,"holders":["t1"]}],
"leases":[],
"waiting":[{"holder":"judge:bbbbbbbb","prd_id":"aaaaaaaa",
 "holder_name":{"id":"judge:bbbbbbbb","kind":"judge","readable":"Judging: Wire up the retry path (bbbbbbbb)","short_id":"bbbbbbbb"},
 "reason":"waiting for node a: 1/1 engines in use, held by judge:aaaaaaaa","since":"2026-10-22T16:00:00Z"}],
"tasks":{"waiting":[],
 "running":[{"task_id":"t1","prd_id":"p7","readable":"Ship billing / Add exports (t1aaaaaa)","short_id":"t1aaaaaa",
 "pools":["host"],"session_id":"s-1","since":"2026-10-22T16:55:00Z"}]}}`

	var buf bytes.Buffer
	if err := renderCapacity(&buf, []byte(body), false); err != nil {
		t.Fatalf("renderCapacity failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"node:a: 1/1 held by Judging rules: Automate the release pipeline (aaaaaaaa)",
		"Judging: Wire up the retry path (bbbbbbbb) (PRD aaaaaaaa) 1h0m0s: waiting for node a: 1/1 engines in use, held by judge:aaaaaaaa",
		"Ship billing / Add exports (t1aaaaaa): holds host, session s-1 - running 5m0s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The queued judge's raw id never surfaces in a holder position.
	if strings.Contains(out, "judge:bbbbbbbb") {
		t.Fatalf("raw queued judge id leaked into the CLI text:\n%s", out)
	}
}
