package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// captureResumeServer stands a fake daemon up and returns the decoded
// PUT /api/autonomous/config body on each call.
func captureResumeServer(t *testing.T) (*Server, *map[string]any) {
	t.Helper()
	captured := new(map[string]any)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(b, &m); err == nil {
			*captured = m
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}, captured
}

// B165 — 0 (never resume) and -1 (no age limit) are meaningful sentinels,
// so both must be forwarded to the daemon; a caller that never mentions
// resume_max_age_seconds must not add it to the PUT body.
func TestAutonomousConfigSet_ResumeMaxAgeSentinels(t *testing.T) {
	for _, v := range []int{0, -1, 1800} {
		s, captured := captureResumeServer(t)
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"resume_max_age_seconds": v})); err != nil {
			t.Fatal(err)
		}
		if *captured == nil {
			t.Fatalf("no PUT body captured for %v", v)
		}
		got, ok := (*captured)["resume_max_age_seconds"]
		if !ok {
			t.Fatalf("resume_max_age_seconds %v not forwarded: %v", v, *captured)
		}
		fv, ok := got.(float64)
		if !ok || fv != float64(v) {
			t.Fatalf("resume_max_age_seconds = %v (%T), want %d", got, got, v)
		}
	}

	// Not mentioning the field at all must not add it to the body.
	s, captured := captureResumeServer(t)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_concurrent_tasks": 2})); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*captured)["resume_max_age_seconds"]; ok {
		t.Fatalf("resume_max_age_seconds present when the caller never mentioned it: %v", *captured)
	}
	if _, ok := (*captured)["max_concurrent_tasks"]; !ok {
		t.Fatalf("control key max_concurrent_tasks missing from body: %v", *captured)
	}
}

// The key is advertised in both tool schemas/descriptions so an LLM
// caller knows it exists and that 0 and -1 are legitimate values.
func TestAutonomousConfigTools_DocumentResumeMaxAge(t *testing.T) {
	s := &Server{}
	props := s.toolAutonomousConfigSet().InputSchema.Properties
	prop, ok := props["resume_max_age_seconds"]
	if !ok {
		t.Fatalf("resume_max_age_seconds missing from autonomous_config_set schema: %v", props)
	}
	desc, err := json.Marshal(prop)
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"0 = never resume", "-1 = no age limit"} {
		if !strings.Contains(string(desc), phrase) {
			t.Fatalf("resume_max_age_seconds description must document %q: %s", phrase, desc)
		}
	}
	if !strings.Contains(s.toolAutonomousConfigGet().Description, "resume_max_age_seconds") {
		t.Fatalf("autonomous_config_get description lacks resume_max_age_seconds: %s", s.toolAutonomousConfigGet().Description)
	}
}
