package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAutomationImportGraphCLI(t *testing.T) {
	c := &captureDaemon{}
	withCaptureServer(t, c)

	t.Run("live import prints the Automation id", func(t *testing.T) {
		c.replyCode = http.StatusOK
		c.replyBody = `{"id":"ab12","name":"g1","status":"draft","members":[{"id":"m1"}]}`
		var err error
		out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "import-graph", "g1") })
		if err != nil {
			t.Fatalf("import-graph: %v", err)
		}
		if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations/import-graph/g1" {
			t.Errorf("proxied %s %s, want POST /api/automations/import-graph/g1", c.gotMethod, c.gotPath)
		}
		if got := c.body(t)["dry_run"]; got != false {
			t.Errorf("dry_run = %v, want false", got)
		}
		if strings.TrimSpace(out) != "ab12" {
			t.Errorf("output = %q, want the Automation id", out)
		}
	})

	t.Run("dry run prints indented JSON", func(t *testing.T) {
		c.replyCode = http.StatusOK
		c.replyBody = `{"input":{"name":"g1","members":[{"id":"m1"}]},"warnings":["w1"],"dry_run":true}`
		var err error
		out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "import-graph", "g1", "--dry-run") })
		if err != nil {
			t.Fatalf("import-graph --dry-run: %v", err)
		}
		if got := c.body(t)["dry_run"]; got != true {
			t.Errorf("dry_run = %v, want true", got)
		}
		if !strings.Contains(out, "\n  \"input\": {") || !strings.Contains(out, "\"w1\"") {
			t.Errorf("output is not indented JSON of the dry run result:\n%s", out)
		}
	})

	t.Run("refusal returns the reason as an error", func(t *testing.T) {
		c.replyCode = http.StatusUnprocessableEntity
		c.replyBody = "graph cannot be expressed as an Automation: node n3 has two parents"
		err := runCmd(t, newAutomationCmd(), "import-graph", "g1")
		if err == nil || !strings.Contains(err.Error(), "node n3 has two parents") {
			t.Errorf("err = %v, want one carrying the refusal reason", err)
		}
	})
}
