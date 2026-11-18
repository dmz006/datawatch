package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// prdIndexFake serves the list from entries and counts every full-record read.
type prdIndexFake struct {
	prdListFake
	rev       uint64
	listCalls int
	getCalls  int
}

func (f *prdIndexFake) ListPRDs() []any { f.listCalls++; return f.prdListFake.ListPRDs() }

func (f *prdIndexFake) GetPRD(id string) (any, bool) {
	f.getCalls++
	for _, p := range f.prds {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

func (f *prdIndexFake) SummaryIndexEntries() []autonomous.SummaryEntry {
	out := make([]autonomous.SummaryEntry, 0, len(f.prds))
	for _, p := range f.prds {
		out = append(out, autonomous.SummaryEntryOf(p))
	}
	return out
}
func (f *prdIndexFake) SummaryIndexRevision() uint64 { return f.rev }
func (f *prdIndexFake) NotifyEffectiveForEntry(autonomous.SummaryEntry) autonomous.NotifyEventSet {
	return autonomous.NotifyEventSet{Events: []string{"x"}, Source: autonomous.NotifySourceGlobal}
}

func prdIndexServer(n int) (*Server, *prdIndexFake) {
	f := &prdIndexFake{prdListFake: prdListFake{prds: prdListFixture(n)}, rev: 1}
	return &Server{autonomousMgr: f}, f
}

func TestPRDListDefaultReadsOnlyTheIndex(t *testing.T) {
	s, f := prdIndexServer(40)
	rows, resp := prdListDecode(t, prdListGet(t, s, ""))
	if len(rows) == 0 || resp["total"].(float64) != 40 {
		t.Fatalf("rows=%d total=%v", len(rows), resp["total"])
	}
	if f.listCalls != 0 || f.getCalls != 0 {
		t.Fatalf("default list read full records: ListPRDs=%d GetPRD=%d", f.listCalls, f.getCalls)
	}
	if _, has := rows[0]["stories"]; has {
		t.Error("row carries a stories breakdown")
	}
	eff, _ := rows[0]["review_state"].(map[string]any)
	if eff == nil {
		t.Error("row lost review_state")
	}
	if rows[0]["notify_events_effective"] == nil {
		t.Error("row lost notify_events_effective")
	}
	prdListGet(t, s, "?view=summary&status=blocked")
	if f.listCalls != 0 || f.getCalls != 0 {
		t.Fatalf("filtered list read full records: %d %d", f.listCalls, f.getCalls)
	}
}

func TestPRDListIndexMatchesRecordPath(t *testing.T) {
	idxSrv, _ := prdIndexServer(60)
	recSrv := prdListServer(60)
	for _, q := range []string{"", "?limit=7", "?status=running,blocked", "?active=1", "?q=automaton%201", "?limit=5&status=completed"} {
		a, _ := prdListDecode(t, prdListGet(t, idxSrv, q))
		b, _ := prdListDecode(t, prdListGet(t, recSrv, q))
		if len(a) != len(b) {
			t.Fatalf("%q: index %d rows, record path %d", q, len(a), len(b))
		}
		for i := range a {
			if a[i]["id"] != b[i]["id"] || a[i]["effective_status"] != b[i]["effective_status"] {
				t.Fatalf("%q row %d: %v vs %v", q, i, a[i]["id"], b[i]["id"])
			}
		}
	}
}

func TestPRDListCursorOnIndex(t *testing.T) {
	s, _ := prdIndexServer(30)
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		q := "?status=completed,failed,draft&limit=4"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		rows, resp := prdListDecode(t, prdListGet(t, s, q))
		for _, r := range rows {
			id := r["id"].(string)
			if seen[id] {
				t.Fatalf("row %s repeated", id)
			}
			seen[id] = true
		}
		cursor, _ = resp["next_cursor"].(string)
		if cursor == "" {
			break
		}
	}
	if len(seen) != 18 {
		t.Fatalf("walked %d rows, want 18", len(seen))
	}
}

func TestPRDListNotModifiedOnUnchangedRevision(t *testing.T) {
	s, f := prdIndexServer(20)
	first := prdListGet(t, s, "?limit=5")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	again := prdListGet(t, s, "?limit=5", "If-None-Match", etag)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("repeat: %d, %d body bytes", again.Code, again.Body.Len())
	}
	// A different query is a different list.
	if other := prdListGet(t, s, "?limit=6", "If-None-Match", etag); other.Code != http.StatusOK {
		t.Fatalf("other query answered %d", other.Code)
	}
	f.rev++
	if moved := prdListGet(t, s, "?limit=5", "If-None-Match", etag); moved.Code != http.StatusOK {
		t.Fatalf("after a revision change got %d, want 200", moved.Code)
	}
	if f.listCalls != 0 {
		t.Fatalf("ListPRDs called %d times", f.listCalls)
	}
}

func TestPRDListFullFlagReturnsCompleteRecords(t *testing.T) {
	s, f := prdIndexServer(30)
	// Without a limit: every record, the compatibility form.
	rows, _ := prdListDecode(t, prdListGet(t, s, "?full=true"))
	if len(rows) != 30 {
		t.Fatalf("full without limit: %d records", len(rows))
	}
	if _, ok := rows[0]["stories"]; !ok {
		if _, ok := rows[0]["story"]; !ok {
			t.Error("full record has no story tree")
		}
	}
	// With a limit: a capped page that reads only the matching ids.
	f.getCalls = 0
	rows, resp := prdListDecode(t, prdListGet(t, s, "?full=true&limit=3&status=running"))
	if len(rows) != 3 || f.getCalls != 3 {
		t.Fatalf("full page: %d rows, %d record reads", len(rows), f.getCalls)
	}
	if resp["next_cursor"] == "" || resp["total"].(float64) != 6 {
		t.Fatalf("full page paging: %v", resp)
	}
	// The limit is capped.
	big, _ := prdIndexServer(prdListFullMaxLimit + 20)
	rows, _ = prdListDecode(t, prdListGet(t, big, "?full=true&limit=9999"))
	if len(rows) != prdListFullMaxLimit {
		t.Fatalf("full limit not capped: %d", len(rows))
	}
}

