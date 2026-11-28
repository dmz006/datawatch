package server

// A tag filter takes a tag's name the same as its id, and a tag nothing knows
// is reported instead of answering with an empty result.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type tagNameFixture struct {
	s *Server
	// before and other are tag ids; a name with a colon is the shape that
	// used to be read as a derived axis tag by the list filter.
	before, other, child string
}

func tagNameServer(t *testing.T) tagNameFixture {
	t.Helper()
	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name, parent string) string {
		tg, err := m.CreateTag(name, parent, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return tg.ID
	}
	f := tagNameFixture{before: mk("unpause:before", ""), other: mk("later", "")}
	f.child = mk("sub", f.before)
	for id, tag := range map[string]string{"e0000001": f.before, "e0000002": f.other, "e0000003": f.child} {
		if err := m.Store().SavePRD(&autonomous.PRD{ID: id, Title: "Add " + id, Status: autonomous.PRDDraft,
			ProjectDir: "/tmp/demo", TagIDs: []string{tag}}); err != nil {
			t.Fatal(err)
		}
	}
	f.s = &Server{autonomousMgr: autonomous.NewAPI(m), cfg: config.DefaultConfig()}
	return f
}

type tagNameSurface struct {
	name string
	h    func(*Server) http.HandlerFunc
	path string // the query is appended
}

func tagNameSurfaces() []tagNameSurface {
	prds := func(s *Server) http.HandlerFunc { return s.handleAutonomousPRDs }
	return []tagNameSurface{
		{"timeline", func(s *Server) http.HandlerFunc { return s.handleAutonomousTimeline }, "/api/autonomous/timeline?"},
		{"progress", func(s *Server) http.HandlerFunc { return s.handleAutonomousProgress }, "/api/autonomous/progress?since=2000-01-01&"},
		{"progress trend", func(s *Server) http.HandlerFunc { return s.handleAutonomousProgressTrend }, "/api/autonomous/progress/trend?"},
		{"list", prds, "/api/autonomous/prds?"},
		{"list, any match", prds, "/api/autonomous/prds?tag_match=any&"},
		{"list, summary view", prds, "/api/autonomous/prds?view=summary&tag_match=all&"},
	}
}

