// Tests for the project history read (GET /api/projects/{name}/history).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// historyNow is the fixed clock the pure-function tests read against.
var historyNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func historyPRD(id string, status autonomous.PRDStatus, updated time.Time) *autonomous.PRD {
	return &autonomous.PRD{
		ID: id, Title: "title " + id, Status: status,
		ProjectDir: "/srv/acme", CreatedAt: updated.Add(-48 * time.Hour), UpdatedAt: updated,
	}
}

func historySnap(date string, taken time.Time, reason string, byProject map[string]autonomous.ProgressGroup) autonomous.ProgressSnapshot {
	return autonomous.ProgressSnapshot{
		Date: date, TakenAt: taken, Reason: reason,
		Grouped: autonomous.ProgressGrouped{ByProject: byProject},
	}
}

func TestParseHistoryLimit(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"", historyDefaultLimit, false},
		{"5", 5, false},
		{"100000", historyMaxLimit, false},
		{"0", 0, true},
		{"-3", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseHistoryLimit(c.raw)
		if (err != nil) != c.wantErr {
			t.Errorf("parseHistoryLimit(%q) err = %v, wantErr %v", c.raw, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseHistoryLimit(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestHistoryCursorRoundTrip(t *testing.T) {
	e := historyEntry{Time: historyNow, key: historyEntryKey(historyKindAutomaton, "abc")}
	raw := encodeHistoryCursor(e)
	c, err := decodeHistoryCursor(raw)
	if err != nil || c == nil {
		t.Fatalf("decode: %v", err)
	}
	if c.T != historyNow.UnixNano() || c.K != e.key {
		t.Errorf("cursor = %+v", c)
	}
	if c, err := decodeHistoryCursor(""); err != nil || c != nil {
		t.Errorf("empty cursor = %+v, %v; want nil, nil", c, err)
	}
	if _, err := decodeHistoryCursor("!!!not-base64"); err == nil {
		t.Errorf("bad base64 cursor must fail")
	}
	if _, err := decodeHistoryCursor("bm90IGpzb24"); err == nil {
		t.Errorf("non-JSON cursor must fail")
	}
}

func TestBuildProjectHistory_SelectsFinishedAndArchivedOnly(t *testing.T) {
	done := historyPRD("done1", autonomous.PRDCompleted, historyNow.Add(-1*time.Hour))
	archived := historyPRD("arch1", autonomous.PRDArchived, historyNow.Add(-2*time.Hour))
	running := historyPRD("run1", autonomous.PRDRunning, historyNow.Add(-3*time.Hour))
	tmpl := historyPRD("tmpl1", autonomous.PRDCompleted, historyNow.Add(-4*time.Hour))
	tmpl.IsTemplate = true
	other := historyPRD("other1", autonomous.PRDCompleted, historyNow.Add(-5*time.Hour))
	other.ProjectDir = "/srv/elsewhere"
	out := buildProjectHistory(historyInput{
		Project: "acme", Dir: "/srv/acme",
		PRDs:  []*autonomous.PRD{done, archived, running, tmpl, other},
		Limit: 50, Now: historyNow,
	})
	ids := []string{}
	for _, it := range out.Items {
		if it.Kind == historyKindAutomaton {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) != 2 || ids[0] != "done1" || ids[1] != "arch1" {
		t.Errorf("automaton ids = %v, want [done1 arch1] newest first", ids)
	}
	if out.Total != 2 || out.HasMore || out.NextCursor != "" {
		t.Errorf("total/has_more/next = %d/%v/%q", out.Total, out.HasMore, out.NextCursor)
	}
}

func TestBuildProjectHistory_ArcHeldAutomatonCountsEvenOutsideDir(t *testing.T) {
	p := historyPRD("arc1", autonomous.PRDCompleted, historyNow.Add(-time.Hour))
	p.ProjectDir = "/srv/elsewhere"
	p.AutomationID = "auto-9"
	out := buildProjectHistory(historyInput{
		Project: "acme", Dir: "/srv/acme",
		Arcs:  []config.ProjectArc{{AutomationID: "auto-9"}},
		PRDs:  []*autonomous.PRD{p},
		Limit: 50, Now: historyNow,
	})
	if out.Total != 1 || out.Items[0].AutomationID != "auto-9" {
		t.Fatalf("items = %+v", out.Items)
	}
}

func TestBuildProjectHistory_MembershipViaAutomationMembers(t *testing.T) {
	p := historyPRD("m1", autonomous.PRDCompleted, historyNow.Add(-time.Hour))
	p.ProjectDir = "/srv/elsewhere"
	out := buildProjectHistory(historyInput{
		Project: "acme", Dir: "/srv/acme",
		Arcs:        []config.ProjectArc{{AutomationID: "auto-7"}},
		Automations: []*autonomous.Automation{{ID: "auto-7", Members: []autonomous.AutomationMember{{PRDID: "m1"}}}},
		PRDs:        []*autonomous.PRD{p},
		Limit:       50, Now: historyNow,
	})
	if out.Total != 1 || out.Items[0].AutomationID != "auto-7" {
		t.Fatalf("items = %+v", out.Items)
	}
}

func TestBuildProjectHistory_MergesSnapshotsNewestFirst(t *testing.T) {
	p := historyPRD("a1", autonomous.PRDCompleted, historyNow.Add(-6*time.Hour))
	snaps := []autonomous.ProgressSnapshot{
		historySnap("2026-11-07", historyNow.AddDate(0, 0, -2), autonomous.ProgressSnapshotDaily,
			map[string]autonomous.ProgressGroup{"acme": {PRDs: 3, Done: 1, Tasks: 10, TasksDone: 4}}),
		historySnap("2026-11-08", historyNow.AddDate(0, 0, -1), autonomous.ProgressSnapshotMilestone,
			map[string]autonomous.ProgressGroup{"acme": {PRDs: 3}}),
		historySnap("2026-11-08", historyNow.AddDate(0, 0, -1).Add(time.Hour), autonomous.ProgressSnapshotDaily,
			map[string]autonomous.ProgressGroup{"acme": {PRDs: 4, Done: 2}}),
		historySnap("2026-11-05", historyNow.AddDate(0, 0, -4), autonomous.ProgressSnapshotDaily, nil), // no groups
	}
	out := buildProjectHistory(historyInput{
		Project: "acme", Dir: "/srv/acme",
		PRDs: []*autonomous.PRD{p}, Snapshots: snaps, Limit: 50, Now: historyNow,
	})
	if out.Total != 3 {
		t.Fatalf("total = %d, want 3 (2 daily with groups + 1 automaton)", out.Total)
	}
	// Order: the automaton (finished 6h ago), then the 2026-11-08 snapshot (23h ago),
	// then the 2026-11-07 snapshot (48h ago).
	want := []string{historyKindAutomaton + ":a1", historyKindSnapshot + ":2026-11-08", historyKindSnapshot + ":2026-11-07"}
	for i, it := range out.Items {
		if it.key != want[i] {
			t.Errorf("item %d key = %q, want %q", i, it.key, want[i])
		}
	}
	snap := out.Items[1]
	if snap.Progress == nil || snap.Progress.PRDs != 4 || snap.Progress.Done != 2 || snap.Progress.Reason != autonomous.ProgressSnapshotDaily {
		t.Errorf("snapshot progress = %+v", snap.Progress)
	}
}

func TestBuildProjectHistory_PagesWithCursor(t *testing.T) {
	var prds []*autonomous.PRD
	for i := 0; i < 5; i++ {
		prds = append(prds, historyPRD(string(rune('a'+i)), autonomous.PRDCompleted, historyNow.Add(-time.Duration(i+1)*time.Hour)))
	}
	in := historyInput{Project: "acme", Dir: "/srv/acme", PRDs: prds, Limit: 2, Now: historyNow}

	var seen []string
	var cursor *historyCursor
	pages := 0
	for {
		in.Cursor = cursor
		out := buildProjectHistory(in)
		pages++
		for _, it := range out.Items {
			seen = append(seen, it.ID)
		}
		if out.Total != 5 {
			t.Fatalf("total = %d, want 5", out.Total)
		}
		if !out.HasMore {
			if out.NextCursor != "" {
				t.Errorf("last page has next_cursor %q", out.NextCursor)
			}
			break
		}
		c, err := decodeHistoryCursor(out.NextCursor)
		if err != nil || c == nil {
			t.Fatalf("next cursor decode: %v", err)
		}
		cursor = c
		if pages > 10 {
			t.Fatalf("paging did not terminate")
		}
	}
	want := []string{"a", "b", "c", "d", "e"}
	if len(seen) != len(want) {
		t.Fatalf("seen = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("seen = %v, want %v", seen, want)
			break
		}
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
}

func TestBuildProjectHistory_EmptyLists(t *testing.T) {
	out := buildProjectHistory(historyInput{Project: "acme", Dir: "/srv/acme", Now: historyNow})
	if out.Items == nil || out.Gantt == nil {
		t.Errorf("items and gantt must encode as [] not null")
	}
	if out.Total != 0 || out.HasMore {
		t.Errorf("empty history = %+v", out)
	}
	b, _ := json.Marshal(out)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	if _, ok := raw["items"].([]any); !ok {
		t.Errorf("items JSON = %v, want array", raw["items"])
	}
	if _, ok := raw["gantt"].([]any); !ok {
		t.Errorf("gantt JSON = %v, want array", raw["gantt"])
	}
}

func TestBuildProjectHistory_GanttRowsFromTimestamps(t *testing.T) {
	approved := historyNow.Add(-40 * time.Hour)
	started := historyNow.Add(-30 * time.Hour)
	completed := historyNow.Add(-20 * time.Hour)
	p := historyPRD("g1", autonomous.PRDCompleted, historyNow.Add(-10*time.Hour))
	p.ApprovedAt = &approved
	p.Story = []autonomous.Story{{ID: "s1", Tasks: []autonomous.Task{
		{ID: "t1", Title: "first", Status: autonomous.TaskCompleted, StartedAt: &started, CompletedAt: &completed},
		{ID: "t2", Title: "never started", Status: autonomous.TaskPending},
	}}}
	out := buildProjectHistory(historyInput{Project: "acme", Dir: "/srv/acme", PRDs: []*autonomous.PRD{p}, Limit: 50, Now: historyNow})
	if len(out.Gantt) != 3 {
		t.Fatalf("gantt rows = %d, want 3 (automaton + 2 tasks)", len(out.Gantt))
	}
	auto := out.Gantt[0]
	if auto.Kind != historyKindAutomaton || auto.AutomatonID != "g1" || auto.TaskID != "" {
		t.Errorf("automaton row = %+v", auto)
	}
	if auto.Start == nil || !auto.Start.Equal(approved) {
		t.Errorf("automaton start = %v, want ApprovedAt %v", auto.Start, approved)
	}
	if auto.End == nil || !auto.End.Equal(p.UpdatedAt) {
		t.Errorf("automaton end = %v, want UpdatedAt", auto.End)
	}
	t1 := out.Gantt[1]
	if t1.Kind != historyKindTask || t1.TaskID != "t1" || t1.State != string(autonomous.TaskCompleted) {
		t.Errorf("task row = %+v", t1)
	}
	if t1.Start == nil || !t1.Start.Equal(started) || t1.End == nil || !t1.End.Equal(completed) {
		t.Errorf("task t1 start/end = %v/%v", t1.Start, t1.End)
	}
	t2 := out.Gantt[2]
	if t2.Start != nil || t2.End != nil {
		t.Errorf("unstarted task must have null start/end, got %v/%v", t2.Start, t2.End)
	}
}

func TestBuildProjectHistory_AutomatonStartFallsBackToCreated(t *testing.T) {
	p := historyPRD("c1", autonomous.PRDCompleted, historyNow.Add(-time.Hour))
	out := buildProjectHistory(historyInput{Project: "acme", Dir: "/srv/acme", PRDs: []*autonomous.PRD{p}, Limit: 50, Now: historyNow})
	if out.Items[0].StartedAt == nil || !out.Items[0].StartedAt.Equal(p.CreatedAt) {
		t.Errorf("started_at = %v, want CreatedAt %v", out.Items[0].StartedAt, p.CreatedAt)
	}
}

func TestProjectHistory_HandlerErrors(t *testing.T) {
	s, _ := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: "/srv/acme"}}

	call := func(srv *Server, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rr := httptest.NewRecorder()
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/projects/{name}/history", srv.handleProjectHistory)
		mux.ServeHTTP(rr, req)
		return rr
	}

	if rr := call(s, http.MethodPost, "/api/projects/acme/history"); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rr.Code)
	}
	if rr := call(s, http.MethodGet, "/api/projects/missing/history"); rr.Code != http.StatusNotFound {
		t.Errorf("unknown project status = %d, want 404", rr.Code)
	}
	if rr := call(s, http.MethodGet, "/api/projects/acme/history?limit=0"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad limit status = %d, want 400", rr.Code)
	}
	if rr := call(s, http.MethodGet, "/api/projects/acme/history?cursor=!!!"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad cursor status = %d, want 400", rr.Code)
	}
	noMgr := &Server{cfg: s.cfg}
	if rr := call(noMgr, http.MethodGet, "/api/projects/acme/history"); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no automations manager status = %d, want 503", rr.Code)
	}
}

func TestProjectHistory_HandlerReturnsFinishedAutomata(t *testing.T) {
	dir := t.TempDir()
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: dir}}

	prd, err := mgr.CreatePRD("history spec", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	stored, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("PRD %s not in store", prd.ID)
	}
	finished := time.Now()
	stored.Status = autonomous.PRDArchived
	stored.UpdatedAt = finished
	if err := mgr.Store().SavePRD(stored); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme/history?limit=10", nil)
	req.SetPathValue("name", "acme")
	rr := httptest.NewRecorder()
	s.handleProjectHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got projectHistory
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Project != "acme" || got.Limit != 10 {
		t.Errorf("project/limit = %q/%d", got.Project, got.Limit)
	}
	if len(got.Items) != 1 || got.Items[0].ID != prd.ID || got.Items[0].Status != string(autonomous.PRDArchived) {
		t.Fatalf("items = %+v, want the archived PRD", got.Items)
	}
	if len(got.Gantt) != 1 || got.Gantt[0].AutomatonID != prd.ID {
		t.Errorf("gantt = %+v, want one automaton row", got.Gantt)
	}
}
