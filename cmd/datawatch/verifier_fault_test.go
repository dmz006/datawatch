package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// A verifier session that produced nothing is the verifier's fault, whatever
// the error text says, so the worker is not run again.
func TestVerifierSessionFailureIsMarkedAsTheVerifiersFault(t *testing.T) {
	res := verifierSessionInfraResult(errors.New("session killed before delivery"))
	if !res.VerifierFault || res.WorkerFault || res.Category != "infra" {
		t.Errorf("result = %+v, want an infra result marked as the verifier's fault", res)
	}
}

// A reply with no verdict in it is unreadable, not a finding about the work.
func TestParseVerifierReply(t *testing.T) {
	for _, bad := range []string{"", "I could not check this.", `{"ok": tru`, `{}`, `{"summary":"looks fine"}`, `{"error":"rate limited"}`} {
		if _, err := parseVerifierReply(bad); err == nil {
			t.Errorf("%q was read as a verdict", bad)
		}
	}
	v, err := parseVerifierReply("```json\n{\"ok\": false, \"severity\": \"high\", \"summary\": \"missing test\"}\n```")
	if err != nil || v.OK || v.Summary != "missing test" {
		t.Errorf("verdict = %+v, err %v", v, err)
	}
	if v, err = parseVerifierReply(`{"ok": true, "summary": "done"}`); err != nil || !v.OK {
		t.Errorf("verdict = %+v, err %v", v, err)
	}
}

// The daemon verify function marks each verifier fault, marks a worker crash as
// the worker's, does not wait for a pruned worker session on a verifier-only
// run, and starts the verify timer after the worker wait.
func TestDaemonVerifyWiring(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "autonomousVerify := func(")
	end := strings.Index(src, "autonomousGuardrailAttempt := func(")
	if start < 0 || end < start {
		t.Fatal("daemon verify function not found")
	}
	fn := src[start:end]
	if n := strings.Count(fn, "VerifierFault: true") + strings.Count(fn, "VerifierFault = true"); n < 3 {
		t.Errorf("verifier faults marked at %d sites, want at least 3 (diff, capacity, ask)", n)
	}
	if n := strings.Count(fn, "parseVerifierReply("); n != 2 {
		t.Errorf("parseVerifierReply used %d times, want 2 (session and ask replies)", n)
	}
	if !strings.Contains(fn, "WorkerFault:") {
		t.Error("a harness crash is not marked as the worker's fault")
	}
	if !strings.Contains(fn, "task.VerifierFaultRetries > 0") {
		t.Error("a verifier-only run still waits for a worker session that may be gone")
	}
	wait := strings.Index(fn, "mapSessionStartError(stallErr)")
	note := strings.Index(fn, "NoteVerificationStarted(task.ID)")
	diff := strings.Index(fn, "BL366 — git-diff grounding")
	if wait < 0 || diff < 0 || note < wait || note > diff {
		t.Errorf("NoteVerificationStarted is not called between the worker wait and the verification (wait %d, call %d, diff %d)", wait, note, diff)
	}
}
