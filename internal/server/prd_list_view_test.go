package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type prdListFake struct {
	fakeOrchAutonomous
	prds []*autonomous.PRD
}

func (f *prdListFake) ListPRDs() []any {
	out := make([]any, len(f.prds))
	for i, p := range f.prds {
		out[i] = p
	}
	return out
}

var prdListBase = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func prdListFixture(n int) []*autonomous.PRD {
	statuses := []autonomous.PRDStatus{autonomous.PRDCompleted, autonomous.PRDRunning, autonomous.PRDBlocked, autonomous.PRDFailed, autonomous.PRDDraft}
	var out []*autonomous.PRD
	for i := 0; i < n; i++ {
		out = append(out, &autonomous.PRD{
			ID:        fmt.Sprintf("%08x", i),
			Title:     fmt.Sprintf("Automaton %d", i),
			Status:    statuses[i%len(statuses)],
			Rev:       i,
			CreatedAt: prdListBase,
			// Several rows share a timestamp so the id tiebreak is exercised.
			UpdatedAt: prdListBase.Add(time.Duration(i/2) * time.Minute),
			TagIDs:    []string{fmt.Sprintf("tag%d", i%3)},
			Story: []autonomous.Story{{ID: "s", Tasks: []autonomous.Task{
				{ID: "t1", Status: autonomous.TaskPending}, {ID: "t2", Status: autonomous.TaskCompleted},
			}}},
		})
	}
	return out
}

func prdListGet(t *testing.T, s *Server, query string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds"+query, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)
	return rr
}

func prdListDecode(t *testing.T, rr *httptest.ResponseRecorder) (rows []map[string]any, resp map[string]any) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	raw, _ := resp["prds"].([]any)
	for _, r := range raw {
		rows = append(rows, r.(map[string]any))
	}
	return rows, resp
}

func prdListServer(n int) *Server {
	return &Server{autonomousMgr: &prdListFake{prds: prdListFixture(n)}}
}

func TestPRDListDefaultsAreSummaryRows(t *testing.T) {
	rows, resp := prdListDecode(t, prdListGet(t, prdListServer(10), ""))
	if len(rows) != 10 || resp["total"].(float64) != 10 {
		t.Fatalf("rows=%d total=%v", len(rows), resp["total"])
	}
	r := rows[0]
	if _, ok := r["stories"]; ok {
		t.Fatal("summary row carries stories")
	}
	if r["story_count"].(float64) != 1 || r["task_count"].(float64) != 2 {
		t.Fatalf("counts: %v", r)
	}
	counts := r["task_status_counts"].(map[string]any)
	if counts["pending"].(float64) != 1 || counts["completed"].(float64) != 1 {
		t.Fatalf("task_status_counts: %v", counts)
	}
}

func TestPRDListFullFlagKeepsOldShape(t *testing.T) {
	rows, resp := prdListDecode(t, prdListGet(t, prdListServer(4), "?full=1"))
	if _, ok := resp["total"]; ok {
		t.Fatal("full=1 must keep the bare {prds} shape")
	}
	if len(rows) != 4 || rows[0]["stories"] == nil {
		t.Fatalf("full=1 rows should hold stories: %v", rows)
	}
}

func TestPRDListBadParams(t *testing.T) {
	s := prdListServer(3)
	for _, q := range []string{"?limit=0", "?limit=abc", "?cursor=!!!"} {
		if rr := prdListGet(t, s, q); rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: got %d", q, rr.Code)
		}
	}
	rows, _ := prdListDecode(t, prdListGet(t, prdListServer(600), "?status=completed,running,blocked,failed,draft&limit=9999"))
	if len(rows) != 500 {
		t.Fatalf("limit must clamp to 500, got %d", len(rows))
	}
}

func TestPRDListDefaultIsActivePlusRecent(t *testing.T) {
	s := prdListServer(100)
	rows, resp := prdListDecode(t, prdListGet(t, s, "?limit=5"))
	active, other := 0, 0
	for _, r := range rows {
		if r["status"] == "running" || r["status"] == "blocked" {
			active++
		} else {
			other++
		}
	}
	if active != 40 || other != 5 {
		t.Fatalf("active=%d other=%d", active, other)
	}
	if resp["next_cursor"] == nil {
		t.Fatal("expected next_cursor")
	}
}

func TestPRDListFilters(t *testing.T) {
	s := prdListServer(30)
	count := func(q string) int { r, _ := prdListDecode(t, prdListGet(t, s, q)); return len(r) }
	if n := count("?status=completed,failed"); n != 12 {
		t.Fatalf("status filter: %d", n)
	}
	if n := count("?active=1"); n != 12 {
		t.Fatalf("active filter: %d", n)
	}
	if n := count("?q=AUTOMATON%2012"); n != 1 {
		t.Fatalf("q title: %d", n)
	}
	if n := count("?q=0000001"); n != 15 {

	}
	if n := count("?tag_id=tag1"); n != 10 {
		t.Fatalf("tag filter: %d", n)
	}
	if n := count("?status=running&tag_id=tag1"); n != 2 {
		t.Fatalf("combined: %d", n)
	}
}