// tagNameAnswer is the reply with the clock readings removed, so two reads of
// the same records compare equal.
func tagNameAnswer(t *testing.T, s *Server, sf tagNameSurface, query string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	sf.h(s)(rec, httptest.NewRequest(http.MethodGet, sf.path+query, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		return rec.Code, body
	}
	ids := []string{}
	for _, id := range []string{"e0000001", "e0000002", "e0000003"} {
		if strings.Contains(body, `"`+id+`"`) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	scope := ""
	if i := strings.Index(body, `"scope":"`); i >= 0 {
		rest := body[i+len(`"scope":"`):]
		scope = rest[:strings.Index(rest, `"`)]
	}
	count := ""
	if i := strings.Index(body, `"totals":{"prds":`); i >= 0 {
		rest := body[i+len(`"totals":{"prds":`):]
		count = rest[:strings.Index(rest, ",")]
	}
	return rec.Code, strings.Join(ids, ",") + " scope=" + scope + " count=" + count
}

func TestTagFilterByNameWorksLikeItsID(t *testing.T) {
	f := tagNameServer(t)
	for _, sf := range tagNameSurfaces() {
		for _, key := range []string{"tag", "tag_id"} {
			if sf.name == "timeline" && key == "tag_id" {
				continue // the timeline takes only tag
			}
			wantCode, want := tagNameAnswer(t, f.s, sf, key+"="+f.before)
			code, got := tagNameAnswer(t, f.s, sf, key+"="+url.QueryEscape("unpause:before"))
			if code != wantCode || got != want {
				t.Errorf("%s %s=name: %d %q, by id: %d %q", sf.name, key, code, got, wantCode, want)
			}
		}
	}
	// A name with no colon, and a name beside an id.
	for _, sf := range tagNameSurfaces() {
		_, want := tagNameAnswer(t, f.s, sf, "tag="+f.other)
		_, got := tagNameAnswer(t, f.s, sf, "tag=later")
		if got != want {
			t.Errorf("%s tag=later: %q, by id: %q", sf.name, got, want)
		}
	}
	progress := tagNameSurfaces()[1]
	_, want := tagNameAnswer(t, f.s, progress, "tag="+f.before+","+f.other)
	_, got := tagNameAnswer(t, f.s, progress, "tag=unpause%3Abefore&tag="+f.other)
	if got != want {
		t.Errorf("progress name+id: %q, by ids: %q", got, want)
	}
}

func TestTagFilterUnknownTagIsReported(t *testing.T) {
	f := tagNameServer(t)
	for _, sf := range tagNameSurfaces() {
		for _, bad := range []string{"no-such-tag", "unpause:befroe"} {
			code, body := tagNameAnswer(t, f.s, sf, "tag="+url.QueryEscape(bad))
			if code != http.StatusNotFound || !strings.Contains(body, "tag not found") || !strings.Contains(body, bad) {
				t.Errorf("%s tag=%s: %d %q, want 404 naming the unknown tag", sf.name, bad, code, body)
			}
		}
	}
	// The numbers never silently leave out a mistyped tag.
	code, body := tagNameAnswer(t, f.s, tagNameSurfaces()[1], "tag=later&tag=laterr")
	if code != http.StatusNotFound || !strings.Contains(body, "laterr") {
		t.Errorf("progress with one unknown tag: %d %q, want 404", code, body)
	}
}

// The object lists also match derived axis tags, which may match nothing. They
// report a tag only when it leaves the filter unable to match anything.
func TestTagFilterObjectListsKeepDerivedTags(t *testing.T) {
	f := tagNameServer(t)
	lists := []tagNameSurface{
		{"list", func(s *Server) http.HandlerFunc { return s.handleAutonomousPRDs }, "/api/autonomous/prds?"},
		{"templates", func(s *Server) http.HandlerFunc { return s.handleAutonomousTemplates }, "/api/autonomous/templates?"},
		{"automations", func(s *Server) http.HandlerFunc { return s.handleAutomations }, "/api/automations?"},
		{"tasks of one Automaton", func(s *Server) http.HandlerFunc { return s.handleAutonomousPRDs }, "/api/autonomous/prds/e0000001?detail=tasks&"},
	}
	for _, sf := range lists {
		for _, ok := range []string{"stage:implement", "project:nowhere", url.QueryEscape("unpause:before"), "later,no-such-tag"} {
			if code, body := tagNameAnswer(t, f.s, sf, "tag="+ok+"&tag_match=any"); code != http.StatusOK {
				t.Errorf("%s tag=%s: %d %q, want 200", sf.name, ok, code, body)
			}
		}
		for _, bad := range []string{"tag=no-such-tag&tag_match=any", "tag=later,no-such-tag&tag_match=all", "tag=" + url.QueryEscape("unpause:befroe")} {
			code, body := tagNameAnswer(t, f.s, sf, bad)
			if code != http.StatusNotFound || !strings.Contains(body, "tag not found") {
				t.Errorf("%s %s: %d %q, want 404", sf.name, bad, code, body)
			}
		}
	}
}

// Two tags may share a name; the filter does not guess which one is meant.
func TestTagFilterSharedNameAsksForTheID(t *testing.T) {
	f := tagNameServer(t)
	api := f.s.autonomousMgr.(*autonomous.API)
	if _, err := api.M.CreateTag("sub", f.other, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, sf := range tagNameSurfaces() {
		code, body := tagNameAnswer(t, f.s, sf, "tag=sub")
		if code != http.StatusBadRequest || !strings.Contains(body, f.child) {
			t.Errorf("%s tag=sub: %d %q, want 400 naming the ids", sf.name, code, body)
		}
	}
}
