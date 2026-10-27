package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// fakeSessionTagRegistry knows only the tag IDs it was given and refuses the
// rest with the autonomous not-found error, as the real registry does.
type fakeSessionTagRegistry struct{ known map[string]bool }

func (f *fakeSessionTagRegistry) RequireTag(id string) error {
	if !f.known[id] {
		return fmt.Errorf("%w: %q", autonomous.ErrTagNotFound, id)
	}
	return nil
}

// fakeSessionTagOwners maps an Automaton's PRD ID to its tag IDs.
type fakeSessionTagOwners struct{ owners map[string][]string }

func (f *fakeSessionTagOwners) AutomatonTagIDs(prdID string) ([]string, bool) {
	ids, ok := f.owners[prdID]
	return ids, ok
}

// useFakeSessionTags wires the fake registry and owner lookup into the
// server's session manager. prd-1 is an Automaton carrying tag-a.
func useFakeSessionTags(t *testing.T, s *Server) *fakeSessionTagOwners {
	t.Helper()
	s.manager.SetTagRegistry(&fakeSessionTagRegistry{known: map[string]bool{"tag-a": true, "tag-b": true}})
	owners := &fakeSessionTagOwners{owners: map[string][]string{"prd-1": {"tag-a"}}}
	s.manager.SetTagOwnerLookup(owners)
	return owners
}

// startTagsSession starts a session through the start handler and returns
// its full ID. The body is the JSON start request.
func startTagsSession(t *testing.T, s *Server, body string) string {
	t.Helper()
	w := startTagsSessionRaw(t, s, body)
	if w.Code != http.StatusOK {
		t.Fatalf("start session %s: %d %s", body, w.Code, w.Body.String())
	}
	var out struct {
		FullID string `json:"full_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.FullID == "" {
		t.Fatalf("decode started session: %v (%s)", err, w.Body.String())
	}
	return out.FullID
}

// startTagsSessionRaw posts the start request and returns the recorder.
func startTagsSessionRaw(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleStartSession(w, req)
	cleanupStartedSession(t, s, w)
	return w
}

// sessionTagsCall posts {id, tag_ids} to the tags route, optionally through
// the federation auth middleware with the given bearer token.
func sessionTagsCall(t *testing.T, s *Server, token, id string, tagIDs ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "tag_ids": tagIDs})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/tags", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler := http.HandlerFunc(s.handleSessionTags)
	if token == "" {
		handler.ServeHTTP(w, req)
		return w
	}
	req.Header.Set("Authorization", "Bearer "+token)
	s.fedAuthMiddleware(handler).ServeHTTP(w, req)
	return w
}

// sessionTagsFields is the part of a session JSON this suite reads.
type sessionTagsFields struct {
	FullID        string   `json:"full_id"`
	Tags          []string `json:"tags"`
	InheritedTags []string `json:"inherited_tags"`
}

func decodeSessionTagsFields(t *testing.T, w *httptest.ResponseRecorder) sessionTagsFields {
	t.Helper()
	var out sessionTagsFields
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode session: %v (%s)", err, w.Body.String())
	}
	return out
}

func TestSessionTagsRouteSetsOwnTagsByShortID(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	useFakeSessionTags(t, s)
	fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`"}`)
	shortID := fullID[strings.LastIndex(fullID, "-")+1:]

	w := sessionTagsCall(t, s, "", shortID, "tag-b", "tag-a", "tag-b")
	if w.Code != http.StatusOK {
		t.Fatalf("set tags: %d %s", w.Code, w.Body.String())
	}
	got := decodeSessionTagsFields(t, w)
	if want := []string{"tag-b", "tag-a"}; !reflect.DeepEqual(got.Tags, want) {
		t.Errorf("own tags = %v, want %v (duplicates dropped, first wins)", got.Tags, want)
	}
	if len(got.InheritedTags) != 0 {
		t.Errorf("session outside an Automaton: inherited_tags = %v, want empty", got.InheritedTags)
	}
}

func TestSessionTagsRouteRefusesUnknownTagAndKeepsOldTags(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	useFakeSessionTags(t, s)
	fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`","tags":["tag-a"]}`)

	w := sessionTagsCall(t, s, "", fullID, "tag-b", "nope")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown tag: got %d %s, want 404", w.Code, w.Body.String())
	}
	sess, ok := s.manager.GetSession(fullID)
	if !ok || !reflect.DeepEqual(sess.Tags, []string{"tag-a"}) {
		t.Errorf("refused update changed own tags: %v", sess.Tags)
	}
}

