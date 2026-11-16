// autonomous landing over the chat channel — `autonomous landing <id>`
// shows the record; `autonomous landing keep|drop <id> <reason...>` posts
// the operator decision to the landing routes. The reply renders the
// record with the exact shared rendering (autonomous.RenderLanding) every
// surface uses, so the text here is the canonical landing line.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// landingPRDKept is the daemon reply after a keep, as the read serves it.
const landingPRDKept = `{"id":"prd-1","landing":{"state":"kept","actor":"parity-op","at":"2026-11-09T00:00:00Z","reason":"keep for reference"}}`

// landingPathLog records each request the comm router sent to the daemon.
type landingPathLog struct {
	calls  int
	method string
	path   string
	body   string
}

// landingCommDaemon answers every daemon request with the fixed body and
// records the last request's method, path and body.
func landingCommDaemon(t *testing.T, status int, body string) (*landingPathLog, int) {
	t.Helper()
	log := &landingPathLog{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		log.calls++
		log.method = r.Method
		log.path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		log.body = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return log, port
}

func TestLandingComm_ShowRendersTheSharedLandingLine(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, landingPRDKept)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing prd-1"), " ")
	if log.method != http.MethodGet || log.path != "/api/autonomous/prds/prd-1" {
		t.Fatalf("proxied %s %s, want GET /api/autonomous/prds/prd-1", log.method, log.path)
	}
	for _, want := range []string{"landing: kept", "keep for reference", "parity-op"} {
		if !strings.Contains(got, want) {
			t.Errorf("show reply missing %q: %q", want, got)
		}
	}
}

func TestLandingComm_ShowWithoutRecordSaysNone(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, `{"id":"prd-1"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing prd-1"), " ")
	if log.path != "/api/autonomous/prds/prd-1" {
		t.Errorf("path = %s", log.path)
	}
	if !strings.Contains(got, "no landing record yet") {
		t.Errorf("want the no-record line, got %q", got)
	}
}

func TestLandingComm_KeepPostsTheKeepRoute(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, landingPRDKept)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing keep prd-1 keep this for the archive"), " ")
	if log.method != http.MethodPost || log.path != "/api/autonomous/prds/prd-1/landing/keep" {
		t.Fatalf("proxied %s %s, want POST .../landing/keep", log.method, log.path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(log.body), &body); err != nil {
		t.Fatalf("body %q: %v", log.body, err)
	}
	if body["reason"] != "keep this for the archive" {
		t.Errorf("body = %v, want the reason", body)
	}
	if _, hasActor := body["actor"]; hasActor {
		t.Errorf("body carries actor %q — the operator default is the REST handler's job", body["actor"])
	}
	if !strings.Contains(got, "landing: kept") {
		t.Errorf("want the kept line in the reply, got %q", got)
	}
}

func TestLandingComm_KeepPassesTheActorWhenGiven(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, landingPRDKept)
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous landing keep prd-1 kept on purpose actor=parity-op")
	if !strings.Contains(log.body, `"actor":"parity-op"`) {
		t.Errorf("body = %s, want actor=parity-op passed through", log.body)
	}
	if !strings.Contains(log.body, `"reason":"kept on purpose"`) {
		t.Errorf("body = %s, want the reason minus the actor flag", log.body)
	}
}

func TestLandingComm_DropPostsTheDropRoute(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK,
		`{"id":"prd-1","landing":{"state":"dropped","actor":"operator","at":"2026-11-09T00:00:00Z","reason":"superseded"}}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing drop prd-1 superseded by trunk"), " ")
	if log.method != http.MethodPost || log.path != "/api/autonomous/prds/prd-1/landing/drop" {
		t.Fatalf("proxied %s %s, want POST .../landing/drop", log.method, log.path)
	}
	if !strings.Contains(log.body, `"reason":"superseded by trunk"`) {
		t.Errorf("body = %s, want the reason", log.body)
	}
	if !strings.Contains(got, "landing: dropped") {
		t.Errorf("want the dropped line in the reply, got %q", got)
	}
}

func TestLandingComm_AlternateVerbSpellingsRouteToBothDecisions(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, landingPRDKept)
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous landing-keep prd-1 r1")
	if log.calls != 1 || log.path != "/api/autonomous/prds/prd-1/landing/keep" {
		t.Fatalf("landing-keep: calls=%d path=%s", log.calls, log.path)
	}
	log.calls = 0
	r.HandleTestMessage("autonomous landing_drop prd-1 r2")
	if log.calls != 1 || log.path != "/api/autonomous/prds/prd-1/landing/drop" {
		t.Fatalf("landing_drop: calls=%d path=%s", log.calls, log.path)
	}
}

func TestLandingComm_WithoutARenderableRecordStillReplies(t *testing.T) {
	// The daemon reply carries no landing field (e.g. the PRD predates the
	// record or the daemon is old): the decision reply must not be empty.
	_, port := landingCommDaemon(t, http.StatusOK, `{"id":"prd-1"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing keep prd-1 keep for the archive"), " ")
	if !strings.Contains(got, "landing recorded") {
		t.Errorf("want the recorded fallback, got %q", got)
	}
}

func TestLandingComm_KeepRejectsWithoutARenderableReason(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, landingPRDKept)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing keep prd-1"), " ")
	if log.calls != 0 {
		t.Errorf("%d daemon calls, want none without a reason", log.calls)
	}
	if !strings.Contains(got, "reason is required") {
		t.Errorf("want the usage, got %q", got)
	}
}

func TestLandingComm_UnknownSubcommandShowsUsageWithoutCalling(t *testing.T) {
	log, port := landingCommDaemon(t, http.StatusOK, landingPRDKept)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous landing bogus prd-1"), " ")
	if log.calls != 0 {
		t.Errorf("%d daemon calls, want none", log.calls)
	}
	if !strings.Contains(got, "usage") {
		t.Errorf("want the usage line, got %q", got)
	}
}

func TestLandingComm_ParityListHasAllThreeVerbs(t *testing.T) {
	verbs := LandingCommVerbs()
	want := []string{"landing", "landing-keep", "landing-drop"}
	if len(verbs) != len(want) {
		t.Fatalf("landing comm verbs = %v, want %v", verbs, want)
	}
	for i, v := range want {
		if verbs[i] != v {
			t.Errorf("verb[%d] = %q, want %q", i, verbs[i], v)
		}
	}
}
