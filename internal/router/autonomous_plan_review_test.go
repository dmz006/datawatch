// 2026-10-17 — comm-channel parity for the plan-review gate surface:
// `autonomous plan-review <id>` must POST to the daemon's plan-review
// endpoint with the operator note, and `autonomous config-set` must
// carry the five new gate/smoke-verification keys through the generic
// config-set merge path untouched.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedDaemonRequest records one outbound daemon call.
type capturedDaemonRequest struct {
	method string
	path   string
	body   string
}

// fakeDaemonPlanReview stands in for the local daemon loopback and
// records every request the router sends (config-set does a GET then a
// PUT; plan-review does one POST).
func fakeDaemonPlanReview(t *testing.T, reqs *[]capturedDaemonRequest) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*reqs = append(*reqs, capturedDaemonRequest{method: r.Method, path: r.URL.Path, body: string(b)})
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"enabled":true}`))
		default:
			_, _ = w.Write([]byte(`{"id":"prd_pr","plan_review":{"ok":true,"mode":"warn"}}`))
		}
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

func TestAutonomousPlanReview_PostsPlanReviewEndpoint(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanReview(t, &reqs)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := r.HandleTestMessage("autonomous plan-review prd_pr re-check story 2 split")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodPost {
		t.Errorf("method = %q want POST", reqs[0].method)
	}
	if reqs[0].path != "/api/autonomous/prds/prd_pr/plan-review" {
		t.Errorf("path = %q want /api/autonomous/prds/prd_pr/plan-review", reqs[0].path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, reqs[0].body)
	}
	if body["actor"] != "operator" {
		t.Errorf("actor = %q want operator", body["actor"])
	}
	if body["note"] != "re-check story 2 split" {
		t.Errorf("note = %q want %q", body["note"], "re-check story 2 split")
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "autonomous plan-review") {
		t.Errorf("reply does not surface the verdict: %q", joined)
	}
	if !strings.Contains(joined, "plan_review") {
		t.Errorf("reply does not include the verdict JSON: %q", joined)
	}
}

func TestAutonomousPlanReview_UseageWithoutID(t *testing.T) {
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	out := r.HandleTestMessage("autonomous plan-review")
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "usage: autonomous plan-review <prd-id>") {
		t.Errorf("expected usage hint, got: %q", joined)
	}
}

// The five 2026-10-17 gate/smoke-verification keys must reach the daemon
// PUT untouched through the generic config-set merge (no allow-list).
func TestAutonomousConfigSet_PlanReviewAndSmokeKeys(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanReview(t, &reqs)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	r.HandleTestMessage("autonomous config-set plan_review_backend=llm plan_review_model=claude-x plan_review_mode=warn smoke_verification_backend=llm2 smoke_verification_model=claude-y")

	// config-set does a GET (current config) then a PUT (merged).
	var put *capturedDaemonRequest
	for i := range reqs {
		if reqs[i].method == http.MethodPut && reqs[i].path == "/api/autonomous/config" {
			put = &reqs[i]
		}
	}
	if put == nil {
		t.Fatalf("no PUT /api/autonomous/config seen; requests = %v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(put.body), &body); err != nil {
		t.Fatalf("put body is not valid JSON: %v (body=%q)", err, put.body)
	}
	want := map[string]string{
		"plan_review_backend":        "llm",
		"plan_review_model":          "claude-x",
		"plan_review_mode":           "warn",
		"smoke_verification_backend": "llm2",
		"smoke_verification_model":   "claude-y",
	}
	for k, v := range want {
		if got, _ := body[k].(string); got != v {
			t.Errorf("body[%q] = %v want %q", k, body[k], v)
		}
	}
}
