// REST-level tests for the object tag filter, added to the Automaton,
// Automation and task lists (task filter-the-automaton task,
// docs/plans/2026-11-06-automata-pages-redesign.md section 12): they drive
// the real Server handlers with httptest so the repeatable ?tag= grammar,
// the tag_match any/all rule, the project/automation/automaton shortcuts and
// the 400 on a bad tag_match are all exercised end to end against each list.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// tagFilterFake embeds the full autonomous.AutonomousAPI stub and overrides
// just the list/get reads the object tag filter relies on.
type tagFilterFake struct {
	*fakeOrchAutonomous
	prds  []any
	autos []any
}

func (f *tagFilterFake) ListPRDs() []any { return f.prds }

func (f *tagFilterFake) GetPRD(id string) (any, bool) {
	for _, p := range f.prds {
		prd, err := toPRD(p)
		if err == nil && prd != nil && prd.ID == id {
			return prd, true
		}
	}
	return nil, false
}

func (f *tagFilterFake) ListAutomations() []any { return f.autos }

func newTagFilterServer(prds []any, autos []any) *Server {
	return &Server{cfg: config.DefaultConfig(), autonomousMgr: &tagFilterFake{fakeOrchAutonomous: &fakeOrchAutonomous{}, prds: prds, autos: autos}}
}

// tagPRD builds an Automaton (stored as a PRD) carrying the given system
// tags.
func tagPRD(id, project string) *autonomous.PRD {
	return &autonomous.PRD{
		ID:         id,
		Title:      "prd " + id,
		Status:     autonomous.PRDDraft,
		SystemTags: []string{"project:" + project, "automaton:" + id},
	}
}

// tagAutomation builds an Automation carrying the given system tags.
func tagAutomation(id, arc string) *autonomous.Automation {
	return &autonomous.Automation{
		ID:         id,
		Name:       "arc " + id,
		SystemTags: []string{"automation:" + arc, "project:datawatch"},
	}
}

func prdsListPath(query string) string {
	if query == "" {
		return "/api/autonomous/prds"
	}
	return "/api/autonomous/prds?" + query
}

func doGet(t *testing.T, s *Server, path string) (status int, body string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr.Code, rr.Body.String()
}

