package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

const checksCommSample = `{"policy":{"pre_start_verifier":"on","block_when_missing":true,"plan_rules":"on","after_done":"shadow","registry_readme":"on"},
"findings":[{"prd_id":"p1","at":"2026-01-02T00:00:00Z","source":"after_done_checklist","text":"checklist found one"}]}`

const shipCommSample = `{"policy":{"push":"off","tag":"on","ci_watch":"shadow","tag_format":"v{version}","remote":"origin","ci_workflows":[]},"findings":[]}`

func TestChecksComm_RendersAndFilters(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, checksCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous checks p1"), " ")
	if log.path != "/api/autonomous/checks" || log.query != "prd_id=p1" {
		t.Errorf("path=%s query=%q", log.path, log.query)
	}
	for _, want := range []string{"after_done=shadow", "checklist found one"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestShipComm_RendersAndRejectsExtraArgs(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, shipCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous ship"), " ")
	if log.path != "/api/autonomous/ship" || log.query != "" {
		t.Errorf("path=%s query=%q", log.path, log.query)
	}
	if !strings.Contains(got, "push=off") || !strings.Contains(got, "no findings recorded") {
		t.Errorf("reply = %q", got)
	}
	calls := log.calls
	bad := strings.Join(r.HandleTestMessage("autonomous ship a b"), " ")
	if !strings.Contains(bad, "usage: autonomous ship") || log.calls != calls {
		t.Errorf("extra args must give usage without a call: %q", bad)
	}
}

func TestChecksShipComm_VerbsRegistered(t *testing.T) {
	for _, v := range []string{"checks", "ship"} {
		if !slices.Contains(ChecksShipCommVerbs(), v) {
			t.Errorf("verb %q missing", v)
		}
	}
	help := strings.Join(setupPeerRouter(t, 0).HandleTestMessage("autonomous"), " ")
	if !strings.Contains(help, "checks [automaton-id]") || !strings.Contains(help, "ship [automaton-id]") {
		t.Errorf("help must list both verbs: %s", help)
	}
}
