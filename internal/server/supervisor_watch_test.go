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

// supervisorWatchSpy is the double for the supervisor watch read route: a
// fixed read view and a count of read calls so the tests can prove a
// refused request never reaches the manager.
type supervisorWatchSpy struct {
	fakeOrchAutonomous
	view  autonomous.SupervisorWatchStatus
	calls int
}

func (s *supervisorWatchSpy) SupervisorWatchStatus() autonomous.SupervisorWatchStatus {
	s.calls++
	return s.view
}

func supervisorWatchGET(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSupervisorWatch(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch", nil))
	return rec
}

func supervisorWatchBaseTime() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }

// supervisorWatchFixtureView is the read view the route serves: two open
// findings (one shadow-marked alert-due, one with a raised alert) and a
// comparison view carrying one last comparison with a one-sided
// difference.
func supervisorWatchFixtureView() autonomous.SupervisorWatchStatus {
	base := supervisorWatchBaseTime()
	alertedAt := base.Add(40 * time.Minute)
	return autonomous.SupervisorWatchStatus{
		Mode:       autonomous.SupervisorWatchModeShadow,
		LastPollAt: base.Add(-time.Minute),
		Findings: []*autonomous.SupervisorWatchFinding{
			{
				Kind:        autonomous.SupervisorWatchTaskFailed,
				Subject:     "1001",
				Text:        "Automaton 1001 task failed: its last task ended non-zero",
				FirstSeenAt: base,
				LastSeenAt:  base.Add(time.Minute),
				WouldAlert:  true,
			},
			{
				Kind:        autonomous.SupervisorWatchCIRed,
				Subject:     "42",
				Text:        "CI run 42 is red on main",
				FirstSeenAt: base.Add(-50 * time.Minute),
				LastSeenAt:  base.Add(2 * time.Minute),
				AlertedAt:   &alertedAt,
			},
		},
		Comparison: autonomous.SupervisorWatchComparisonView{
			Last: &autonomous.SupervisorWatchComparison{
				At:          base.Add(-time.Minute),
				WatchKinds:  []string{"task_failed/1001"},
				ScriptKinds: []string{},
				OnlyWatch:   []string{"task_failed/1001"},
				OnlyScript:  []string{},
				Agreed:      false,
			},
			AgreedDays: 2,
			OnlyWatch:  []string{"task_failed/1001"},
			OnlyScript: []string{},
		},
	}
}

func supervisorWatchFixtureServer() (*Server, *supervisorWatchSpy) {
	spy := &supervisorWatchSpy{view: supervisorWatchFixtureView()}
	return &Server{autonomousMgr: spy}, spy
}

