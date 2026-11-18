// Comm verb tests for "autonomous event-kinds": argument parsing, the request
// sent to the REST route, the group filter and the rendered text.

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const eventKindsTestBody = `{"groups":[
 {"group":"task","kinds":[
  {"kind":"task.failed","description":"a task failed","wake_default":true},
  {"kind":"task.completed","description":"a task finished","wake_default":false}]},
 {"group":"automaton","kinds":[
  {"kind":"automaton.stuck_running","description":"an Automaton is stuck running","wake_default":true}]}]}`

// eventKindsDaemon serves GET /api/autonomous/event-kinds with the given body
// and records each request it receives.
func eventKindsDaemon(t *testing.T, body string, reqs *[]subsDaemonRequest) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reqs = append(*reqs, subsDaemonRequest{method: r.Method, path: r.URL.Path, rawquery: r.URL.RawQuery})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/autonomous/event-kinds" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

func TestEventKindsCommParse(t *testing.T) {
	g, err := parseCommEventKinds([]string{"group=Task"})
	if err != nil || g != "Task" {
		t.Fatalf("group = %q, %v", g, err)
	}
	none, err := parseCommEventKinds(nil)
	if err != nil || none != "" {
		t.Fatalf("no args = %q, %v", none, err)
	}
	blank, err := parseCommEventKinds([]string{"group="})
	if err != nil || blank != "" {
		t.Fatalf("blank value should mean unset: %q, %v", blank, err)
	}
	for _, bad := range [][]string{{"bogus=1"}, {"task"}, {"=x"}} {
		_, err := parseCommEventKinds(bad)
		if err == nil || !strings.Contains(err.Error(), commEventKindsUsage) {
			t.Errorf("%v: want an error carrying the usage line, got %v", bad, err)
		}
	}
}

func TestEventKindsCommListsAllGroups(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := eventKindsDaemon(t, eventKindsTestBody, &reqs)
	defer stop()

	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous event-kinds"))
	if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].path != "/api/autonomous/event-kinds" {
		t.Fatalf("daemon requests = %v", reqs)
	}
	for _, want := range []string{
		"task:",
		"  * task.failed  a task failed",
		"    task.completed  a task finished",
		"automaton:",
		"  * automaton.stuck_running  an Automaton is stuck running",
		"* wakes the supervisor by default",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "task:") > strings.Index(out, "automaton:") {
		t.Errorf("groups must keep the daemon's order:\n%s", out)
	}
}

func TestEventKindsCommGroupFilter(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := eventKindsDaemon(t, eventKindsTestBody, &reqs)
	defer stop()
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous event-kinds group=AUTOMATON"))
	if !strings.Contains(out, "automaton.stuck_running") || strings.Contains(out, "task.failed") {
		t.Errorf("group filter should keep only the automaton group:\n%s", out)
	}

	out = joinReplies(r.HandleTestMessage("autonomous event-kinds group=nope"))
	if !strings.Contains(out, `unknown group "nope"`) || !strings.Contains(out, "groups: task, automaton") {
		t.Errorf("unknown group should list the real groups:\n%s", out)
	}
}

func TestEventKindsCommUsageAndWording(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := eventKindsDaemon(t, eventKindsTestBody, &reqs)
	defer stop()
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous event-kinds bogus=1"))
	if !strings.Contains(out, commEventKindsUsage) {
		t.Errorf("a bad argument should show the usage line:\n%s", out)
	}
	if len(reqs) != 0 {
		t.Errorf("a refused argument must not reach the daemon (%v)", reqs)
	}
	// The operator text says Automaton/Automata; the old internal word must not leak.
	old := strings.Join([]string{"p", "r", "d"}, "")
	if strings.Contains(strings.ToLower(commEventKindsUsage), old) {
		t.Errorf("operator text must say Automaton/Automata: %s", commEventKindsUsage)
	}
}

func TestEventKindsCommEmptyAndBadBody(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := eventKindsDaemon(t, `{"groups":[]}`, &reqs)
	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous event-kinds"))
	stop()
	if !strings.Contains(out, "no event kinds") {
		t.Errorf("empty listing reply:\n%s", out)
	}

	port, stop = eventKindsDaemon(t, `not json`, &reqs)
	defer stop()
	out = joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous event-kinds"))
	if !strings.Contains(out, "autonomous event-kinds failed") {
		t.Errorf("a bad body should be reported as a failure:\n%s", out)
	}
}

func TestEventKindsCommInHelp(t *testing.T) {
	out := joinReplies(subsRouter(t, "grp1", 1).HandleTestMessage("autonomous nosuchverb"))
	if !strings.Contains(out, "event-kinds [group=G]") {
		t.Errorf("the autonomous help should list event-kinds:\n%s", out)
	}
}
