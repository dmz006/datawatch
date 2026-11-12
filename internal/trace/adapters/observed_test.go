package adapters

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

func TestHTTPObservedParse(t *testing.T) {
	for _, a := range []*HTTPObserved{NewOllama(), NewOpenWebUI()} {
		if a.Coverage() != trace.CoverageEventsOnly {
			t.Fatalf("%s coverage %s", a.Name(), a.Coverage())
		}
		evs, _ := parseFix(t, a, "ollama.jsonl")
		if len(evs) != 3 {
			t.Fatalf("%s: %d events", a.Name(), len(evs))
		}
		for _, e := range evs {
			if e.Kind != trace.KindModelRequest || e.Backend != a.Name() {
				t.Errorf("event %+v", e)
			}
		}
		if e := evs[0]; e.Name != "qwen3:8b" || e.TokensIn != 120 || e.TokensOut != 30 || e.DurationMS != 2500 || e.Outcome != "ok" {
			t.Errorf("first: %+v", e)
		}
		if evs[1].Outcome != "error" || !strings.Contains(evs[1].ArgSummary, "HTTP 500") {
			t.Errorf("error: %+v", evs[1])
		}
		if !evs[2].Timestamp.Equal(fixed) {
			t.Errorf("fixed clock not used: %v", evs[2].Timestamp)
		}
	}
}

func TestShellParse(t *testing.T) {
	a := NewShell()
	if a.Name() != "shell" || a.Coverage() != trace.CoverageEventsOnly {
		t.Fatal("meta")
	}
	evs, _ := parseFix(t, a, "shell.jsonl")
	if len(evs) != 5 {
		t.Fatalf("%d events %+v", len(evs), evs)
	}
	if evs[0].Name != "command" || evs[0].ArgSummary != "make test" || evs[0].Kind != trace.KindStateChange {
		t.Errorf("cmd: %+v", evs[0])
	}
	if evs[1].Name != "command_exit" || evs[1].Outcome != "ok" || evs[1].ArgSummary != "exit 0" {
		t.Errorf("exit: %+v", evs[1])
	}
	if want := time.Date(2026, 2, 3, 4, 0, 4, 0, time.UTC); !evs[1].Timestamp.Equal(want) {
		t.Errorf("exit ts %v", evs[1].Timestamp)
	}
	if evs[3].Outcome != "error" || evs[3].ArgSummary != "exit 1" {
		t.Errorf("fail: %+v", evs[3])
	}
	if evs[4].Name != "command" {
		t.Errorf("running command should have no exit event: %+v", evs[4])
	}
}

func TestObservedRegistered(t *testing.T) {
	r := DefaultRegistry()
	for _, n := range []string{"ollama", "openwebui", "shell"} {
		if _, ok := r.Get(n); !ok {
			t.Errorf("%s not registered", n)
		}
	}
}
