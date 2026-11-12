// list_sessions tag filter: the tags array and tag_match rule work on the
// in-process path (counting a session's own tags and the tags it inherits
// from the Automaton that spawned it) and are forwarded to the REST list
// view on the view/states/since path.

package mcp

import (
	"context"
	"net/url"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/session"
)

type listSessionsTagLookup struct{ owners map[string][]string }

func (l listSessionsTagLookup) AutomatonTagIDs(prdID string) ([]string, bool) {
	ids, ok := l.owners[prdID]
	return ids, ok
}

func TestListSessionsTagFilterLegacyPath(t *testing.T) {
	s := bl91Server(t)
	s.manager.SetTagOwnerLookup(listSessionsTagLookup{
		owners: map[string][]string{"11110001": {"owner-tag"}},
	})
	add := func(id, prdID string, tags ...string) {
		t.Helper()
		if err := s.manager.SaveSession(&session.Session{
			ID: id, FullID: "testhost-" + id, Hostname: "testhost",
			State: session.StateRunning, PRDID: prdID, Tags: tags,
		}); err != nil {
			t.Fatalf("SaveSession %s: %v", id, err)
		}
	}
	add("s-own", "", "web")
	add("s-inh", "11110001", "other") // inherits owner-tag
	add("s-both", "", "web", "owner-tag")
	add("s-none", "")

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

	anyOf := text(map[string]any{"format": "json", "tags": []any{"web", "owner-tag"}})
	for _, id := range []string{"s-own", "s-inh", "s-both"} {
		if !strings.Contains(anyOf, `"`+id+`"`) {
			t.Errorf("any-of list lacks %s: %s", id, anyOf)
		}
	}
	if strings.Contains(anyOf, `"s-none"`) {
		t.Errorf("any-of list should not contain s-none: %s", anyOf)
	}

	all := text(map[string]any{"format": "json", "tags": []any{"web", "owner-tag"}, "tag_match": "all"})
	if !strings.Contains(all, `"s-both"`) {
		t.Errorf("all-of list should contain s-both: %s", all)
	}
	if strings.Contains(all, `"s-own"`) || strings.Contains(all, `"s-inh"`) {
		t.Errorf("all-of list should contain only s-both: %s", all)
	}

	none := text(map[string]any{"format": "json", "tags": []any{"nope"}})
	if strings.Contains(none, `"id"`) {
		t.Errorf("unknown tag should list nothing: %s", none)
	}
}

// The REST list-view path forwards the same parameters.
func TestListSessionsViewQuery_ForwardsTags(t *testing.T) {
	q, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{
		"view": "summary", "tags": []any{"web", "perf"}, "tag_match": "all",
	}))
	if !ok || errMsg != "" {
		t.Fatalf("ok=%v err=%q", ok, errMsg)
	}
	want := url.Values{"view": {"summary"}, "tag": {"web", "perf"}, "tag_match": {"all"}}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", q.Encode(), want.Encode())
	}

	if _, ok, errMsg := listSessionsViewQuery(callReq(map[string]any{
		"view": "summary", "tags": []any{"web"}, "tag_match": "some",
	})); ok || !strings.Contains(errMsg, "tag_match") {
		t.Errorf("invalid tag_match: ok=%v err=%q", ok, errMsg)
	}
}
