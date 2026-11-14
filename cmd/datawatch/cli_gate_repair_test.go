// datawatch autonomous gate-repair on the CLI: the status read and the run
// verb (one pass, or one failed gate). The text is the shared renderer the
// chat verb renders; --json prints the REST body untouched.

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const gateRepairStatusBody = `{"mode":"report","budget":["t1:infra","t1:code_or_test","t2:infra"],"records":[
 {"at":"2026-11-09T20:00:00Z","group":"gate_repair","subject":"a1 / t1","kind":"would","found":"infra: timeout after poll 4","done":"would re-run the gate (report mode; nothing changed)","undo":"re-run the gate yourself"}
]}`

const gateRepairRunBody = `{"runs":[
 {"mode":"on","class":"infra","subject":"a1 / t1","found":"infra: timeout after poll 4","did":"gated the gate re-run","done":"re-run finished green","before":"","after":"","held":"q1","kind":"did"}
],"status":` + gateRepairStatusBody + `}`

type gateRepairDaemonLog struct {
	calls int
	path  string
	body  string
}

func gateRepairDaemon(t *testing.T, status int, body string) *gateRepairDaemonLog {
	t.Helper()
	l := &gateRepairDaemonLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.calls++
		l.path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		l.body = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return l
}

func runGateRepairCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := &cobra.Command{Use: "autonomous", SilenceUsage: true, SilenceErrors: true}
	c.AddCommand(newAutonomousGateRepairCmd())
	c.SetArgs(args)
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

func TestGateRepairCLIRegistered(t *testing.T) {
	found, haveSub := false, map[string]bool{}
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "gate-repair" {
			found = true
			for _, s := range sub.Commands() {
				haveSub[s.Name()] = true
			}
		}
	}
	if !found {
		t.Fatal("gate-repair is not registered under autonomous")
	}
	for _, want := range []string{"status", "run"} {
		if !haveSub[want] {
			t.Errorf("gate-repair subcommand %q missing (have %v)", want, haveSub)
		}
	}
}

func TestGateRepairCLISTextAndJSON(t *testing.T) {
	l := gateRepairDaemon(t, http.StatusOK, gateRepairStatusBody)
	out, err := runGateRepairCmd(t, "gate-repair", "status")
	if err != nil {
		t.Fatal(err)
	}
	if l.path != "/api/autonomous/gate-repair" || l.calls != 1 {
		t.Errorf("path=%s calls=%d", l.path, l.calls)
	}
	for _, frag := range []string{"mode report", "a1 / t1", "would re-run the gate", "undo: re-run the gate yourself", "repair budget spent on 3 (task, class) pair(s)"} {
		if !strings.Contains(out, frag) {
			t.Errorf("status output lacks %q:\n%s", frag, out)
		}
	}

	gateRepairDaemon(t, http.StatusOK, gateRepairRunBody)
	out, err = runGateRepairCmd(t, "gate-repair", "run", "--automaton", "a1", "--task", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "did: gated the gate re-run") || !strings.Contains(out, "held question: q1") {
		t.Errorf("run output lacks outcome text:\n%s", out)
	}
	if !strings.Contains(out, "repair budget spent on 3 (task, class) pair(s)") {
		t.Errorf("run output lacks the fresh status:\n%s", out)
	}
}

func TestGateRepairCLIRunBodyAndRefusals(t *testing.T) {
	l := gateRepairDaemon(t, http.StatusOK, gateRepairRunBody)
	if _, err := runGateRepairCmd(t, "gate-repair", "run"); err != nil {
		t.Fatal(err)
	}
	if l.path != "/api/autonomous/gate-repair/run" {
		t.Errorf("path = %s", l.path)
	}
	if strings.Contains(l.body, "automaton_id") || strings.Contains(l.body, "task_id") {
		t.Errorf("a passive pass must send no target ids: %q", l.body)
	}

	l = gateRepairDaemon(t, http.StatusOK, gateRepairRunBody)
	if _, err := runGateRepairCmd(t, "gate-repair", "run", "--automaton", "a1", "--task", "t1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.body, `"automaton_id":"a1"`) || !strings.Contains(l.body, `"task_id":"t1"`) {
		t.Errorf("target body = %q", l.body)
	}

	for _, args := range [][2]string{
		{"--automaton", "a1"},
		{"--task", "t1"},
	} {
		gateRepairDaemon(t, http.StatusOK, gateRepairRunBody)
		_, err := runGateRepairCmd(t, "gate-repair", "run", args[0], args[1])
		if err == nil || !strings.Contains(err.Error(), "--automaton and --task") {
			t.Errorf("one-of target must be refused (args %v), err=%v", args, err)
		}
	}

	if _, err := runGateRepairCmd(t, "gate-repair", "reboot"); err == nil {
		t.Error("an unknown subcommand must fail")
	}
	if _, err := runGateRepairCmd(t, "gate-repair", "status", "extra"); err == nil {
		t.Error("an extra argument must be refused")
	}
}

func TestGateRepairCLIJSONUntouchedAndErrors(t *testing.T) {
	gateRepairDaemon(t, http.StatusOK, gateRepairStatusBody)
	out, err := runGateRepairCmd(t, "gate-repair", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"budget":["t1:infra"`) {
		t.Errorf("--json must pass the body untouched: %q", out)
	}

	gateRepairDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	if _, err := runGateRepairCmd(t, "gate-repair", "status"); err == nil {
		t.Error("a server failure must return an error")
	}
}
