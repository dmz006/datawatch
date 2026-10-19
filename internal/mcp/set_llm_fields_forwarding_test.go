// Per-field presence forwarding: set_llm forwards exactly the legacy fields
// the tool call provided (plus actor), nothing more.

package mcp

import (
	"context"
	"reflect"
	"testing"
)

func TestMCPSetLLM_ForwardsOnlyProvidedFields(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want map[string]string
	}{
		{
			name: "decomposition_profile only",
			args: map[string]any{"id": "p1", "decomposition_profile": "x"},
			want: map[string]string{"decomposition_profile": "x", "actor": "operator"},
		},
		{
			name: "empty backend preserved as key",
			args: map[string]any{"id": "p1", "backend": ""},
			want: map[string]string{"backend": "", "actor": "operator"},
		},
		{
			name: "backend with guardrail_model",
			args: map[string]any{"id": "p1", "backend": "opencode", "guardrail_model": "m1"},
			want: map[string]string{"backend": "opencode", "guardrail_model": "m1", "actor": "operator"},
		},
		{
			name: "model only",
			args: map[string]any{"id": "p1", "model": "q"},
			want: map[string]string{"model": "q", "actor": "operator"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := captureSetLLMBody(t, func(s *Server) error {
				_, err := s.handleAutonomousPRDSetLLM(context.Background(), callReq(tc.args))
				return err
			})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("body = %v, want %v", got, tc.want)
			}
		})
	}
}
