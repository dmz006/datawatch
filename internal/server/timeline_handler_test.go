package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

var timelineTestNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// timelineFake is a manager double that supplies a fixed record source.
type timelineFake struct {
	fakeOrchAutonomous
	prds []*autonomous.PRD
}

func (f *timelineFake) TimelineRecords() []*autonomous.PRD { return f.prds }

func timelinePtr(t time.Time) *time.Time { return &t }

func timelineTestPRDs() []*autonomous.PRD {
	return []*autonomous.PRD{
		{
			ID: "aaaa0001", Title: "Fix the crash", Status: autonomous.PRDRunning,
			ProjectDir: "/home/dmz/projA", Kind: autonomous.PRDKindBug, TagIDs: []string{"tag1"},
			Story: []autonomous.Story{
				{
					ID: "aaaa0001-s1", Title: "Find and fix it", Status: autonomous.StoryInProgress,
					Tasks: []autonomous.Task{
						{ID: "aaaa0001-t1", Title: "Reproduce the crash", Status: autonomous.TaskCompleted,
							StartedAt:   timelinePtr(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)),
							CompletedAt: timelinePtr(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))},
						{ID: "aaaa0001-t2", Title: "Land the fix", Status: autonomous.TaskPending},
					},
				},
			},
		},
		{
			ID: "bbbb0002", Title: "Add the widget", Status: autonomous.PRDRunning,
			ProjectDir: "/home/dmz/projB", Kind: autonomous.PRDKindFeature,
			Story: []autonomous.Story{
				{
					ID: "bbbb0002-s1", Title: "Wire it up", Status: autonomous.StoryInProgress,
					Tasks: []autonomous.Task{
						{ID: "bbbb0002-t1", Title: "Build the widget", Status: autonomous.TaskCompleted,
							StartedAt:   timelinePtr(time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)),
							CompletedAt: timelinePtr(time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC))},
					},
				},
			},
		},
	}
}

func timelineServer(t *testing.T, mgr AutonomousAPI) *Server {
	t.Helper()
	return &Server{autonomousMgr: mgr, timelineNow: func() time.Time { return timelineTestNow }}
}

func timelineGet(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeTimeline(t *testing.T, rec *httptest.ResponseRecorder) autonomous.TimelineResult {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var r autonomous.TimelineResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("response is not a timeline object: %v\n%s", err, rec.Body.String())
	}
	return r
}

func TestTimelineRESTReturnsBuildTimelineObject(t *testing.T) {
	s := timelineServer(t, &timelineFake{prds: timelineTestPRDs()})
	rec := timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want, err := json.Marshal(autonomous.BuildTimeline(timelineTestNow, timelineTestPRDs(), autonomous.TimelineFilter{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSpace(rec.Body.Bytes()); !bytes.Equal(got, want) {
		t.Fatalf("response differs from BuildTimeline on the same input\n got: %s\nwant: %s", got, want)
	}
	res := decodeTimeline(t, rec)
	if res.Counts.Projects != 2 || res.Counts.Automata != 2 || res.Counts.Stages != 2 || res.Counts.Members != 3 {
		t.Fatalf("unexpected counts: %+v", res.Counts)
	}
	if got := res.Today.UTC(); !got.Equal(timelineTestNow) {
		t.Fatalf("today marker must be the server clock, got %s", got)
	}
}

func TestTimelineRESTFilters(t *testing.T) {
	s := timelineServer(t, &timelineFake{prds: timelineTestPRDs()})

	res := decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline?project=/home/dmz/projA"))
	if len(res.Projects) != 1 || res.Projects[0].Project != "/home/dmz/projA" || res.Counts.Automata != 1 {
		t.Fatalf("project filter: %+v", res)
	}

	// With automaton set only that Automaton's chart comes back — by id and by
	// title.
	for _, q := range []string{"automaton=aaaa0001", "automaton=" + strings.ReplaceAll("Fix the crash", " ", "+")} {
		res := decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline?"+q))
		if res.Counts.Automata != 1 {
			t.Fatalf("%s: want the single chart, got %+v", q, res.Counts)
		}
		if len(res.Projects) != 1 || len(res.Projects[0].Automata) != 1 || res.Projects[0].Automata[0].ID != "aaaa0001" {
			t.Fatalf("%s: wrong chart: %+v", q, res)
		}
	}

	res = decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline?stage=Wire+it+up"))
	if res.Counts.Automata != 1 || res.Counts.Stages != 1 || res.Projects[0].Automata[0].ID != "bbbb0002" {
		t.Fatalf("stage filter: %+v", res)
	}

	res = decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline?kind=bug"))
	if res.Counts.Automata != 1 || res.Projects[0].Automata[0].ID != "aaaa0001" {
		t.Fatalf("kind filter: %+v", res)
	}

	res = decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline?tag=tag1"))
	if res.Counts.Automata != 1 || res.Projects[0].Automata[0].ID != "aaaa0001" {
		t.Fatalf("tag filter: %+v", res)
	}
}

func TestTimelineRESTUnknownAutomatonIsEmpty(t *testing.T) {
	s := timelineServer(t, &timelineFake{prds: timelineTestPRDs()})
	res := decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/api/autonomous/timeline?automaton=ghost"))
	if res.Counts.Automata != 0 || len(res.Projects) != 0 {
		t.Fatalf("unknown automaton must come back empty: %+v", res)
	}
}

func TestTimelineRESTMethodNotAllowed(t *testing.T) {
	s := timelineServer(t, &timelineFake{prds: timelineTestPRDs()})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleAutonomousTimeline(rec, httptest.NewRequest(method, "/x", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
}

func TestTimelineRESTNoAutonomousManager(t *testing.T) {
	s := &Server{}
	rec := timelineGet(s.handleAutonomousTimeline, "/x")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "autonomous disabled") {
		t.Fatalf("want the neighbouring 503 empty state, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestTimelineRESTManagerWithoutProviderDrawsNothing(t *testing.T) {
	s := timelineServer(t, &fakeOrchAutonomous{})
	res := decodeTimeline(t, timelineGet(s.handleAutonomousTimeline, "/x"))
	if res.Counts.Automata != 0 || len(res.Projects) != 0 {
		t.Fatalf("no records: %+v", res)
	}
}

func TestTimelineRESTFederationDenial(t *testing.T) {
	s := timelineServer(t, &timelineFake{prds: timelineTestPRDs()})
	deny := httptest.NewRequest(http.MethodGet, "/x", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleAutonomousTimeline(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/x", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleAutonomousTimeline(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}
