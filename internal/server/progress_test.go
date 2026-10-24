package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

var progressTestNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// progressFake is a manager double that supplies a fixed record source and
// snapshot store.
type progressFake struct {
	fakeOrchAutonomous
	prds  []*autonomous.PRD
	snaps *autonomous.ProgressSnapshotStore
}

type progressFakeSource struct{ prds []*autonomous.PRD }

func (f progressFakeSource) ListPRDs() []*autonomous.PRD { return f.prds }

func (p *progressFake) ProgressSource() autonomous.ProgressSource {
	return progressFakeSource{prds: p.prds}
}
func (p *progressFake) ProgressSnapshots() *autonomous.ProgressSnapshotStore { return p.snaps }

func progressTask(id string, status autonomous.TaskStatus, start, done time.Time) autonomous.Task {
	t := autonomous.Task{ID: id, Status: status}
	if !start.IsZero() {
		t.StartedAt = &start
	}
	if !done.IsZero() {
		t.CompletedAt = &done
	}
	return t
}

// progressPRDs returns four records: two completed (one in an Automation, one
// carrying a tag), one running and one waiting.
func progressPRDs() []*autonomous.PRD {
	h := func(d, hr int) time.Time { return time.Date(2026, 10, d, hr, 0, 0, 0, time.UTC) }
	return []*autonomous.PRD{
		{
			ID: "aaaa0001", Title: "Fix the crash", Status: autonomous.PRDCompleted, CreatedAt: h(12, 8),
			AutomationID: "auto1", Kind: autonomous.PRDKindBug,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{
				progressTask("t1", autonomous.TaskCompleted, h(12, 9), h(12, 11)),
				progressTask("t2", autonomous.TaskCompleted, h(12, 11), h(12, 14)),
			}}},
		},
		{
			ID: "aaaa0002", Title: "Add the widget", Status: autonomous.PRDCompleted, CreatedAt: h(18, 8),
			TagIDs: []string{"tag1"}, Kind: autonomous.PRDKindFeature,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{
				progressTask("t3", autonomous.TaskCompleted, h(18, 9), h(18, 12)),
			}}},
		},
		{
			ID: "aaaa0003", Title: "Add the gadget", Status: autonomous.PRDRunning, CreatedAt: h(21, 8),
			TagIDs: []string{"tag1"}, Kind: autonomous.PRDKindFeature,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{
				progressTask("t4", autonomous.TaskCompleted, h(22, 9), h(22, 10)),
				progressTask("t5", autonomous.TaskPending, time.Time{}, time.Time{}),
			}}},
		},
		{
			ID: "aaaa0004", Title: "Tidy the notes", Status: autonomous.PRDApproved, CreatedAt: h(22, 8),
			Kind: autonomous.PRDKindOther,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{
				progressTask("t6", autonomous.TaskPending, time.Time{}, time.Time{}),
			}}},
		},
	}
}

// progressServer builds a server with the fake manager, the fixed clock and a
// config whose backlog sits in dir.
func progressServer(t *testing.T, fake *progressFake, dir string) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Autonomous.Progress.BacklogPath = filepath.Join(dir, "README.md")
	return &Server{
		autonomousMgr: fake,
		cfg:           cfg,
		progressNow:   func() time.Time { return progressTestNow },
	}
}

const progressTestBacklog = "| 1 | Add the gadget `aaaa0003` | Running |\n| 2 | Write the guide | Not started |\n"

func writeProgressFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func progressGet(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeProgress(t *testing.T, rec *httptest.ResponseRecorder) autonomous.ProgressMetrics {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var m autonomous.ProgressMetrics
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not a metrics object: %v\n%s", err, rec.Body.String())
	}
	return m
}

