// Tests for the repeatable ?tag= and ?tag_match= filters on GET
// /api/sessions: a session's own tags and the tags it inherits from the
// Automaton that spawned it both count.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

type sessionsTagTestManager struct {
	sm *session.Manager
}

// tagTestOwnerLookup reports the tags of one test Automaton.
type tagTestOwnerLookup struct{ owners map[string][]string }

func (l tagTestOwnerLookup) AutomatonTagIDs(prdID string) ([]string, bool) {
	ids, ok := l.owners[prdID]
	return ids, ok
}

func newSessionsTagTestManager(t *testing.T) *sessionsTagTestManager {
	t.Helper()
	sm, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(sm)
	trackSessionManager(t, sm)
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	// The owner of PRD 11110001 carries owner-tag; its sessions inherit it.
	sm.SetTagOwnerLookup(tagTestOwnerLookup{owners: map[string][]string{"11110001": {"owner-tag"}}})
	return &sessionsTagTestManager{sm: sm}
}

func (tm *sessionsTagTestManager) add(t *testing.T, id, prdID string, tags ...string) {
	t.Helper()
	sess := &session.Session{ID: id, FullID: "testhost-" + id, State: session.StateRunning, PRDID: prdID, Tags: tags}
	if err := tm.sm.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession %s: %v", id, err)
	}
}

func sessionIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var out []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("bad json: %v: %s", err, body)
	}
	ids := make([]string, 0, len(out))
	for _, s := range out {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestSessionsFilterByOwnAndInheritedTags(t *testing.T) {
	tm := newSessionsTagTestManager(t)
	tm.add(t, "s-own", "", "web")
	tm.add(t, "s-inh", "11110001", "other") // inherits owner-tag
	tm.add(t, "s-both", "", "web", "owner-tag")
	tm.add(t, "s-none", "")

	s := &Server{token: "admin-token", manager: tm.sm}

	list := func(rawQuery string) []string {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleSessions(rr, httptest.NewRequest(http.MethodGet, "/api/sessions"+rawQuery, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: code = %d, body %s", rawQuery, rr.Code, rr.Body.String())
		}
		return sessionIDs(t, rr.Body.Bytes())
	}

	check := func(got []string, want ...string) {
		t.Helper()
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	check(list("?tag=web"), "s-both", "s-own")
	check(list("?tag=owner-tag"), "s-both", "s-inh") // inherited tag
	check(list("?tag=web&tag=owner-tag"), "s-both", "s-inh", "s-own")
	check(list("?tag=web,owner-tag"), "s-both", "s-inh", "s-own") // comma form
	check(list("?tag=web&tag=owner-tag&tag_match=all"), "s-both")
	check(list(""), "s-both", "s-inh", "s-none", "s-own")
}

func TestSessionsRejectsUnknownTagMatch(t *testing.T) {
	tm := newSessionsTagTestManager(t)
	s := &Server{token: "admin-token", manager: tm.sm}
	rr := httptest.NewRecorder()
	s.handleSessions(rr, httptest.NewRequest(http.MethodGet, "/api/sessions?tag=web&tag_match=some", nil))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "tag_match") {
		t.Fatalf("code = %d body %q, want 400 naming tag_match", rr.Code, rr.Body.String())
	}
}
