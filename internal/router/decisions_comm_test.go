package router

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func decisionsCommBody(n int) string {
	var parts []string
	for i := n; i >= 1; i-- {
		parts = append(parts, fmt.Sprintf(`{"id":"q%d","question":"Question %d?","state":"open","prd_id":"p1","task_ids":[],"waiting_task_ids":[],"waiting_tasks":[],"answer":null}`, i, i))
	}
	return `{"decisions":[` + strings.Join(parts, ",") + `]}`
}

func TestDecisionsComm(t *testing.T) {
	t.Run("verb reply", func(t *testing.T) {
		body := `{"decisions":[
 {"id":"q2","question":"Which store?","state":"open","prd_id":"p1","task_ids":["t1"],"waiting_task_ids":["t1"],"waiting_tasks":[{"task_id":"t1","wait_reason":"waiting on decision q2"}],"answer":null},
 {"id":"q1","question":"Use sqlite?","state":"answered","prd_id":"p1","task_ids":[],"waiting_task_ids":[],"waiting_tasks":[],"answer":{"id":"a1","quote":"yes, sqlite","actor":"dmz","surface":"cli"}}]}`
		log, port := laneFillCommDaemon(t, http.StatusOK, body)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous decisions p1"), " ")
		if log.path != "/api/autonomous/decisions" || !strings.Contains(log.query, "prd_id=p1") {
			t.Errorf("path=%s query=%q", log.path, log.query)
		}
		for _, want := range []string{
			"q2 open Which store? | waiting: task t1 (waiting on decision q2)",
			`q1 answered Use sqlite? | answer a1 by dmz: "yes, sqlite"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q: %q", want, got)
			}
		}
		if strings.Index(got, "q2 open") > strings.Index(got, "q1 answered") {
			t.Errorf("newest must come first: %q", got)
		}
	})

	t.Run("at most ten lines", func(t *testing.T) {
		_, port := laneFillCommDaemon(t, http.StatusOK, decisionsCommBody(14))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous decisions"), "\n")
		if n := strings.Count(got, "Question "); n != 10 {
			t.Errorf("want 10 decision lines, got %d: %q", n, got)
		}
		if !strings.Contains(got, "q14 open") || strings.Contains(got, "q4 open") {
			t.Errorf("must keep the newest ten: %q", got)
		}
	})

	t.Run("usage text", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK, decisionsCommBody(1))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous decisions a b c"), " ")
		if !strings.Contains(got, "usage: autonomous decisions [automaton-id]") {
			t.Errorf("extra args must be refused with the usage, got: %q", got)
		}
		if log.calls != 0 {
			t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
		}
		help := strings.Join(r.HandleTestMessage("autonomous"), " ")
		if !strings.Contains(help, "decisions [automaton-id]") {
			t.Errorf("the autonomous help must list the verb: %q", help)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		log, port := laneFillCommDaemon(t, http.StatusOK, `{"decisions":[]}`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous decisions nope"), " ")
		if !strings.Contains(log.query, "prd_id=nope") {
			t.Errorf("query=%q", log.query)
		}
		if !strings.Contains(got, "No decisions to show.") {
			t.Errorf("an unknown id gets the empty-result text, got %q", got)
		}
	})
}
