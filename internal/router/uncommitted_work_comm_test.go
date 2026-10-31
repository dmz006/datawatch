package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// uncommittedWorkCommSample is the uncommitted-work status body for the comm
// tests: one task held on two files, one of them outside the task's declared
// files, and one whole-worktree hold.
const uncommittedWorkCommSample = `{"policy":"hold","scope":"task_files","held":[{` +
	`"automaton_id":"a1","name":"Add paging","task_id":"t7","task_title":"Wire the pager",` +
	`"kind":"task","files":["internal/pager/pager.go","docs/notes.md"],` +
	`"outside_scope":["docs/notes.md"],"reason":"2 files left uncommitted after verification",` +
	`"question_id":"q42","held_at":"2026-11-01T12:00:00Z"},{` +
	`"automaton_id":"a2","name":"Tidy config","kind":"worktree","files":["config.yaml"],` +
	`"outside_scope":[],"reason":"files left in the worktree at the end of the Automaton",` +
	`"question_id":"q43","held_at":"2026-11-01T12:30:00Z"}]}`

func TestUncommittedCommVerbReportsHeld(t *testing.T) {
	if !slices.Contains(UncommittedWorkCommVerbs(), "uncommitted-work") {
		t.Fatalf("UncommittedWorkCommVerbs() = %v, must contain %q", UncommittedWorkCommVerbs(), "uncommitted-work")
	}
	for _, verb := range []string{"uncommitted-work", "uncommitted_work"} {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}

	log, port := laneFillCommDaemon(t, http.StatusOK, uncommittedWorkCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous uncommitted-work"), " ")
	if log.path != "/api/autonomous/uncommitted-work" || log.query != "" {
		t.Errorf("path=%s query=%q; want the uncommitted-work route with no filter", log.path, log.query)
	}
	if log.calls != 1 {
		t.Errorf("the read is one call, got %d", log.calls)
	}
	for _, want := range []string{
		"policy: hold, scope: task_files",
		"2 held",
		"Automaton Add paging (a1), task Wire the pager (t7)",
		"internal/pager/pager.go",
		"outside the task's declared files: docs/notes.md",
		"2 files left uncommitted after verification",
		"q42",
		"Automaton Tidy config (a2), whole worktree",
		"q43",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}

	// The underscore alias answers the same read, and an Automaton id
	// narrows it.
	log, port = laneFillCommDaemon(t, http.StatusOK, uncommittedWorkCommSample)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous uncommitted_work a1"), " ")
	if log.query != "automaton=a1" {
		t.Errorf("query=%q, want automaton=a1", log.query)
	}
	if !strings.Contains(got, "Add paging") {
		t.Errorf("the underscore alias must answer the read, got: %q", got)
	}
}

func TestUncommittedCommVerbEmpty(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"policy":"hold","scope":"task_files","held":[]}`)
	r := setupPeerRouter(t, port)
	out := r.HandleTestMessage("autonomous uncommitted-work")
	if log.path != "/api/autonomous/uncommitted-work" {
		t.Errorf("path=%s, want the uncommitted-work route", log.path)
	}
	got := strings.Join(out, "\n")
	if !strings.Contains(got, "No Automaton has uncommitted work held") {
		t.Errorf("empty read must say nothing is held: %q", got)
	}
	// One line after the header: nothing is listed.
	if strings.Contains(got, "held:") || strings.Contains(got, "question:") {
		t.Errorf("empty read must list no holds: %q", got)
	}
	body := got[strings.Index(got, "No Automaton"):]
	if strings.Contains(strings.TrimSpace(body), "\n") {
		t.Errorf("empty read must be one line, got: %q", body)
	}
}

func TestUncommittedCommConfigureRoundTrip(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"autonomous.automation.uncommitted_work_policy", "warn"},
		{"autonomous.automation.uncommitted_work_scope", "worktree"},
	} {
		set := map[string]string{}
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
		r.SetConfigureFunc(func(k, v string) error {
			set[k] = v
			return nil
		})
		out := strings.Join(r.HandleTestMessage("configure "+tc.key+"="+tc.value), "\n")
		if set[tc.key] != tc.value {
			t.Errorf("configure %s: the handler received %v, want %s=%s", tc.key, set, tc.key, tc.value)
		}
		if !strings.Contains(out, "Set "+tc.key+" = "+tc.value) {
			t.Errorf("configure %s: reply does not confirm the write: %q", tc.key, out)
		}
	}
}

func TestUncommittedCommHelpListsVerb(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	help := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	if !strings.Contains(help, "uncommitted-work [automaton-id]") {
		t.Errorf("autonomous help lacks the uncommitted-work verb: %q", help)
	}

	// configure help and list name both settings.
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		out := strings.Join(r.HandleTestMessage("configure "+text), "\n")
		for _, want := range []string{
			"autonomous.automation.uncommitted_work_policy",
			"autonomous.automation.uncommitted_work_scope",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("configure %s lacks %q", text, want)
			}
		}
	}
}
