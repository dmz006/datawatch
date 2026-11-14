package mcp

// Tests of the release plan MCP tools. A stub daemon (newPlanRecordTestDaemon)
// records what each tool proxies, so these check the route, the body and the
// refusals that happen before the daemon is reached. Nothing here sleeps.

import (
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// releaseTools maps each new tool to its definition, whether it only reads and
// the capability the federation map must give it.
func releaseTools(s *Server) map[string]struct {
	tool     mcpsdk.Tool
	readOnly bool
} {
	type row = struct {
		tool     mcpsdk.Tool
		readOnly bool
	}
	return map[string]row{
		"plan_theme_list":        {s.toolPlanThemeList(), true},
		"plan_theme_set":         {s.toolPlanThemeSet(), false},
		"plan_archive":           {s.toolPlanArchive(), false},
		"plan_restore":           {s.toolPlanRestore(), false},
		"plan_unattributed_list": {s.toolPlanUnattributedList(), true},
		"plan_attach_bulk":       {s.toolPlanAttachBulk(), false},
		"plan_row_plan_list":     {s.toolPlanRowPlanList(), true},
		"plan_row_plan_convert":  {s.toolPlanRowPlanConvert(), false},
	}
}

func TestPlanReleaseMCP_DefinitionsAndCapabilities(t *testing.T) {
	s := &Server{}
	rows := releaseTools(s)
	if len(rows) != len(PlanReleaseToolNames()) {
		t.Fatalf("PlanReleaseToolNames has %d names, the table %d", len(PlanReleaseToolNames()), len(rows))
	}
	for _, name := range PlanReleaseToolNames() {
		row, ok := rows[name]
		if !ok {
			t.Errorf("%s is listed in PlanReleaseToolNames but not in the table", name)
			continue
		}
		if row.tool.Name != name {
			t.Errorf("%s: tool is named %q", name, row.tool.Name)
		}
		ro := row.tool.Annotations.ReadOnlyHint
		if ro == nil || *ro != row.readOnly {
			t.Errorf("%s: read-only hint = %v, want %v", name, ro, row.readOnly)
		}
		want := federation.CapAutonomousWrite
		if row.readOnly {
			want = federation.CapAutonomousRead
		}
		if got := federation.MCPToolCap[name]; got != want {
			t.Errorf("%s: federation capability = %q, want %q", name, got, want)
		}
		if !federation.IsNonConfigWriteTool(name) {
			t.Errorf("%s is not classified as a non-config-write tool", name)
		}
		if federation.IsConfigWriteTool(name) {
			t.Errorf("%s must not be a config-write tool", name)
		}
		d := row.tool.Description
		if !strings.Contains(d, "docs/howto/plan-records.md") {
			t.Errorf("%s: the description must link the how-to", name)
		}
		for _, banned := range []string{"PRD", "BL4", "GH#"} {
			if strings.Contains(d, banned) {
				t.Errorf("%s: description mentions %q", name, banned)
			}
		}
	}
}

func TestPlanReleaseMCP_ExtendedToolsKeepTheirCapability(t *testing.T) {
	s := &Server{}
	for name, d := range map[string]string{
		"plan_create":  s.toolPlanCreate().Description,
		"plan_migrate": s.toolPlanMigrate().Description,
	} {
		if got := federation.MCPToolCap[name]; got != federation.CapAutonomousWrite {
			t.Errorf("%s: capability = %q, want autonomous write", name, got)
		}
		if !strings.Contains(d, "release") || !strings.Contains(d, "docs/howto/plan-records.md") {
			t.Errorf("%s: the description must cover the release form and link the how-to: %q", name, d)
		}
		for _, banned := range []string{"PRD", "BL4", "GH#"} {
			if strings.Contains(d, banned) {
				t.Errorf("%s: description mentions %q", name, banned)
			}
		}
	}
}

func TestPlanReleaseMCP_Registered(t *testing.T) {
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
	have := map[string]bool{}
	for _, tl := range tools {
		have[tl.Name] = true
	}
	for _, name := range PlanReleaseToolNames() {
		if !have[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}

func TestPlanReleaseMCP_ListsThemes(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"plan":{"title":"Release 2","id":"rel-2","plan_kind":"release"},"themes":[{"id":"t1","name":"Plans"}],"unplaced":[]}`)
	res := d.call(t, d.s.handlePlanThemeList, map[string]any{"plan": "Release 2"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodGet || d.rawPath != "/api/plans/Release%202/themes" {
		t.Fatalf("proxied %s %s", d.method, d.rawPath)
	}
	if !strings.Contains(planResultText(res), `"name":"Plans"`) {
		t.Fatalf("the daemon's themes must come back: %s", planResultText(res))
	}
	if res := d.call(t, d.s.handlePlanThemeList, map[string]any{}); !res.IsError {
		t.Error("a missing plan must be a tool error")
	}
}

func TestPlanReleaseMCP_ThemeSetBodies(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"themes":[]}`)
	cases := []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{"create", map[string]any{"plan": "p", "action": "create", "name": " Plans "}, map[string]any{"action": "create", "name": "Plans"}},
		{"rename", map[string]any{"plan": "p", "action": "RENAME", "theme": "t1", "name": "Pages"}, map[string]any{"action": "rename", "theme": "t1", "name": "Pages"}},
		{"delete", map[string]any{"plan": "p", "action": "delete", "theme": "t1"}, map[string]any{"action": "delete", "theme": "t1"}},
		{"place", map[string]any{"plan": "p", "action": "place", "theme": "t1", "kind": "Automaton", "id": "a1"}, map[string]any{"action": "place", "theme": "t1", "kind": "automaton", "id": "a1"}},
		{"unplace", map[string]any{"plan": "p", "action": "unplace", "kind": "automation", "id": "au1"}, map[string]any{"action": "unplace", "kind": "automation", "id": "au1"}},
	}
	for _, c := range cases {
		res := d.call(t, d.s.handlePlanThemeSet, c.args)
		if res.IsError {
			t.Errorf("%s: %s", c.name, planResultText(res))
			continue
		}
		if d.method != http.MethodPost || d.path != "/api/plans/p/themes" {
			t.Errorf("%s: proxied %s %s", c.name, d.method, d.path)
		}
		got := d.bodyMap(t)
		if len(got) != len(c.want) {
			t.Errorf("%s: body = %v, want %v", c.name, got, c.want)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: body[%s] = %v, want %v", c.name, k, got[k], v)
			}
		}
	}
	d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "reorder", "themes": []any{"b", "a"}})
	if got := anyStrings(d.bodyMap(t)["themes"]); len(got) != 2 || got[0] != "b" {
		t.Errorf("reorder body = %v", d.rawBody)
	}
	d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "reorder_items", "theme": "t1", "kind": "automaton", "items": `["x","y"]`})
	b := d.bodyMap(t)
	if got := anyStrings(b["items"]); len(got) != 2 || got[1] != "y" || b["theme"] != "t1" || b["kind"] != "automaton" {
		t.Errorf("reorder_items body = %v", d.rawBody)
	}
}

