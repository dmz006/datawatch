package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The repair route is a dry run unless the caller says dry_run=false.
func TestTagRepairEndpointWritesOnlyWhenAsked(t *testing.T) {
	s, mgr := newTagTestServer(t)
	made, err := mgr.CreateTag("made-up label", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	copied := *made
	prd := &autonomous.PRD{ID: "0000beef", TagIDs: []string{made.ID}}
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateTagRecords(nil, []string{made.ID}); err != nil {
		t.Fatal(err)
	}
	copies, err := json.Marshal([]autonomous.Tag{copied})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, body string) (autonomous.TagRepairReport, int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleTagRepair(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		var rep autonomous.TagRepairReport
		_ = json.Unmarshal(rec.Body.Bytes(), &rep)
		return rep, rec.Code, rec.Body.String()
	}
	restored := func() bool {
		for _, tg := range mgr.ListTags() {
			if tg.ID == made.ID {
				return true
			}
		}
		return false
	}
	for name, req := range map[string][2]string{
		"no body":            {"/api/autonomous/tags/repair", ""},
		"copies only":        {"/api/autonomous/tags/repair", `{"copies":` + string(copies) + `}`},
		"dry run in query":   {"/api/autonomous/tags/repair?dry_run=true", `{"copies":` + string(copies) + `}`},
		"body says dry run":  {"/api/autonomous/tags/repair?dry_run=false", `{"dry_run":true,"copies":` + string(copies) + `}`},
		"query value unread": {"/api/autonomous/tags/repair?dry_run=maybe", ""},
	} {
		rep, code, body := call(req[0], req[1])
		if code != http.StatusOK || !rep.DryRun || rep.Missing != 1 {
			t.Fatalf("%s: %d %s", name, code, body)
		}
		if restored() {
			t.Fatalf("%s: a dry run restored the tag", name)
		}
	}
	if _, code, body := call("/api/autonomous/tags/repair", `{"dry_run":fal`); code != http.StatusBadRequest {
		t.Fatalf("a broken body: %d %s, want 400", code, body)
	}
	rep, code, body := call("/api/autonomous/tags/repair", `{"dry_run":false,"copies":`+string(copies)+`}`)
	if code != http.StatusOK || rep.DryRun || rep.Restore != 1 {
		t.Fatalf("repair: %d %s", code, body)
	}
	if !restored() {
		t.Fatal("the repair did not restore the tag")
	}
	if !strings.Contains(body, `"unused_copies"`) {
		t.Errorf("report lacks unused_copies: %s", body)
	}
}

func TestTagRepairEndpointMethodNotAllowed(t *testing.T) {
	s, _ := newTagTestServer(t)
	rec := httptest.NewRecorder()
	s.handleTagRepair(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/tags/repair", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET repair: %d, want 405", rec.Code)
	}
}
