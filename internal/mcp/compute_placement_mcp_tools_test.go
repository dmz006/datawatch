// The two placement MCP tools — compute_placement_list (read) and
// compute_placement_answer (approve/deny, gated on CapComputeWrite) — must
// reach the daemon REST surface on the same wire the operators already know:
// GET /api/compute/placements and the one-shot decision POSTs.

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// placementAskFake stands in for the daemon placement REST surface.
func placementAskFake(t *testing.T) (*string, *string, *httptest.Server) {
	t.Helper()
	var gotApprove, gotDeny string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/compute/placements", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"asks":[{"id":"ask-1","model":"llama3.1-8b","node":"gpu-box","decision":"pending"}],"pending":1}`))
	})
	mux.HandleFunc("/api/compute/placements/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/") // [ "", "api", "compute", "placements", id, action ]
		id, action := parts[len(parts)-2], parts[len(parts)-1]
		w.Header().Set("Content-Type", "application/json")
		if id != "ask-1" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"placement ask ` + id + ` not found"}`))
			return
		}
		switch action {
		case "approve":
			gotApprove = "yes"
			_, _ = w.Write([]byte(`{"id":"ask-1","decision":"approved"}`))
		case "deny":
			gotDeny = "yes"
			_, _ = w.Write([]byte(`{"id":"ask-1","decision":"denied"}`))
		default:
			_, _ = w.Write([]byte(`{"error":"unknown placement action"}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &gotApprove, &gotDeny, srv
}

func placementMCP(t *testing.T) *Server {
	t.Helper()
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	port, _ := strconv.Atoi(host[strings.LastIndexByte(host, ':')+1:]) //nolint:errcheck
	return &Server{webPort: port}
}

func placementToolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil {
		return "<nil result>"
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestComputePlacementListTool(t *testing.T) {
	gotApprove, gotDeny, fakesrv := placementAskFake(t)
	_ = gotApprove
	_ = gotDeny
	host := strings.TrimPrefix(fakesrv.URL, "http://")
	port, _ := strconv.Atoi(host[strings.LastIndexByte(host, ':')+1:]) //nolint:errcheck
	s := &Server{webPort: port}

	res, err := s.handleComputePlacementListMCP(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	body := placementToolText(t, res)
	for _, want := range []string{"ask-1", "llama3.1-8b", "gpu-box"} {
		if !strings.Contains(body, want) {
			t.Errorf("list body missing %q: %s", want, body)
		}
	}
	if *gotApprove != "" {
		t.Errorf("list must not hit the decision endpoint, approve=%q", *gotApprove)
	}
}

func TestComputePlacementAnswerToolApproveAndDeny(t *testing.T) {
	gotApprove, gotDeny, fakesrv := placementAskFake(t)
	host := strings.TrimPrefix(fakesrv.URL, "http://")
	port, _ := strconv.Atoi(host[strings.LastIndexByte(host, ':')+1:]) //nolint:errcheck
	s := &Server{webPort: port}

	for _, decision := range []string{"approve", "deny"} {
		req := mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: map[string]any{
			"id": "ask-1", "decision": decision,
		}}}
		res, err := s.handleComputePlacementAnswerMCP(context.Background(), req)
		if err != nil {
			t.Fatalf("%s handler err: %v", decision, err)
		}
		if res == nil || res.IsError {
			t.Fatalf("%s expected success, got %+v", decision, res)
		}
		want := decision
		if decision == "deny" {
			want = "denied"
		} else {
			want = "approved"
		}
		if !strings.Contains(placementToolText(t, res), want) {
			t.Errorf("%s body missing %q: %s", decision, want, placementToolText(t, res))
		}
	}
	if *gotApprove != "yes" || *gotDeny != "yes" {
		t.Errorf("fake saw approve=%q deny=%q; want both yes", *gotApprove, *gotDeny)
	}
}

func TestComputePlacementAnswerToolRejectsBadInput(t *testing.T) {
	s := placementMCP(t)

	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"empty id", map[string]any{"decision": "approve"}, "id"},
		{"missing decision", map[string]any{"id": "ask-1"}, "decision"},
		{"bad decision", map[string]any{"id": "ask-1", "decision": "maybe"}, "approve"},
	}
	for _, c := range cases {
		res, err := s.handleComputePlacementAnswerMCP(context.Background(),
			mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: c.args}})
		if err != nil {
			t.Fatalf("%s: handler err: %v", c.name, err)
		}
		got := placementToolText(t, res)
		if !strings.Contains(got, c.wantErr) {
			t.Errorf("%s: body = %q; want mention of %q", c.name, got, c.wantErr)
		}
	}
}

// A federation peer whose CBAC grant list lacks CapComputeWrite is refused
// the answer tool (and still allowed the read-only list tool — that one is
// ungated by design).
func TestComputePlacementAnswerToolRequiresCapComputeWrite(t *testing.T) {
	_, _, fakesrv := placementAskFake(t)
	host := strings.TrimPrefix(fakesrv.URL, "http://")
	port, _ := strconv.Atoi(host[strings.LastIndexByte(host, ':')+1:]) //nolint:errcheck
	s := &Server{webPort: port}

	limitedPeer := &multiserver.Entry{Name: "limited", Capabilities: []string{"compute:read"}}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, limitedPeer)

	req := mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: map[string]any{
		"id": "ask-1", "decision": "approve",
	}}}
	res, err := s.handleComputePlacementAnswerMCP(ctx, req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected capability refusal, got %+v", res)
	}
	if got := placementToolText(t, res); !strings.Contains(got, "capability") {
		t.Errorf("refusal body = %q; want capability mention", got)
	}

	// The list tool has no write requirement — the same peer may still read.
	if lres, lerr := s.handleComputePlacementListMCP(ctx, mcpsdk.CallToolRequest{}); lerr != nil || lres == nil || lres.IsError {
		t.Errorf("list with read-only peer should succeed: %+v / %v", lres, lerr)
	}
}

// With the loopback unconfigured both tools fail cleanly, not with a panic.
func TestComputePlacementToolsLoopbackUnconfigured(t *testing.T) {
	s := &Server{}
	if _, err := s.handleComputePlacementListMCP(context.Background(), mcpsdk.CallToolRequest{}); err == nil {
		t.Errorf("list with webPort=0 should fail, not succeed")
	}
	req := mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: map[string]any{
		"id": "ask-1", "decision": "approve",
	}}}
	if _, err := s.handleComputePlacementAnswerMCP(context.Background(), req); err == nil {
		t.Errorf("answer with webPort=0 should fail, not succeed")
	}
}
