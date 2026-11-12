package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The held-for-protected-path alert and the question announcement reach the
// chat channel through the alert sink and the question channel. The alert body
// is built in internal/autonomous (recordProtectedPathsEvent), not in this
// package, so there is no alert-text test here: the wording the alert carries
// (the Automaton, the task and the changed paths) is covered by the
// protected-paths hold tests in that package.

// protectedPathsCommSample is the protected-paths status body for the comm
// tests: one task held on two protected paths with a question waiting, one
// task awaiting a spec change, and one Automaton that authorises a path.
const protectedPathsCommSample = `{"action":"hold","rules_source":"base_commit","paths":["AGENT.md","docs/plans/"],` +
	`"held":[{"automaton_id":"a1","automaton_title":"Add paging","task_id":"t7","task_title":"Wire the pager",` +
	`"paths":["AGENT.md","docs/plans/a.md"],"action":"hold","reason":"the commit changed protected paths",` +
	`"question_id":"q42","since":"2026-11-01T12:00:00Z","awaiting_spec_change":false},` +
	`{"automaton_id":"a2","automaton_title":"Tidy config","task_id":"t3","task_title":"Trim defaults",` +
	`"paths":["docs/plans/b.md"],"action":"hold","question_id":"q43","since":"2026-11-01T12:30:00Z","awaiting_spec_change":true}],` +
	`"authorized":[{"automaton_id":"a3","automaton_title":"Release notes","paths":["CHANGELOG.md"]}]}`

