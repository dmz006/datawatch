// MCP-surface tests for the project-policy tools: project_upsert's
// optional policy block and the new project_policy_effective reader.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestProjectPolicyEffectiveToolSchema(t *testing.T) {
	s := &Server{}
	tool := s.toolProjectPolicyEffective()
	if tool.Name != "project_policy_effective" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.InputSchema.Properties["name"] == nil {
		t.Error("name property missing")
	}
	if tool.InputSchema.Properties["automaton"] == nil {
		t.Error("automaton property missing")
	}
	required := map[string]bool{}
	for _, r := range tool.InputSchema.Required {
		required[r] = true
	}
	if !required["name"] {
		t.Errorf("name must be required: %v", tool.InputSchema.Required)
	}
	if required["automaton"] {
		t.Errorf("automaton must be optional: %v", tool.InputSchema.Required)
	}
}

func TestProjectUpsertToolHasOptionalPolicy(t *testing.T) {
	s := &Server{}
	tool := s.toolProjectUpsert()
	if tool.InputSchema.Properties["policy"] == nil {
		t.Fatal("project_upsert is missing the policy argument")
	}
	for _, r := range tool.InputSchema.Required {
		if r == "policy" {
			t.Errorf("policy must not be required: %v", tool.InputSchema.Required)
		}
	}
}

func TestProjectPolicyEffectiveHandler(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		if r.URL.Path != "/api/projects/acme/policy/effective" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project":"acme","automaton":"prd-1","policy":{
			"plan_review_mode":{"value":"block","source":"automaton"},
			"max_concurrent_tasks":{"value":2,"source":"project"}}}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "acme", "automaton": "prd-1"}
	res, err := s.handleProjectPolicyEffective(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	out := resContentText(t, res)
	for _, want := range []string{`"plan_review_mode"`, `"block"`, `"automaton"`, `"max_concurrent_tasks"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if gotPath != "/api/projects/acme/policy/effective" {
		t.Errorf("path = %q", gotPath)
	}
	if strings.Contains(gotQuery, "prd-1") == false {
		t.Errorf("automaton id not forwarded; query = %q", gotQuery)
	}
}

func TestProjectPolicyEffectiveHandlerNameRequired(t *testing.T) {
	s := &Server{}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleProjectPolicyEffective(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("call with no name must be an error")
	}
}

func TestProjectUpsertHandlerPolicyMap(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"acme","policy":{"plan_review_mode":"block"}}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "acme",
		"dir":  "/tmp/acme",
		"policy": map[string]any{
			"plan_review_mode":     "block",
			"max_concurrent_tasks": float64(2),
			"retry_policy":         map[string]any{"infra": float64(3)},
		},
	}
	res, err := s.handleProjectUpsert(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects" {
		t.Fatalf("got %s %s", gotMethod, gotPath)
	}
	policy, ok := gotBody["policy"].(map[string]any)
	if !ok {
		t.Fatalf("policy not forwarded as an object: %v", gotBody["policy"])
	}
	if policy["plan_review_mode"] != "block" {
		t.Errorf("plan_review_mode = %v", policy["plan_review_mode"])
	}
	if got, ok := policy["max_concurrent_tasks"].(float64); !ok || got != 2 {
		t.Errorf("max_concurrent_tasks = %v", policy["max_concurrent_tasks"])
	}
}

func TestProjectUpsertHandlerPolicyJSONString(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"name":"acme"}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":   "acme",
		"dir":    "/tmp/acme",
		"policy": `{"uncommitted_work_policy":"hold"}`,
	}
	res, err := s.handleProjectUpsert(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("JSON-string policy must be accepted: err=%v res=%+v", err, res)
	}
	policy, ok := gotBody["policy"].(map[string]any)
	if !ok || policy["uncommitted_work_policy"] != "hold" {
		t.Errorf("policy = %v", gotBody["policy"])
	}
}

func TestProjectUpsertHandlerPolicyWrongType(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "acme", "dir": "/tmp/acme", "policy": float64(42)}
	res, err := s.handleProjectUpsert(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("numeric policy must be rejected")
	}
	if called {
		t.Error("rejected call must not reach the daemon")
	}
}
