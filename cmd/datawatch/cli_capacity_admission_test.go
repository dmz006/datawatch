package main

import (
	"bytes"
	"strings"
	"testing"
)

const admissionQueuedFixture = `{"kind":"alternate_available","queue_position":1,"reason":"pool llm:x full (1/1)","alternates":[{"pool":"llm:y","model":"m"}]}`

func TestRenderAdmission_TextAndJSON(t *testing.T) {
	var b bytes.Buffer
	if err := renderAdmission(&b, []byte(admissionQueuedFixture), false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"would be queued at position 1", "pool llm:x full", "alternate available: llm:y (m)"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, b.String())
		}
	}
	b.Reset()
	if err := renderAdmission(&b, []byte(`{"kind":"admit","queue_position":0}`), true); err != nil || !strings.Contains(b.String(), `"kind": "admit"`) {
		t.Fatalf("json output %q err %v", b.String(), err)
	}
	b.Reset()
	if err := renderAdmission(&b, []byte(`{"kind":"admit","queue_position":0}`), false); err != nil || strings.TrimSpace(b.String()) != "would be admitted now" {
		t.Fatalf("admit text %q err %v", b.String(), err)
	}
}

func TestCapacityAdmissionCmd_Registered(t *testing.T) {
	c, _, err := newCapacityCmd().Find([]string{"admission"})
	if err != nil || c.Name() != "admission" {
		t.Fatalf("admission subcommand missing: %v", err)
	}
	for _, f := range []string{"backend", "node", "model", "role", "priority", "json"} {
		if c.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s", f)
		}
	}
}

func TestRenderCapacity_RatePoolShowsRateSegment(t *testing.T) {
	body := `{"pools":[{"name":"llm:x","limit":2,"held":0,"rate_requests":10,"requests_in_window":4,"rate_window_seconds":3600}],"leases":[],"waiting":[]}`
	var b bytes.Buffer
	if err := renderCapacity(&b, []byte(body), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "req 4/10 per 1h0m0s") {
		t.Fatalf("rate segment missing:\n%s", b.String())
	}
}
