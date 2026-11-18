package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// planRecordTestDaemon records the last proxied request and answers canned JSON.
type planRecordTestDaemon struct {
	s       *Server
	method  string
	path    string
	rawPath string
	query   map[string][]string
	rawBody string
	status  int
	reply   string
}

func newPlanRecordTestDaemon(t *testing.T, status int, reply string) *planRecordTestDaemon {
	t.Helper()
	d := &planRecordTestDaemon{status: status, reply: reply}
	d.s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		d.method, d.path, d.rawPath, d.query = r.Method, r.URL.Path, r.URL.EscapedPath(), r.URL.Query()
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			d.rawBody = string(b)
		}
		if d.status != 0 {
			w.WriteHeader(d.status)
		}
		_, _ = w.Write([]byte(d.reply))
	})
	return d
}

func (d *planRecordTestDaemon) call(t *testing.T, h func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error), args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := h(context.Background(), callReq(args))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func (d *planRecordTestDaemon) bodyMap(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(d.rawBody), &m); err != nil {
		t.Fatalf("decode body %q: %v", d.rawBody, err)
	}
	return m
}

func planResultText(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestPlanRecordMCP_Registered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, tl := range tools {
		byName[tl.Name] = true
	}
	for _, name := range PlanRecordToolNames() {
		if !byName[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
	built := map[string]mcpsdk.Tool{
		"plan_record_list": s.toolPlanRecordList(), "plan_record_get": s.toolPlanRecordGet(),
		"plan_record_search": s.toolPlanRecordSearch(), "plan_record_create": s.toolPlanRecordCreate(),
		"plan_record_update": s.toolPlanRecordUpdate(), "plan_record_import": s.toolPlanRecordImport(),
	}
	wantRO := map[string]bool{"plan_record_list": true, "plan_record_get": true, "plan_record_search": true,
		"plan_record_create": false, "plan_record_update": false, "plan_record_import": false}
	wantCap := map[string]string{
		"plan_record_list": federation.CapAutonomousRead, "plan_record_get": federation.CapAutonomousRead,
		"plan_record_search": federation.CapAutonomousRead, "plan_record_create": federation.CapAutonomousWrite,
		"plan_record_update": federation.CapAutonomousWrite, "plan_record_import": federation.CapAutonomousWrite,
	}
	for name, want := range wantRO {
		hint := built[name].Annotations.ReadOnlyHint
		if hint == nil || *hint != want {
			t.Errorf("%q read-only hint = %v, want %v", name, hint, want)
		}
		if got := federation.MCPToolCap[name]; got != wantCap[name] {
			t.Errorf("federation.MCPToolCap[%q] = %q, want %q", name, got, wantCap[name])
		}
		if !federation.IsNonConfigWriteTool(name) {
			t.Errorf("%q is not classified as a non-config-write tool", name)
		}
		desc := built[name].Description
		for _, banned := range []string{"PRD", "BL4", "GH#"} {
			if strings.Contains(desc, banned) {
				t.Errorf("%q description mentions %q", name, banned)
			}
		}
	}
}

func TestPlanRecordMCP_ListForwardsFilters(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"records":[],"total":0}`)
	res := d.call(t, d.s.handlePlanRecordList, map[string]any{
		"kind": "registry-row", "project": "datawatch", "state": "open", "q": "restart", "limit": 25, "offset": 5,
	})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodGet || d.path != "/api/plan-records/registry-rows" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	for k, want := range map[string]string{"project": "datawatch", "state": "open", "q": "restart", "limit": "25", "offset": "5"} {
		if q := d.query[k]; len(q) != 1 || q[0] != want {
			t.Errorf("query %s = %v, want %s", k, q, want)
		}
	}
	d.call(t, d.s.handlePlanRecordList, map[string]any{"kind": "finding"})
	if d.path != "/api/plan-records/findings" {
		t.Errorf("finding list proxied %s", d.path)
	}
}

func TestPlanRecordMCP_ArgumentValidation(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{}`)
	cases := []struct {
		name string
		h    func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
		args map[string]any
		want string
	}{
		{"list no kind", d.s.handlePlanRecordList, map[string]any{}, "kind is required"},
		{"list bad kind", d.s.handlePlanRecordList, map[string]any{"kind": "story"}, "not valid"},
		{"list fractional limit", d.s.handlePlanRecordList, map[string]any{"kind": "finding", "limit": 1.5}, "whole number"},
		{"get no ref", d.s.handlePlanRecordGet, map[string]any{"kind": "finding"}, "ref is required"},
		{"get no kind", d.s.handlePlanRecordGet, map[string]any{"ref": "x"}, "kind is required"},
		{"create no project", d.s.handlePlanRecordCreate, map[string]any{"kind": "finding", "title": "t"}, "project is required"},
		{"create no title", d.s.handlePlanRecordCreate, map[string]any{"kind": "finding", "project": "datawatch"}, "title is required"},
		{"create bad links", d.s.handlePlanRecordCreate, map[string]any{"kind": "finding", "project": "p", "title": "t", "links": "nope"}, "JSON array"},
		{"update no ref", d.s.handlePlanRecordUpdate, map[string]any{"kind": "finding", "title": "t"}, "ref is required"},
		{"update nothing", d.s.handlePlanRecordUpdate, map[string]any{"kind": "finding", "ref": "r"}, "nothing to change"},
		{"update legacy number", d.s.handlePlanRecordUpdate, map[string]any{"kind": "registry-row", "ref": "r", "legacy_number": 3}, "cannot change"},
		{"search no q", d.s.handlePlanRecordSearch, map[string]any{}, "q is required"},
		{"search bad kind", d.s.handlePlanRecordSearch, map[string]any{"q": "x", "kind": "zzz"}, "not valid"},
		{"import bad source", d.s.handlePlanRecordImport, map[string]any{"source": "everything"}, "source"},
		{"import bad policy", d.s.handlePlanRecordImport, map[string]any{"conflict_policy": "overwrite"}, "conflict_policy"},
		{"import bad dry_run", d.s.handlePlanRecordImport, map[string]any{"dry_run": "maybe"}, "dry_run"},
	}
	for _, c := range cases {
		res := d.call(t, c.h, c.args)
		if !res.IsError {
			t.Errorf("%s: want a tool error", c.name)
			continue
		}
		if txt := planResultText(res); !strings.Contains(txt, c.want) {
			t.Errorf("%s: error %q does not contain %q", c.name, txt, c.want)
		}
	}
	if d.method != "" {
		t.Errorf("a refused call reached the daemon: %s %s", d.method, d.path)
	}
}