func TestPlanReleaseMCP_RefusesBadArgumentsBeforeTheDaemon(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{}`)
	type call struct {
		name string
		do   func() *mcpsdk.CallToolResult
	}
	calls := []call{
		{"theme set no action", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p"})
		}},
		{"theme set bad action", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "explode"})
		}},
		{"theme create no name", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "create"})
		}},
		{"theme rename no new name", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "rename", "theme": "t"})
		}},
		{"theme place bad kind", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "place", "theme": "t", "kind": "finding", "id": "x"})
		}},
		{"theme place no theme", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "place", "kind": "automaton", "id": "x"})
		}},
		{"theme reorder empty", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"plan": "p", "action": "reorder", "themes": []any{}})
		}},
		{"theme set no plan", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanThemeSet, map[string]any{"action": "create", "name": "n"})
		}},
		{"archive no plan", func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanArchive, map[string]any{}) }},
		{"archive reason not text", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanArchive, map[string]any{"plan": "p", "reason": 5})
		}},
		{"restore no plan", func() *mcpsdk.CallToolResult { return d.call(t, d.s.handlePlanRestore, map[string]any{}) }},
		{"unattributed bad limit", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanUnattributedList, map[string]any{"limit": 0})
		}},
		{"bulk attach no ids", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanAttachBulk, map[string]any{"plan": "p"})
		}},
		{"bulk attach blank id", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanAttachBulk, map[string]any{"plan": "p", "ids": []any{"a", " "}})
		}},
		{"bulk attach finding kind", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanAttachBulk, map[string]any{"plan": "p", "ids": []any{"a"}, "kind": "finding"})
		}},
		{"row plan convert bad apply", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanRowPlanConvert, map[string]any{"apply": "maybe"})
		}},
		{"create release without label", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p", "title": "t", "plan_kind": "release"})
		}},
		{"create bad plan kind", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p", "title": "t", "plan_kind": "grand"})
		}},
		{"create label on an ordinary plan", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p", "title": "t", "release": "2.0"})
		}},
		{"migrate bad mode", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "sideways"})
		}},
		{"migrate release no project", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "release"})
		}},
		{"migrate arcs with release field", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanMigrate, map[string]any{"project": "p"})
		}},
		{"migrate release bad marks", func() *mcpsdk.CallToolResult {
			return d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "release", "project": "p", "marks": "[1]"})
		}},
	}
	for _, c := range calls {
		d.method = ""
		if res := c.do(); !res.IsError {
			t.Errorf("%s: want a tool error", c.name)
		}
		if d.method != "" {
			t.Errorf("%s: the refused call reached the daemon (%s %s)", c.name, d.method, d.path)
		}
	}
}

func TestPlanReleaseMCP_ArchiveAndRestore(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"plan":{"title":"Old","id":"old","archived":true},"automata_held":0,"changed":true}`)
	res := d.call(t, d.s.handlePlanArchive, map[string]any{"plan": "Old plan", "reason": " done ", "protected_plan": "rel-2"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPost || d.rawPath != "/api/plans/Old%20plan/archive" {
		t.Fatalf("archive proxied %s %s", d.method, d.rawPath)
	}
	if b := d.bodyMap(t); b["reason"] != "done" || b["protected_plan"] != "rel-2" || len(b) != 2 {
		t.Fatalf("archive body %v", b)
	}
	if !strings.Contains(planResultText(res), `"archived":true`) {
		t.Fatalf("the daemon's answer must come back: %s", planResultText(res))
	}

	// With nothing to say the body is empty, not null.
	d.call(t, d.s.handlePlanArchive, map[string]any{"plan": "old"})
	if b := d.bodyMap(t); len(b) != 0 {
		t.Fatalf("archive body %v, want {}", b)
	}

	d.reply = `{"plan":{"title":"Old","id":"old"},"changed":true}`
	res = d.call(t, d.s.handlePlanRestore, map[string]any{"plan": "old"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans/old/restore" {
		t.Fatalf("restore proxied %s %s", d.method, d.path)
	}
}

func TestPlanReleaseMCP_ArchiveProtectedPlanRefusalSurfaces(t *testing.T) {
	d := newPlanRecordTestDaemon(t, http.StatusConflict, `{"error":"plan \"Release 2\" holds 3 Automata and is protected","plan_id":"rel-2","automata_held":3}`)
	res := d.call(t, d.s.handlePlanArchive, map[string]any{"plan": "rel-2", "protected_plan": "rel-2"})
	if !res.IsError || !strings.Contains(planResultText(res), "protected") {
		t.Fatalf("the refusal must surface: %v %s", res.IsError, planResultText(res))
	}
}

func TestPlanReleaseMCP_UnattributedQuery(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"automata":[{"title":"Held","id":"a1","holding_plan":"Old plan","holding_plan_id":"old"}],"page":{"total":1}}`)
	res := d.call(t, d.s.handlePlanUnattributedList, map[string]any{
		"project": "datawatch", "current_release": "2.0", "next_release": "2.1", "limit": 20, "offset": 40})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodGet || d.path != "/api/plans/unattributed" {
		t.Fatalf("proxied %s %s", d.method, d.path)
	}
	for k, want := range map[string]string{"project": "datawatch", "current_release": "2.0", "next_release": "2.1", "limit": "20", "offset": "40"} {
		if q := d.query[k]; len(q) != 1 || q[0] != want {
			t.Errorf("query %s = %v, want %s", k, q, want)
		}
	}
	// An Automaton an ordinary plan holds is reported with that plan.
	if !strings.Contains(planResultText(res), `"holding_plan":"Old plan"`) {
		t.Fatalf("the holding plan must come back: %s", planResultText(res))
	}
}

func TestPlanReleaseMCP_BulkAttachReportsARefusedAutomaton(t *testing.T) {
	reply := `{"attached":"automaton","plan":{"title":"Release 2","id":"rel-2"},"theme":"t1","counts":{"attached":1,"refused":1},` +
		`"results":[{"id":"a1","title":"One","status":"attached","theme":"t1"},` +
		`{"id":"a2","title":"Two","status":"refused","holder":"old","error":"automaton \"a2\" is already linked to old; detach it first"}]}`
	d := newPlanRecordTestDaemon(t, 0, reply)
	res := d.call(t, d.s.handlePlanAttachBulk, map[string]any{"plan": "rel-2", "ids": []any{" a1 ", "a2"}, "theme": "Plans"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans/rel-2/attach" {
		t.Fatalf("proxied %s %s", d.method, d.path)
	}
	b := d.bodyMap(t)
	if got := anyStrings(b["ids"]); len(got) != 2 || got[0] != "a1" || b["kind"] != "automaton" || b["theme"] != "Plans" {
		t.Fatalf("body %s", d.rawBody)
	}
	if _, has := b["id"]; has {
		t.Fatalf("a bulk attach must not send id: %s", d.rawBody)
	}
	text := planResultText(res)
	for _, want := range []string{`"status":"refused"`, `"holder":"old"`, "detach it first"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refused Automaton must be reported (missing %q): %s", want, text)
		}
	}
	if res.IsError {
		t.Error("a refused id is a result line, not a failed call")
	}
	// A JSON array string and the automation kind work the same.
	d.call(t, d.s.handlePlanAttachBulk, map[string]any{"plan": "rel-2", "ids": `["u1"]`, "kind": "automation"})
	if b := d.bodyMap(t); b["kind"] != "automation" || len(anyStrings(b["ids"])) != 1 {
		t.Fatalf("body %s", d.rawBody)
	}
}

func TestPlanReleaseMCP_RowPlansListAndPreviewDefault(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"row_plans":[{"title":"Plan: row","id":"p1"}],"total":1}`)
	if res := d.call(t, d.s.handlePlanRowPlanList, map[string]any{"project": "datawatch"}); res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodGet || d.path != "/api/plans/migrate/row-plans" || (len(d.query["project"]) != 1 || d.query["project"][0] != "datawatch") {
		t.Fatalf("list proxied %s %s %v", d.method, d.path, d.query)
	}

	d.reply = `{"note":"Preview only","applied":false,"preview_id":"abc123","counts":{}}`
	res := d.call(t, d.s.handlePlanRowPlanConvert, map[string]any{"project": "datawatch", "current_release": "2.0"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans/migrate/row-plans" {
		t.Fatalf("convert proxied %s %s", d.method, d.path)
	}
	if b := d.bodyMap(t); b["apply"] != false || b["project"] != "datawatch" || b["current_release"] != "2.0" {
		t.Fatalf("convert body = %v, want a preview", b)
	}
	if !strings.Contains(planResultText(res), "abc123") {
		t.Fatalf("the preview id must come back: %s", planResultText(res))
	}
}

func TestPlanReleaseMCP_ConvertWritesOnlyWithThePreviewID(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"applied":true,"counts":{}}`)
	for _, args := range []map[string]any{
		{"apply": true},
		{"apply": "true", "preview_id": "  "},
		{"apply": true, "project": "datawatch"},
	} {
		d.method = ""
		res := d.call(t, d.s.handlePlanRowPlanConvert, args)
		if !res.IsError || !strings.Contains(planResultText(res), "preview_id") {
			t.Errorf("%v: a write without the preview id must be refused: %s", args, planResultText(res))
		}
		if d.method != "" {
			t.Errorf("%v: the refused write reached the daemon", args)
		}
	}
	res := d.call(t, d.s.handlePlanRowPlanConvert, map[string]any{"apply": true, "preview_id": "abc123"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if b := d.bodyMap(t); b["apply"] != true || b["preview_id"] != "abc123" {
		t.Fatalf("body = %v", b)
	}
}

func TestPlanReleaseMCP_ConvertStalePreviewSurfaces(t *testing.T) {
	d := newPlanRecordTestDaemon(t, http.StatusConflict, `{"error":"the plans changed since the preview; preview again"}`)
	res := d.call(t, d.s.handlePlanRowPlanConvert, map[string]any{"apply": true, "preview_id": "old"})
	if !res.IsError || !strings.Contains(planResultText(res), "preview again") {
		t.Fatalf("a stale preview must surface: %v %s", res.IsError, planResultText(res))
	}
}

func TestPlanReleaseMCP_MigrateReleaseModePreviewThenApply(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"mode":"release","applied":false,"preview_id":"p-1","counts":{}}`)
	res := d.call(t, d.s.handlePlanMigrate, map[string]any{
		"mode": "release", "project": "datawatch", "current_release": "2.0", "next_release": "2.1",
		"marks": `{"a1":{"release":"2.1","theme":"Plans"}}`})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if d.method != http.MethodPost || d.path != "/api/plans/migrate" {
		t.Fatalf("proxied %s %s", d.method, d.path)
	}
	b := d.bodyMap(t)
	if b["mode"] != "release" || b["apply"] != false || b["project"] != "datawatch" || b["current_release"] != "2.0" || b["next_release"] != "2.1" {
		t.Fatalf("preview body = %v", b)
	}
	if _, has := b["preview_id"]; has {
		t.Fatalf("a preview carries no preview id: %v", b)
	}
	marks, _ := b["marks"].(map[string]any)
	if mk, _ := marks["a1"].(map[string]any); mk["theme"] != "Plans" {
		t.Fatalf("marks = %v", b["marks"])
	}

	// A write without the preview id never reaches the daemon.
	d.method = ""
	res = d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "release", "project": "datawatch", "apply": true})
	if !res.IsError || !strings.Contains(planResultText(res), "preview_id") || d.method != "" {
		t.Fatalf("a write without the preview id must be refused: %v %s (daemon reached: %q)", res.IsError, planResultText(res), d.method)
	}

	res = d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "release", "project": "datawatch", "apply": true, "preview_id": "p-1"})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if b := d.bodyMap(t); b["apply"] != true || b["preview_id"] != "p-1" || b["mode"] != "release" {
		t.Fatalf("apply body = %v", b)
	}
}

