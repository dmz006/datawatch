package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/router"
	dwserver "github.com/dmz006/datawatch/internal/server"
)

func TestCapacityAdmission_ToolProxiesReadOnlyGET(t *testing.T) {
	tool := (&Server{}).toolCapacityAdmission()
	if tool.Name != "capacity_admission" || tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("tool %q must be read-only: %+v", tool.Name, tool.Annotations)
	}
	var path, method, query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"kind":"admit","queue_position":0}`))
	})
	_, err := s.handleCapacityAdmission(context.Background(), callReq(map[string]any{"backend": "x", "node": "n1", "model": "m", "priority": "3"}))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/capacity/admission" || method != http.MethodGet {
		t.Fatalf("proxied %s %s", method, path)
	}
	for _, want := range []string{"backend=x", "node=n1", "model=m", "priority=3"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query %q missing %q", query, want)
		}
	}
}

// TestCapacityAdmissionParity_AllSurfaces asserts REST, MCP, CLI text and the
// comm command answer the same Decision for the same ledger state.
func TestCapacityAdmissionParity_AllSurfaces(t *testing.T) {
	reg := inference.NewRegistry()
	for _, n := range []string{"x", "y"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: "ollama"}); err != nil {
			t.Fatal(err)
		}
	}
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("llm:x", 1)
	led.SetLimit("llm:y", 2)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "t1", Pools: []string{"llm:x"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(dwserver.AdmissionHandler(led, reg))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndexByte(ts.URL, ':')+1:])

	want := capacity.FormatDecision(led.Check(capacity.Request{Pools: []string{"host", "llm:x"}}))
	if !strings.Contains(want, "queued at position 1") || !strings.Contains(want, "alternate available: llm:y") {
		t.Fatalf("unexpected decision text %q", want)
	}

	// REST body; the CLI prints exactly FormatDecisionJSON of it.
	resp, err := http.Get(ts.URL + "/?backend=x")
	if err != nil {
		t.Fatal(err)
	}
	restBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if got, err := capacity.FormatDecisionJSON(restBody); err != nil || got != want {
		t.Fatalf("REST/CLI text %q (err %v), want %q", got, err, want)
	}

	// MCP: the same body through the tool.
	res, err := (&Server{webPort: port}).handleCapacityAdmission(context.Background(), callReq(map[string]any{"backend": "x"}))
	if err != nil || res == nil || len(res.Content) == 0 {
		t.Fatalf("mcp: %v %v", res, err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if got, err := capacity.FormatDecisionJSON([]byte(text)); err != nil || got != want {
		t.Fatalf("MCP text %q (err %v), want %q", got, err, want)
	}

	// Comm.
	rt := router.NewRouter("h", "g", nil, nil, 0, nil)
	rt.SetWebPort(port)
	replies := rt.HandleTestMessage("capacity admission x")
	if len(replies) != 1 || !strings.HasSuffix(replies[0], want) {
		t.Fatalf("comm reply %q, want suffix %q", replies, want)
	}
}
