package main

import (
	"bytes"
	"strings"
	"testing"
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
