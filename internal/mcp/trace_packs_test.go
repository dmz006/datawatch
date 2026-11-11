package mcp

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestTracePackArg_SchemaOnRollupAndSession(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]func() (has bool){
		"trace_session": func() bool { _, ok := s.toolTraceSession().InputSchema.Properties["pack"]; return ok },
		"trace_rollup":  func() bool { _, ok := s.toolTraceRollup().InputSchema.Properties["pack"]; return ok },
	} {
		if !tool() {
			t.Errorf("%s schema has no optional pack argument", name)
		}
	}
	if _, ok := s.toolTraceAutomaton().InputSchema.Properties["pack"]; ok {
		t.Error("trace_automaton should not gain a pack argument in this change")
	}
	for _, req := range s.toolTraceSession().InputSchema.Required {
		if req == "pack" {
			t.Error("pack must be optional on trace_session")
		}
	}
	for _, req := range s.toolTraceRollup().InputSchema.Required {
		if req == "pack" {
			t.Error("pack must be optional on trace_rollup")
		}
	}
}

func TestTracePackArg_ProxiedAsQuery(t *testing.T) {
	s, got := traceToolServer(t, http.StatusOK, `{"ok":true}`)
	ctx := context.Background()

	if res, err := s.handleTraceRollup(ctx, callReq(map[string]any{"pack": "  gitops  "})); err != nil || res.IsError {
		t.Fatalf("rollup err=%v res=%+v", err, res)
	}
	if want := "GET /api/trace/rollup?pack=gitops"; *got != want {
		t.Errorf("rollup proxied %q, want %q", *got, want)
	}

	if res, err := s.handleTraceSession(ctx, callReq(map[string]any{"session": "host/s1", "pack": "gitops", "tool": "Read"})); err != nil || res.IsError {
		t.Fatalf("session err=%v res=%+v", err, res)
	}
	if want := "GET /api/trace/session/host%2Fs1?pack=gitops&tool=Read"; *got != want {
		t.Errorf("session proxied %q, want %q", *got, want)
	}
}

func TestTracePackArg_BlankOrAbsentIsDropped(t *testing.T) {
	s, got := traceToolServer(t, http.StatusOK, `{}`)
	ctx := context.Background()

	if _, err := s.handleTraceRollup(ctx, callReq(map[string]any{"pack": "   "})); err != nil {
		t.Fatal(err)
	}
	if *got != "GET /api/trace/rollup?" {
		t.Errorf("blank pack was forwarded: %q", *got)
	}
	if _, err := s.handleTraceSession(ctx, callReq(map[string]any{"session": "s1"})); err != nil {
		t.Fatal(err)
	}
	if *got != "GET /api/trace/session/s1?" {
		t.Errorf("absent pack changed the query: %q", *got)
	}
}

func TestAddTracePackQuery_KeepsExistingValues(t *testing.T) {
	q := url.Values{"dimension": {"pack_name"}}
	out := addTracePackQuery(callReq(map[string]any{"pack": "gitops"}), q)
	if out.Get("pack") != "gitops" || out.Get("dimension") != "pack_name" {
		t.Errorf("query = %v", out)
	}
}
