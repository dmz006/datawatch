package server

import (
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

// repoCIWatchSpy is the double for the repo CI watch read route: a
// fixed read view and a count of read calls so the tests can prove a
// refused request never reaches the manager.
type repoCIWatchSpy struct {
	fakeOrchAutonomous
	view  autonomous.RepoCIWatchStatus
	calls int
}

func (s *repoCIWatchSpy) RepoCIWatchStatus() autonomous.RepoCIWatchStatus {
	s.calls++
	return s.view
}

func repoCIWatchGET(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleRepoCIWatch(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/repo-ci-watch"+query, nil))
	return rec
}

func repoCIWatchBaseTime() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }

// repoCIWatchFixtureView is the read view the route serves: three
// records across two repositories, with refresh times that differ on
// purpose — the route must return them newest first whatever order the
// view yields them.
func repoCIWatchFixtureView() autonomous.RepoCIWatchStatus {
	base := repoCIWatchBaseTime()
	return autonomous.RepoCIWatchStatus{
		Mode:         autonomous.RepoCIWatchModeShadow,
		Repos:        []string{"dmz006/datawatch", "dmz006/other"},
		AlertMinutes: 30,
		LastPollAt:   base.Add(-time.Minute),
		Records: []*autonomous.RepoCIWatchRecord{
			{
				Repo: "dmz006/datawatch", RefKind: "main", RefName: "main",
				SHA: "aaa", Workflow: "ci.yml", RunID: "1",
				Status: "green", FirstSeenAt: base, UpdatedAt: base,
			},
			{
				Repo: "dmz006/datawatch", RefKind: "main", RefName: "main",
				SHA: "bbb", Workflow: "ci.yml", RunID: "2",
				Status: "failing", Classification: "infra",
				FirstSeenAt: base, UpdatedAt: base.Add(time.Minute),
			},
			{
				Repo: "dmz006/other", RefKind: "tag", RefName: "v1.2.0",
				SHA: "ccc", Workflow: "security.yml", RunID: "3",
				Status: "failing", Classification: "security",
				FirstSeenAt: base, UpdatedAt: base.Add(2 * time.Minute),
			},
		},
	}
}

func repoCIWatchFixtureServer() (*Server, *repoCIWatchSpy) {
	spy := &repoCIWatchSpy{view: repoCIWatchFixtureView()}
	return &Server{autonomousMgr: spy}, spy
}

func repoCIWatchRecords(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var got struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	return got.Records
}

func TestRepoCIWatchRESTShape(t *testing.T) {
	s, _ := repoCIWatchFixtureServer()
	rec := repoCIWatchGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	var body struct {
		Mode         string   `json:"mode"`
		AlertMinutes int      `json:"alert_minutes"`
		Repos        []string `json:"repos"`
		LastPollAt   string   `json:"last_poll_at"`
		Records      []any    `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if body.Mode != autonomous.RepoCIWatchModeShadow {
		t.Errorf("mode = %q, want %q", body.Mode, autonomous.RepoCIWatchModeShadow)
	}
	if body.AlertMinutes != 30 {
		t.Errorf("alert_minutes = %d, want 30", body.AlertMinutes)
	}
	if strings.Join(body.Repos, ",") != "dmz006/datawatch,dmz006/other" {
		t.Errorf("repos = %v, want the configured paths", body.Repos)
	}
	if body.LastPollAt == "" {
		t.Errorf("last_poll_at is empty; want the coordinator's last poll instant")
	}
	if len(body.Records) != 3 {
		t.Fatalf("records = %d, want all three stored records", len(body.Records))
	}
}

func TestRepoCIWatchRESTNewestFirst(t *testing.T) {
	s, _ := repoCIWatchFixtureServer()
	rec := repoCIWatchGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	got := []string{}
	for _, r := range repoCIWatchRecords(t, rec.Body.Bytes()) {
		got = append(got, r["run_id"].(string))
	}
	want := []string{"3", "2", "1"} // newest UpdatedAt first
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("record order = %v, want newest first %v", got, want)
	}
}

func TestRepoCIWatchRESTAlertMinutesZeroWhenOff(t *testing.T) {
	spy := &repoCIWatchSpy{view: autonomous.RepoCIWatchStatus{
		Mode: autonomous.RepoCIWatchModeOff, Repos: []string{}, Records: []*autonomous.RepoCIWatchRecord{},
	}}
	s := &Server{autonomousMgr: spy}
	rec := repoCIWatchGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Mode         string `json:"mode"`
		AlertMinutes int    `json:"alert_minutes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if body.Mode != "off" || body.AlertMinutes != 0 {
		t.Errorf("mode %q / alert_minutes %d; want off / 0 while the watch is off", body.Mode, body.AlertMinutes)
	}
}

func TestRepoCIWatchRESTFilters(t *testing.T) {
	cases := []struct {
		query                string
		wantCount            int
		wantRepo, wantStatus string
	}{
		{`?repo=dmz006/datawatch`, 2, "dmz006/datawatch", ""},
		{`?repo=dmz006/other`, 1, "dmz006/other", ""},
		{`?status=failing`, 2, "", "failing"},
		{`?status=green`, 1, "", "green"},
		{`?repo=dmz006/datawatch&status=failing`, 1, "dmz006/datawatch", "failing"},
		{`?repo=nobody`, 0, "", ""},
		{`?status=superseded`, 0, "", ""},
		{`?repo=&status=&x=ignored`, 3, "", ""},
	}
	for _, c := range cases {
		t.Run("query="+c.query, func(t *testing.T) {
			s, _ := repoCIWatchFixtureServer()
			rec := repoCIWatchGET(t, s, c.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			records := repoCIWatchRecords(t, rec.Body.Bytes())
			if len(records) != c.wantCount {
				t.Fatalf("records = %d, want %d: %v", len(records), c.wantCount, records)
			}
			for _, r := range records {
				if c.wantRepo != "" && r["repo"] != c.wantRepo {
					t.Errorf("record out of the filter: %v", r)
				}
				if c.wantStatus != "" && r["status"] != c.wantStatus {
					t.Errorf("record out of the filter: %v", r)
				}
			}
		})
	}
}

func TestRepoCIWatchRESTBadStatusIsRefused(t *testing.T) {
	for _, query := range []string{`?status=pending-green`, `?status=Failing`, `?status=done`} {
		s, spy := repoCIWatchFixtureServer()
		rec := repoCIWatchGET(t, s, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: want 400, got %d: %s", query, rec.Code, rec.Body.String())
		}
		for _, name := range []string{"green", "failing", "pending", "hung", "rerunning", "superseded"} {
			if !strings.Contains(rec.Body.String(), `"`+name+`"`) {
				t.Errorf("query %q: refusal lost a status name %q: %s", query, name, rec.Body.String())
			}
		}
		if spy.calls != 0 {
			t.Errorf("query %q: a refused status must not reach the manager", query)
		}
	}
}

func TestRepoCIWatchRESTMatchesNothingListsNothing(t *testing.T) {
	s, _ := repoCIWatchFixtureServer()
	rec := repoCIWatchGET(t, s, `?repo=nobody`)
	if rec.Code != http.StatusOK { // a read that matches nothing is an empty list, not a 404
		t.Fatalf("want 200 with an empty list, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"records":[]`) {
		t.Errorf("body lacks the empty list: %s", body)
	}
	if strings.Contains(rec.Body.String(), `"dmz006/datawatch","ref`) ||
		strings.Contains(rec.Body.String(), `"run_id"`) {
		t.Errorf("a filter that matches nothing must yield no records: %s", rec.Body)
	}
}

func TestRepoCIWatchRESTMethodGuard(t *testing.T) {
	s, spy := repoCIWatchFixtureServer()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleRepoCIWatch(rec, httptest.NewRequest(method, "/api/autonomous/repo-ci-watch", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if spy.calls != 0 {
		t.Errorf("refused methods must not reach the manager, got %d calls", spy.calls)
	}
}

func TestRepoCIWatchRESTDeniedWithoutCapability(t *testing.T) {
	s, _ := repoCIWatchFixtureServer()
	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/repo-ci-watch", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleRepoCIWatch(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/repo-ci-watch", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleRepoCIWatch(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}

func TestRepoCIWatchRESTAutonomousDisabled(t *testing.T) {
	s := &Server{}
	rec := repoCIWatchGET(t, s, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 with the manager down, got %d", rec.Code)
	}
}
