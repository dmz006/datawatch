package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

const mergedMembersBody = `{"automation_id":"ab12cd34","count":1,"merged_members":[{"automation_id":"ab12cd34","automation_name":"n","member":"m1","branch":"automaton/x","default_branch":"main","proving_commit":"abc123","policy":"completed","outcome":"advanced","at":"2026-10-30T10:00:00Z"}]}`

func TestMergedMemberMCP(t *testing.T) {
	var method, path string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(mergedMembersBody))
	})

	// one Automaton: GET /api/automations/{id}/merged-members, body passed through
	res, err := s.handleAutomationMergedMembers(context.Background(), callReq(map[string]any{"id": "ab12cd34"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/automations/ab12cd34/merged-members" {
		t.Fatalf("proxied %s %s", method, path)
	}
	text := automationText(t, res)
	for _, want := range []string{`"proving_commit":"abc123"`, `"outcome":"advanced"`, `"default_branch":"main"`, `"policy":"completed"`} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %s: %s", want, text)
		}
	}

	// no id: every Automaton
	if _, err := s.handleAutomationMergedMembers(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/api/automations/merged-members" {
		t.Fatalf("no-id call proxied %s %s, want GET /api/automations/merged-members", method, path)
	}
}

func TestMergedMemberMCP_ErrorIsAToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	res, err := s.handleAutomationMergedMembers(context.Background(), callReq(map[string]any{"id": "nope"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v, want a tool error result", err, res)
	}
}

// The tool is read-only, listed with the Automation tools, mapped to the
// autonomous read capability, and its description says Automaton or Automata
// and never the internal word PRD.
func TestMergedMemberMCP_ToolDefinition(t *testing.T) {
	tool := (&Server{}).toolAutomationMergedMembers()
	if tool.Name != "automation_merged_members" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("tool is not marked read-only")
	}
	if !strings.Contains(tool.Description, "Automaton") || !strings.Contains(tool.Description, "Automata") {
		t.Errorf("description must say Automaton or Automata: %s", tool.Description)
	}
	if strings.Contains(tool.Description, "PRD") {
		t.Errorf("description uses the internal word PRD: %s", tool.Description)
	}
	found := false
	for _, n := range AutomationToolNames() {
		found = found || n == "automation_merged_members"
	}
	if !found {
		t.Error("automation_merged_members is missing from AutomationToolNames")
	}
	if got, ok := federation.RequiredCapForMCPTool("automation_merged_members"); !ok || got != federation.CapAutonomousRead {
		t.Errorf("federation capability = %q (known=%v), want %q", got, ok, federation.CapAutonomousRead)
	}
}
