package router

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func mergedMemberCommBody(n int) string {
	var parts []string
	for i := 1; i <= n; i++ {
		parts = append(parts, fmt.Sprintf(`{"automation_id":"a1","automation_name":"Ship","member":"m%d","branch":"automaton/b%d","default_branch":"main","proving_commit":"c%d","policy":"completed","outcome":"advanced","at":"2026-01-%02dT00:00:00Z"}`, i, i, i, i))
	}
	return `{"automation_id":"a1","merged_members":[` + strings.Join(parts, ",") + `]}`
}

func TestMergedMemberComm(t *testing.T) {
	t.Run("verb reply", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK, mergedMemberCommBody(2))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous merged-members a1"), " ")
		if log.path != "/api/automations/a1/merged-members" {
			t.Errorf("path=%s", log.path)
		}
		for _, want := range []string{
			"Ship | member m1 advanced | branch automaton/b1 on main | commit c1 | policy completed",
			"Ship | member m2 advanced | branch automaton/b2 on main | commit c2 | policy completed",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q: %q", want, got)
			}
		}
		if strings.Index(got, "member m2") > strings.Index(got, "member m1") {
			t.Errorf("newest must come first: %q", got)
		}
	})

	t.Run("all automata when no id", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK, mergedMemberCommBody(1))
		r := setupPeerRouter(t, port)
		r.HandleTestMessage("autonomous merged-members")
		if log.path != "/api/automations/merged-members" {
			t.Errorf("path=%s, want the list-all route", log.path)
		}
	})

	t.Run("at most ten lines", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusOK, mergedMemberCommBody(14))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous merged-members"), "\n")
		if n := strings.Count(got, "member m"); n != 10 {
			t.Errorf("want 10 lines, got %d: %q", n, got)
		}
		if !strings.Contains(got, "member m14 ") || strings.Contains(got, "member m4 ") {
			t.Errorf("must keep the newest ten: %q", got)
		}
	})

	t.Run("usage text", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK, mergedMemberCommBody(1))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous merged-members a b c"), " ")
		if !strings.Contains(got, "usage: autonomous merged-members [automation-id]") {
			t.Errorf("extra args must be refused with the usage, got: %q", got)
		}
		if log.calls != 0 {
			t.Errorf("nothing may be sent, saw %d calls", log.calls)
		}
	})

	t.Run("error surfaces", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusNotFound, "404: Automaton zz not found")
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous merged-members zz"), " ")
		if !strings.Contains(got, "autonomous merged-members failed") {
			t.Errorf("got: %q", got)
		}
	})
}

func TestMergedMemberComm_ParityListHasVerb(t *testing.T) {
	verbs := MergedMemberCommVerbs()
	if !slices.Contains(verbs, "merged-members") {
		t.Fatalf("MergedMemberCommVerbs() = %v, must contain merged-members", verbs)
	}
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "|merged-members [automation-id]|") {
		t.Errorf("the autonomous help must list the verb; got:\n%s", got)
	}
}
