package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// The milestone argument on the automation_create and automation_update MCP
// tools is forwarded to the daemon as a boolean. Omitted means "not sent", so
// an update keeps the stored value.

func TestAutomationMilestoneMCP_CreateForwardsFlag(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"a1","milestone":true}`))
	})
	if _, err := s.handleAutomationCreate(context.Background(), callReq(map[string]any{
		"name": "1.0 arc", "milestone": true,
	})); err != nil {
		t.Fatal(err)
	}
	if got["milestone"] != true {
		t.Fatalf("milestone:true not forwarded on create: %v", got)
	}
	if _, err := s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "plain"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["milestone"]; ok {
		t.Fatalf("omitted milestone must not be sent on create: %v", got)
	}
}

func TestAutomationMilestoneMCP_UpdateSetClearAndOmit(t *testing.T) {
	var path, method string
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		got = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"a1"}`))
	})

	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "milestone": true})); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/api/automations/a1" || got["milestone"] != true {
		t.Fatalf("set: %s %s body=%v", method, path, got)
	}

	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "milestone": false})); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["milestone"]; !ok || v != false {
		t.Fatalf("milestone:false must be sent to clear: %v", got)
	}

	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "name": "x"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["milestone"]; ok {
		t.Fatalf("omitted milestone must not be sent: %v", got)
	}
}

func TestAutomationMilestoneMCP_ToolShape(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]func() bool{
		"automation_create": func() bool {
			p, ok := s.toolAutomationCreate().InputSchema.Properties["milestone"].(map[string]any)
			return ok && p["type"] == "boolean"
		},
		"automation_update": func() bool {
			p, ok := s.toolAutomationUpdate().InputSchema.Properties["milestone"].(map[string]any)
			return ok && p["type"] == "boolean"
		},
	} {
		if !tool() {
			t.Errorf("%s must declare milestone as a boolean", name)
		}
	}
}
