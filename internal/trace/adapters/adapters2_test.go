package adapters

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

func parseFix(t *testing.T, a Adapter, file string) ([]trace.Event, *Context) {
	t.Helper()
	ctx := newCtx()
	body := fixture(t, file)
	evs, err := a.Parse(strings.NewReader(body), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Offset != int64(len(body)) {
		t.Errorf("%s offset %d want %d", file, ctx.Offset, len(body))
	}
	return evs, ctx
}

func TestOpenCodeParse(t *testing.T) {
	want := map[string]trace.Coverage{"opencode": trace.CoveragePartial, "opencode-prompt": trace.CoveragePartial, "opencode-acp": trace.CoverageFull}
	for name, cov := range want {
		a := NewOpenCode(name)
		if a.Name() != name || a.Coverage() != cov {
			t.Fatalf("%s meta: %s", name, a.Coverage())
		}
		evs, ctx := parseFix(t, a, "opencode.jsonl")
		if len(evs) != 5 {
			t.Fatalf("%s: %d events %+v", name, len(evs), evs)
		}
		if evs[0].Kind != trace.KindToolCall || evs[0].Name != "bash" || evs[0].ArgSummary != `{"command":"ls"}` {
			t.Errorf("call: %+v", evs[0])
		}
		if evs[1].Kind != trace.KindToolResult || evs[1].DurationMS != 1500 || evs[1].Outcome != "ok" {
			t.Errorf("result: %+v", evs[1])
		}
		if evs[3].Outcome != "error" || evs[3].DurationMS != 200 {
			t.Errorf("error result: %+v", evs[3])
		}
		if evs[4].Kind != trace.KindModelRequest || evs[4].TokensIn != 115 || evs[4].TokensOut != 25 || evs[4].Backend != name {
			t.Errorf("model: %+v", evs[4])
		}
		if len(ctx.Pending) != 0 {
			t.Errorf("pending %v", ctx.Pending)
		}
	}
}

func TestGooseParse(t *testing.T) {
	for _, name := range []string{"goose", "goose-prompt"} {
		a := NewGoose(name)
		if a.Name() != name || a.Coverage() != trace.CoverageFull {
			t.Fatalf("%s meta", name)
		}
		evs, ctx := parseFix(t, a, "goose.jsonl")
		if len(evs) != 5 {
			t.Fatalf("%d events %+v", len(evs), evs)
		}
		if evs[0].Name != "developer__shell" || evs[1].DurationMS != 3000 || evs[1].Name != "developer__shell" {
			t.Errorf("pair: %+v %+v", evs[0], evs[1])
		}
		if evs[3].Outcome != "error" {
			t.Errorf("error: %+v", evs[3])
		}
		if evs[4].Kind != trace.KindModelRequest || evs[4].TokensIn != 300 || evs[4].TokensOut != 40 {
			t.Errorf("model: %+v", evs[4])
		}
		if len(ctx.Pending) != 0 {
			t.Errorf("pending %v", ctx.Pending)
		}
	}
}

func TestAiderParse(t *testing.T) {
	a := NewAider()
	if a.Coverage() != trace.CoveragePartial || a.Name() != "aider" {
		t.Fatal("meta")
	}
	evs, _ := parseFix(t, a, "aider.txt")
	if len(evs) != 3 {
		t.Fatalf("%d events %+v", len(evs), evs)
	}
	byKind := map[trace.Kind][]trace.Event{}
	for _, e := range evs {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	mr := byKind[trace.KindModelRequest]
	if len(mr) != 1 || mr[0].TokensIn != 1200 || mr[0].TokensOut != 80 || mr[0].Name != "ollama_chat/qwen3" {
		t.Errorf("model: %+v", mr)
	}
	names := map[string]string{}
	for _, e := range byKind[trace.KindToolCall] {
		names[e.Name] = e.ArgSummary
		if !e.Timestamp.Equal(fixed) {
			t.Errorf("fixed clock: %v", e.Timestamp)
		}
	}
	if names["edit"] != "main.go" || names["run"] != "go test ./..." {
		t.Errorf("tool calls: %v", names)
	}
}

func TestGeminiParse(t *testing.T) {
	a := NewGemini()
	if a.Coverage() != trace.CoveragePartial || a.Name() != "gemini" {
		t.Fatal("meta")
	}
	evs, _ := parseFix(t, a, "gemini.jsonl")
	if len(evs) != 5 {
		t.Fatalf("%d events %+v", len(evs), evs)
	}
	if evs[0].ArgSummary != `{"path":"a.go"}` || evs[1].DurationMS != 120 || evs[1].Outcome != "ok" {
		t.Errorf("first: %+v %+v", evs[0], evs[1])
	}
	if evs[3].Outcome != "error" {
		t.Errorf("failure: %+v", evs[3])
	}
	if evs[4].TokensIn != 500 || evs[4].Name != "gemini-2.5-pro" ||
		!evs[4].Timestamp.Equal(time.Date(2026, 1, 2, 3, 4, 7, 0, time.UTC)) {
		t.Errorf("model: %+v", evs[4])
	}
}

func TestNewAdaptersFallBack(t *testing.T) {
	obs := []Observation{{State: "running"}}
	for _, a := range []Adapter{NewOpenCode("opencode"), NewGoose("goose"), NewAider(), NewGemini()} {
		evs, _ := ParseOrFallback(a, strings.NewReader("noise\n"), newCtx(), obs)
		if len(evs) != 1 || evs[0].ArgSummary != FallbackNote || evs[0].Backend != a.Name() {
			t.Errorf("%s: %+v", a.Name(), evs)
		}
		if !a.Coverage().Valid() {
			t.Errorf("%s coverage invalid", a.Name())
		}
	}
}

func TestNewAdaptersIncremental(t *testing.T) {
	full := fixture(t, "goose.jsonl")
	a := NewGoose("goose")
	whole, _ := a.Parse(strings.NewReader(full), newCtx())
	cut := len(full) - 20
	ctx := newCtx()
	first, _ := a.Parse(strings.NewReader(full[:cut]), ctx)
	second, _ := a.Parse(strings.NewReader(full[ctx.Offset:]), ctx)
	if len(first)+len(second) != len(whole) || ctx.Offset != int64(len(full)) {
		t.Errorf("incremental %d+%d vs %d, offset %d", len(first), len(second), len(whole), ctx.Offset)
	}
}
