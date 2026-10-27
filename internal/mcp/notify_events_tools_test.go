// MCP tests for autonomous_prd_set_notify_events and the guardrail
// profile notify_events_json parameter, proxied against an httptest
// backend.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestNotifyEvents_PostsPathAndBody(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"a1","status":"active"}`)
	res, err := s.handleAutonomousPRDSetNotifyEvents(context.Background(), callReq(map[string]any{
		"id":            "a1",
		"notify_events": []string{"prd.completed", "task.session_started"},
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/prds/a1/set_notify_events" {
		t.Errorf("proxied %s %s, want POST /api/autonomous/prds/a1/set_notify_events", rec.method, rec.path)
	}
	got, ok := rec.body["notify_events"].([]any)
	if !ok {
		t.Fatalf("body[notify_events] = %v (%T), want array", rec.body["notify_events"], rec.body["notify_events"])
	}
	want := []string{"prd.completed", "task.session_started"}
	if len(got) != len(want) {
		t.Fatalf("body[notify_events] = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("body[notify_events][%d] = %v, want %v", i, got[i], w)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, `"a1"`) {
		t.Errorf("result = %s", text)
	}
}

func TestNotifyEvents_ClearSendsEmptyList(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"a1","notify_events":null}`)
	// Omitted → clears the Automaton-level set and inherits from the next tier.
	if _, err := s.handleAutonomousPRDSetNotifyEvents(context.Background(), callReq(map[string]any{"id": "a1"})); err != nil {
		t.Fatal(err)
	}
	if got, ok := rec.body["notify_events"].([]any); !ok || len(got) != 0 {
		t.Errorf("omitted: body[notify_events] = %v, want empty list", rec.body["notify_events"])
	}
	// Explicitly empty → also sends an empty list.
	rec.method = ""
	if _, err := s.handleAutonomousPRDSetNotifyEvents(context.Background(), callReq(map[string]any{"id": "a1", "notify_events": []string{}})); err != nil {
		t.Fatal(err)
	}
	if got, ok := rec.body["notify_events"].([]any); !ok || len(got) != 0 {
		t.Errorf("explicit empty: body[notify_events] = %v, want empty list", rec.body["notify_events"])
	}
}

func TestNotifyEvents_ToolDefinition(t *testing.T) {
	def := (&Server{}).toolAutonomousPRDSetNotifyEvents()
	if def.Name != "autonomous_prd_set_notify_events" {
		t.Fatalf("tool name = %q", def.Name)
	}
	if len(def.InputSchema.Required) != 1 || def.InputSchema.Required[0] != "id" {
		t.Errorf("required = %v, want only id", def.InputSchema.Required)
	}
	if sch, ok := def.InputSchema.Properties["notify_events"].(map[string]any); !ok || sch["type"] != "array" {
		t.Errorf("notify_events schema = %v, want array type", def.InputSchema.Properties["notify_events"])
	}
	eventsDesc, _ := def.InputSchema.Properties["notify_events"].(map[string]any)
	desc := def.Description + " " + anyToString(eventsDesc["description"])
	for _, want := range []string{
		"prd.needs_review", "prd.blocked", "prd.failed", "prd.completed", "prd.cancelled",
		"task.session_started", "task.session_cleared", "scope_warning", "guardrail_warn",
		"Automaton", "guardrail profile", "subscription",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("tool descriptions missing %q", want)
		}
	}
}

// TestNotifyEvents_ProfileToolDescriptionsNameOptInKinds pins the
// guardrail-profile notify_events_json parameter documentation: create
// and update must name the new opt-in warning kinds and say they are off
// by default (warnings never block work).
func TestNotifyEvents_ProfileToolDescriptionsNameOptInKinds(t *testing.T) {
	toolDefs := map[string]mcpsdk.Tool{
		"guardrail_profile_create": (&Server{}).toolGuardrailProfileCreate(),
		"guardrail_profile_update": (&Server{}).toolGuardrailProfileUpdate(),
	}
	for name, def := range toolDefs {
		props, ok := def.InputSchema.Properties["notify_events_json"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no notify_events_json property in schema %v", name, def.InputSchema.Properties)
		}
		desc, _ := props["description"].(string)
		for _, want := range []string{"scope_warning", "guardrail_warn", "off by default"} {
			if !strings.Contains(desc, want) {
				t.Errorf("%s notify_events_json description missing %q: %s", name, want, desc)
			}
		}
	}
}

func TestNotifyEvents_ProfileToolsOnlySendWhenSupplied(t *testing.T) {
	// Create: omitted → key absent; supplied → forwarded as an array.
	s, rec := activateServer(t, http.StatusOK, `{"id":"prof1"}`)
	res, err := s.handleGuardrailProfileCreate(context.Background(), callReq(map[string]any{"name": "n1"}))
	if err != nil || res == nil {
		t.Fatalf("create omitted: err=%v res=%v", err, res)
	}
	if _, ok := rec.body["notify_events"]; ok {
		t.Errorf("create omitted notify_events_json but sent %v, want no key", rec.body["notify_events"])
	}
	rec.method = ""
	res, err = s.handleGuardrailProfileCreate(context.Background(), callReq(map[string]any{
		"name": "n1", "notify_events_json": `["prd.blocked","task.session_started"]`,
	}))
	if err != nil || res == nil {
		t.Fatalf("create supplied: err=%v res=%v", err, res)
	}
	got, ok := rec.body["notify_events"].([]any)
	if !ok || len(got) != 2 || got[0] != "prd.blocked" || got[1] != "task.session_started" {
		t.Errorf("create supplied body[notify_events] = %v", rec.body["notify_events"])
	}
	if text := automationText(t, res); !strings.Contains(text, `"prof1"`) {
		t.Errorf("create result = %s", text)
	}

	// Update: omitted → key absent (profile unchanged); "[]" → empty list.
	su, recu := activateServer(t, http.StatusOK, `{}`)
	if _, err := su.handleGuardrailProfileUpdate(context.Background(), callReq(map[string]any{"id": "prof1", "name": "n2"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := recu.body["notify_events"]; ok {
		t.Errorf("update omitted notify_events_json but sent %v, want no key", recu.body["notify_events"])
	}
	recu.method = ""
	if _, err := su.handleGuardrailProfileUpdate(context.Background(), callReq(map[string]any{"id": "prof1", "notify_events_json": "[]"})); err != nil {
		t.Fatal(err)
	}
	if got, ok := recu.body["notify_events"].([]any); !ok || len(got) != 0 {
		t.Errorf(`update "[]" body[notify_events] = %v, want empty list`, recu.body["notify_events"])
	}
}

func TestNotifyEvents_InvalidJSONIsReported(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	for _, raw := range []string{"not-json", `"single-string"`, `["ok", 42]`} {
		res, err := s.handleGuardrailProfileCreate(context.Background(), callReq(map[string]any{"name": "n1", "notify_events_json": raw}))
		if err != nil || res == nil {
			t.Fatalf("create %q: err=%v res=%v", raw, err, res)
		}
		if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "JSON array of event kind strings") {
			t.Errorf("create %q: result = %q, want error naming the expected shape", raw, text)
		}
		if rec.method != "" {
			t.Errorf("create %q reached the server", raw)
		}
	}

	su, recu := activateServer(t, http.StatusOK, `{}`)
	res, err := su.handleGuardrailProfileUpdate(context.Background(), callReq(map[string]any{"id": "p", "notify_events_json": `{"nope":true}`}))
	if err != nil || res == nil {
		t.Fatalf("update: err=%v res=%v", err, res)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "JSON array of event kind strings") {
		t.Errorf("update invalid notify_events_json: result = %q", text)
	}
	if recu.method != "" {
		t.Errorf("update invalid notify_events_json reached the server")
	}
}

func TestNotifyEvents_RegisteredWithCapabilityEntry(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})

	tools, err := s.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "autonomous_prd_set_notify_events" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("autonomous_prd_set_notify_events is not registered on the MCP server")
	}
	if cap, ok := federation.MCPToolCap["autonomous_prd_set_notify_events"]; !ok || cap != federation.CapAutonomousWrite {
		t.Errorf("federation.MCPToolCap entry = %q (present=%v), want %q", cap, ok, federation.CapAutonomousWrite)
	}
}

func anyToString(v any) string {
	s, _ := v.(string)
	return s
}
