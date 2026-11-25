package server

// POST /api/autonomous/prds/{id}/landing/accept and the selection read.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestLandingAcceptRouteRecordsActorAndAudits(t *testing.T) {
	s, mgr, repo, _, _, log := switchServer(t)
	p := selAutomaton(t, mgr, autonomous.PRDNeedsReview, "feature")
	tip := postShipGit(t, repo, "rev-parse", "feature")
	path := "/api/autonomous/prds/" + p.ID + "/landing/accept"

	if rr := liveDo(s.handleAutonomousPRDs, http.MethodGet, path, ""); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", rr.Code)
	}
	rr := liveDo(s.handleAutonomousPRDs, http.MethodPost, path, `{"commit":"`+tip[:10]+`","reason":"r"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "full id") {
		t.Errorf("short commit: %d %s", rr.Code, rr.Body)
	}

	// A caller the daemon knows to be a session is recorded as that session,
	// whatever the body names.
	store, err := auth.NewSessionTokenStore(filepath.Join(t.TempDir(), "session-tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := store.Mint("host-sup1", []string{"autonomous:write"})
	if err != nil {
		t.Fatal(err)
	}
	s.sessionTokens = store
	ctx := context.WithValue(context.Background(), sessionCapsKey, []string{"autonomous:write"})
	ctx = context.WithValue(ctx, callerTokenKey, tok)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"actor":"operator","commit":"`+tip+`","reason":"checked"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	got, _ := mgr.Store().GetPRD(p.ID)
	if a := got.MergeAcceptance; a == nil || a.Commit != tip || a.Actor != "session:host-sup1" || a.Reason != "checked" {
		t.Fatalf("acceptance = %+v", got.MergeAcceptance)
	}
	entries, err := log.Read(audit.QueryFilter{Action: "merge_accept"})
	if err != nil || len(entries) != 2 {
		t.Fatalf("audit entries = %d (%v), want the refusal and the acceptance", len(entries), err)
	}

	rr = liveDo(s.handleAutonomousMerge, http.MethodGet, "/api/autonomous/merge?automaton="+p.ID, "")
	var sel autonomous.MergeSelection
	if err := json.Unmarshal(rr.Body.Bytes(), &sel); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("selection read: %d %s %v", rr.Code, rr.Body, err)
	}
	if len(sel.Eligible) != 1 || sel.Eligible[0].Basis != autonomous.MergeBasisAccepted || sel.Eligible[0].Tip != tip {
		t.Fatalf("selection = %+v", sel)
	}
}
