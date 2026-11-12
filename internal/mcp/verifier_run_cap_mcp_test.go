package mcp

import (
	"context"
	"testing"
)

func TestVerifierRunCapMCPPresence(t *testing.T) {
	s, captured := captureResumeServer(t)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"verifier_max_run_seconds": 1800}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if got, ok := (*captured)["verifier_max_run_seconds"].(float64); !ok || got != 1800 {
		t.Fatalf("body %v", *captured)
	}
	s, captured = captureResumeServer(t)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_concurrent_tasks": 2})); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*captured)["verifier_max_run_seconds"]; ok {
		t.Fatalf("key present when absent: %v", *captured)
	}
}

func TestVerifierRunCapMCPZeroAccepted(t *testing.T) {
	s, captured := captureResumeServer(t)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"verifier_max_run_seconds": 0}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if got, ok := (*captured)["verifier_max_run_seconds"].(float64); !ok || got != 0 {
		t.Fatalf("0 not forwarded: %v", *captured)
	}
}

func TestVerifierRunCapMCPRefusesNegativeAndFractional(t *testing.T) {
	for _, v := range []float64{-1, 2.5} {
		s, captured := captureResumeServer(t)
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"verifier_max_run_seconds": v}))
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("%v accepted", v)
		}
		if len(*captured) != 0 {
			t.Fatalf("%v: request sent despite refusal: %v", v, *captured)
		}
	}
}