// TestSupervisorWatchRESTEmptyStore — an empty store reads as an empty
// findings list and an empty comparison view, never a 404 and never
// nulls where the shape promises a list.
func TestSupervisorWatchRESTEmptyStore(t *testing.T) {
	spy := &supervisorWatchSpy{view: autonomous.SupervisorWatchStatus{
		Mode:       autonomous.SupervisorWatchModeShadow,
		Findings:   []*autonomous.SupervisorWatchFinding{},
		Comparison: autonomous.SupervisorWatchComparisonView{OnlyWatch: []string{}, OnlyScript: []string{}},
	}}
	s := &Server{autonomousMgr: spy}
	rec := supervisorWatchGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"findings":[]`) {
		t.Errorf("body lacks the empty findings list: %s", body)
	}
	if !strings.Contains(body, `"last":null`) {
		t.Errorf("body lacks the null last comparison: %s", body)
	}
	if !strings.Contains(body, `"agreed_days":0`) {
		t.Errorf("body lacks agreed_days 0: %s", body)
	}
	if spy.calls != 1 {
		t.Errorf("manager read calls = %d, want exactly one", spy.calls)
	}
}

// TestSupervisorWatchRESTPopulated — the stored findings come back with
// their identity, the operator line, the seen instants and the alert
// marks intact, and the comparison view carries the last comparison.
func TestSupervisorWatchRESTPopulated(t *testing.T) {
	s, _ := supervisorWatchFixtureServer()
	rec := supervisorWatchGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Mode       string `json:"mode"`
		LastPollAt string `json:"last_poll_at"`
		Findings   []struct {
			Kind       string  `json:"kind"`
			Subject    string  `json:"subject"`
			Text       string  `json:"text"`
			FirstSeen  string  `json:"first_seen_at"`
			LastSeen   string  `json:"last_seen_at"`
			WouldAlert bool    `json:"would_alert"`
			AlertedAt  *string `json:"alerted_at"`
		} `json:"findings"`
		Comparison struct {
			Last *struct {
				Agreed bool `json:"agreed"`
			} `json:"last"`
			AgreedDays int      `json:"agreed_days"`
			OnlyWatch  []string `json:"only_watch"`
			OnlyScript []string `json:"only_script"`
		} `json:"comparison"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}

	if body.LastPollAt == "" {
		t.Errorf("last_poll_at is empty; want the coordinator's last poll instant")
	}
	if len(body.Findings) != 2 {
		t.Fatalf("findings = %d, want both stored findings", len(body.Findings))
	}
	failed, red := body.Findings[0], body.Findings[1]
	if failed.Kind != "task_failed" || failed.Subject != "1001" {
		t.Errorf("finding 0 = %q/%q, want task_failed/1001", failed.Kind, failed.Subject)
	}
	if failed.Text == "" {
		t.Errorf("finding 0 lost its operator line")
	}
	if failed.FirstSeen == "" || failed.LastSeen == "" {
		t.Errorf("finding 0 lost its seen instants")
	}
	if !failed.WouldAlert {
		t.Errorf("finding 0 lost its shadow would-alert mark")
	}
	if red.Kind != "ci_red" || red.Subject != "42" {
		t.Errorf("finding 1 = %q/%q, want ci_red/42", red.Kind, red.Subject)
	}
	if red.AlertedAt == nil {
		t.Errorf("finding 1 lost its alerted_at")
	}

	if body.Comparison.Last == nil {
		t.Errorf("comparison.last is null; want the stored last comparison")
	}
	if body.Comparison.Last != nil && body.Comparison.Last.Agreed {
		t.Errorf("comparison.last.agreed = true, want false")
	}
	if body.Comparison.AgreedDays != 2 {
		t.Errorf("comparison.agreed_days = %d, want 2", body.Comparison.AgreedDays)
	}
	if strings.Join(body.Comparison.OnlyWatch, ",") != "task_failed/1001" {
		t.Errorf("comparison.only_watch = %v, want [task_failed/1001]", body.Comparison.OnlyWatch)
	}
	if body.Comparison.OnlyScript == nil || len(body.Comparison.OnlyScript) != 0 {
		t.Errorf("comparison.only_script = %v, want an empty list", body.Comparison.OnlyScript)
	}
}

// TestSupervisorWatchRESTModeInPayload — the mode in effect rides in the
// payload, shadow and on both, and is what the payload says.
func TestSupervisorWatchRESTModeInPayload(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{autonomous.SupervisorWatchModeShadow, "shadow"},
		{autonomous.SupervisorWatchModeOn, "on"},
		{autonomous.SupervisorWatchModeOff, "off"},
	}
	for _, c := range cases {
		spy := &supervisorWatchSpy{view: autonomous.SupervisorWatchStatus{Mode: c.mode}}
		s := &Server{autonomousMgr: spy}
		rec := supervisorWatchGET(t, s)
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %q: want 200, got %d", c.mode, rec.Code)
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("mode %q: decode: %v", c.mode, err)
		}
		if body.Mode != c.want {
			t.Errorf("mode = %q, want %q", body.Mode, c.want)
		}
	}
}

// TestSupervisorWatchRESTMethodNotAllowed — the route is read-only:
// POST (and the other write methods) is refused with a 405 before the
// manager is read.
func TestSupervisorWatchRESTMethodNotAllowed(t *testing.T) {
	s, spy := supervisorWatchFixtureServer()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleSupervisorWatch(rec, httptest.NewRequest(method, "/api/autonomous/supervisor-watch", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if spy.calls != 0 {
		t.Errorf("refused methods must not reach the manager, got %d calls", spy.calls)
	}
}

// TestSupervisorWatchRESTAuthentication — the capability gate behaves
// like the sibling read routes: without the autonomous read capability
// the request is refused with a 403 naming it, with it the same request
// is served.
func TestSupervisorWatchRESTAuthentication(t *testing.T) {
	s, _ := supervisorWatchFixtureServer()

	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleSupervisorWatch(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleSupervisorWatch(rec, allow)
	if rec.Code != http.StatusOK {
		t.Errorf("with the read capability: want 200, got %d", rec.Code)
	}

	// the manager is still down when autonomous is disabled, like the
	// sibling routes
	off := &Server{}
	rec = supervisorWatchGET(t, off)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("autonomous disabled: want 503, got %d", rec.Code)
	}
}
