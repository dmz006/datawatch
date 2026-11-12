package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// overlapCommSample is the overlap read body for the comm tests: one
// Automaton and one registry row, best match first.
const overlapCommSample = `{"warnings":[
  {"kind": "automaton", "id": "prd-77", "status": "running", "score": 0.82,
   "reason": "Automaton prd-77 already describes this plan."},
  {"kind": "registry", "row": 4, "score": 0.61,
   "reason": "An existing Automaton plan is already on the roadmap (row 4)."}
],"threshold": 0.5,"mode": "on"}`

func TestOverlapComm_VerbParses(t *testing.T) {
	if !slices.Contains(OverlapCommVerbs(), "overlap") {
		t.Fatalf("OverlapCommVerbs() = %v, must contain %q", OverlapCommVerbs(), "overlap")
	}
	c := Parse("autonomous overlap Page the ingestion path")
	if c.Type != CmdAutonomous {
		t.Errorf(`Parse("autonomous overlap ...").Type = %v, want CmdAutonomous`, c.Type)
	}

	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "overlap <text>") {
		t.Errorf("the autonomous help must list the overlap verb; got:\n%s", got)
	}
}

func TestOverlapComm_SendsSpecAsQuery(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"warnings":[],"threshold":0.5,"mode":"on"}`)
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous overlap Page the ingestion path")
	if log.path != "/api/autonomous/overlap" {
		t.Errorf("path=%s; want the overlap route", log.path)
	}
	if log.query != "spec=Page+the+ingestion+path" {
		t.Errorf("query=%q; want the whole text sent as spec", log.query)
	}
}

func TestOverlapComm_UsageWithNoText(t *testing.T) {
	for _, msg := range []string{"autonomous overlap", "autonomous overlap    "} {
		log, port := laneFillCommDaemon(t, http.StatusOK, overlapCommSample)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage: autonomous overlap <text>") {
			t.Errorf("%q must be refused with the usage, got: %q", msg, got)
		}
		if log.calls != 0 {
			t.Errorf("%q: the refusal must not reach the daemon, got %d calls", msg, log.calls)
		}
	}
}

func TestOverlapComm_ReplyListsOneWarningPerLine(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, overlapCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous overlap Page the ingestion path"), " ")
	for _, want := range []string{
		"automaton prd-77 status running score 0.82: Automaton prd-77 already describes this plan.",
		"registry row 4 score 0.61: An existing Automaton plan is already on the roadmap (row 4).",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	first := strings.Index(got, "automaton prd-77")
	second := strings.Index(got, "registry row 4")
	if first < 0 || second < 0 || first > second {
		t.Errorf("warnings must keep the daemon's best-match-first order: %q", got)
	}
}

func TestOverlapComm_EmptyResultSaysNoneFound(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, `{"warnings":[],"threshold":0.5,"mode":"on"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous overlap Page the ingestion path"), " ")
	if !strings.Contains(got, "No overlaps found.") {
		t.Errorf("empty result must say none were found: %q", got)
	}
}

func TestOverlapComm_DaemonErrorPassesThrough(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusBadRequest, "title or spec is required")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous overlap Page the ingestion path"), " ")
	if !strings.Contains(got, "autonomous overlap failed") ||
		!strings.Contains(got, "HTTP 400") || !strings.Contains(got, "title or spec is required") {
		t.Errorf("the daemon refusal must pass through, got: %q", got)
	}
}
