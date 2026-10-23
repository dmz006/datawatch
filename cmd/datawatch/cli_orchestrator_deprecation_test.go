package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/orchestrator"
)

func TestOrchestratorCLIDeprecationNotice(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"g1"}`}
	withCaptureServer(t, c)

	cases := []struct {
		args []string
		tool string
		dep  bool
	}{
		{[]string{"graph-create", "t", `["p1"]`}, "orchestrator_graph_create", true},
		{[]string{"graph-plan", "g1"}, "orchestrator_graph_plan", true},
		{[]string{"graph-run", "g1"}, "orchestrator_graph_run", true},
		{[]string{"graph-get", "g1"}, "", false},
		{[]string{"graph-list"}, "", false},
		{[]string{"config-get"}, "", false},
		{[]string{"verdicts"}, "", false},
		{[]string{"graph-cancel", "g1"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.args[0], func(t *testing.T) {
			cmd := newOrchestratorCmd()
			var errBuf bytes.Buffer
			cmd.SetErr(&errBuf)
			var err error
			out := captureStdout(t, func() { err = runCmd(t, cmd, tc.args...) })
			if err != nil {
				t.Fatalf("%v: %v", tc.args, err)
			}
			if strings.Contains(out, "deprecated") {
				t.Errorf("stdout must stay unchanged, got %q", out)
			}
			if !tc.dep {
				if errBuf.Len() != 0 {
					t.Errorf("%s printed a notice: %q", tc.args[0], errBuf.String())
				}
				return
			}
			if !strings.Contains(errBuf.String(), orchestrator.DeprecationNotice(tc.tool)) {
				t.Errorf("stderr = %q, want notice for %s", errBuf.String(), tc.tool)
			}
			if !strings.Contains(errBuf.String(), "automation import-graph") {
				t.Errorf("notice does not name automation import-graph: %q", errBuf.String())
			}
		})
	}

	for _, name := range []string{"graph-create", "graph-plan", "graph-run"} {
		sub, _, err := newOrchestratorCmd().Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sub.Short, "Deprecated") || !strings.Contains(sub.Short, "automation import-graph") {
			t.Errorf("%s help = %q", name, sub.Short)
		}
	}
}