func prdListWalk(t *testing.T, s *Server, base string, limit int) []string {
	t.Helper()
	var ids []string
	cursor := ""
	for page := 0; page < 100; page++ {
		q := fmt.Sprintf("%slimit=%d", base, limit)
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		rows, resp := prdListDecode(t, prdListGet(t, s, q))
		for _, r := range rows {
			ids = append(ids, r["id"].(string))
		}
		next, _ := resp["next_cursor"].(string)
		if next == "" {
			return ids
		}
		cursor = next
	}
	t.Fatal("paging did not terminate")
	return nil
}

func TestPRDListPagingNoDuplicatesOrGaps(t *testing.T) {
	s := prdListServer(47)
	for _, base := range []string{"?status=completed,running,blocked,failed,draft&", "?"} {
		ids := prdListWalk(t, s, base, 7)
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("%s duplicate %s", base, id)
			}
			seen[id] = true
		}
		if len(ids) != 47 {
			t.Fatalf("%s got %d rows, want 47", base, len(ids))
		}
	}
}

func TestPRDListPagingOrderNewestFirst(t *testing.T) {
	ids := prdListWalk(t, prdListServer(20), "?status=completed,running,blocked,failed,draft&", 6)
	if ids[0] != fmt.Sprintf("%08x", 19) || ids[1] != fmt.Sprintf("%08x", 18) {
		t.Fatalf("unexpected order: %v", ids[:4])
	}
}

func TestPRDListETagRoundTrip(t *testing.T) {
	s := prdListServer(5)
	rr := prdListGet(t, s, "")
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	rr2 := prdListGet(t, s, "", "If-None-Match", etag)
	if rr2.Code != http.StatusNotModified || rr2.Body.Len() != 0 {
		t.Fatalf("want 304 empty, got %d %q", rr2.Code, rr2.Body.String())
	}
	if rr3 := prdListGet(t, s, "", "If-None-Match", `"other"`); rr3.Code != http.StatusOK {
		t.Fatalf("stale etag: %d", rr3.Code)
	}
}

func TestPRDListETagChangesWithRow(t *testing.T) {
	f := &prdListFake{prds: prdListFixture(5)}
	s := &Server{autonomousMgr: f}
	before := prdListGet(t, s, "").Header().Get("ETag")
	f.prds[0].Rev++
	f.prds[0].UpdatedAt = f.prds[0].UpdatedAt.Add(time.Second)
	after := prdListGet(t, s, "", "If-None-Match", before)
	if after.Code != http.StatusOK || after.Header().Get("ETag") == before {
		t.Fatalf("etag must change when a row changes: %d", after.Code)
	}
}

func TestPRDListSummaryRowStaysSmallForHugeAutomaton(t *testing.T) {
	big := strings.Repeat("x", 4096)
	prd := &autonomous.PRD{ID: "deadbeef", Title: "Huge", Status: autonomous.PRDRunning, Spec: big,
		CreatedAt: prdListBase, UpdatedAt: prdListBase}
	var stories []autonomous.Story
	for i := 0; i < 20; i++ {
		st := autonomous.Story{ID: fmt.Sprintf("s%d", i), Title: "story"}
		for j := 0; j < 160; j++ {
			st.Tasks = append(st.Tasks, autonomous.Task{ID: fmt.Sprintf("t%d-%d", i, j), Spec: big, Status: autonomous.TaskCompleted})
		}
		stories = append(stories, st)
	}
	for len(stories) < 20 || func() bool { b, _ := json.Marshal(stories); return len(b) < 13<<20 }() {
		st := &stories[0]
		st.Tasks = append(st.Tasks, autonomous.Task{ID: fmt.Sprintf("x%d", len(st.Tasks)), Spec: big, Status: autonomous.TaskPending})
	}
	prd.Story = stories
	full, _ := json.Marshal(prd)
	if len(full) < 13<<20 {
		t.Fatalf("fixture only %d bytes", len(full))
	}
	s := &Server{autonomousMgr: &prdListFake{prds: []*autonomous.PRD{prd}}}
	rr := prdListGet(t, s, "")
	rows, _ := prdListDecode(t, rr)
	rowJSON, _ := json.Marshal(rows[0])
	if len(rowJSON) >= 2048 {
		t.Fatalf("summary row is %d bytes", len(rowJSON))
	}
	if rows[0]["story_count"].(float64) != 20 {
		t.Fatalf("story_count %v", rows[0]["story_count"])
	}
}