func TestPlanReleaseMCP_MigrateReleaseStaleAndConflictSurface(t *testing.T) {
	d := newPlanRecordTestDaemon(t, http.StatusConflict, `{"error":"the preview is stale: the plans changed since you read it"}`)
	res := d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "release", "project": "p", "apply": true, "preview_id": "old"})
	if !res.IsError || !strings.Contains(planResultText(res), "stale") {
		t.Fatalf("a stale preview must surface: %v %s", res.IsError, planResultText(res))
	}

	// A refused Automaton is a conflict line in the preview answer, not a failure.
	d2 := newPlanRecordTestDaemon(t, 0, `{"mode":"release","applied":false,"preview_id":"p-2","conflicts":[{"kind":"automaton","id":"a9","reason":"held by plan old"}]}`)
	res = d2.call(t, d2.s.handlePlanMigrate, map[string]any{"mode": "release", "project": "p"})
	if res.IsError || !strings.Contains(planResultText(res), "held by plan old") {
		t.Fatalf("the conflict must be reported: %v %s", res.IsError, planResultText(res))
	}
}

func TestPlanReleaseMCP_ArcsMigrationIsUnchanged(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"counts":{}}`)
	d.call(t, d.s.handlePlanMigrate, map[string]any{"mode": "arcs", "apply": true})
	b := d.bodyMap(t)
	if len(b) != 1 || b["apply"] != true {
		t.Fatalf("the arcs migration body = %v, want only apply", b)
	}
}

func TestPlanReleaseMCP_CreateReleasePlan(t *testing.T) {
	d := newPlanRecordTestDaemon(t, 0, `{"plan":{"title":"Release 2.1","id":"rel-2-1","plan_kind":"release","release":"2.1"}}`)
	res := d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p", "title": "Release 2.1", "plan_kind": "Release", "release": " 2.1 "})
	if res.IsError {
		t.Fatal(planResultText(res))
	}
	if b := d.bodyMap(t); b["plan_kind"] != "release" || b["release"] != "2.1" || b["project"] != "p" {
		t.Fatalf("create body = %v", b)
	}
	// An ordinary plan sends neither field.
	d.call(t, d.s.handlePlanCreate, map[string]any{"project": "p", "title": "T"})
	b := d.bodyMap(t)
	if _, has := b["plan_kind"]; has {
		t.Fatalf("an ordinary plan must not send plan_kind: %v", b)
	}
	if _, has := b["release"]; has {
		t.Fatalf("an ordinary plan must not send release: %v", b)
	}
}
