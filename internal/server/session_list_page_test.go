package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func pagedServer(t *testing.T, n int) *Server {
	t.Helper()
	base := time.Now().Add(-time.Hour)
	var sessions []*session.Session
	for i := 0; i < n; i++ {
		st := session.StateComplete
		if i%4 == 0 {
			st = session.StateRunning
		}
		sessions = append(sessions, &session.Session{
			ID: fmt.Sprintf("%04x", i), FullID: fmt.Sprintf("h-%04x", i), Name: fmt.Sprintf("job-%d", i),
			Task: fmt.Sprintf("task number %d", i), State: st,
			// Equal timestamps in groups of 3 exercise the id tiebreak.
			UpdatedAt: base.Add(time.Duration(i/3) * time.Minute),
		})
	}
	return sessionViewServer(t, 0, 200, sessions...)
}

func decodePage(t *testing.T, rr *httptest.ResponseRecorder) sessionListPage {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var p sessionListPage
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func walkPages(t *testing.T, s *Server, extra string, limit int) []string {
	t.Helper()
	var ids []string
	cursor := ""
	for i := 0; i < 100; i++ {
		q := url.Values{"limit": {fmt.Sprint(limit)}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		p := decodePage(t, getSessions(t, s, q.Encode()+extra))
		for _, r := range p.Sessions {
			ids = append(ids, r.FullID)
		}
		if p.NextCursor == "" {
			return ids
		}
		cursor = p.NextCursor
	}
	t.Fatal("paging did not terminate")
	return nil
}

func TestSessionListPageNoGapsNoDuplicates(t *testing.T) {
	s := pagedServer(t, 23)
	for _, limit := range []int{1, 4, 7, 23, 50} {
		ids := walkPages(t, s, "", limit)
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("limit=%d duplicate %s", limit, id)
			}
			seen[id] = true
		}
		if len(ids) != 23 {
			t.Fatalf("limit=%d got %d rows want 23", limit, len(ids))
		}
	}
	p := decodePage(t, getSessions(t, s, "limit=5"))
	if p.Total != 23 || len(p.Sessions) != 5 || p.NextCursor == "" {
		t.Errorf("first page total=%d rows=%d next=%q", p.Total, len(p.Sessions), p.NextCursor)
	}
}

