package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestVerifierConfigRoundTripMCP(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	_, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"verifier_infra_escalation_threshold": 3,
		"fallback_verification_backend":       "gemini",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got["verifier_infra_escalation_threshold"] != float64(3) || got["fallback_verification_backend"] != "gemini" {
		t.Fatalf("not forwarded: %v", got)
	}
}