func TestProgressRESTReturnsComputeProgressObject(t *testing.T) {
	dir := t.TempDir()
	writeProgressFile(t, filepath.Join(dir, "README.md"), progressTestBacklog)
	writeProgressFile(t, filepath.Join(dir, "operator-wait-windows.tsv"),
		"# start\tend\tkind\treason\n2026-10-19T00:00:00Z\t2026-10-20T00:00:00Z\toperator_wait\tweekend\n")
	fake := &progressFake{prds: progressPRDs()}
	s := progressServer(t, fake, dir)

	rec := progressGet(s.handleAutonomousProgress, "/api/autonomous/progress")
	m := decodeProgress(t, rec)

	windows, err := autonomous.ParseWindowsTSV(strings.NewReader(
		"2026-10-19T00:00:00Z\t2026-10-20T00:00:00Z\toperator_wait\tweekend\n"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := autonomous.LoadBacklog(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := autonomous.ComputeProgress(autonomous.ProgressInput{
		PRDs: fake.prds, Backlog: rows, Windows: windows,
		Options: autonomous.ProgressOptions{Now: progressTestNow, Lanes: 5, GapHours: 3},
	})
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSpace(rec.Body.Bytes()); !bytes.Equal(got, wantJSON) {
		t.Fatalf("response differs from ComputeProgress on the same input\n got: %s\nwant: %s", got, wantJSON)
	}
	if m.Scope != "all" || !m.BacklogAvailable || m.Totals.PRDs != 4 || m.CompletedPRDs != 2 {
		t.Fatalf("unexpected metrics: scope=%q backlog=%v totals=%d completed=%d", m.Scope, m.BacklogAvailable, m.Totals.PRDs, m.CompletedPRDs)
	}
	if len(m.Inactive.Windows) == 0 {
		t.Fatal("the explicit windows file next to the backlog was not read")
	}
	if len(m.UnfiledRows) != 1 {
		t.Fatalf("want 1 unfiled backlog row, got %d", len(m.UnfiledRows))
	}
}

func TestProgressRESTScopeFilters(t *testing.T) {
	dir := t.TempDir()
	s := progressServer(t, &progressFake{prds: progressPRDs()}, dir)

	m := decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress?tag_id=tag1"))
	if m.Scope != "tag:tag1" || m.Totals.PRDs != 2 {
		t.Fatalf("tag scope: scope=%q prds=%d", m.Scope, m.Totals.PRDs)
	}
	m = decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress?automation_id=auto1"))
	if m.Scope != "automation:auto1" || m.Totals.PRDs != 1 || m.Totals.Done != 1 {
		t.Fatalf("automation scope: scope=%q prds=%d done=%d", m.Scope, m.Totals.PRDs, m.Totals.Done)
	}
	m = decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress?automation_id=auto1&tag_id=tag1"))
	if m.Scope != "automation:auto1+tag:tag1" || m.Totals.PRDs != 0 {
		t.Fatalf("both scopes: scope=%q prds=%d", m.Scope, m.Totals.PRDs)
	}
}

func TestProgressRESTSinceAndUntil(t *testing.T) {
	s := progressServer(t, &progressFake{prds: progressPRDs()}, t.TempDir())

	m := decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress?since=2026-10-20"))
	if m.Totals.PRDs != 2 {
		t.Fatalf("since 20th: want the 2 later records, got %d", m.Totals.PRDs)
	}
	if got := m.Since.UTC().Format("2006-01-02"); got != "2026-10-20" {
		t.Fatalf("since not applied: %s", got)
	}
	// until names a whole day: a record created that day at 08:00 still counts.
	m = decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress?until=2026-10-18"))
	if m.Totals.PRDs != 2 {
		t.Fatalf("until 18th: want the 2 earlier records, got %d", m.Totals.PRDs)
	}
}

func TestProgressRESTBadParameters(t *testing.T) {
	s := progressServer(t, &progressFake{prds: progressPRDs()}, t.TempDir())
	for _, target := range []string{
		"/api/autonomous/progress?since=yesterday",
		"/api/autonomous/progress?since=2026-13-01",
		"/api/autonomous/progress?until=23/10/2026",
		"/api/autonomous/progress?since=2026-10-20&until=2026-10-10",
	} {
		rec := progressGet(s.handleAutonomousProgress, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", target, rec.Code)
		}
	}
	for _, target := range []string{
		"/api/autonomous/progress/trend?days=0",
		"/api/autonomous/progress/trend?days=-3",
		"/api/autonomous/progress/trend?days=abc",
		"/api/autonomous/progress/trend?days=2.5",
		"/api/autonomous/progress/trend?days=366",
	} {
		rec := progressGet(s.handleAutonomousProgressTrend, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", target, rec.Code)
		}
	}
}

func TestProgressRESTMissingBacklogIsNotAnError(t *testing.T) {
	s := progressServer(t, &progressFake{prds: progressPRDs()}, t.TempDir()) // no README.md in the dir
	m := decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress"))
	if m.BacklogAvailable {
		t.Fatal("backlog_available must be false when the file is missing")
	}
	if m.Totals.PRDs != 4 {
		t.Fatalf("records must still be counted, got %d", m.Totals.PRDs)
	}

	// No config at all behaves the same.
	s.cfg = nil
	m = decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress"))
	if m.BacklogAvailable || m.Totals.PRDs != 4 {
		t.Fatalf("nil config: backlog=%v prds=%d", m.BacklogAvailable, m.Totals.PRDs)
	}
}

func TestProgressRESTManagerWithoutSourceCountsNothing(t *testing.T) {
	s := &Server{autonomousMgr: &fakeOrchAutonomous{}, progressNow: func() time.Time { return progressTestNow }}
	m := decodeProgress(t, progressGet(s.handleAutonomousProgress, "/api/autonomous/progress"))
	if m.Totals.PRDs != 0 || len(m.Groups) != 3 {
		t.Fatalf("empty source: prds=%d groups=%d", m.Totals.PRDs, len(m.Groups))
	}
	rec := progressGet(s.handleAutonomousProgressTrend, "/api/autonomous/progress/trend")
	var tr struct {
		Days      int               `json:"days"`
		Points    []json.RawMessage `json:"points"`
		Direction string            `json:"direction"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("trend without a store: code=%d err=%v", rec.Code, err)
	}
	if tr.Days != 30 || tr.Points == nil || len(tr.Points) != 0 || tr.Direction != "unknown" {
		t.Fatalf("trend without a store: %+v", tr)
	}
}

func TestProgressRESTNoAutonomousManager(t *testing.T) {
	s := &Server{}
	for name, h := range map[string]http.HandlerFunc{
		"progress": s.handleAutonomousProgress,
		"trend":    s.handleAutonomousProgressTrend,
	} {
		rec := progressGet(h, "/x")
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "autonomous disabled") {
			t.Errorf("%s: want the neighbouring 503 empty state, got %d %q", name, rec.Code, rec.Body.String())
		}
	}
}

func TestProgressRESTOperationsParity(t *testing.T) {
	s := progressServer(t, &progressFake{prds: progressPRDs()}, t.TempDir())
	handlers := map[string]http.HandlerFunc{
		"progress":       s.handleAutonomousProgress,
		"progress_trend": s.handleAutonomousProgressTrend,
	}
	ops := ProgressRESTOperations()
	if len(ops) != len(handlers) {
		t.Fatalf("want %d operations, got %d", len(handlers), len(ops))
	}
	for _, op := range ops {
		h, ok := handlers[op.Name]
		if !ok {
			t.Fatalf("operation %q has no handler", op.Name)
		}
		if op.Method != http.MethodGet {
			t.Fatalf("operation %q: want GET, got %s", op.Name, op.Method)
		}
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(op.Method, "/x", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %s want 200, got %d", op.Name, op.Method, rec.Code)
		}
		for _, bad := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(bad, "/x", nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: %s want 405, got %d", op.Name, bad, rec.Code)
			}
		}
	}
	// The returned list is a copy.
	ops[0].Name = "changed"
	if ProgressRESTOperations()[0].Name != "progress" {
		t.Fatal("ProgressRESTOperations must return a copy")
	}
}

func TestProgressRESTFederationDenial(t *testing.T) {
	s := progressServer(t, &progressFake{prds: progressPRDs()}, t.TempDir())
	for name, h := range map[string]http.HandlerFunc{
		"progress": s.handleAutonomousProgress,
		"trend":    s.handleAutonomousProgressTrend,
	} {
		deny := httptest.NewRequest(http.MethodGet, "/x", nil)
		deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
		rec := httptest.NewRecorder()
		h(rec, deny)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
			t.Errorf("%s without the read capability: want 403 naming it, got %d %q", name, rec.Code, rec.Body.String())
		}

		allow := httptest.NewRequest(http.MethodGet, "/x", nil)
		allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
		rec = httptest.NewRecorder()
		h(rec, allow)
		if rec.Code != http.StatusOK {
			t.Errorf("%s with the read capability: want 200, got %d", name, rec.Code)
		}
	}
}

func TestProgressRESTDeniedMethodStillChecksMethodFirst(t *testing.T) {
	// The capability check comes after the method guard, so a wrong method is a
	// 405 even for a caller that lacks the capability.
	s := progressServer(t, &progressFake{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleAutonomousProgress(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestProgressRESTTrendFromSeededSnapshots(t *testing.T) {
	store, _, err := autonomous.NewProgressSnapshotStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := func(v float64) *float64 { return &v }
	seed := func(day int, low, high *float64, done int) {
		m := autonomous.ProgressMetrics{CompletedPRDs: done}
		m.Completion.LowDays, m.Completion.HighDays = low, high
		at := time.Date(2026, 10, day, 6, 0, 0, 0, time.UTC)
		if err := store.RecordDaily(at, m); err != nil {
			t.Fatal(err)
		}
	}
	seed(1, f(5), f(25), 1)  // outside a 7-day window ending the 23rd
	seed(18, f(5), f(25), 2) // width 20
	seed(20, f(6), f(20), 3) // width 14
	seed(23, f(7), f(12), 5) // width 5
	s := progressServer(t, &progressFake{snaps: store}, t.TempDir())

	rec := progressGet(s.handleAutonomousProgressTrend, "/api/autonomous/progress/trend?days=7")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var tr struct {
		Days   int `json:"days"`
		Points []struct {
			Date          string `json:"date"`
			CompletedPRDs int    `json:"completed_prds"`
		} `json:"points"`
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Days != 7 || len(tr.Points) != 3 || tr.Direction != autonomous.ProgressTrendConverging {
		t.Fatalf("7-day trend: %+v", tr)
	}
	if tr.Points[0].Date != "2026-10-18" || tr.Points[2].CompletedPRDs != 5 {
		t.Fatalf("points not oldest first from the window: %+v", tr.Points)
	}

	// The default window is 30 days and includes the early snapshot.
	rec = progressGet(s.handleAutonomousProgressTrend, "/api/autonomous/progress/trend")
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Days != 30 || len(tr.Points) != 4 {
		t.Fatalf("default trend: days=%d points=%d", tr.Days, len(tr.Points))
	}

	// The body is exactly days, points and direction.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("want exactly days, points and direction, got %v", keys)
	}
}
