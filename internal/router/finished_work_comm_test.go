package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

const finishedWorkCommBody = `{"mode":"on","generated_at":"2026-11-24T12:00:00Z","waiting":[
 {"automaton":"abcd0001","title":"Git hosts","status":"completed","cause":"finished_not_merged","branch":"automaton/abcd0001","ahead":57,"owner":"dave","since":"2026-11-20T12:00:00Z","question_id":"q-1"}]}`

func TestFinishedWorkCommVerbReadsTheDigest(t *testing.T) {
	if !slices.Contains(FinishedWorkCommVerbs(), "finished-work") {
		t.Fatalf("FinishedWorkCommVerbs() = %v", FinishedWorkCommVerbs())
	}
	for _, verb := range []string{"finished-work", "finished_work"} {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v`, verb, c.Type)
		}
		log, port := laneFillCommDaemon(t, http.StatusOK, finishedWorkCommBody)
		got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous "+verb), " ")
		if log.path != "/api/autonomous/finished-work" || log.method != http.MethodGet || log.calls != 1 {
			t.Errorf("path=%s method=%s calls=%d", log.path, log.method, log.calls)
		}
		for _, want := range []string{"abcd0001", "Git hosts", "dave", "q-1"} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q: %q", want, got)
			}
		}
	}
}

func TestFinishedWorkCommRunAndUsage(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"mode":"on","git_checks":4,"found":2,"asked":2}`)
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous finished-work run sweep"), " ")
	if log.path != "/api/autonomous/finished-work/run" || log.method != http.MethodPost || !strings.Contains(log.body, `"run":"sweep"`) {
		t.Errorf("%s %s %s", log.method, log.path, log.body)
	}
	if !strings.Contains(got, "compared 4 branch") || !strings.Contains(got, "2 question(s) raised") {
		t.Errorf("reply = %q", got)
	}
	log2, port2 := laneFillCommDaemon(t, http.StatusOK, `{}`)
	got = strings.Join(setupPeerRouter(t, port2).HandleTestMessage("autonomous finished-work run everything"), " ")
	if log2.calls != 0 || !strings.Contains(got, "usage: autonomous finished-work") {
		t.Errorf("calls=%d reply=%q", log2.calls, got)
	}
}

func TestCancelCommCarriesTheDecision(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"status":"cancelled"}`)
	setupPeerRouter(t, port).HandleTestMessage("autonomous cancel abcd0001 keep branch is for review")
	if log.method != http.MethodDelete || !strings.HasSuffix(log.path, "/abcd0001") ||
		!strings.Contains(log.query, "decision=keep") || !strings.Contains(log.query, "reason=branch+is+for+review") {
		t.Errorf("%s %s ?%s", log.method, log.path, log.query)
	}
	log2, port2 := laneFillCommDaemon(t, http.StatusOK, `{}`)
	got := strings.Join(setupPeerRouter(t, port2).HandleTestMessage("autonomous cancel abcd0001 maybe"), " ")
	if log2.calls != 0 || !strings.Contains(got, "usage: autonomous cancel") {
		t.Errorf("calls=%d reply=%q", log2.calls, got)
	}
}