func doAutomations(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutomations(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr.Code, rr.Body.String()
}

func TestObjectTagFilterAutomatonList(t *testing.T) {
	dw := tagPRD("dwa", "datawatch")
	other := tagPRD("oth", "other")
	s := newTagFilterServer([]any{dw, other}, nil)

	// No filter returns both, and the wire shape now carries system_tags.
	code, body := doGet(t, s, prdsListPath(""))
	if code != http.StatusOK || !strings.Contains(body, "datawatch") || !strings.Contains(body, "other") {
		t.Fatalf("unfiltered: status %d body %s", code, body)
	}
	if !strings.Contains(body, `system_tags`) {
		t.Fatalf("list must expose system_tags: %s", body)
	}

	// A repeatable comma-separated tag narrows to the matching Automaton.
	code, body = doGet(t, s, prdsListPath("tag=project:datawatch"))
	if code != http.StatusOK || strings.Contains(body, "other") || !strings.Contains(body, "datawatch") {
		t.Fatalf("tag=project:datawatch: status %d body %s", code, body)
	}

	// Repeatable parameters accumulate, and any-match keeps a hit on either.
	code, body = doGet(t, s, prdsListPath("tag=project:datawatch,automaton:dwa&tag=does-not-exist"))
	if code != http.StatusOK || !strings.Contains(body, "datawatch") || strings.Contains(body, "other") {
		t.Fatalf("accumulated any-match: status %d body %s", code, body)
	}

	// The project= shortcut matches the project tag.
	code, body = doGet(t, s, prdsListPath("project=datawatch"))
	if code != http.StatusOK || !strings.Contains(body, "datawatch") || strings.Contains(body, "other") {
		t.Fatalf("project=datawatch: status %d body %s", code, body)
	}
}

func TestObjectTagFilterAutomationList(t *testing.T) {
	a := tagAutomation("arc1", "nightly")
	b := tagAutomation("arc2", "weekly")
	s := newTagFilterServer(nil, []any{a, b})

	code, body := doAutomations(t, s, "/api/automations?tag=automation:nightly")
	if code != http.StatusOK || strings.Contains(body, "weekly") || !strings.Contains(body, "nightly") {
		t.Fatalf("tag=automation:nightly: status %d body %s", code, body)
	}

	// The automation= shortcut matches the automation tag.
	code, body = doAutomations(t, s, "/api/automations?automation=weekly")
	if code != http.StatusOK || !strings.Contains(body, "weekly") || strings.Contains(body, "nightly") {
		t.Fatalf("automation=weekly: status %d body %s", code, body)
	}
}

func TestObjectTagFilterTaskList(t *testing.T) {
	prd := tagPRD("cap0", "datawatch")
	prd.Story = []autonomous.Story{{
		ID:    "s1",
		PRDID: "cap0",
		Tasks: []autonomous.Task{
			{ID: "t1", StoryID: "s1", PRDID: "cap0", Status: autonomous.TaskCompleted, SystemTags: []string{"stage:implement"}},
			{ID: "t2", StoryID: "s1", PRDID: "cap0", Status: autonomous.TaskCompleted, SystemTags: []string{"stage:review"}},
		},
	}}
	s := newTagFilterServer([]any{prd}, nil)

	// No tag: both tasks come back.
	code, body := doGet(t, s, "/api/autonomous/prds/cap0?detail=tasks")
	if code != http.StatusOK || !strings.Contains(body, "\"t1\"") || !strings.Contains(body, "\"t2\"") {
		t.Fatalf("unfiltered tasks: status %d body %s", code, body)
	}

	// A tag narrows the listing to the matching task.
	code, body = doGet(t, s, "/api/autonomous/prds/cap0?detail=tasks&tag=stage:implement")
	if code != http.StatusOK || strings.Contains(body, "\"t2\"") || !strings.Contains(body, "\"t1\"") {
		t.Fatalf("tag=stage:implement: status %d body %s", code, body)
	}
}

func TestObjectTagFilterAllMatch(t *testing.T) {
	dw := tagPRD("dwboth", "datawatch") // project + automaton
	dw2 := tagPRD("dwp", "datawatch")   // project only
	s := newTagFilterServer([]any{dw, dw2}, nil)

	// any (the default) keeps both, since both carry project:datawatch.
	_, anyBody := doGet(t, s, prdsListPath("tag=project:datawatch,automaton:dwboth"))
	if !strings.Contains(anyBody, "dwp") || !strings.Contains(anyBody, "dwboth") {
		t.Fatalf("any-match should keep both: %s", anyBody)
	}

	// all keeps only the one carrying every requested tag.
	code, allBody := doGet(t, s, prdsListPath("tag=project:datawatch,automaton:dwboth&tag_match=all"))
	if code != http.StatusOK || strings.Contains(allBody, "dwp,") || strings.Contains(allBody, "\"dwp\"") || !strings.Contains(allBody, "dwboth") {
		t.Fatalf("all-match: status %d body %s", code, allBody)
	}
}

func TestObjectTagFilterBadTagMatchIs400(t *testing.T) {
	s := newTagFilterServer([]any{tagPRD("x", "datawatch")}, []any{tagAutomation("a", "x")})

	if code, body := doGet(t, s, prdsListPath("tag=x&tag_match=both")); code != http.StatusBadRequest || !strings.Contains(body, "tag_match") {
		t.Fatalf("automaton list bad tag_match: status %d body %s", code, body)
	}
	if code, body := doAutomations(t, s, "/api/automations?tag=x&tag_match=nope"); code != http.StatusBadRequest {
		t.Fatalf("automation list bad tag_match: status %d body %s", code, body)
	}
	if code, body := doGet(t, s, "/api/autonomous/prds/x?detail=tasks&tag=x&tag_match=everything"); code != http.StatusBadRequest {
		t.Fatalf("task list bad tag_match: status %d body %s", code, body)
	}
}
