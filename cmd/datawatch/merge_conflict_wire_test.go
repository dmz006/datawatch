package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// The conflict model is the named registry entry: an API entry is called
// through the dispatcher and a session entry as a one-shot session, each with
// the prompt unchanged. An unknown or disabled entry is never replaced.
func TestMergeConflictCallerReachesTheNamedEntry(t *testing.T) {
	reg := inference.NewRegistry()
	for _, l := range []*inference.LLM{
		{Name: "api-entry", Kind: inference.KindOllama, Model: "m1", ComputeNodes: []string{"n1"}},
		{Name: "session-entry", Kind: inference.KindClaudeCode, Model: "sonnet"},
		{Name: "off-entry", Kind: inference.KindClaudeCode, Disabled: true},
	} {
		if err := reg.Add(l); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	call := mergeConflictCaller(reg,
		func(_ context.Context, entry, prompt string) (string, error) {
			got = []string{"direct", entry, prompt}
			return "d", nil
		},
		func(_ context.Context, backend, model, prompt string) (string, error) {
			got = []string{"session", backend, model, prompt}
			return "s", nil
		})
	for entry, want := range map[string]string{"api-entry": "direct|api-entry|P", "session-entry": "session|session-entry|sonnet|P"} {
		got = nil
		if _, err := call(context.Background(), entry, "P"); err != nil || strings.Join(got, "|") != want {
			t.Errorf("%s: %v %v, want %s", entry, got, err, want)
		}
	}
	for _, entry := range []string{"off-entry", "claude-sonnet-5-5"} {
		got = nil
		if _, err := call(context.Background(), entry, "P"); err == nil || got != nil {
			t.Errorf("%s: called %v err %v, want a refusal and no call", entry, got, err)
		}
	}
}

func TestDaemonWiresTheMergeConflictModel(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "wireMergeConflictModel(amgr, cfg, autonomousInferenceReg, inferenceDispatcher)") {
		t.Error("main.go does not wire the merge conflict model")
	}
}