func TestPlanRecordMCP_GetEscapesRefAndForwardsProject(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"record":{"title":"T","id":"pr-1"}}`)
	res := d.call(t, d.s.handlePlanRecordGet, map[string]any{"kind": "registry_row", "ref": "row 12", "project": "datawatch"})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodGet || d.path != "/api/plan-records/registry-rows/row 12" || !strings.Contains(d.rawPath, "row%2012") {
		t.Errorf("proxied %s %s (%s)", d.method, d.path, d.rawPath)
	}
	if q := d.query["project"]; len(q) != 1 || q[0] != "datawatch" {
		t.Errorf("project query = %v", q)
	}
	if planResultText(res) != `{"record":{"title":"T","id":"pr-1"}}` {
		t.Errorf("body not passed through: %s", planResultText(res))
	}
}

func TestPlanRecordMCP_AmbiguousReferenceReturnsCandidates(t *testing.T) {
	body := `{"error":"\"Fix\" matches more than one record; say which one (use its id)","candidates":[{"title":"Fix A","id":"a"},{"title":"Fix B","id":"b"}]}`
	d := newPlanRecordTestDaemon(t, http.StatusConflict, body)
	res := d.call(t, d.s.handlePlanRecordGet, map[string]any{"kind": "finding", "ref": "Fix"})
	if !res.IsError {
		t.Fatal("an ambiguous reference must be a tool error")
	}
	txt := planResultText(res)
	for _, want := range []string{"HTTP 409", "candidates", "Fix A", "Fix B", "use its id"} {
		if !strings.Contains(txt, want) {
			t.Errorf("error %q lacks %q", txt, want)
		}
	}
}

func TestPlanRecordMCP_CreateForwardsContent(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"record":{"title":"Plan it","id":"pr-1"}}`)
	res := d.call(t, d.s.handlePlanRecordCreate, map[string]any{
		"kind": "registry-row", "project": "datawatch", "title": " Plan it ", "body": "text",
		"state": "planned", "legacy_number": 12, "links": `[{"kind":"doc","ref":"docs/x.md"}]`,
		"aliases": []any{"Old name"}, "needs_review": true, "review_reason": "suffixed",
	})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plan-records/registry-rows" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	body := d.bodyMap(t)
	for k, want := range map[string]any{"project": "datawatch", "title": "Plan it", "body": "text", "state": "planned",
		"legacy_number": float64(12), "needs_review": true, "review_reason": "suffixed"} {
		if body[k] != want {
			t.Errorf("body[%s] = %v, want %v", k, body[k], want)
		}
	}
	if l, ok := body["links"].([]any); !ok || len(l) != 1 {
		t.Errorf("links = %v", body["links"])
	}
	if a, ok := body["aliases"].([]any); !ok || len(a) != 1 {
		t.Errorf("aliases = %v", body["aliases"])
	}
	if _, has := body["kind"]; has {
		t.Error("kind must come from the route, not the body")
	}
}