func TestSessionTagsRouteRequiresFederationWriteCapability(t *testing.T) {
	s, fullToken, roToken := fedCapFixture(t)
	useFakeSessionTags(t, s)
	fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`"}`)

	if w := sessionTagsCall(t, s, roToken, fullID, "tag-a"); w.Code != http.StatusForbidden {
		t.Errorf("read-only peer: got %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if w := sessionTagsCall(t, s, fullToken, fullID, "tag-a"); w.Code != http.StatusOK {
		t.Errorf("full-control peer: got %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if w := sessionTagsCall(t, s, "admin-token", fullID, "tag-b"); w.Code != http.StatusOK {
		t.Errorf("admin token: got %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestSessionTagsStartRefusesUnknownTagAndCreatesNothing(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	useFakeSessionTags(t, s)
	before := len(s.manager.ListSessions())

	w := startTagsSessionRaw(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`","tags":["nope"]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("start with unknown tag: got %d %s, want 404", w.Code, w.Body.String())
	}
	if after := len(s.manager.ListSessions()); after != before {
		t.Errorf("refused start still created %d session(s)", after-before)
	}
}

func TestSessionTagsListAndDetailCarryInheritedTags(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	owners := useFakeSessionTags(t, s)

	childID := startTagsSession(t, s,
		`{"task":"child","project_dir":"`+t.TempDir()+`","prd_id":"prd-1","tags":["tag-b"]}`)
	outsideID := startTagsSession(t, s, `{"task":"outside","project_dir":"`+t.TempDir()+`"}`)

	check := func(label string, wantChildInherited []string) {
		t.Helper()
		list := httptest.NewRecorder()
		s.handleSessions(list, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
		if list.Code != http.StatusOK {
			t.Fatalf("%s list: %d %s", label, list.Code, list.Body.String())
		}
		var rows []sessionTagsFields
		if err := json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
			t.Fatalf("%s decode list: %v", label, err)
		}
		byID := map[string]sessionTagsFields{}
		for _, r := range rows {
			byID[r.FullID] = r
		}
		if got := byID[childID]; !reflect.DeepEqual(got.Tags, []string{"tag-b"}) ||
			!reflect.DeepEqual(got.InheritedTags, wantChildInherited) {
			t.Errorf("%s list child: tags=%v inherited=%v, want tags=[tag-b] inherited=%v",
				label, got.Tags, got.InheritedTags, wantChildInherited)
		}
		if got := byID[outsideID]; got.InheritedTags == nil || len(got.InheritedTags) != 0 {
			t.Errorf("%s list outside session: inherited=%#v, want empty array", label, got.InheritedTags)
		}

		detail := httptest.NewRecorder()
		s.handleSessionSubpath(detail, httptest.NewRequest(http.MethodGet, "/api/sessions/"+childID, nil))
		if detail.Code != http.StatusOK {
			t.Fatalf("%s detail: %d %s", label, detail.Code, detail.Body.String())
		}
		got := decodeSessionTagsFields(t, detail)
		if !reflect.DeepEqual(got.InheritedTags, wantChildInherited) {
			t.Errorf("%s detail inherited = %v, want %v", label, got.InheritedTags, wantChildInherited)
		}
		if !reflect.DeepEqual(got.Tags, []string{"tag-b"}) {
			t.Errorf("%s detail own tags = %v, want [tag-b]", label, got.Tags)
		}
	}

	check("before owner change", []string{"tag-a"})

	// The Automaton's tags change; the next read shows the new inheritance.
	owners.owners["prd-1"] = []string{"tag-b"}
	check("after owner change", []string{"tag-b"})
}

func TestSessionTagsDeleteRefusedWhileSessionCarriesTag(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	amgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	s.autonomousMgr = newTestAPI(amgr)
	s.wireSessionTags()

	tg := createTag(t, s, `{"name":"work"}`)
	fullID := startTagsSession(t, s, `{"task":"tagged","project_dir":"`+t.TempDir()+`","tags":["`+tg.ID+`"]}`)

	del := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+tg.ID, "")
	if del.Code != http.StatusConflict {
		t.Fatalf("delete tag carried by a session: %d %s, want 409", del.Code, del.Body.String())
	}
	var refusal struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(del.Body.Bytes(), &refusal); err != nil || refusal.Count != 1 {
		t.Errorf("409 body count = %d (err %v), want 1: %s", refusal.Count, err, del.Body.String())
	}

	if w := sessionTagsCall(t, s, "", fullID); w.Code != http.StatusOK {
		t.Fatalf("clear session tags: %d %s", w.Code, w.Body.String())
	}
	if del := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+tg.ID, ""); del.Code != http.StatusOK {
		t.Errorf("delete after the session let go: %d %s, want 200", del.Code, del.Body.String())
	}
}
