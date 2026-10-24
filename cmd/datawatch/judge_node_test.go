package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// TestGuardrailJudgeRunsOnHeldNode: a guardrail judgment of a two-node entry
// takes its node lease, and the judgment starts on the node that lease holds,
// on the session path (compute_node on the start request) and on the ask path
// (compute_node on the /api/ask body).
func TestGuardrailJudgeRunsOnHeldNode(t *testing.T) {
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	mgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetCapacity(led, func(string, string) ([]string, string) { return []string{"host", "llm:pool"}, "" })
	mgr.SetCapacityChoices(func(string, string) []capacity.NodeChoice {
		return []capacity.NodeChoice{{Name: "slow", Priority: 10}, {Name: "fast", Priority: 90}}
	})
	mgr.SetNodeLoad(func(context.Context, string, string, func(string)) error { return nil }, nil)

	const prompt = "judge the unit against the guardrail"
	holder := guardrailJudgeHolder("unit-1", "sec", prompt)
	release, node, err := guardrailAdmitOn(context.Background(), mgr, "pi", "ollama/q", holder, "prd-g")
	if err != nil {
		t.Fatalf("guardrailAdmitOn: %v", err)
	}
	defer release()
	if node != "fast" {
		t.Fatalf("guardrail judge node = %q, want fast (highest priority)", node)
	}
	if ls := led.LeaseOf(holder); ls == nil || ls.Node != "fast" {
		t.Fatalf("guardrail judge lease = %+v, want node fast", ls)
	}

	// Session path: the one-shot judgment starts on the held node.
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".guardrail-output-held.json")
	srv, started := fakeSessionServerCaptureStart(t, "running", outputFile,
		`{"outcome":"pass","severity":"info","summary":"ok"}`, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()
	if _, _, err := runOneShotJudgeSessionCaptureID(cfg, "prd-g", projectDir, "pi", "ollama/q", "",
		"autonomous:guardrail:unit-1", prompt, outputFile, "guardrail-unit-1-sec.json",
		10*time.Second, node); err != nil {
		t.Fatalf("runOneShotJudgeSessionCaptureID: %v", err)
	}
	if got := started["compute_node"]; got != "fast" {
		t.Fatalf("judgment start compute_node = %v, want fast", got)
	}

	// Ask path: the same node pins the /api/ask call.
	body := judgeAskBody(prompt, "pi", "pi-entry", "ollama/q", node)
	if got := body["compute_node"]; got != "fast" {
		t.Fatalf("ask body compute_node = %v, want fast", got)
	}
	if got := body["llm"]; got != "pi-entry" {
		t.Fatalf("ask body llm = %v, want pi-entry", got)
	}
}

// TestJudgeAskBodyWithoutNodeUnchanged: with no held node the ask body carries
// exactly the fields it carried before judges held leases.
func TestJudgeAskBodyWithoutNodeUnchanged(t *testing.T) {
	body := judgeAskBody("q", "ollama", "ollama", "m", "")
	if _, ok := body["compute_node"]; ok {
		t.Fatalf("compute_node set with no held node: %v", body)
	}
	if _, ok := body["llm"]; ok {
		t.Fatalf("llm set for an entry named after its kind: %v", body)
	}
	if body["question"] != "q" || body["backend"] != "ollama" || body["model"] != "m" {
		t.Fatalf("ask body = %v", body)
	}
}
