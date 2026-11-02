// MCP surface tests for autonomous_shipped_record: it proxies a POST to the
// PRD or Automation shipped endpoint, requires the merge commit, and turns a
// daemon refusal into a tool error. No real daemon or home dir is touched —
// a loopback httptest server stands in (same shape as the other tool tests).

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestShippedRecordMCP_ProxiesPRD(t *testing.T) {
	var method, path string
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"p1","status":"completed","merged_in_release":"v1.2.0","merge_commit":"c0ffee42","shipped_source":"backfill"}`))
	})
	res, err := s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{
		"id":                "p1",
		"merge_commit":      "c0ffee42",
		"merged_in_release": "v1.2.0",
		"source":            "backfill",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/p1/shipped" {
		t.Fatalf("proxied %s %s; want POST /api/autonomous/prds/p1/shipped", method, path)
	}
	if body["merge_commit"] != "c0ffee42" || body["merged_in_release"] != "v1.2.0" || body["source"] != "backfill" {
		t.Errorf("body = %+v; want commit/release/source", body)
	}
	if _, ok := body["status"]; ok {
		t.Errorf("body must not carry a status key: %+v", body)
	}
	if out := resContentText(t, res); !strings.Contains(out, "v1.2.0") || !strings.Contains(out, "completed") {
		t.Errorf("tool result = %q; want the reply (shipped state + unchanged status)", out)
	}
}

func TestShippedRecordMCP_ProxiesAutomation(t *testing.T) {
	var method, path string
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"a1","status":"paused","merged_in_release":"v3.0.0","merge_commit":"bead1","shipped_source":"merge_step"}`))
	})
	res, err := s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{
		"id":                "a1",
		"automation":        true,
		"merge_commit":      "bead1",
		"merged_in_release": "v3.0.0",
		"source":            "merge_step",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/automations/a1/shipped" {
		t.Fatalf("proxied %s %s; want POST /api/automations/a1/shipped", method, path)
	}
	if body["merge_commit"] != "bead1" || body["source"] != "merge_step" {
		t.Errorf("body = %+v; want commit bead1 source merge_step", body)
	}
}

func TestShippedRecordMCP_MissingCommitIsAToolError(t *testing.T) {
	var calls int
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{"id": "p1"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a missing merge_commit must be a tool error: err=%v res=%+v", err, res)
	}
	if calls != 0 {
		t.Errorf("missing-commit still made %d daemon call(s); want 0", calls)
	}
}

func TestShippedRecordMCP_RefusalIsAToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "404: automation nope not found", http.StatusNotFound)
	})
	res, err := s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{
		"id": "nope", "automation": true, "merge_commit": "bead1",
	}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 404 from the daemon must come back as a tool error: err=%v res=%+v", err, res)
	}
}

func TestShippedRecordMCP_SourceDefaultsBackfill(t *testing.T) {
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"p1","merge_commit":"c0"}`))
	})
	res, err := s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{
		"id": "p1", "merge_commit": "c0", "merged_in_release": "v1.0.0",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if body["source"] != "backfill" {
		t.Errorf("body source = %v; want backfill (the daemon refuses an empty source)", body["source"])
	}
}

func TestShippedRecordMCP_ToolShape(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousShippedRecord()
	if tool.Name != "autonomous_shipped_record" {
		t.Fatalf("name = %q", tool.Name)
	}
	required := strings.Join(tool.InputSchema.Required, ",")
	if !strings.Contains(required, "id") || !strings.Contains(required, "merge_commit") {
		t.Fatalf("id and merge_commit must be required, got %q", required)
	}
	for _, opt := range []string{"merged_in_release", "merged_at", "source", "automation"} {
		if _, ok := tool.InputSchema.Properties[opt]; !ok {
			t.Errorf("tool is missing the optional %q parameter", opt)
		}
	}
}
