package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

var homeDir = "/ho" + "me"

var fixed = time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

func newCtx() *Context {
	return &Context{SessionID: "s1", TaskID: "t1", Now: func() time.Time { return fixed }}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClaudeCodeParse(t *testing.T) {
	ctx := newCtx()
	evs, err := NewClaudeCode().Parse(strings.NewReader(fixture(t, "claude-code.jsonl")), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 6 {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	call, mr, res := evs[0], evs[1], evs[2]
	if call.Kind != trace.KindToolCall || call.Name != "Bash" || call.ArgSummary != `{"command":"go test ./..."}` {
		t.Errorf("call: %+v", call)
	}
	if mr.Kind != trace.KindModelRequest || mr.TokensIn != 900 || mr.TokensOut != 20 || mr.Name != "claude-sonnet-5-5" {
		t.Errorf("model request: %+v", mr)
	}
	if res.Kind != trace.KindToolResult || res.Name != "Bash" || res.Outcome != "ok" || res.DurationMS != 2500 {
		t.Errorf("result: %+v", res)
	}
	if evs[5].Outcome != "error" || evs[5].Name != "Read" || evs[5].DurationMS != 1000 {
		t.Errorf("error result: %+v", evs[5])
	}
	if call.SessionID != "s1" || call.TaskID != "t1" || call.Backend != "claude-code" {
		t.Errorf("stamp: %+v", call)
	}
	if len(ctx.Pending) != 0 {
		t.Errorf("pending left: %v", ctx.Pending)
	}
}

func TestClaudeCodeIncremental(t *testing.T) {
	full := fixture(t, "claude-code.jsonl")
	a := NewClaudeCode()
	whole, _ := a.Parse(strings.NewReader(full), newCtx())

	// Split mid-line after the tool_use turn plus a partial next line.
	cut := strings.Index(full, "not json") + 4
	ctx := newCtx()
	first, err := a.Parse(strings.NewReader(full[:cut]), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Offset >= int64(cut) {
		t.Fatalf("partial line was consumed: offset %d cut %d", ctx.Offset, cut)
	}
	if len(ctx.Pending) != 1 {
		t.Fatalf("pending = %v", ctx.Pending)
	}
	second, err := a.Parse(strings.NewReader(full[ctx.Offset:]), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(first) + len(second); got != len(whole) {
		t.Fatalf("incremental %d events, whole %d", got, len(whole))
	}
	if second[0].Kind != trace.KindToolResult || second[0].DurationMS != 2500 {
		t.Errorf("pairing across calls lost: %+v", second[0])
	}
	if ctx.Offset != int64(len(full)) {
		t.Errorf("offset %d want %d", ctx.Offset, len(full))
	}
}

func TestParseFileResumesAndHandlesTruncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	full := fixture(t, "claude-code.jsonl")
	lines := strings.SplitAfter(full, "\n")
	if err := os.WriteFile(p, []byte(strings.Join(lines[:2], "")), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := newCtx()
	a := NewClaudeCode()
	e1, err := ParseFile(a, p, ctx)
	if err != nil || len(e1) != 2 {
		t.Fatalf("first: %v %d", err, len(e1))
	}
	if err := os.WriteFile(p, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	e2, err := ParseFile(a, p, ctx)
	if err != nil || len(e2) != 4 {
		t.Fatalf("second: %v %d", err, len(e2))
	}
	// Truncate: re-reads from the start.
	if err := os.WriteFile(p, []byte(lines[0]), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(a, p, ctx); err != nil || ctx.Offset != int64(len(lines[0])) {
		t.Fatalf("truncate: %v offset %d", err, ctx.Offset)
	}
}

func TestPiParse(t *testing.T) {
	for _, name := range []string{"pi", "pi-prompt"} {
		a := NewPi(name)
		if a.Name() != name || a.Coverage() != trace.CoverageFull {
			t.Fatalf("%s meta", name)
		}
		ctx := newCtx()
		evs, err := a.Parse(strings.NewReader(fixture(t, "pi.jsonl")), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != 4 {
			t.Fatalf("%s: %d events: %+v", name, len(evs), evs)
		}
		if evs[0].Kind != trace.KindModelRequest || evs[0].TokensIn != 16 || evs[0].TokensOut != 4 {
			t.Errorf("model request: %+v", evs[0])
		}
		if evs[1].Name != "bash" || evs[1].ArgSummary != `{"command":"ls"}` || evs[2].Name != "read" {
			t.Errorf("calls: %+v %+v", evs[1], evs[2])
		}
		if evs[3].Kind != trace.KindModelRequest || evs[3].Backend != name {
			t.Errorf("last: %+v", evs[3])
		}
		if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !evs[1].Timestamp.Equal(want) {
			t.Errorf("ts %v", evs[1].Timestamp)
		}
		if ctx.Offset != int64(len(fixture(t, "pi.jsonl"))) {
			t.Errorf("offset %d", ctx.Offset)
		}
	}
}

func TestFallbackWhenNoDetail(t *testing.T) {
	obs := []Observation{{State: "running", At: fixed.Add(-time.Minute)}, {State: "complete"}}
	evs, err := ParseOrFallback(NewClaudeCode(), strings.NewReader("garbage\n"), newCtx(), obs)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("%+v", evs)
	}
	for _, e := range evs {
		if e.Kind != trace.KindStateChange || e.ArgSummary != FallbackNote || e.Backend != "claude-code" {
			t.Errorf("%+v", e)
		}
	}
	if !evs[1].Timestamp.Equal(fixed) {
		t.Errorf("fixed clock not used: %v", evs[1].Timestamp)
	}
	// Missing transcript (nil reader) also falls back.
	evs, _ = ParseOrFallback(NewPi("pi"), nil, newCtx(), obs)
	if len(evs) != 2 {
		t.Errorf("nil reader: %d", len(evs))
	}
	// With detail, no fallback.
	evs, _ = ParseOrFallback(NewClaudeCode(), strings.NewReader(fixture(t, "claude-code.jsonl")), newCtx(), obs)
	if len(evs) != 6 {
		t.Errorf("detail: %d", len(evs))
	}
}

func TestRegistryHonorsBackends(t *testing.T) {
	r := DefaultRegistry()
	all := r.Enabled(trace.Settings{})
	if len(all) != 13 || all[0].Name() != "aider" || all[1].Name() != "claude-code" {
		t.Fatalf("%v", all)
	}
	s := trace.Settings{Backends: map[string]bool{"pi": false}}
	if _, ok := r.For("pi", s); ok {
		t.Error("pi should be disabled")
	}
	if _, ok := r.For("pi-prompt", s); !ok {
		t.Error("pi-prompt should be on")
	}
	if got := len(r.Enabled(s)); got != 12 {
		t.Errorf("enabled %d", got)
	}
	if got := len(r.Enabled(trace.Settings{Mode: trace.ModeOff})); got != 0 {
		t.Errorf("mode off: %d", got)
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("unknown backend")
	}
}

func TestClaudePaths(t *testing.T) {
	if got := ClaudeProjectDirName(homeDir + "/a.b/c"); got != "-home-a-b-c" {
		t.Errorf("%q", got)
	}
	got := ClaudeTranscriptPath("/cfg", "/w/x", "abc")
	if got != "/cfg/projects/-w-x/abc.jsonl" {
		t.Errorf("%q", got)
	}
}
