// start_session `git` param — tool schema exposure and REST body forwarding.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestStartSession_Git_InSchema verifies the start_session tool schema
// exposes the optional `git` boolean param.
func TestStartSession_Git_InSchema(t *testing.T) {
	s := &Server{}
	tool := s.toolStartSession()

	prop, ok := tool.InputSchema.Properties["git"]
	if !ok {
		t.Fatal("start_session schema: missing `git` property")
	}
	propJSON, _ := json.Marshal(prop)
	propStr := string(propJSON)
	if !strings.Contains(propStr, "\"boolean\"") {
		t.Errorf("git: want type boolean, got %s", propStr)
	}
	if !strings.Contains(propStr, "escription") {
		t.Error("git: expected a description in the property definition")
	}
	// Optional: must NOT be in the required list.
	for _, r := range tool.InputSchema.Required {
		if r == "git" {
			t.Error("git: should be optional (not in Required list)")
		}
	}
}

// TestStartSession_Git_ForwardedToREST verifies that git=true is forwarded
// to the REST /api/sessions/start body.
func TestStartSession_Git_ForwardedToREST(t *testing.T) {
	var gotBody map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sessions/start" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ab12","task":"do the thing"}`))
	})

	res, err := s.handleStartSession(context.Background(), callReq(map[string]any{
		"task": "do the thing",
		"git":  true,
	}))
	if err != nil {
		t.Fatalf("handleStartSession: %v", err)
	}
	if gotBody == nil {
		t.Fatal("handler did not forward a request to REST")
	}
	if gotBody["git"] != true {
		t.Errorf("REST body git: want true, got %v", gotBody["git"])
	}
	if gotBody["task"] != "do the thing" {
		t.Errorf("REST body task: want %q, got %v", "do the thing", gotBody["task"])
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected tool result")
	}
}

// TestStartSession_Git_DefaultFalse verifies that when the caller omits
// `git`, the REST body does not carry a git field (default false).
func TestStartSession_Git_DefaultFalse(t *testing.T) {
	var gotBody map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cd34","task":"no git"}`))
	})

	// Use chrome=true to force the REST-forward path without setting git.
	if _, err := s.handleStartSession(context.Background(), callReq(map[string]any{
		"task":   "no git",
		"chrome": true,
	})); err != nil {
		t.Fatalf("handleStartSession: %v", err)
	}
	if gotBody == nil {
		t.Fatal("handler did not forward a request to REST")
	}
	if _, present := gotBody["git"]; present {
		t.Errorf("REST body: git field should be absent by default, got %v", gotBody["git"])
	}
}