func TestProtectedPathsComm(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		if !slices.Equal(ProtectedPathsCommVerbs(), []string{"protected-paths", "authorize-paths"}) {
			t.Fatalf("ProtectedPathsCommVerbs() = %v", ProtectedPathsCommVerbs())
		}
		for _, verb := range ProtectedPathsCommVerbs() {
			if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
				t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
			}
		}

		log, port := laneFillCommDaemon(t, http.StatusOK, protectedPathsCommSample)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous protected-paths"), "\n")
		if log.path != "/api/autonomous/protected-paths" || log.query != "" || log.calls != 1 {
			t.Errorf("%d x %s?%s; want one read of the route with no filter", log.calls, log.path, log.query)
		}
		for _, want := range []string{
			"Automaton protected paths (action: hold, rules source: base_commit)",
			"Protected list (2): AGENT.md, docs/plans/",
			"Held: 2",
			"Automaton Add paging (a1), task Wire the pager (t7): AGENT.md, docs/plans/a.md",
			"reason: the commit changed protected paths",
			"question: q42 (approve, reject or change the spec)",
			"Automaton Tidy config (a2), task Trim defaults (t3): docs/plans/b.md",
			"waiting for the task spec to change",
			"Authorised in advance: 1",
			"Automaton Release notes (a3): CHANGELOG.md",
			// The reply points at the question queue for the answer.
			"question list",
			"question answer <question-id> approve | reject | change_spec",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q:\n%s", want, got)
			}
		}
		for _, bad := range []string{"PRD", "BL"} {
			if strings.Contains(got, bad) {
				t.Errorf("reply contains %q:\n%s", bad, got)
			}
		}

		// The underscore alias answers the same read, and an Automaton id
		// narrows it.
		log, port = laneFillCommDaemon(t, http.StatusOK, protectedPathsCommSample)
		r = setupPeerRouter(t, port)
		got = strings.Join(r.HandleTestMessage("autonomous protected_paths a1"), "\n")
		if log.query != "automaton=a1" {
			t.Errorf("query=%q, want automaton=a1", log.query)
		}
		if !strings.Contains(got, "Add paging") {
			t.Errorf("the underscore alias must answer the read, got: %q", got)
		}
	})

	t.Run("read empty", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusOK,
			`{"action":"warn","rules_source":"worktree","paths":[],"held":[],"authorized":[]}`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous protected-paths"), "\n")
		for _, want := range []string{
			"(action: warn, rules source: worktree)",
			"Protected list: none",
			"no Automaton task is held on protected paths",
			"no Automaton authorises protected paths",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q:\n%s", want, got)
			}
		}
		// Nothing is held, so there is no question to point at.
		if strings.Contains(got, "question answer") {
			t.Errorf("an empty read must not send the operator to the question queue:\n%s", got)
		}
	})

	t.Run("read bounds a long list", func(t *testing.T) {
		var paths []string
		for i := 0; i < 25; i++ {
			paths = append(paths, `"p`+string(rune('a'+i))+`/"`)
		}
		_, port := laneFillCommDaemon(t, http.StatusOK,
			`{"action":"hold","rules_source":"base_commit","paths":[`+strings.Join(paths, ",")+`],"held":[],"authorized":[]}`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous protected-paths"), "\n")
		if !strings.Contains(got, "Protected list (25):") || !strings.Contains(got, "and 5 more") {
			t.Errorf("a long list must be counted and cut at 20:\n%s", got)
		}
	})

	t.Run("read errors", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous protected-paths"), "\n")
		if !strings.Contains(got, "autonomous protected-paths failed") || !strings.Contains(got, "autonomous disabled") {
			t.Errorf("a refused read must say so with the daemon's text: %q", got)
		}
		// Too many arguments is a usage error and sends nothing.
		log, port := laneFillCommDaemon(t, http.StatusOK, protectedPathsCommSample)
		r = setupPeerRouter(t, port)
		got = strings.Join(r.HandleTestMessage("autonomous protected-paths a1 extra"), "\n")
		if !strings.Contains(got, "usage: autonomous protected-paths [automaton-id]") {
			t.Errorf("extra arguments must read the usage line: %q", got)
		}
		if log.calls != 0 {
			t.Errorf("a usage error must send nothing, got %d call(s)", log.calls)
		}
	})

	t.Run("authorize", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK,
			`{"id":"a1","title":"Add paging","authorized_paths":["AGENT.md","docs/plans/"]}`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous authorize-paths a1 docs/plans/ AGENT.md"), "\n")
		if log.calls != 1 || log.path != "/api/autonomous/prds/a1/set_authorized_paths" {
			t.Errorf("%d x %s; want one POST of set_authorized_paths", log.calls, log.path)
		}
		if log.body != `{"paths":["docs/plans/","AGENT.md"]}` {
			t.Errorf("body = %s, want the paths in order", log.body)
		}
		for _, want := range []string{
			"Automaton Add paging (a1) authorises 2 protected path(s): AGENT.md, docs/plans/",
			"does not answer a task that is already held",
			"question answer <question-id> approve | reject | change_spec",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q:\n%s", want, got)
			}
		}

		// The underscore alias is the same verb.
		log, port = laneFillCommDaemon(t, http.StatusOK, `{"id":"a1","authorized_paths":["x/"]}`)
		r = setupPeerRouter(t, port)
		got = strings.Join(r.HandleTestMessage("autonomous authorize_paths a1 x/"), "\n")
		if log.body != `{"paths":["x/"]}` || !strings.Contains(got, "authorises 1 protected path(s): x/") {
			t.Errorf("the underscore alias must set the list: body=%s reply=%q", log.body, got)
		}
	})

	t.Run("clear", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK, `{"id":"a1","title":"Add paging"}`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous authorize-paths a1 clear"), "\n")
		if log.calls != 1 || log.path != "/api/autonomous/prds/a1/set_authorized_paths" {
			t.Errorf("%d x %s; want one POST of set_authorized_paths", log.calls, log.path)
		}
		// The word clear empties the list: [] on the wire, never null and never
		// a path called clear.
		if log.body != `{"paths":[]}` {
			t.Errorf("body = %s, want {\"paths\":[]}", log.body)
		}
		if !strings.Contains(got, "Automaton Add paging (a1) authorises no protected paths") {
			t.Errorf("reply must say the list is empty: %q", got)
		}

		// The word is case-insensitive; with other paths it is only a path.
		log, port = laneFillCommDaemon(t, http.StatusOK, `{"id":"a1"}`)
		r = setupPeerRouter(t, port)
		r.HandleTestMessage("autonomous authorize-paths a1 CLEAR")
		if log.body != `{"paths":[]}` {
			t.Errorf("CLEAR: body = %s, want {\"paths\":[]}", log.body)
		}
		log, port = laneFillCommDaemon(t, http.StatusOK, `{"id":"a1","authorized_paths":["clear","a/"]}`)
		r = setupPeerRouter(t, port)
		r.HandleTestMessage("autonomous authorize-paths a1 clear a/")
		if log.body != `{"paths":["clear","a/"]}` {
			t.Errorf("clear among other paths: body = %s, want it kept as a path", log.body)
		}
	})

	t.Run("missing args usage", func(t *testing.T) {
		for _, msg := range []string{
			"autonomous authorize-paths",
			"autonomous authorize-paths a1",
			"autonomous authorize-paths ab/cd x/",
		} {
			log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
			r := setupPeerRouter(t, port)
			got := strings.Join(r.HandleTestMessage(msg), "\n")
			for _, want := range []string{
				"autonomous authorize-paths failed",
				"usage: autonomous authorize-paths <automaton-id> <path> [<path>...]",
				"autonomous authorize-paths <automaton-id> clear",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%q: reply lacks %q:\n%s", msg, want, got)
				}
			}
			if log.calls != 0 {
				t.Errorf("%q: a usage error must send nothing, got %d call(s)", msg, log.calls)
			}
		}
	})

	t.Run("unknown automaton", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusNotFound, "not found")
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous authorize-paths nosuch a/"), "\n")
		if !strings.Contains(got, "autonomous authorize-paths failed") || !strings.Contains(got, "Automaton nosuch was not found") {
			t.Errorf("an unknown Automaton must be named: %q", got)
		}
		// The read of an unknown Automaton is not an error: the daemon answers
		// with empty lists and the reply says nothing is held.
		_, port = laneFillCommDaemon(t, http.StatusOK,
			`{"action":"hold","rules_source":"base_commit","paths":["AGENT.md"],"held":[],"authorized":[]}`)
		r = setupPeerRouter(t, port)
		got = strings.Join(r.HandleTestMessage("autonomous protected-paths nosuch"), "\n")
		if !strings.Contains(got, "no Automaton task is held on protected paths") {
			t.Errorf("an unknown Automaton reads as nothing held: %q", got)
		}
	})

	t.Run("daemon refusal is passed back", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusBadRequest, `entry "/etc" is absolute; use a repository-relative path`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous authorize-paths a1 /etc"), "\n")
		if !strings.Contains(got, "autonomous authorize-paths failed") || !strings.Contains(got, "is absolute") {
			t.Errorf("a refused write must carry the daemon's text: %q", got)
		}
	})
}

