package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// archivedServer saves one live session and n old terminal ones, then runs the
// archive pass so the old ones live only in the archive.
func archivedServer(t *testing.T, n int) *Server {
	t.Helper()
	old := time.Now().Add(-30 * 24 * time.Hour)
	sessions := []*session.Session{
		{ID: "live", FullID: "h-live", Name: "live-job", Task: "running now", State: session.StateRunning, UpdatedAt: time.Now()},
	}
	for i := 0; i < n; i++ {
		sessions = append(sessions, &session.Session{
			ID: fmt.Sprintf("o%03d", i), FullID: fmt.Sprintf("h-o%03d", i), Name: fmt.Sprintf("old-%d", i),
			Task: fmt.Sprintf("ancient task %d zebra-%d", i, i), State: session.StateComplete,
			UpdatedAt: old.Add(time.Duration(i) * time.Minute),
		})
	}
	s := sessionViewServer(t, 0, 200, sessions...)
	moved, err := s.manager.ArchiveSessions(time.Now())
	if err != nil || moved != n {
		t.Fatalf("archived %d (want %d), err %v", moved, n, err)
	}
	return s
}

func listFullIDs(t *testing.T, rr *httptest.ResponseRecorder) []string {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var rows []struct {
		FullID string `json:"full_id"`
	}
	body := rr.Body.Bytes()
	var page sessionListPage
	if json.Unmarshal(body, &page) == nil && page.Sessions != nil {
		for _, r := range page.Sessions {
			rows = append(rows, struct {
				FullID string `json:"full_id"`
			}{r.FullID})
		}
	} else if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.FullID)
	}
	return out
}

func archIDsHave(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func TestSessionsArchiveListed(t *testing.T) {
	s := archivedServer(t, 3)

	// The plain list stays live-only, as before.
	if ids := listFullIDs(t, getSessions(t, s, "")); len(ids) != 1 || ids[0] != "h-live" {
		t.Fatalf("plain list = %v", ids)
	}
	// History adds the archive, in the unchanged response shapes.
	ids := listFullIDs(t, getSessions(t, s, "history=1"))
	if len(ids) != 4 || !archIDsHave(ids, "h-o000") {
		t.Fatalf("history list = %v", ids)
	}
	ids = listFullIDs(t, getSessions(t, s, "view=summary&history=1"))
	if len(ids) != 4 {
		t.Fatalf("summary history = %v", ids)
	}
	// Paged history walks live and archived rows without repeats.
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 10; i++ {
		q := "view=summary&history=1&limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		rr := getSessions(t, s, q)
		var p sessionListPage
		if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		for _, r := range p.Sessions {
			if seen[r.FullID] {
				t.Fatalf("%s repeated across pages", r.FullID)
			}
			seen[r.FullID] = true
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(seen) != 4 {
		t.Fatalf("paged history saw %v", seen)
	}
	// GET by ID falls back to the archive.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/h-o001", nil)
	rr := httptest.NewRecorder()
	s.handleSessionSubpath(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET archived by id: %d %s", rr.Code, rr.Body.String())
	}
	var got session.Session
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.Task != "ancient task 1 zebra-1" {
		t.Fatalf("archived session body = %+v, %v", got, err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/h-missing", nil)
	rr = httptest.NewRecorder()
	s.handleSessionSubpath(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rr.Code)
	}
}

func TestSessionsHistorySearchFindsArchived(t *testing.T) {
	s := archivedServer(t, 3)
	ids := listFullIDs(t, getSessions(t, s, "view=summary&q=zebra-2"))
	if len(ids) != 1 || ids[0] != "h-o002" {
		t.Fatalf("search for an archived task = %v", ids)
	}
	ids = listFullIDs(t, getSessions(t, s, "q=old-1"))
	if len(ids) != 1 || ids[0] != "h-o001" {
		t.Fatalf("legacy-shape search = %v", ids)
	}
	// active=1 never includes archived (they are terminal).
	if ids := listFullIDs(t, getSessions(t, s, "view=summary&history=1&active=1")); len(ids) != 1 || ids[0] != "h-live" {
		t.Fatalf("active+history = %v", ids)
	}
	// A search with no archived hit still finds the live session.
	if ids := listFullIDs(t, getSessions(t, s, "view=summary&q=live-job")); len(ids) != 1 || ids[0] != "h-live" {
		t.Fatalf("live search = %v", ids)
	}
	// Restoring puts the session back in the plain list.
	if _, err := s.manager.RestoreArchivedSession("h-o002"); err != nil {
		t.Fatal(err)
	}
	if ids := listFullIDs(t, getSessions(t, s, "")); !archIDsHave(ids, "h-o002") {
		t.Fatalf("restored session missing from list: %v", ids)
	}
}