func TestPRDListIndexBodyStaysSmall(t *testing.T) {
	s, _ := prdIndexServer(700)
	rr := prdListGet(t, s, "")
	if rr.Body.Len() > 2<<20 {
		t.Fatalf("list body %d bytes for 700 Automata", rr.Body.Len())
	}
}

func prdTasksFixtureServer() (*Server, *prdIndexFake) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	long := strings.Repeat("e", 1000)
	var s1, s2 []autonomous.Task
	for i := 0; i < 6; i++ {
		st := autonomous.TaskCompleted
		if i%2 == 1 {
			st = autonomous.TaskFailed
		}
		s1 = append(s1, autonomous.Task{ID: "a" + string(rune('0'+i)), Title: "A", Status: st, CreatedAt: now, SessionID: "sess", Error: long})
	}
	for i := 0; i < 3; i++ {
		s2 = append(s2, autonomous.Task{ID: "b" + string(rune('0'+i)), Title: "B", Status: autonomous.TaskPending, CreatedAt: now})
	}
	p := &autonomous.PRD{ID: "pp", Title: "P", Story: []autonomous.Story{{ID: "s1", Tasks: s1}, {ID: "s2", Tasks: s2}}}
	f := &prdIndexFake{prdListFake: prdListFake{prds: []*autonomous.PRD{p}}}
	return &Server{autonomousMgr: f}, f
}

func prdTasksGet(s *Server, id, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+"/tasks"+query, nil)
	req.SetPathValue("id", id)
	rr := httptest.NewRecorder()
	s.handlePRDTasks(rr, req)
	return rr
}

func prdTasksDecode(t *testing.T, rr *httptest.ResponseRecorder) (tasks []map[string]any, resp map[string]any) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, r := range resp["tasks"].([]any) {
		tasks = append(tasks, r.(map[string]any))
	}
	return
}

func TestPRDTasksPageSizeAndCursor(t *testing.T) {
	s, f := prdTasksFixtureServer()
	var ids []string
	cursor := ""
	for i := 0; i < 10; i++ {
		q := "?limit=4"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		tasks, resp := prdTasksDecode(t, prdTasksGet(s, "pp", q))
		if len(tasks) > 4 || resp["total"].(float64) != 9 {
			t.Fatalf("page %d: %d tasks, total %v", i, len(tasks), resp["total"])
		}
		for _, tk := range tasks {
			ids = append(ids, tk["id"].(string))
		}
		cursor, _ = resp["next_cursor"].(string)
		if cursor == "" {
			break
		}
	}
	if len(ids) != 9 || ids[0] != "a0" || ids[8] != "b2" {
		t.Fatalf("walk order: %v", ids)
	}
	if f.listCalls != 0 {
		t.Error("task page read the whole list")
	}
	if rr := prdTasksGet(s, "pp", "?cursor=!!"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad cursor: %d", rr.Code)
	}
	if rr := prdTasksGet(s, "pp", "?limit=0"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad limit: %d", rr.Code)
	}
}

func TestPRDTasksFilters(t *testing.T) {
	s, _ := prdTasksFixtureServer()
	tasks, resp := prdTasksDecode(t, prdTasksGet(s, "pp", "?story=s2"))
	if len(tasks) != 3 || resp["total"].(float64) != 3 {
		t.Fatalf("story filter: %d", len(tasks))
	}
	tasks, _ = prdTasksDecode(t, prdTasksGet(s, "pp", "?status=failed"))
	if len(tasks) != 3 {
		t.Fatalf("status filter: %d", len(tasks))
	}
	tasks, _ = prdTasksDecode(t, prdTasksGet(s, "pp", "?status=failed,pending&story=s1"))
	if len(tasks) != 3 {
		t.Fatalf("combined filter: %d", len(tasks))
	}
	row := tasks[0]
	if e, _ := row["error"].(string); len(e) == 0 || len([]rune(e)) > prdTasksErrorRunes+1 {
		t.Errorf("error text not shortened: %d runes", len([]rune(e)))
	}
	if _, has := row["spec"]; has {
		t.Error("task row carries spec")
	}
	if row["session_id"] != "sess" || row["story"] != "s1" {
		t.Errorf("row fields: %v", row)
	}
}

func TestPRDTasksUnknownAutomaton(t *testing.T) {
	s, _ := prdTasksFixtureServer()
	if rr := prdTasksGet(s, "nope", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown Automaton: %d", rr.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/pp/tasks", nil)
	req.SetPathValue("id", "pp")
	rr := httptest.NewRecorder()
	s.handlePRDTasks(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rr.Code)
	}
}

func TestPRDTasksRoutedThroughPRDHandler(t *testing.T) {
	s, _ := prdTasksFixtureServer()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/pp/tasks?limit=2", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)
	tasks, _ := prdTasksDecode(t, rr)
	if len(tasks) != 2 {
		t.Fatalf("routed: %d tasks", len(tasks))
	}
}