// The three protected-paths settings are written with the existing
// `configure <key>=<value>`: the key and value reach the configure handler
// unchanged, so no parallel configure verb exists. The configure help names all
// three and the autonomous help names both new verbs.
func TestProtectedPathsCommConfigureIsGeneric(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"autonomous.automation.protected_paths", "docs/plans/,AGENT.md"},
		{"autonomous.automation.protected_paths_action", "block"},
		{"autonomous.automation.rules_review_source", "worktree"},
	} {
		set := map[string]string{}
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
		r.SetConfigureFunc(func(k, v string) error {
			set[k] = v
			return nil
		})
		out := strings.Join(r.HandleTestMessage("configure "+tc.key+"="+tc.value), "\n")
		if set[tc.key] != tc.value || len(set) != 1 {
			t.Errorf("configure %s: the handler received %v, want only %s=%s", tc.key, set, tc.key, tc.value)
		}
		if !strings.Contains(out, "Set "+tc.key+" = "+tc.value) {
			t.Errorf("configure %s: reply does not confirm the write: %q", tc.key, out)
		}
	}

	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	help := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	for _, want := range []string{
		"protected-paths [automaton-id]",
		"authorize-paths <automaton-id> <path> [<path>...]",
		"authorize-paths <automaton-id> clear",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		out := strings.Join(r.HandleTestMessage("configure "+text), "\n")
		for _, want := range []string{
			"autonomous.automation.protected_paths ",
			"autonomous.automation.protected_paths_action",
			"autonomous.automation.rules_review_source",
			"autonomous protected-paths [automaton-id]",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("configure %s lacks %q", text, want)
			}
		}
	}
}
