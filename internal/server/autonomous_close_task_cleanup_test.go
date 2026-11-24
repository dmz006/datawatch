// The cancel and archive replies say how many task worktrees and branches were
// kept and why.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type keptWorktreeAutonomous struct{ sessionSpyAutonomous }

func (k *keptWorktreeAutonomous) GetPRD(id string) (any, bool) {
	return &autonomous.PRD{ID: id, Status: autonomous.PRDCancelled, TaskWorktreeCleanup: &autonomous.TaskWorktreeCleanup{
		Mode: "on", WorktreesKept: 2, BranchesKept: 3, Reasons: map[string]int{"uncommitted_work": 2},
		Kept: []autonomous.TaskWorktreeKept{{TaskID: "t1", Reason: "uncommitted_work"}},
	}}, true
}

func TestAutonomousCancelRepliesSayWhatWasKept(t *testing.T) {
	for _, tc := range []struct {
		method, url string
		list        bool
	}{
		{http.MethodDelete, "/api/autonomous/prds/prd-k1", true},
		{http.MethodDelete, "/api/autonomous/prds/prd-k1?view=compact", false},
		{http.MethodPost, "/api/autonomous/prds/prd-k1/cancel", true},
	} {
		s, _ := newCancelTestServer(t, nil)
		s.autonomousMgr = &keptWorktreeAutonomous{}
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(tc.method, tc.url, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.url, rr.Code, rr.Body.String())
		}
		var body struct {
			C *autonomous.TaskWorktreeCleanup `json:"task_worktree_cleanup"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.C == nil || body.C.WorktreesKept != 2 || body.C.BranchesKept != 3 || body.C.Reasons["uncommitted_work"] != 2 {
			t.Fatalf("%s %s: reply lacks the kept summary: %s", tc.method, tc.url, rr.Body.String())
		}
		if (len(body.C.Kept) > 0) != tc.list {
			t.Errorf("%s %s: per-task list present=%v, want %v", tc.method, tc.url, len(body.C.Kept) > 0, tc.list)
		}
	}
}
