package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestProgressKindMCP_SetKindProxiesPOST(t *testing.T) {
	var path, method string
	var got map[string]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"p1","kind":"bug"}`))
	})
	res, err := s.handleAutonomousPRDSetKind(context.Background(), callReq(map[string]any{"id": "p1", "kind": "bug"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/p1/set_kind" || got["kind"] != "bug" {
		t.Fatalf("proxied %s %s body=%v", method, path, got)
	}
}

func TestProgressKindMCP_SetKindRefusalIsAToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown Automaton kind \"chore\": use feature, bug or other", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousPRDSetKind(context.Background(), callReq(map[string]any{"id": "p1", "kind": "chore"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: err=%v res=%+v", err, res)
	}
}

func TestProgressKindMCP_ToolShape(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousPRDSetKind()
	if tool.Name != "autonomous_prd_set_kind" {
		t.Fatalf("name = %q", tool.Name)
	}
	required := strings.Join(tool.InputSchema.Required, ",")
	if !strings.Contains(required, "id") || !strings.Contains(required, "kind") {
		t.Fatalf("id and kind must be required, got %q", required)
	}
	create := s.toolAutonomousPRDCreate()
	if _, ok := create.InputSchema.Properties["kind"]; !ok {
		t.Fatal("autonomous_prd_create lacks the optional kind parameter")
	}
	for _, r := range create.InputSchema.Required {
		if r == "kind" {
			t.Fatal("kind must be optional on create")
		}
	}
}

func TestProgressKindMCP_CreateSendsKindOnlyWhenGiven(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousPRDCreate(context.Background(), callReq(map[string]any{"spec": "fix the leak", "kind": "bug"})); err != nil {
		t.Fatal(err)
	}
	if got["kind"] != "bug" {
		t.Fatalf("kind not sent: %v", got)
	}
	if _, err := s.handleAutonomousPRDCreate(context.Background(), callReq(map[string]any{"spec": "add paging"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["kind"]; ok {
		t.Fatalf("kind must be left out when not given: %v", got)
	}
}

func TestProgressKindMCP_RequiresAutonomousWriteCapability(t *testing.T) {
	if got := federation.MCPToolCap["autonomous_prd_set_kind"]; got != federation.CapAutonomousWrite {
		t.Fatalf("autonomous_prd_set_kind capability = %q, want %q", got, federation.CapAutonomousWrite)
	}
}
