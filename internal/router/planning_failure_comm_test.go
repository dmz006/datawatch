package router

import (
	"net/http"
	"strings"
	"testing"
)

const planningFailureObj = `"planning_failure":{"reason":"spec holds a fixed version number","category":"content","source":"release-version-fixed","phrases":["v9.1.0","release 9"],"retryable":false,"retry_counts":{"content":1},"retry_budget":3}`

func TestPlanningFailureCommGetShowsReasonAndPhrases(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"p1","status":"draft",` + planningFailureObj + `}`))
	})
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous get p1"), "\n")
	if !strings.Contains(got, "Planning failed: spec holds a fixed version number (phrases: v9.1.0; release 9)") {
		t.Errorf("get reply lacks the one-line failure: %s", got)
	}
}

func TestPlanningFailureCommGetOmitsLineWithoutFailure(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"p1","status":"draft"}`))
	})
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous get p1"), "\n")
	if strings.Contains(got, "Planning failed") {
		t.Errorf("no failure, yet reply shows one: %s", got)
	}
}

func TestPlanningFailureCommListShowsReasonAndPhrases(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"prds":[{"id":"p1","title":"Alpha",` + planningFailureObj + `},{"id":"p2","title":"Beta"}]}`))
	})
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous list"), "\n")
	if !strings.Contains(got, "Alpha (p1): Planning failed: spec holds a fixed version number (phrases: v9.1.0; release 9)") {
		t.Errorf("list reply lacks the failure line: %s", got)
	}
	if strings.Contains(got, "Beta (p2): Planning failed") {
		t.Errorf("row without a failure is marked: %s", got)
	}
}
