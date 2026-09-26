package main

import (
	"bytes"
	"strings"
	"testing"
)

const capacityFixture = `{"pools":[{"name":"host","limit":9,"external":1,"held":2,"holders":["t1","t2"]},{"name":"node:a","limit":1,"held":1,"holders":["t1"]}],
"leases":[],"waiting":[{"holder":"t3","prd_id":"p9","pools":["node:a"],"since":"2020-01-01T00:00:00Z","reason":"pool node:a full (1/1, held by t1)"}]}`

func TestRenderCapacity_Text(t *testing.T) {
	var b bytes.Buffer
	if err := renderCapacity(&b, []byte(capacityFixture), false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"host: 3/9 (1 external) held by t1,t2", "node:a: 1/1", "Waiting (1)", "t3 (PRD p9)", "pool node:a full"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderCapacity_JSON(t *testing.T) {
	var b bytes.Buffer
	if err := renderCapacity(&b, []byte(capacityFixture), true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\"pools\"") {
		t.Fatalf("json output: %s", b.String())
	}
}

func TestCapacityCmd_Registered(t *testing.T) {
	c := newCapacityCmd()
	if c.Use != "capacity" || c.Flags().Lookup("json") == nil {
		t.Fatal("capacity command misconfigured")
	}
}
