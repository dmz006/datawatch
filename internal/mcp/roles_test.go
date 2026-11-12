package mcp

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestRoleParityMCPForwardsRolesToREST(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]mcpsdk.Tool{"llm_add": s.toolLLMAdd(), "llm_update": s.toolLLMUpdate()} {
		prop, ok := tool.InputSchema.Properties["roles"].(map[string]any)
		if !ok || prop["type"] != "array" {
			t.Errorf("%s has no roles array argument: %v", name, tool.InputSchema.Properties["roles"])
		}
	}

	rec := &budgetRecorder{}
	srv := planTierWebServer(t, rec.handler)
	ctx := context.Background()

	if res, err := srv.handleLLMAddMCP(ctx, callReq(map[string]any{"name": "a", "kind": "ollama", "roles": []any{"planning", "Review"}})); err != nil || res.IsError {
		t.Fatalf("llm_add: err=%v res=%+v", err, res)
	}
	add := rec.find(http.MethodPost, "/api/llms")
	if add == nil {
		t.Fatal("llm_add did not POST /api/llms")
	}
	if got := add.body["roles"]; !reflect.DeepEqual(got, []any{"planning", "Review"}) {
		t.Errorf("llm_add roles = %v", got)
	}

	// update: omitted keeps, empty clears
	if _, err := srv.handleLLMUpdateMCP(ctx, callReq(map[string]any{"name": "a", "kind": "ollama"})); err != nil {
		t.Fatal(err)
	}
	if _, has := rec.find(http.MethodPut, "/api/llms/a").body["roles"]; has {
		t.Error("llm_update without roles must not send roles")
	}
	rec2 := &budgetRecorder{}
	srv2 := planTierWebServer(t, rec2.handler)
	if _, err := srv2.handleLLMUpdateMCP(ctx, callReq(map[string]any{"name": "a", "kind": "ollama", "roles": []any{}})); err != nil {
		t.Fatal(err)
	}
	got, has := rec2.find(http.MethodPut, "/api/llms/a").body["roles"]
	if !has || !reflect.DeepEqual(got, []any{}) {
		t.Errorf("llm_update with empty roles sent %v (present=%v), want []", got, has)
	}

	// a non-string element is a tool error, not a request
	res, err := srv.handleLLMAddMCP(ctx, callReq(map[string]any{"name": "b", "kind": "ollama", "roles": []any{1}}))
	if err != nil || res == nil || !res.IsError {
		t.Errorf("non-string role should be a tool error: err=%v res=%+v", err, res)
	}
}
