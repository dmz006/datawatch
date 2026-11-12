// parent_prd_id over MCP: the tool schema lists it and autonomous_prd_create
// forwards it to the same REST body the CLI and comm verb send.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChildInheritParity_MCP_SchemaListsParent(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousPRDCreate())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "parent_prd_id") {
		t.Fatalf("autonomous_prd_create schema has no parent_prd_id: %s", b)
	}
}

func TestChildInheritParity_MCP_ForwardsParent(t *testing.T) {
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/autonomous/prds" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1"}`))
	})
	ctx := context.Background()
	res, err := s.handleAutonomousPRDCreate(ctx, callReq(map[string]any{
		"spec": "child", "parent_prd_id": " p1 ", "project_dir": "/explicit", "backend": "b", "model": "m",
	}))
	if err != nil || res.IsError {
		t.Fatalf("create: %+v / %v", res, err)
	}
	if body["parent_prd_id"] != "p1" || body["project_dir"] != "/explicit" || body["backend"] != "b" || body["model"] != "m" {
		t.Fatalf("forwarded body = %v", body)
	}
	if _, err := s.handleAutonomousPRDCreate(ctx, callReq(map[string]any{"spec": "plain"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["parent_prd_id"]; ok {
		t.Fatalf("parent_prd_id must not be sent when omitted: %v", body)
	}
}
