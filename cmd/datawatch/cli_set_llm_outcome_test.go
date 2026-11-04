// `datawatch autonomous prd-set-llm` / `prd-set-story-llm` /
// `prd-set-task-llm` — the CLI prints the pretty daemon reply AND the
// queued-requeue structured outcome (changed / left-alone tasks, planning
// requeue or deferral) as a short "outcome:" line, the same result the
// REST / MCP / comm surfaces show.

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const cliSetLLMOutcomeEnvelope = `{"ok":true,
  "override":{"changed":[{"task_id":"t1","capacity_moved":true},{"task_id":"t2"}],
    "left_alone":[{"task_id":"t3","reason":"task is running; the override takes no effect while it runs"}]},
  "planning":{"outcome":"requeued","old_pools":["claude-code"],"new_pools":["opencode"],
    "reason":"the queued planning run was retargeted from claude-code to opencode"}}`

func newCLISetLLMFakeDaemon(t *testing.T, reply []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(req.URL.Path, "/api/autonomous/prds/") {
			t.Errorf("unexpected path %q", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runCLISetLLM(t *testing.T, srv *httptest.Server, c *cobra.Command, args []string) string {
	t.Helper()
	oldURL := serverURL
	serverURL = srv.URL
	defer func() { serverURL = oldURL }()
	var out string
	out = captureStdout(t, func() {
		if err := c.RunE(c, args); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	return out
}

var cliSetLLMOutcomeWants = []string{
	"outcome: changed: t1 (capacity retargeted), t2",
	"left alone: t3 \u2014 task is running; the override takes no effect while it runs",
	"planning requeued (pools claude-code \u2192 opencode)",
	// the pretty daemon reply still prints its JSON
	`"capacity_moved": true`,
}

func TestCLI_PRDSetLLM_PrintsQueuedOutcome(t *testing.T) {
	srv := newCLISetLLMFakeDaemon(t, []byte(cliSetLLMOutcomeEnvelope))
	c := newAutonomousPRDSetLLMCmd()
	if err := c.Flags().Set("backend", "opencode"); err != nil {
		t.Fatal(err)
	}
	out := runCLISetLLM(t, srv, c, []string{"p1"})
	for _, want := range cliSetLLMOutcomeWants {
		if !strings.Contains(out, want) {
			t.Errorf("prd-set-llm output missing %q:\n%s", want, out)
		}
	}
}

func TestCLI_PRDSetStoryLLM_PrintsQueuedOutcome(t *testing.T) {
	srv := newCLISetLLMFakeDaemon(t, []byte(cliSetLLMOutcomeEnvelope))
	c := newAutonomousPRDSetStoryLLMCmd()
	for k, v := range map[string]string{"story": "s1", "backend": "opencode"} {
		if err := c.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	out := runCLISetLLM(t, srv, c, []string{"p1"})
	for _, want := range cliSetLLMOutcomeWants {
		if !strings.Contains(out, want) {
			t.Errorf("prd-set-story-llm output missing %q:\n%s", want, out)
		}
	}
}

func TestCLI_PRDSetTaskLLM_PrintsQueuedOutcome(t *testing.T) {
	srv := newCLISetLLMFakeDaemon(t, []byte(cliSetLLMOutcomeEnvelope))
	c := newAutonomousPRDSetTaskLLMCmd()
	for k, v := range map[string]string{"task": "t1", "backend": "opencode"} {
		if err := c.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	out := runCLISetLLM(t, srv, c, []string{"p1"})
	for _, want := range cliSetLLMOutcomeWants {
		if !strings.Contains(out, want) {
			t.Errorf("prd-set-task-llm output missing %q:\n%s", want, out)
		}
	}
}

func TestCLI_PRDSetLLM_ClassicReply_NoOutcomeLine(t *testing.T) {
	srv := newCLISetLLMFakeDaemon(t, []byte(`{"ok":true}`))
	c := newAutonomousPRDSetLLMCmd()
	if err := c.Flags().Set("backend", "opencode"); err != nil {
		t.Fatal(err)
	}
	out := runCLISetLLM(t, srv, c, []string{"p1"})
	if !strings.Contains(out, `"ok": true`) {
		t.Errorf("classic reply not printed: %s", out)
	}
	if strings.Contains(out, "outcome:") {
		t.Errorf("outcome line invented for a classic reply: %s", out)
	}
}
