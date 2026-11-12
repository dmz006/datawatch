// MCP spec-guard tests for the autonomous edit tools:
// autonomous_prd_edit_task and autonomous_prd_edit_story refuse a new_spec /
// new_description carrying the capped-view truncation marker before anything
// reaches the daemon, with the daemon's own refusal wording (the shared
// autonomous.TruncationRefusal, which the 400 also delegates to). A clean
// spec/description is proxied through as the full text, and the edit tools
// never issue a read request — let alone one with a capped detail.
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

	"github.com/dmz006/datawatch/internal/autonomous"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// editGuardReq is one request the fake daemon received, in arrival order.
type editGuardReq struct {
	method string
	path   string
	query  string
	body   map[string]any
}

// editGuardFake records every request (activateServer pattern) and answers
// with a minimal JSON reply.
type editGuardFake struct{ recs []editGuardReq }

func (f *editGuardFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		f.recs = append(f.recs, editGuardReq{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: m})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"prd-1"}`))
	}
}

func editGuardServer(t *testing.T) (*Server, *editGuardFake) {
	t.Helper()
	f := &editGuardFake{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(strings.TrimPrefix(strings.SplitN(srv.URL, ":", 3)[2], "/"))
	if err != nil {
		t.Fatalf("parse fake daemon port: %v", err)
	}
	return &Server{webPort: port}, f
}

// markerText is a spec excerpt copied out of a capped view: it carries the
// marker capText appends to a cut field.
const markerText = "Task spec\n\n- do the thing\n…[truncated: kept 4096 of 32768 bytes]"

func refusalFor(t *testing.T, field, value string) string {
	t.Helper()
	msg, refuse := autonomous.TruncationRefusal(field, value)
	if !refuse {
		t.Fatalf("TruncationRefusal(%q) should refuse marker text", field)
	}
	return msg
}

func TestToolAutonomousPRDEditTaskFullSpecSentAsWritten(t *testing.T) {
	s, f := editGuardServer(t)
	fullSpec := "Task spec\n\n- step one\n- step two\n- acceptance"
	res, err := s.handleAutonomousPRDEditTask(context.Background(), callReq(map[string]any{
		"id": "prd-1", "task_id": "task-9", "new_spec": fullSpec,
	}))
	if err != nil {
		t.Fatalf("handleAutonomousPRDEditTask: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(f.recs) != 1 {
		t.Fatalf("got %d requests, want exactly the edit write: %v", len(f.recs), f.recs)
	}
	r := f.recs[0]
	if r.method != http.MethodPost || r.path != "/api/autonomous/prds/prd-1/edit_task" {
		t.Errorf("route = %s %s, want POST /api/autonomous/prds/prd-1/edit_task", r.method, r.path)
	}
	if strings.Contains(r.query, "detail") {
		t.Errorf("edit write carries a detail query: %q (a capped read)", r.query)
	}
	if r.body["new_spec"] != fullSpec {
		t.Errorf("new_spec %q != the full spec text passed to the tool", r.body["new_spec"])
	}
}

func TestToolAutonomousPRDEditTaskMarkerSpecRefusedWithoutRequest(t *testing.T) {
	s, f := editGuardServer(t)
	res, err := s.handleAutonomousPRDEditTask(context.Background(), callReq(map[string]any{
		"id": "prd-1", "task_id": "task-9", "new_spec": markerText,
	}))
	if err != nil {
		t.Fatalf("handleAutonomousPRDEditTask: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if !res.IsError {
		t.Error("marker spec accepted by the tool")
	}
	want := refusalFor(t, "new_spec", markerText)
	if got := automationText(t, res); !strings.Contains(got, want) {
		t.Errorf("refusal text %q, want the daemon's wording %q", got, want)
	}
	if len(f.recs) != 0 {
		t.Errorf("marker spec sent %d requests, want none: %v", len(f.recs), f.recs)
	}
}

func TestToolAutonomousPRDEditStoryFullDescriptionSentAsWritten(t *testing.T) {
	s, f := editGuardServer(t)
	description := "Story description\n\nFull text of what the story covers and why."
	res, err := s.handleAutonomousPRDEditStory(context.Background(), callReq(map[string]any{
		"id": "prd-1", "story_id": "story-2", "new_description": description,
	}))
	if err != nil {
		t.Fatalf("handleAutonomousPRDEditStory: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(f.recs) != 1 {
		t.Fatalf("got %d requests, want exactly the edit write: %v", len(f.recs), f.recs)
	}
	r := f.recs[0]
	if r.method != http.MethodPost || r.path != "/api/autonomous/prds/prd-1/edit_story" {
		t.Errorf("route = %s %s, want POST /api/autonomous/prds/prd-1/edit_story", r.method, r.path)
	}
	if r.body["new_description"] != description {
		t.Errorf("new_description %q != the full description passed to the tool", r.body["new_description"])
	}
}

func TestToolAutonomousPRDEditStoryMarkerDescriptionRefusedWithoutRequest(t *testing.T) {
	s, f := editGuardServer(t)
	markerDesc := "Story description\n…[truncated: kept 512 of 40960 bytes]"
	res, err := s.handleAutonomousPRDEditStory(context.Background(), callReq(map[string]any{
		"id": "prd-1", "story_id": "story-2", "new_description": markerDesc,
	}))
	if err != nil {
		t.Fatalf("handleAutonomousPRDEditStory: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if !res.IsError {
		t.Error("marker description accepted by the tool")
	}
	want := refusalFor(t, "new_description", markerDesc)
	if got := automationText(t, res); !strings.Contains(got, want) {
		t.Errorf("refusal text %q, want the daemon's wording %q", got, want)
	}
	if len(f.recs) != 0 {
		t.Errorf("marker description sent %d requests, want none: %v", len(f.recs), f.recs)
	}
}

// TestPrdEditToolDescriptionsAdvertiseFullReads pins the guidance the tools
// advertise: the spec/description arguments want the full text read from
// detail=full, and a text carrying the truncation marker is refused.
func TestPrdEditToolDescriptionsAdvertiseFullReads(t *testing.T) {
	s, _ := editGuardServer(t)
	if d := s.toolAutonomousPRDGet().Description; !strings.Contains(d, "whole and uncut") {
		t.Errorf("autonomous_prd_get description does not say full returns the text whole and uncut: %q", d)
	}
}

// toolParamDesc renders a tool parameter's description so the tests can pin
// the operator guidance on new_spec / new_description.
func toolParamDesc(t *testing.T, tool mcpsdk.Tool, param string) string {
	t.Helper()
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal %s input schema: %v", tool.Name, err)
	}
	var doc struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		t.Fatalf("parse %s input schema: %v", tool.Name, err)
	}
	desc, ok := doc.Properties[param]
	if !ok {
		t.Fatalf("%s has no parameter %q", tool.Name, param)
	}
	return desc.Description
}

func TestPrdEditParamsAdvertiseFullReads(t *testing.T) {
	s, _ := editGuardServer(t)
	cases := []struct {
		tool  mcpsdk.Tool
		param string
	}{
		{s.toolAutonomousPRDEditTask(), "new_spec"},
		{s.toolAutonomousPRDEditStory(), "new_description"},
	}
	for _, c := range cases {
		desc := toolParamDesc(t, c.tool, c.param)
		if !strings.Contains(desc, "detail=full") {
			t.Errorf("%s.%s does not point at the full read: %q", c.tool.Name, c.param, desc)
		}
		if !strings.Contains(desc, "truncation marker") {
			t.Errorf("%s.%s does not mention the truncation marker: %q", c.tool.Name, c.param, desc)
		}
	}
}
