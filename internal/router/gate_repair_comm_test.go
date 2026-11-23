// Gate repair over the chat channel: the status read and the run surface
// (one pass, or one failed gate), rendered with the same renderer the CLI
// uses. The verb is `gate-repair`, distinct from the read-only `gate-repairs`.

package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

const gateRepairCommStatusBody = `{"mode":"on","budget":["t1:infra"],"records":[
 {"at":"2026-11-09T20:00:00Z","group":"gate_repair","subject":"a1 / t1","kind":"did","found":"infra: timeout after poll 4","done":"re-ran the gate","undo":"re-run the gate yourself","before":"old text","after":"new text"}
]}`

const gateRepairCommRunBody = `{"runs":[
 {"mode":"on","class":"infra","subject":"a1 / t1","found":"infra: timeout after poll 4","did":"re-ran the gate","done":"re-run finished green","before":"","after":"","held":"q1","kind":"did"}
],"status":` + gateRepairCommStatusBody + `}`

func TestGateRepairComm_VerbOutput(t *testing.T) {
	if !slices.Contains(GateRepairCommVerbs(), "gate-repair") ||
		!slices.Contains(GateRepairCommVerbs(), "gate_repair") {
		t.Fatalf("verbs = %v", GateRepairCommVerbs())
	}
	if c := Parse("autonomous gate-repair status"); c.Type != CmdAutonomous {
		t.Errorf("Parse(gate-repair status).Type = %v", c.Type)
	}

	log, port := laneFillCommDaemon(t, http.StatusOK, gateRepairCommStatusBody)
	r := setupPeerRouter(t, port)
	out := strings.Join(r.HandleTestMessage("autonomous gate-repair status"), "\n")
	if log.path != "/api/autonomous/gate-repair" || log.calls != 1 {
		t.Errorf("path=%s calls=%d", log.path, log.calls)
	}
	for _, frag := range []string{"mode on", "a1 / t1", "found: infra: timeout after poll 4", "done: re-ran the gate", "undo: re-run the gate yourself", "repair budget spent on 1 (task, class) pair(s)"} {
		if !strings.Contains(out, frag) {
			t.Errorf("status reply lacks %q:\n%s", frag, out)
		}
	}

	// A passive pass sends the POST with an empty body; one gate sends both ids.
	log, port = laneFillCommDaemon(t, http.StatusOK, gateRepairCommRunBody)
	r = setupPeerRouter(t, port)
	out = strings.Join(r.HandleTestMessage("autonomous gate-repair run"), " ")
	if log.path != "/api/autonomous/gate-repair/run" || log.calls != 1 {
		t.Errorf("path=%s calls=%d", log.path, log.calls)
	}
	if log.body != "{}" {
		t.Errorf("passive run body = %q, want {}", log.body)
	}
	for _, frag := range []string{"did: re-ran the gate", "done: re-run finished green", "held question: q1", "repair budget spent on 1 (task, class) pair(s)"} {
		if !strings.Contains(out, frag) {
			t.Errorf("run reply lacks %q:\n%s", frag, out)
		}
	}

	log, port = laneFillCommDaemon(t, http.StatusOK, gateRepairCommRunBody)
	r = setupPeerRouter(t, port)
	out = strings.Join(r.HandleTestMessage("autonomous gate-repair run automaton=a1 task=t1"), " ")
	if strings.Contains(out, "failed") || !strings.Contains(out, "did: re-ran the gate") {
		t.Errorf("targeted run reply: %q (body %q)", out, log.body)
	}
	if !strings.Contains(log.body, `"automaton_id":"a1"`) || !strings.Contains(log.body, `"task_id":"t1"`) {
		t.Errorf("targeted run body = %q", log.body)
	}

	// The underscore alias renders too.
	_, port = laneFillCommDaemon(t, http.StatusOK, gateRepairCommStatusBody)
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous gate_repair status"), " "); !strings.Contains(got, "mode on") {
		t.Errorf("underscore alias: %q", got)
	}
}

func TestGateRepairComm_RefusesWithoutDaemonCalls(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, gateRepairCommStatusBody)
	r := setupPeerRouter(t, port)

	for _, msg := range []string{
		"autonomous gate-repair",                  // no subcommand
		"autonomous gate-repair reboot",           // unknown subcommand
		"autonomous gate-repair status extra",     // unknown argument
		"autonomous gate-repair run foo=bar",      // unknown argument
		"autonomous gate-repair run automaton=a1", // one id without the other
	} {
		out := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(out, "failed") || !strings.Contains(out, "usage: autonomous gate-repair status") {
			t.Errorf("%q reply lacks refusal and usage: %q", msg, out)
		}
	}
	if log.calls != 0 {
		t.Errorf("refused verbs reached the daemon: %d calls", log.calls)
	}

	_, port = laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	r = setupPeerRouter(t, port)
	if out := strings.Join(r.HandleTestMessage("autonomous gate-repair status"), " "); !strings.Contains(out, "failed") {
		t.Errorf("error reply: %q", out)
	}
}
