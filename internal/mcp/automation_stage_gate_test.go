package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// The stage_gate argument on the automation_create and automation_update MCP
// tools is forwarded to the daemon as a boolean. Omitted means "not sent", so an
// update keeps the stored value and false is sent so it can turn the gate off.

func TestAutomationStageGateMCP_CreateForwardsFlag(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"a1","stage_gate":true}`))
	})
	if _, err := s.handleAutomationCreate(context.Background(), callReq(map[string]any{
		"name": "gated", "stage_gate": true,
	})); err != nil {
		t.Fatal(err)
	}
	if got["stage_gate"] != true {
		t.Fatalf("stage_gate:true not forwarded on create: %v", got)
	}
	if _, err := s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "plain"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["stage_gate"]; ok {
		t.Fatalf("omitted stage_gate must not be sent on create: %v", got)
	}
}

func TestAutomationStageGateMCP_UpdateSetClearAndOmit(t *testing.T) {
	var path, method string
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		got = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"a1"}`))
	})

	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "stage_gate": true})); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/api/automations/a1" || got["stage_gate"] != true {
		t.Fatalf("set: %s %s body=%v", method, path, got)
	}

	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "stage_gate": false})); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["stage_gate"]; !ok || v != false {
		t.Fatalf("clear: stage_gate = %v (present %v), want false sent", v, ok)
	}

	if _, err := s.handleAutomationUpdate(context.Background(), callReq(map[string]any{"id": "a1", "name": "renamed"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["stage_gate"]; ok {
		t.Fatalf("omitted stage_gate must not be sent on update: %v", got)
	}
}
