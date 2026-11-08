// Tests for the comm-channel tag listing: `session tags` (list the tags
// sessions carry, with counts) and `autonomous session-tags list` (the same
// inventory through the tag command). `list tag=...` filtering is covered
// in comm_list_tag_test.go.

package router

import (
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// sessionTagsRouter builds a comm router over a session manager whose
// sessions carry the named tags.
func sessionTagsRouter(t *testing.T, add func(sm *session.Manager, id, prdID string, tags ...string)) *Router {
	t.Helper()
	sm, err := session.NewManager("h", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sm.WaitBackground)
	trackSessionManager(t, sm)
	sm.WithFakeTmux()
	if add != nil {
		add(sm, "s-own", "", "web")
		add(sm, "s-both", "", "web", "owner-tag")
		add(sm, "s-none", "")
	}
	return &Router{hostname: "h", manager: sm}
}

func TestSessionTagsVerb_ListsTagInventory(t *testing.T) {
	r := sessionTagsRouter(t, func(sm *session.Manager, id, prdID string, tags ...string) {
		if err := sm.SaveSession(&session.Session{ID: id, FullID: "h-" + id, Hostname: "h",
			State: session.StateRunning, PRDID: prdID, Tags: tags}); err != nil {
			t.Fatalf("SaveSession %s: %v", id, err)
		}
	})
	resp := r.HandleTestMessage("session tags")
	if len(resp) != 1 {
		t.Fatalf("responses = %v", resp)
	}
	got := resp[0]
	if !strings.Contains(got, "session tags (2)") {
		t.Errorf("response = %q, want 2 tags listed", got)
	}
	if !strings.Contains(got, "web count=2") {
		t.Errorf("response = %q, want web count=2", got)
	}
	if !strings.Contains(got, "owner-tag count=1") {
		t.Errorf("response = %q, want owner-tag count=1", got)
	}
}

func TestSessionTagsVerb_NoSessions(t *testing.T) {
	r := sessionTagsRouter(t, nil)
	resp := r.HandleTestMessage("session tags")
	if len(resp) != 1 || !strings.Contains(resp[0], "no tags") {
		t.Fatalf("responses = %v, want a no-tags note", resp)
	}
}

// The parser takes `session tags` as its own verb and still refuses unknown
// verbs.
func TestSessionTagsVerb_Parsing(t *testing.T) {
	cmd := Parse("session tags")
	if cmd.Type != CmdSession || cmd.SessionVerb != SessionVerbTags {
		t.Errorf("cmd = %+v, want CmdSession/SessionVerbTags", cmd)
	}

	cmd = Parse("session bogus")
	if !strings.Contains(cmd.Text, "unknown session verb: bogus") {
		t.Errorf("text = %q, want the unknown-verb refusal", cmd.Text)
	}
}

// `autonomous session-tags list` routes to GET /api/sessions/tags, the same
// inventory endpoint `session tags` lists.
func TestBuildCommTagRequest_SessionTagsList(t *testing.T) {
	for _, arg := range []string{"list", "LIST"} {
		req, usage := buildCommTagRequest("session-tags", []string{"session-tags", arg})
		if usage != "" {
			t.Fatalf("%q: usage = %q", arg, usage)
		}
		if req.method != http.MethodGet || req.path != "/api/sessions/tags" {
			t.Errorf("%q: request = %s %s, want GET /api/sessions/tags", arg, req.method, req.path)
		}
	}

	// Assignment keeps working and still requires a session id.
	req, usage := buildCommTagRequest("session-tags", []string{"session-tags", "sess1", "t1,t2"})
	if usage != "" || req.method != http.MethodPost || req.path != "/api/sessions/tags" {
		t.Fatalf("assignment: request = %+v usage = %q", req, usage)
	}
	if !reflect.DeepEqual(req.body["tag_ids"], []string{"t1", "t2"}) {
		t.Errorf("tag_ids = %v", req.body["tag_ids"])
	}
}

// Sanity: the inventory the comm verb prints and the REST endpoint serve are
// the same manager data, so the listing stays in one place.
func TestSessionTagInventoryMatchesManager(t *testing.T) {
	sm, err := session.NewManager("h", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sm.WaitBackground)
	trackSessionManager(t, sm)
	sm.WithFakeTmux()
	if err := sm.SaveSession(&session.Session{ID: "a", FullID: "h-a", Hostname: "h", State: session.StateRunning, Tags: []string{"lane:cpu", "web"}}); err != nil {
		t.Fatal(err)
	}
	if err := sm.SaveSession(&session.Session{ID: "b", FullID: "h-b", Hostname: "h", State: session.StateRunning, Tags: []string{"lane:cpu"}}); err != nil {
		t.Fatal(err)
	}
	inv := sm.TagInventory()
	counts := map[string]int{}
	for _, e := range inv {
		counts[e.Tag] = e.Count
	}
	if counts["lane:cpu"] != 2 || counts["web"] != 1 || len(counts) != 2 {
		t.Fatalf("counts = %v", counts)
	}
	sorted := make([]string, 0, len(inv))
	for _, e := range inv {
		sorted = append(sorted, e.Tag)
	}
	sort.Strings(sorted)
	if sorted[0] != "lane:cpu" || sorted[1] != "web" {
		t.Errorf("order = %v", sorted)
	}
}