func TestSessionListPageStableAcrossInsert(t *testing.T) {
	s := pagedServer(t, 10)
	first := decodePage(t, getSessions(t, s, "limit=4"))
	// A newer session appears between pages; later pages must not repeat rows.
	if err := s.manager.SaveSession(&session.Session{ID: "ffff", FullID: "h-ffff", State: session.StateRunning, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	second := decodePage(t, getSessions(t, s, "limit=100&cursor="+first.NextCursor))
	seen := map[string]bool{}
	for _, r := range first.Sessions {
		seen[r.FullID] = true
	}
	for _, r := range second.Sessions {
		if seen[r.FullID] {
			t.Errorf("row %s repeated after insert", r.FullID)
		}
	}
	if len(first.Sessions)+len(second.Sessions) != 10 {
		t.Errorf("rows=%d want 10 (the new session sorts before the cursor)", len(first.Sessions)+len(second.Sessions))
	}
}

func TestSessionListPageFilters(t *testing.T) {
	s := pagedServer(t, 20)
	if p := decodePage(t, getSessions(t, s, "limit=100&active=1")); p.Total != 5 {
		t.Errorf("active=1 total=%d want 5", p.Total)
	}
	if p := decodePage(t, getSessions(t, s, "limit=100&status=complete")); p.Total != 15 {
		t.Errorf("status=complete total=%d want 15", p.Total)
	}
	if p := decodePage(t, getSessions(t, s, "limit=100&status=running,complete")); p.Total != 20 {
		t.Errorf("status=running,complete total=%d want 20", p.Total)
	}
	if p := decodePage(t, getSessions(t, s, "limit=100&q=JOB-13")); p.Total != 1 || p.Sessions[0].FullID != "h-000d" {
		t.Errorf("q by name: %+v", p)
	}
	if p := decodePage(t, getSessions(t, s, "limit=100&q=000d")); p.Total != 1 {
		t.Errorf("q by id total=%d want 1", p.Total)
	}
	if p := decodePage(t, getSessions(t, s, "limit=100&q=task+number+7")); p.Total != 1 {
		t.Errorf("q by task total=%d want 1", p.Total)
	}
	if ids := walkPages(t, s, "&active=1", 2); len(ids) != 5 {
		t.Errorf("filtered paging rows=%d want 5", len(ids))
	}
	for _, q := range []string{"limit=0", "limit=x", "cursor=!!!", "status=nope"} {
		if rr := getSessions(t, s, q); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: code=%d want 400", q, rr.Code)
		}
	}
}

func TestSessionListPageSummaryDropsHeavyFields(t *testing.T) {
	huge := strings.Repeat("A", 128*1024)
	s := sessionViewServer(t, 50, 200, &session.Session{
		ID: "a1", FullID: "h-a1", Name: "big", State: session.StateRunning, Task: huge,
		PromptContext: huge, LastSummaryLong: huge, LastResponse: "first line\nsecond", UpdatedAt: time.Now(),
	})
	for _, q := range []string{"view=summary", "limit=10"} {
		rr := getSessions(t, s, q)
		if rr.Body.Len() > 4096 {
			t.Errorf("%s: body is %d bytes, heavy fields leaked", q, rr.Body.Len())
		}
		if strings.Contains(rr.Body.String(), "AAAA") {
			t.Errorf("%s: task text present in summary", q)
		}
		if !strings.Contains(rr.Body.String(), `"status_line":"first line"`) {
			t.Errorf("%s: missing status_line: %s", q, rr.Body.String())
		}
	}
	// The detail endpoint still returns the whole record.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/h-a1", nil)
	rr := httptest.NewRecorder()
	s.handleSessionSubpath(rr, req)
	var full map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full["task"] != huge {
		t.Error("GET /api/sessions/{id} lost the task field")
	}
}

func TestSessionListPageLegacyUnchanged(t *testing.T) {
	task := strings.Repeat("t", 5000)
	s := sessionViewServer(t, 1, 20,
		&session.Session{ID: "a1", FullID: "h-a1", State: session.StateComplete, Task: task, UpdatedAt: time.Now()},
		&session.Session{ID: "b2", FullID: "h-b2", State: session.StateComplete, Task: task, UpdatedAt: time.Now().Add(-time.Hour)},
	)
	rr := getSessions(t, s, "")
	if got, want := rr.Body.String(), legacySessionsBody(t, s, s.manager.ListSessions()); got != want {
		t.Errorf("legacy body changed:\n got %s\nwant %s", got, want)
	}
	if rr.Header().Get("ETag") != "" {
		t.Error("legacy response must not carry an ETag")
	}
	var arr []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &arr); err != nil || len(arr) != 2 || arr[0]["task"] != task {
		t.Errorf("legacy array not full: err=%v n=%d", err, len(arr))
	}
	// view=summary without paging params stays an array with the terminal cap.
	rr = getSessions(t, s, "view=summary")
	if err := json.Unmarshal(rr.Body.Bytes(), &arr); err != nil || len(arr) != 1 {
		t.Errorf("summary array: err=%v n=%d", err, len(arr))
	}
}

func TestSessionsListETagNotModified(t *testing.T) {
	s := pagedServer(t, 6)
	for _, q := range []string{"view=summary", "limit=3"} {
		rr := getSessions(t, s, q)
		etag := rr.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("%s: no ETag", q)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/sessions?"+q, nil)
		req.Header.Set("If-None-Match", etag)
		rr2 := httptest.NewRecorder()
		s.handleSessions(rr2, req)
		if rr2.Code != http.StatusNotModified || rr2.Body.Len() != 0 {
			t.Errorf("%s: code=%d body=%d, want 304 empty", q, rr2.Code, rr2.Body.Len())
		}
		req.Header.Set("If-None-Match", `"stale"`)
		rr3 := httptest.NewRecorder()
		s.handleSessions(rr3, req)
		if rr3.Code != http.StatusOK || rr3.Header().Get("ETag") != etag {
			t.Errorf("%s: stale tag code=%d etag=%q", q, rr3.Code, rr3.Header().Get("ETag"))
		}
	}
	before := getSessions(t, s, "view=summary").Header().Get("ETag")
	if err := s.manager.SaveSession(&session.Session{ID: "zz", FullID: "h-zz", State: session.StateRunning, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if after := getSessions(t, s, "view=summary").Header().Get("ETag"); after == before {
		t.Error("ETag did not change after the list changed")
	}
}

func TestSessionsListETagAutomations(t *testing.T) {
	s, _, _ := newAutomationAuditServer(t)
	get := func(inm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/automations", nil)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		rr := httptest.NewRecorder()
		s.handleAutomations(rr, req)
		return rr
	}
	rr := get("")
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("empty list: code=%d body=%q want []", rr.Code, rr.Body.String())
	}
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on /api/automations")
	}
	if rr2 := get(etag); rr2.Code != http.StatusNotModified {
		t.Errorf("code=%d want 304", rr2.Code)
	}
}
