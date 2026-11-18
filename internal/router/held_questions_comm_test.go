// Held-question comm-verb tests: `question held` (with its optional
// prd / state filters) against a fake /api/autonomous/held-questions
// daemon. The verb is read-only — it must never issue a POST.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const heldReportOnBody = `{
  "mode": "on",
  "states": ["blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"],
  "open": [
    {
      "question_id": "q_held_1",
      "state": "awaiting_decision",
      "prd_id": "prd-7",
      "task_id": "t_9",
      "automation_id": "bot-scribe",
      "title": "Scribe Bot",
      "reason": "spec step 4 is ambiguous",
      "recommended_option": "escalate",
      "default": "escalate"
    },
    {
      "question_id": "q_held_2",
      "state": "blocked",
      "prd_id": "prd-3",
      "recommended_option": "continue",
      "default": "hold"
    }
  ],
  "shadow": [
    {"prd_id": "prd-7", "state": "pending_insert", "recommended_option": "escalate", "at": "2026-01-02T03:04:05Z"}
  ]
}`

const heldReportOffBody = `{
  "mode": "off",
  "states": ["blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"],
  "open": [],
  "shadow": []
}`

type heldDaemon struct {
	reqs      []questionRequest
	body      string
	errStatus int // non-zero: return this status with {"error":"..."}
	errText   string
}

func heldDaemonServer(t *testing.T, h *heldDaemon) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.reqs = append(h.reqs, questionRequest{
			method:   r.Method,
			path:     r.URL.Path,
			rawquery: r.URL.RawQuery,
			rawbody:  string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/autonomous/held-questions" {
			http.NotFound(w, r)
			return
		}
		if h.errStatus != 0 {
			if h.errStatus >= http.StatusBadRequest && h.errStatus < 1000 {
				w.WriteHeader(h.errStatus)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": h.errText})
			return
		}
		_, _ = w.Write([]byte(h.body))
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	t.Cleanup(srv.Close)
	return port
}

func TestHeldParse(t *testing.T) {
	v, err := parseQuestionVerb("held")
	if err != nil || v.action != "held" || v.heldPRD != "" || v.heldState != "" {
		t.Fatalf("bare held: %+v %v", v, err)
	}
	v, err = parseQuestionVerb("held prd prd-7")
	if err != nil || v.action != "held" || v.heldPRD != "prd-7" {
		t.Fatalf("held prd: %+v %v", v, err)
	}
	v, err = parseQuestionVerb("held state blocked")
	if err != nil || v.action != "held" || v.heldState != "blocked" {
		t.Fatalf("held state: %+v %v", v, err)
	}
	v, err = parseQuestionVerb("held  prd prd-7  state awaiting_decision")
	if err != nil || v.heldPRD != "prd-7" || v.heldState != "awaiting_decision" {
		t.Fatalf("held both: %+v %v", v, err)
	}
	for _, bad := range []string{
		"held prd",             // missing id
		"held state",           // missing id
		"held prd a prd b",     // two prd filters
		"held state a state b", // two state filters
		"held nope",            // unknown filter word
		"held prd a extra",     // trailing junk
	} {
		if _, err := parseQuestionVerb(bad); err == nil {
			t.Errorf("parse(%q): want an error", bad)
		}
	}
	if got := Parse("question held"); got.Type != CmdQuestion || got.Text != "held" {
		t.Errorf("Parse(question held) = %+v", got)
	}
}

func TestQuestionHeldList(t *testing.T) {
	h := &heldDaemon{body: heldReportOnBody}
	port := heldDaemonServer(t, h)
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("question held"))
	if len(h.reqs) != 1 {
		t.Fatalf("daemon requests = %d, want 1: %+v", len(h.reqs), h.reqs)
	}
	rq := h.reqs[0]
	if rq.method != "GET" || rq.path != "/api/autonomous/held-questions" {
		t.Fatalf("held request = %+v, want a GET of the report route", rq)
	}
	if rq.rawquery != "" {
		t.Errorf("no filters given, want an empty query, got %q", rq.rawquery)
	}
	for _, want := range []string{
		"held mode: on",
		"blocked, review_held, revision_requested, awaiting_decision, pending_insert",
		"held questions are raised now",
		"q_held_1",
		"held: awaiting_decision",
		"bot-scribe (Scribe Bot)",
		"task t_9",
		"why: spec step 4 is ambiguous",
		"recommended: escalate (forced default)",
		"q_held_2",
		"held: blocked",
		"recommended: continue  (default: hold)",
		"shadow  pending_insert  prd-7",
		"recommended: escalate",
		"2026-01-02T03:04:05Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
}

func TestQuestionHeldFilters(t *testing.T) {
	h := &heldDaemon{body: heldReportOffBody}
	port := heldDaemonServer(t, h)
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	joinReplies(r.HandleTestMessage("question held prd prd-7"))
	if len(h.reqs) != 1 || !strings.Contains(h.reqs[0].rawquery, "prd_id=prd-7") {
		t.Fatalf("prd filter not sent: %+v", h.reqs)
	}
	if strings.Contains(h.reqs[0].rawquery, "state=") {
		t.Errorf("no state filter given, must not send one: %q", h.reqs[0].rawquery)
	}

	joinReplies(r.HandleTestMessage("question held state blocked"))
	if len(h.reqs) != 2 || !strings.Contains(h.reqs[1].rawquery, "state=blocked") {
		t.Fatalf("state filter not sent: %+v", h.reqs)
	}

	joinReplies(r.HandleTestMessage("question held prd prd-7 state pending_insert"))
	if len(h.reqs) != 3 {
		t.Fatalf("requests = %+v, want 3", h.reqs)
	}
	if !strings.Contains(h.reqs[2].rawquery, "prd_id=prd-7") || !strings.Contains(h.reqs[2].rawquery, "state=pending_insert") {
		t.Errorf("both filters not sent: %q", h.reqs[2].rawquery)
	}
}

func TestQuestionHeldOffSaysNothingHeld(t *testing.T) {
	h := &heldDaemon{body: heldReportOffBody}
	port := heldDaemonServer(t, h)
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("question held"))
	for _, want := range []string{
		"held mode: off",
		"blocked, review_held, revision_requested, awaiting_decision, pending_insert",
		"neither raises nor logs",
		"nothing on the daemon is held right now",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
}

func TestQuestionHeldDaemonErrorsSurface(t *testing.T) {
	msg503 := "operator questions are disabled: set autonomous.questions.enabled to true first (held questions live under the operator-question subsystem switch)"
	h := &heldDaemon{errStatus: http.StatusServiceUnavailable, errText: msg503}
	port := heldDaemonServer(t, h)
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("question held"))
	if !strings.Contains(out, "failed") || !strings.Contains(out, "operator questions are disabled") {
		t.Errorf("503 message must surface:\n%s", out)
	}
}

func TestQuestionHeldIsReadOnly(t *testing.T) {
	h := &heldDaemon{body: heldReportOnBody}
	port := heldDaemonServer(t, h)
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	joinReplies(r.HandleTestMessage("question held"))
	joinReplies(r.HandleTestMessage("question held prd prd-7 state blocked"))
	for _, rq := range h.reqs {
		if rq.method != "GET" {
			t.Fatalf("held is a read-only verb: %v", rq)
		}
	}
}
