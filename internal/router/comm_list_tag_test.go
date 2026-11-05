// Tests for the chat `list` tag filter: tag=<tag> (repeatable) and
// tag_match=any|all, counting the session's own tags and the tags it
// inherits from the Automaton that spawned it.

package router

import (
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// commListTagLookup reports the tags of one test Automaton.
type commListTagLookup struct{ owners map[string][]string }

func (l commListTagLookup) AutomatonTagIDs(prdID string) ([]string, bool) {
	ids, ok := l.owners[prdID]
	return ids, ok
}

func newCommListTagRouter(t *testing.T) *Router {
	t.Helper()
	sm, err := session.NewManager("h", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sm.WaitBackground)
	trackSessionManager(t, sm)
	sm.WithFakeTmux()
	sm.SetTagOwnerLookup(commListTagLookup{owners: map[string][]string{"11110001": {"owner-tag"}}})
	add := func(id, prdID string, tags ...string) {
		t.Helper()
		if err := sm.SaveSession(&session.Session{ID: id, FullID: "h-" + id, Hostname: "h",
			State: session.StateRunning, PRDID: prdID, Tags: tags}); err != nil {
			t.Fatalf("SaveSession %s: %v", id, err)
		}
	}
	add("s-own", "", "web")
	add("s-inh", "11110001", "other") // inherits owner-tag
	add("s-both", "", "web", "owner-tag")
	add("s-none", "")
	return &Router{hostname: "h", manager: sm}
}

func TestCommListTagFilter(t *testing.T) {
	r := newCommListTagRouter(t)

	listed := func(filter string) []string {
		t.Helper()
		resp := r.HandleTestMessage("list " + filter)
		if len(resp) != 1 {
			t.Fatalf("%q: responses = %v", filter, resp)
		}
		var ids []string
		for _, line := range strings.Split(resp[0], "\n") {
			for _, id := range []string{"s-own", "s-inh", "s-both", "s-none"} {
				if strings.Contains(line, "["+id+"]") {
					ids = append(ids, id)
					break
				}
			}
		}
		return ids
	}

	check := func(get []string, filter string, want ...string) {
		t.Helper()
		got := append([]string(nil), get...)
		exp := append([]string(nil), want...)
		sort.Strings(got)
		sort.Strings(exp)
		if strings.Join(got, ",") != strings.Join(exp, ",") {
			t.Fatalf("list %q listed %q, want %q", filter, strings.Join(got, ","), strings.Join(exp, ","))
		}
	}

	check(listed("tag=web"), "tag=web", "s-both", "s-own")
	check(listed("tag=owner-tag"), "tag=owner-tag", "s-both", "s-inh") // inherited tag counts
	check(listed("tag=web tag=owner-tag"), "tag=web tag=owner-tag", "s-both", "s-inh", "s-own")
	check(listed("tag=web tag=owner-tag tag_match=all"), "tag=web tag=owner-tag tag_match=all", "s-both")
	resp := r.HandleTestMessage("list tag=web tag_match=some")
	if len(resp) != 1 || !strings.Contains(resp[0], "tag_match") {
		t.Fatalf("tag_match=some should be refused naming tag_match, got %v", resp)
	}
	check(listed("active"), "active", "s-both", "s-inh", "s-none", "s-own") // state filter unchanged
}
