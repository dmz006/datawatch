// session_tag_list MCP tool — the tags in use across live and archived
// sessions, each split into its axis and value and counted over the sessions
// that carry it — plus the list_sessions axis shortcut params that expand to
// axis:value tag filters (own, system and inherited tags all count).

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/session"
)

func TestSessionTagListToolDeclaration(t *testing.T) {
	s := &Server{}
	tool := s.toolSessionTagList()
	if tool.Name != "session_tag_list" {
		t.Fatalf("tool.Name = %q, want session_tag_list", tool.Name)
	}
	desc := strings.ToLower(tool.Description)
	for _, word := range []string{"axis", "count", "archived"} {
		if !strings.Contains(desc, word) {
			t.Errorf("description should mention %q: %q", word, tool.Description)
		}
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("session_tag_list takes no parameters; properties = %v", tool.InputSchema.Properties)
	}
}

func TestListSessionsDeclaresAxisShortcuts(t *testing.T) {
	s := &Server{}
	props := s.toolListSessions().InputSchema.Properties
	for _, axis := range []string{"project", "automaton", "automation", "stage", "kind", "role", "lane", "compute"} {
		if _, ok := props[axis]; !ok {
			t.Errorf("list_sessions should declare the %q axis shortcut param", axis)
		}
	}
	if _, ok := props["backend"]; !ok {
		t.Error("list_sessions should still declare its backend filter")
	}
}

// The inventory counts a session's own, system and inherited tags alike, a
// tag once per session, and splits each into axis and value.
func TestHandleSessionTagListCounts(t *testing.T) {
	s := bl91Server(t)
	tm := s.manager
	tm.SetTagOwnerLookup(listSessionsTagLookup{
		owners: map[string][]string{"22220002": {"project:datawatch"}},
	})
	for _, sr := range []*session.Session{
		{ID: "st12", FullID: "testhost-st12", State: session.StateRunning, SystemTags: []string{"stage:build", "alpha"}},
		{ID: "st34", FullID: "testhost-st34", State: session.StateRunning, PRDID: "22220002", Tags: []string{"alpha"}},
		{ID: "st56", FullID: "testhost-st56", State: session.StateRunning},
	} {
		if err := tm.SaveSession(sr); err != nil {
			t.Fatalf("SaveSession %s: %v", sr.ID, err)
		}
	}
	res, err := s.handleSessionTagList(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal("expected success")
	}
	if len(res.Content) != 1 {
		t.Fatalf("want 1 content item, got %d", len(res.Content))
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	var out struct {
		Tags []session.TagInventoryEntry `json:"tags"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v: %s", err, text)
	}
	want := []session.TagInventoryEntry{
		{Tag: "project:datawatch", Axis: "project", Value: "datawatch", Count: 1},
		{Tag: "stage:build", Axis: "stage", Value: "build", Count: 1},
		{Tag: "alpha", Axis: "", Value: "alpha", Count: 2}, // a tag with no axis sorts last
	}
	if len(out.Tags) != len(want) {
		t.Fatalf("got %v, want %v", out.Tags, want)
	}
	for i := range want {
		if out.Tags[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v (full list %v)", i, out.Tags[i], want[i], out.Tags)
		}
	}
}

// The in-process path filters on the expanded axis:value tags; shortcuts
// combine with explicit tags under tag_match, and blank values are ignored.
func TestListSessionsAxisShortcutFilter(t *testing.T) {
	s := bl91Server(t)
	tm := s.manager
	tm.SetTagOwnerLookup(listSessionsTagLookup{
		owners: map[string][]string{"22220002": {"automaton:supervisor"}},
	})
	for _, sr := range []*session.Session{
		{ID: "ax11", FullID: "testhost-ax11", State: session.StateRunning, SystemTags: []string{"automaton:supervisor", "web", "compute:north"}},
		{ID: "ax22", FullID: "testhost-ax22", State: session.StateRunning, PRDID: "22220002", Tags: []string{"web"}},
		{ID: "ax33", FullID: "testhost-ax33", State: session.StateRunning, Tags: []string{"web"}},
	} {
		if err := tm.SaveSession(sr); err != nil {
			t.Fatalf("SaveSession %s: %v", sr.ID, err)
		}
	}
	text := func(args map[string]any) string {
		t.Helper()
		res, err := s.handleListSessions(context.Background(), callReq(args))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Content) != 1 {
			t.Fatalf("want 1 content item, got %d", len(res.Content))
		}
		return res.Content[0].(mcpsdk.TextContent).Text
	}

	match := text(map[string]any{"format": "json", "automaton": "supervisor"})
	for _, id := range []string{"ax11", "ax22"} {
		if !strings.Contains(match, `"`+id+`"`) {
			t.Errorf("shortcut list lacks %s: %s", id, match)
		}
	}
	if strings.Contains(match, `"ax33"`) {
		t.Errorf("shortcut list should not contain ax33: %s", match)
	}

	all := text(map[string]any{"format": "json", "automaton": "supervisor", "tags": []any{"web"}, "tag_match": "all"})
	for _, id := range []string{"ax11", "ax22"} {
		if !strings.Contains(all, `"`+id+`"`) {
			t.Errorf("tag_match=all list lacks %s: %s", id, all)
		}
	}
	if strings.Contains(all, `"ax33"`) {
		t.Errorf("tag_match=all list should not contain ax33: %s", all)
	}

	none := text(map[string]any{"format": "json", "stage": "nosuch"})
	if strings.Contains(none, `"id"`) {
		t.Errorf("unknown axis value should list nothing: %s", none)
	}

	cn := text(map[string]any{"format": "json", "compute": "north"})
	if !strings.Contains(cn, `"ax11"`) || strings.Contains(cn, `"ax33"`) {
		t.Errorf("compute shortcut should keep only ax11: %s", cn)
	}

	blank := text(map[string]any{"format": "json", "lane": ""})
	if !strings.Contains(blank, `"ax33"`) {
		t.Errorf("blank shortcut should be ignored: %s", blank)
	}
}

// The REST list-view branch forwards the shortcuts as axis:value tag params.
func TestListSessionsViewQueryAxisShortcuts(t *testing.T) {
	q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{
		"view":    "summary",
		"stage":   "build",
		"project": "datawatch",
		"lane":    "",             // blank → ignored
		"role":    " supervisor ", // trimmed
		"compute": "north",
		"tags":    []any{"web"},
	}))
	if !ok || errMsg != "" {
		t.Fatalf("ok=%v err=%q", ok, errMsg)
	}
	want := []string{"web", "project:datawatch", "stage:build", "role:supervisor", "compute:north"} // canonical axis order
	got := q["tag"]
	if len(got) != len(want) {
		t.Fatalf("tag params = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("tag params = %v, want %v", got, want)
		}
	}
}
