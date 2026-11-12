package mcp

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

// pendingItemPRDWord matches the word PRD; the parameter name prd_id is not
// the word.
var pendingItemPRDWord = regexp.MustCompile(`(?i)\bprd\b`)

func TestPendingItemToolRead(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range PendingItemToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
	tool := (&Server{}).toolAutonomousPendingItems()
	if hint := tool.Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_pending_items must be marked read-only")
	}
	if pendingItemPRDWord.MatchString(tool.Description) {
		t.Errorf("description must say Automaton, not PRD: %s", tool.Description)
	}

	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"policy":"hold","items":[{"id":"ab12cd34","state":"waiting"}],"held":[]}`))
	})
	res, err := s.handleAutonomousPendingItems(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/pending-items" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s; want the route with no filter", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, `"id":"ab12cd34"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}

	res, err = s.handleAutonomousPendingItems(context.Background(), callReq(map[string]any{
		"state": "waiting", "automation_id": "feed0001"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if path != "/api/autonomous/pending-items" || rawQuery != "automation_id=feed0001&state=waiting" {
		t.Errorf("proxied %s?%s; want both filters", path, rawQuery)
	}
}

func TestPendingItemToolAdd(t *testing.T) {
	s, rec := activateServer(t, http.StatusCreated, `{"id":"ab12cd34","title":"Write the guide","state":"waiting"}`)
	res, err := s.handleAutonomousPendingItemAdd(context.Background(), callReq(map[string]any{
		"title": "  Write the guide ", "ref": " T-7 "}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/pending-items" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["title"] != "Write the guide" || rec.body["ref"] != "T-7" {
		t.Errorf("body = %v; want the trimmed title and ref", rec.body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"id":"ab12cd34"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}

	// The ref is optional and is not sent when absent.
	if _, err := s.handleAutonomousPendingItemAdd(context.Background(), callReq(map[string]any{"title": "No label"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.body["ref"]; ok {
		t.Errorf("an absent ref leaked into the body: %v", rec.body)
	}

	// A REST refusal comes back as a tool error that keeps the status word.
	s, _ = activateServer(t, http.StatusBadRequest, "400: title is required")
	res, err = s.handleAutonomousPendingItemAdd(context.Background(), callReq(map[string]any{"title": "x"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want an error result, got err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, "400: title is required") {
		t.Errorf("error lost the REST text: %s", text)
	}
}

func TestPendingItemToolAttach(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"item":{"id":"ab12cd34","state":"attached"}}`)
	res, err := s.handleAutonomousPendingItemAttach(context.Background(), callReq(map[string]any{
		"item_id": "ab12cd34", "automaton_id": "feed0001"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/pending-items/ab12cd34/attach" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["automaton_id"] != "feed0001" {
		t.Errorf("body = %v; want the automaton id", rec.body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"state":"attached"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}

	s, _ = activateServer(t, http.StatusConflict, "409: item is not waiting")
	res, err = s.handleAutonomousPendingItemAttach(context.Background(), callReq(map[string]any{
		"item_id": "ab12cd34", "automaton_id": "feed0001"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want an error result, got err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, "409: item is not waiting") {
		t.Errorf("error lost the REST text: %s", text)
	}
}

func TestPendingItemToolRefresh(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"configured":true,"rows":3,"added":2,"updated":1}`)
	res, err := s.handleAutonomousPendingItemsRefresh(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/pending-items/refresh" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"added":2`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestPendingItemToolConfigSet(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	names := configSetArgNames()
	for _, p := range pendingItemConfigParams {
		if !strings.Contains(names, `"`+p.param+`"`) {
			t.Errorf("autonomous_config_set does not declare %s", p.param)
		}
	}
	if len(pendingItemConfigParams) != 9 {
		t.Errorf("%d pending item config parameters, want 9", len(pendingItemConfigParams))
	}

	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_pending_item_policy":                "skip",
		"automation_pending_item_auto_resume":           false,
		"automation_pending_item_source_path":           "docs/registry.md",
		"automation_pending_item_source_id_pattern":     `^[A-Z]+-\d+$`,
		"automation_pending_item_source_id_column":      "ID",
		"automation_pending_item_source_title_column":   "3",
		"automation_pending_item_source_state_column":   "Status",
		"automation_pending_item_source_done_values":    []any{"done", "wontfix"},
		"automation_pending_item_source_on_missing_row": "drop",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("config set: err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/autonomous/config" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if auto["pending_item_policy"] != "skip" || auto["pending_item_auto_resume"] != false {
		t.Errorf("automation = %v; want policy skip and auto resume false", rec.body["automation"])
	}
	src, _ := auto["pending_item_source"].(map[string]any)
	want := map[string]any{
		"path": "docs/registry.md", "id_pattern": `^[A-Z]+-\d+$`, "id_column": "ID",
		"title_column": "3", "state_column": "Status", "on_missing_row": "drop",
	}
	for k, v := range want {
		if src[k] != v {
			t.Errorf("pending_item_source.%s = %v, want %v", k, src[k], v)
		}
	}
	if dv, _ := src["done_values"].([]any); len(dv) != 2 || dv[0] != "done" || dv[1] != "wontfix" {
		t.Errorf("done_values = %v, want [done wontfix]", src["done_values"])
	}

	// An explicit empty string clears one source field and an explicit empty
	// list is sent; absent arguments send nothing.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_pending_item_source_path":        "",
		"automation_pending_item_source_done_values": []any{},
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ = rec.body["automation"].(map[string]any)
	src, _ = auto["pending_item_source"].(map[string]any)
	if p, ok := src["path"]; !ok || p != "" {
		t.Errorf("an explicit empty path was not sent: %v", src)
	}
	if dv, ok := src["done_values"].([]any); !ok || len(dv) != 0 {
		t.Errorf("an explicit empty list was not sent: %v", src)
	}
	if _, ok := auto["pending_item_policy"]; ok {
		t.Errorf("policy leaked into a source-only PUT: %v", auto)
	}

	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_idle_alert_minutes": 0,
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ = rec.body["automation"].(map[string]any)
	for _, k := range []string{"pending_item_policy", "pending_item_auto_resume", "pending_item_source"} {
		if _, ok := auto[k]; ok {
			t.Errorf("%s leaked into an idle-only PUT: %v", k, rec.body)
		}
	}
}

func TestPendingItemToolInvalidArguments(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	ctx := context.Background()

	calls := map[string]func() (string, bool){
		"read bad state": func() (string, bool) {
			r, err := s.handleAutonomousPendingItems(ctx, callReq(map[string]any{"state": "finished"}))
			return pendingItemResultText(t, r, err)
		},
		"add no title": func() (string, bool) {
			r, err := s.handleAutonomousPendingItemAdd(ctx, callReq(nil))
			return pendingItemResultText(t, r, err)
		},
		"add blank title": func() (string, bool) {
			r, err := s.handleAutonomousPendingItemAdd(ctx, callReq(map[string]any{"title": "   "}))
			return pendingItemResultText(t, r, err)
		},
		"attach no item": func() (string, bool) {
			r, err := s.handleAutonomousPendingItemAttach(ctx, callReq(map[string]any{"automaton_id": "feed0001"}))
			return pendingItemResultText(t, r, err)
		},
		"attach item with a slash": func() (string, bool) {
			r, err := s.handleAutonomousPendingItemAttach(ctx, callReq(map[string]any{"item_id": "a/b", "automaton_id": "feed0001"}))
			return pendingItemResultText(t, r, err)
		},
		"attach no automaton": func() (string, bool) {
			r, err := s.handleAutonomousPendingItemAttach(ctx, callReq(map[string]any{"item_id": "ab12cd34"}))
			return pendingItemResultText(t, r, err)
		},
		"config bad policy": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_policy": "wait"}))
			return pendingItemResultText(t, r, err)
		},
		"config bad pattern": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_source_id_pattern": "(["}))
			return pendingItemResultText(t, r, err)
		},
		"config bad on missing row": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_source_on_missing_row": "forget"}))
			return pendingItemResultText(t, r, err)
		},
		"config blank column": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_source_id_column": "  "}))
			return pendingItemResultText(t, r, err)
		},
		"config blank done word": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_source_done_values": []any{"done", " "}}))
			return pendingItemResultText(t, r, err)
		},
		"config auto resume wrong type": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_auto_resume": "yes"}))
			return pendingItemResultText(t, r, err)
		},
		"config done values wrong type": func() (string, bool) {
			r, err := s.handleAutonomousConfigSet(ctx, callReq(map[string]any{"automation_pending_item_source_done_values": []any{1, 2}}))
			return pendingItemResultText(t, r, err)
		},
	}
	for name, call := range calls {
		text, isErr := call()
		if !isErr {
			t.Errorf("%s: want an error result, got %s", name, text)
		}
		if pendingItemPRDWord.MatchString(text) {
			t.Errorf("%s: error text must say Automaton, not PRD: %s", name, text)
		}
	}
	if rec.method != "" {
		t.Errorf("a rejected call reached the server: %s %s", rec.method, rec.path)
	}
}

// pendingItemResultText returns the text of a tool result and whether it is an
// error: a Go error, an IsError result, or text starting "error:".
func pendingItemResultText(t *testing.T, res *mcpsdk.CallToolResult, err error) (string, bool) {
	t.Helper()
	if err != nil {
		return err.Error(), true
	}
	if res == nil {
		t.Fatal("nil tool result")
	}
	text := automationText(t, res)
	return text, res.IsError || strings.HasPrefix(text, "error:")
}

func TestPendingItemToolInsertMemberItemID(t *testing.T) {
	tool := (&Server{}).toolAutomationInsertMember()
	if _, ok := tool.InputSchema.Properties["pending_item_id"]; !ok {
		t.Error("automation_insert_member does not declare pending_item_id")
	}
	if pendingItemPRDWord.MatchString(tool.Description) {
		t.Errorf("description must say Automaton, not PRD: %s", tool.Description)
	}

	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12"}`)
	base := map[string]any{"automation_id": "ab12", "expected_version": float64(3), "reason": "because"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	res, err := s.handleAutomationInsertMember(context.Background(), callReq(with(map[string]any{
		"pending_item_id": "ab12cd34", "name": "Docs"})))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/automations/ab12/insert_member" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	member, _ := rec.body["member"].(map[string]any)
	if member["pending_item_id"] != "ab12cd34" || member["name"] != "Docs" {
		t.Errorf("member = %v; want the pending item id and name", rec.body["member"])
	}
	if _, ok := member["prd_id"]; ok {
		t.Errorf("an item insert carried a prd_id: %v", member)
	}

	// An Automaton insert still sends prd_id and no pending item id.
	if _, err := s.handleAutomationInsertMember(context.Background(), callReq(with(map[string]any{"prd_id": "p9"}))); err != nil {
		t.Fatal(err)
	}
	member, _ = rec.body["member"].(map[string]any)
	if member["prd_id"] != "p9" {
		t.Errorf("member = %v; want prd_id p9", member)
	}
	if _, ok := member["pending_item_id"]; ok {
		t.Errorf("an Automaton insert carried a pending_item_id: %v", member)
	}

	// Both ids, or neither, never reach the server.
	rec.method = ""
	for name, extra := range map[string]map[string]any{
		"both":    {"prd_id": "p9", "pending_item_id": "ab12cd34"},
		"neither": {},
	} {
		res, err := s.handleAutomationInsertMember(context.Background(), callReq(with(extra)))
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", name, err, res)
		}
		if text := automationText(t, res); !strings.HasPrefix(text, "error:") {
			t.Errorf("%s: want an error result, got %s", name, text)
		}
	}
	if rec.method != "" {
		t.Errorf("a rejected insert reached the server: %s %s", rec.method, rec.path)
	}
}

// The four tools carry a federation capability: the read tool reads, the
// other three write.
func TestPendingItemToolCapabilities(t *testing.T) {
	want := map[string]string{
		"autonomous_pending_items":         federation.CapAutonomousRead,
		"autonomous_pending_item_add":      federation.CapAutonomousWrite,
		"autonomous_pending_item_attach":   federation.CapAutonomousWrite,
		"autonomous_pending_items_refresh": federation.CapAutonomousWrite,
	}
	for _, name := range PendingItemToolNames() {
		got, ok := federation.RequiredCapForMCPTool(name)
		if !ok || got != want[name] {
			t.Errorf("%s capability = %q (found %v), want %q", name, got, ok, want[name])
		}
	}
}
