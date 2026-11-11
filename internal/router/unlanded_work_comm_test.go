package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

const unlandedCommSample = `{"branches":[{"automaton_id":"a1","branch":"automaton/a1","title":"Add paging","commits":3,` +
	`"last_activity":"2026-11-01T12:00:00Z","recommendation":"merge","reason":"nothing conflicts"}]}`

func TestUnlandedCommVerbListsBranches(t *testing.T) {
	if !slices.Contains(UnlandedWorkCommVerbs(), "unlanded-work") {
		t.Fatalf("UnlandedWorkCommVerbs() = %v", UnlandedWorkCommVerbs())
	}
	for _, verb := range []string{"unlanded-work", "unlanded_work"} {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v`, verb, c.Type)
		}
		log, port := laneFillCommDaemon(t, http.StatusOK, unlandedCommSample)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous "+verb), " ")
		if log.path != "/api/autonomous/unlanded-work" || log.calls != 1 {
			t.Errorf("path=%s calls=%d", log.path, log.calls)
		}
		for _, want := range []string{"Add paging (automaton/a1)", "3 commits", "recommended: merge (nothing conflicts)"} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q: %q", want, got)
			}
		}
	}
}

func TestUnlandedCommVerbEmptyAndError(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, `{"branches":[]}`)
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous unlanded-work"), " ")
	if !strings.Contains(got, "No archived Automaton has work") {
		t.Errorf("empty reply = %q", got)
	}
	_, port = laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	got = strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous unlanded-work"), " ")
	if !strings.Contains(got, "failed") {
		t.Errorf("error reply = %q", got)
	}
}

func TestUnlandedCommVerbActions(t *testing.T) {
	cases := []struct {
		msg, path, bodyHas, replyHas string
		resp                         string
	}{
		{"autonomous unlanded-work keep automaton/a1 still useful", "/api/autonomous/unlanded-work/keep", `"note":"still useful"`, "Kept automaton/a1", `{"branch":"automaton/a1","kept":true}`},
		{"autonomous unlanded-work archive-branch automaton/a1", "/api/autonomous/unlanded-work/archive-branch", `"branch":"automaton/a1"`, "Archived automaton/a1 as refs/archive/a1", `{"branch":"automaton/a1","archive_ref":"refs/archive/a1"}`},
		{"autonomous unlanded-work delete-worktree automaton/a1 confirm", "/api/autonomous/unlanded-work/delete-worktree", `"confirm":true`, "backup bundle: /b/a1.bundle", `{"branch":"automaton/a1","bundle_path":"/b/a1.bundle"}`},
	}
	for _, tc := range cases {
		log, port := laneFillCommDaemon(t, http.StatusOK, tc.resp)
		got := strings.Join(setupPeerRouter(t, port).HandleTestMessage(tc.msg), " ")
		if log.path != tc.path || !strings.Contains(log.body, tc.bodyHas) {
			t.Errorf("%q: path=%s body=%s", tc.msg, log.path, log.body)
		}
		if !strings.Contains(got, tc.replyHas) {
			t.Errorf("%q: reply lacks %q: %q", tc.msg, tc.replyHas, got)
		}
	}
}

func TestUnlandedCommVerbRefusals(t *testing.T) {
	for _, msg := range []string{
		"autonomous unlanded-work bogus",
		"autonomous unlanded-work keep",
		"autonomous unlanded-work delete-worktree automaton/a1",
		"autonomous unlanded-work delete-worktree automaton/a1 yes",
	} {
		log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
		got := strings.Join(setupPeerRouter(t, port).HandleTestMessage(msg), " ")
		if log.calls != 0 {
			t.Errorf("%q must not reach the daemon", msg)
		}
		if !strings.Contains(got, "failed") {
			t.Errorf("%q: reply = %q", msg, got)
		}
	}
	// A daemon refusal on an action surfaces as a failure.
	_, port := laneFillCommDaemon(t, http.StatusNotFound, "branch is not listed")
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous unlanded-work keep x"), " ")
	if !strings.Contains(got, "failed") {
		t.Errorf("daemon refusal reply = %q", got)
	}
}

func TestUnlandedCommVerbDefaultUsage(t *testing.T) {
	r := &Router{}
	r.handleUnlandedWorkVerb("other", nil)
}
