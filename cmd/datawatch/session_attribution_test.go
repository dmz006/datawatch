package main

import (
	"path/filepath"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// TestLaunchersStampAttribution runs each autonomous launcher against a fake
// session start endpoint and asserts the start request carries the Automaton
// id, task id and role, for several backends.
func TestLaunchersStampAttribution(t *testing.T) {
	const out = `{"outcome":"pass","severity":"info","summary":"ok"}`
	launchers := []struct {
		name               string
		wantAuto, wantTask string
		wantRole           string
		run                func(t *testing.T, cfg *config.Config, dir, backend, outFile string) map[string]any
	}{
		{name: "worker", wantAuto: "prd-1", wantTask: "task-1", wantRole: "worker"},
		{name: "decompose", wantAuto: "prd-1", wantTask: "", wantRole: "decompose",
			run: func(t *testing.T, cfg *config.Config, dir, backend, outFile string) map[string]any {
				_, err := runOneShotJSONSessionAttr(decomposeAttr("prd-1"), cfg, "prd-1", dir, backend, "m",
					decomposeSessionName, "task", outFile, "decompose-output.json", 10*time.Second, launchTags{})
				if err != nil {
					t.Fatal(err)
				}
				return nil
			}},
		{name: "verify", wantAuto: "prd-1", wantTask: "task-1", wantRole: "verification",
			run: func(t *testing.T, cfg *config.Config, dir, backend, outFile string) map[string]any {
				_, err := runOneShotJSONSessionWithEffortAttr(verifyAttr("prd-1", "task-1"), cfg, "prd-1", dir, backend, "m", "",
					"autonomous:verify:task-1", "task", outFile, "verify-task-1.json", 10*time.Second, "", launchTags{})
				if err != nil {
					t.Fatal(err)
				}
				return nil
			}},
		{name: "guardrail-task", wantAuto: "prd-1", wantTask: "task-1", wantRole: "guardrail",
			run: judgeRun(autonomouspkg.GuardrailInvocation{PRDID: "prd-1", Level: "task", UnitID: "task-1", Guardrail: "g"})},
		{name: "guardrail-story", wantAuto: "prd-1", wantTask: "", wantRole: "guardrail",
			run: judgeRun(autonomouspkg.GuardrailInvocation{PRDID: "prd-1", Level: "story", UnitID: "story-1", Guardrail: "g"})},
		{name: "rules-review", wantAuto: "prd-1", wantTask: "", wantRole: "rules_review",
			run: judgeRun(autonomouspkg.GuardrailInvocation{PRDID: "prd-1", Level: "prd", UnitID: "prd-1", Guardrail: "rules-review"})},
		{name: "plan-review", wantAuto: "prd-1", wantTask: "", wantRole: "rules_review",
			run: judgeRun(autonomouspkg.GuardrailInvocation{PRDID: "prd-1", Level: planReviewLevel, UnitID: "plan-prd-1", Guardrail: planReviewGuardrailName})},
	}
	for _, backend := range []string{"claude-code", "pi", "opencode", "opencode-acp", "ollama"} {
		for _, l := range launchers {
			t.Run(l.name+"/"+backend, func(t *testing.T) {
				var got map[string]any
				if l.run == nil { // worker: the request body builder
					got = buildAutonomousSessionBody(autonomouspkg.SpawnRequest{
						PRDID: "prd-1", TaskID: "task-1", Title: "t", Backend: backend}, "spec", "claude-code")
				} else {
					dir := t.TempDir()
					outFile := filepath.Join(dir, ".out.json")
					srv, started := fakeSessionServerCaptureStart(t, "running", outFile, out, true)
					withFakeLoopback(t, srv)
					cfg := &config.Config{}
					cfg.DataDir = t.TempDir()
					l.run(t, cfg, dir, backend, outFile)
					got = started
				}
				if v, _ := got["automaton_id"].(string); v != l.wantAuto {
					t.Errorf("automaton_id = %q, want %q", v, l.wantAuto)
				}
				if v, _ := got["task_id"].(string); v != l.wantTask {
					t.Errorf("task_id = %q, want %q", v, l.wantTask)
				}
				if v, _ := got["spend_role"].(string); v != l.wantRole {
					t.Errorf("spend_role = %q, want %q", v, l.wantRole)
				}
			})
		}
	}
}

func judgeRun(inv autonomouspkg.GuardrailInvocation) func(*testing.T, *config.Config, string, string, string) map[string]any {
	return func(t *testing.T, cfg *config.Config, dir, backend, outFile string) map[string]any {
		_, _, err := runOneShotJudgeSessionCaptureIDAttr(guardrailAttr(inv), cfg, inv.PRDID, dir, backend, "m", "",
			"autonomous:guardrail:"+inv.UnitID, "task", outFile, "g.json", 10*time.Second, "", launchTags{})
		if err != nil {
			t.Fatal(err)
		}
		return nil
	}
}