func TestPlanRecordMCP_UpdateSendsOnlyGivenFields(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"record":{"title":"New","id":"pr-1"}}`)
	res := d.call(t, d.s.handlePlanRecordUpdate, map[string]any{
		"kind": "finding", "ref": "pr-1", "title": "New", "state": "fixed", "review_reason": "", "add_aliases": `["Old"]`,
	})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPut || d.path != "/api/plan-records/findings/pr-1" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	body := d.bodyMap(t)
	if body["title"] != "New" || body["state"] != "fixed" {
		t.Errorf("body = %v", body)
	}
	if v, has := body["review_reason"]; !has || v != "" {
		t.Errorf("an empty review_reason must be sent so it can be cleared: %v", body)
	}
	if _, has := body["body"]; has {
		t.Errorf("body field sent although not given: %v", body)
	}
	if a, ok := body["add_aliases"].([]any); !ok || len(a) != 1 {
		t.Errorf("add_aliases = %v", body["add_aliases"])
	}
}

func TestPlanRecordMCP_SearchForwardsQueryAndKind(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"hits":[]}`)
	res := d.call(t, d.s.handlePlanRecordSearch, map[string]any{"q": "boot resume", "kind": "finding", "project": "datawatch", "limit": 5})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodGet || d.path != "/api/plan-records/search" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	for k, want := range map[string]string{"q": "boot resume", "kind": "finding", "project": "datawatch", "limit": "5"} {
		if q := d.query[k]; len(q) != 1 || q[0] != want {
			t.Errorf("query %s = %v, want %s", k, q, want)
		}
	}
}

func TestPlanRecordMCP_ImportDefaultsToPreview(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"note":"Preview only: nothing was written.","result":{}}`)
	res := d.call(t, d.s.handlePlanRecordImport, map[string]any{"project": "datawatch", "source": "registry", "conflict_policy": "skip"})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plan-records/import" {
		t.Errorf("proxied %s %s", d.method, d.path)
	}
	if q := d.query["dry_run"]; len(q) != 1 || q[0] != "true" {
		t.Errorf("dry_run query = %v, want true by default", q)
	}
	body := d.bodyMap(t)
	if body["project"] != "datawatch" || body["source"] != "registry" || body["conflict_policy"] != "skip" {
		t.Errorf("body = %v", body)
	}
	d.call(t, d.s.handlePlanRecordImport, map[string]any{"dry_run": false})
	if q := d.query["dry_run"]; len(q) != 1 || q[0] != "false" {
		t.Errorf("dry_run query = %v, want false when asked", q)
	}
	d.call(t, d.s.handlePlanRecordImport, map[string]any{"dry_run": "true"})
	if q := d.query["dry_run"]; len(q) != 1 || q[0] != "true" {
		t.Errorf("dry_run text true gave %v", q)
	}
}

func TestPlanRecordMCP_DaemonErrorSurfaces(t *testing.T) {
	d := newPlanRecordTestDaemon(t, http.StatusNotFound, `{"error":"no plan record matches that reference"}`)
	res := d.call(t, d.s.handlePlanRecordGet, map[string]any{"kind": "finding", "ref": "zzz"})
	if !res.IsError || !strings.Contains(planResultText(res), "HTTP 404") {
		t.Errorf("want a 404 tool error, got %q", planResultText(res))
	}
}
