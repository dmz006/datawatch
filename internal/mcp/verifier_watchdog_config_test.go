package mcp

import (
	"context"
	"testing"
)

func TestVerifierWatchdogConfigSetForwardsKey(t *testing.T) {
	s, captured := captureResumeServer(t)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"verifier_no_progress_timeout_seconds": 420})); err != nil {
		t.Fatal(err)
	}
	got, ok := (*captured)["verifier_no_progress_timeout_seconds"].(float64)
	if !ok || got != 420 {
		t.Fatalf("verifier_no_progress_timeout_seconds = %v, want 420 (body %v)", (*captured)["verifier_no_progress_timeout_seconds"], *captured)
	}

	s, captured = captureResumeServer(t)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_concurrent_tasks": 2})); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*captured)["verifier_no_progress_timeout_seconds"]; ok {
		t.Fatalf("key present when the caller never mentioned it: %v", *captured)
	}
}
