package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestAutonomousConfigSet_NegativeVerifierHeadroomIsRefused pins that a present
// negative verifier headroom value is a tool error, as the tool description
// says, and is not silently dropped. The refusal happens before any REST call.
func TestAutonomousConfigSet_NegativeVerifierHeadroomIsRefused(t *testing.T) {
	s := &Server{}
	for _, k := range []string{"verifier_min_free_mem_mb", "verifier_headroom_max_age_hours"} {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = map[string]any{k: -1}
		res, err := s.handleAutonomousConfigSet(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: handler error %v", k, err)
		}
		if res == nil || !res.IsError {
			t.Errorf("%s = -1: result is not a tool error: %+v", k, res)
		}
	}
}
