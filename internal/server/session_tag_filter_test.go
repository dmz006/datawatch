// Axis shortcut filters (?project=, ?stage=, …) on GET /api/sessions —
// expanded to axis:value tag filters counting a session's own, system and
// inherited tags — and the tag inventory GET /api/sessions/tags listing the
// tags in use across live and archived sessions with their axis, value and
// session counts.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func newTagShortcutTestManager(t *testing.T) (tm *sessionsTagTestManager, add func(id, prdID string, own, system []string)) {
	t.Helper()
	tm = newSessionsTagTestManager(t)
	// PRD 22220002's Automaton carries project:datawatch; PRD 33330003's
	// carries stage:build.
	tm.sm.SetTagOwnerLookup(tagTestOwnerLookup{owners: map[string][]string{
		"22220002": {"project:datawatch"},
		"33330003": {"stage:build"},
	}})
	return tm, func(id, prdID string, own, system []string) {
		t.Helper()
		sr := &session.Session{
			ID: id, FullID: "testhost-" + id, State: session.StateRunning,
			PRDID: prdID, Tags: own, SystemTags: system,
		}
		if err := tm.sm.SaveSession(sr); err != nil {
			t.Fatalf("SaveSession %s: %v", id, err)
		}
	}
}

func checkIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	got = append([]string(nil), got...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSessionsAxisShortcutFilters(t *testing.T) {
	tm, add := newTagShortcutTestManager(t)
	add("s-system", "", []string{"plain"}, []string{"project:datawatch", "stage:build", "compute:north"})
	add("s-inherit-project", "22220002", []string{"web"}, nil) // inherits project:datawatch
	add("s-inherit-stage", "33330003", []string{"web"}, nil)   // inherits stage:build
	add("s-plain", "", nil, nil)
	add("s-backend-tag", "", nil, []string{"backend:pi"})
	if err := tm.sm.SaveSession(&session.Session{ID: "s-family", FullID: "testhost-s-family", State: session.StateRunning, BackendFamily: "opencode"}); err != nil {
		t.Fatal(err)
	}

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
	check := func(rawQuery string, want ...string) {
		t.Helper()
		checkIDs(t, list(rawQuery), want...)
	}

	check("?project=datawatch", "s-system", "s-inherit-project")
	check("?stage=build", "s-system", "s-inherit-stage")
	check("?automaton=nosuch")
	check("?role=supervisor")
	check("?compute=north", "s-system")
	// Combinations under tag_match.
	check("?project=datawatch&stage=build", "s-system", "s-inherit-project", "s-inherit-stage")
	check("?project=datawatch&stage=build&tag_match=all", "s-system")
	// Shortcuts compose with explicit tags, once per tag (any-of).
	check("?tag=stage:build&project=datawatch", "s-system", "s-inherit-project", "s-inherit-stage")
	check("?tag=stage:build&stage=build", "s-system", "s-inherit-stage") // duplicated once
	// Blank values are ignored.
	check("?role=", "s-system", "s-inherit-project", "s-inherit-stage", "s-plain", "s-backend-tag", "s-family")
	// backend is both the session-backend family filter and the backend
	// axis shortcut: family equals value OR the backend:<value> tag.
	check("?backend=nosuch")
	check("?backend=opencode", "s-family")
	check("?backend=pi", "s-backend-tag")
	check("?tag=backend:pi", "s-backend-tag")
}

// A tag appearing on several of one session's lists counts once.
func TestSessionsTagShortcutCountsOncePerSession(t *testing.T) {
	tm, add := newTagShortcutTestManager(t)
	add("s-both", "22220002", []string{"project:datawatch"}, nil) // own + inherited
	s := &Server{token: "admin-token", manager: tm.sm}
	rr := httptest.NewRecorder()
	s.handleSessions(rr, httptest.NewRequest(http.MethodGet, "/api/sessions?project=datawatch&tag_match=all", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rr.Code, rr.Body.String())
	}
	ids := sessionIDs(t, rr.Body.Bytes())
	if len(ids) != 1 || ids[0] != "s-both" {
		t.Fatalf("got %v, want [s-both]", ids)
	}
}

type inventoryEntry struct {
	Tag   string `json:"tag"`
	Axis  string `json:"axis"`
	Value string `json:"value"`
	Count int    `json:"count"`
}

func TestSessionsTagInventoryEndpoint(t *testing.T) {
	tm, add := newTagShortcutTestManager(t)
	add("s-system", "", []string{"plain"}, []string{"project:datawatch", "stage:build"})
	add("s-inherit", "22220002", nil, nil) // inherits project:datawatch
	add("s-lane", "", []string{"lane:fill", "plain"}, nil)

	// One more tag lives only on an archived session.
	archived := &session.Session{
		ID: "s-arch", FullID: "testhost-s-arch", State: session.StateComplete,
		UpdatedAt: time.Now().Add(-400 * 24 * time.Hour),
		Tags: []string{"kind:worker"},
	}
	if err := tm.sm.SaveSession(archived); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if n, err := tm.sm.ArchiveSessions(time.Now()); err != nil {
		t.Fatalf("ArchiveSessions: %v", err)
	} else if n < 1 {
		t.Fatalf("archived %d, want ≥1", n)
	}

	s := &Server{token: "admin-token", manager: tm.sm}
	rr := httptest.NewRecorder()
	s.handleSessionTags(rr, httptest.NewRequest(http.MethodGet, "/api/sessions/tags", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Tags []inventoryEntry `json:"tags"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v: %s", err, rr.Body.String())
	}
	byTag := map[string]inventoryEntry{}
	for _, e := range out.Tags {
		if _, dup := byTag[e.Tag]; dup {
			t.Errorf("tag %q listed twice", e.Tag)
		}
		if e.Axis == "" && e.Value != e.Tag {
			t.Errorf("%q: axis empty but value %q != tag", e.Tag, e.Value)
		}
		byTag[e.Tag] = e
	}
	want := []struct {
		tag, axis, value string
		count            int
	}{
		{"kind:worker", "kind", "worker", 1},
		{"lane:fill", "lane", "fill", 1},
		{"project:datawatch", "project", "datawatch", 2}, // system + inherited
		{"stage:build", "stage", "build", 1},
		{"plain", "", "plain", 2}, // a tag with no axis sorts last
	}
	for _, w := range want {
		e, ok := byTag[w.tag]
		if !ok {
			t.Errorf("missing tag %q in %v", w.tag, out.Tags)
			continue
		}
		if e.Axis != w.axis || e.Value != w.value || e.Count != w.count {
			t.Errorf("%q = %+v, want axis %q value %q count %d", w.tag, e, w.axis, w.value, w.count)
		}
	}
	if len(out.Tags) != len(want) {
		t.Errorf("got %d entries, want %d: %v", len(out.Tags), len(want), out.Tags)
	}
	// The order is pinned: tagged axes ascending, then the axis-less tag
	// last: kind(1) < lane(1) < project(2) < stage(1) < plain.
	wantTags := []string{"kind:worker", "lane:fill", "project:datawatch", "stage:build", "plain"}
	var gotTags []string
	for _, e := range out.Tags {
		gotTags = append(gotTags, e.Tag)
	}
	for i := range wantTags {
		if i >= len(gotTags) || gotTags[i] != wantTags[i] {
			t.Errorf("order = %v, want %v", gotTags, wantTags)
			break
		}
	}
}

// The same path must still serve the POST tag replacement (and refuse other
// methods), so the GET/POST split on /api/sessions/tags is explicit.
func TestSessionsTagsRouteDispatch(t *testing.T) {
	tm := newSessionsTagTestManager(t)
	tm.add(t, "s1", "", "web")
	s := &Server{token: "admin-token", manager: tm.sm}

	rr := httptest.NewRecorder()
	s.handleSessionTags(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/tags", nil))
	if rr.Code != http.StatusBadRequest { // empty body → bad request, not a method error
		t.Errorf("POST empty: code = %d, want 400, body %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleSessionTags(rr, httptest.NewRequest(http.MethodPut, "/api/sessions/tags", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT: code = %d, want 405, body %s", rr.Code, rr.Body.String())
	}
}
