// comm `autonomous set-llm` verb: only provided tokens are forwarded in the
// REST proxy body (omitted legacy keys absent, empty values forwarded as "").

package router

import (
	"reflect"
	"testing"
)

func TestCommSetLLM_OnlyProvidedTokens(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		path string
		want map[string]string
	}{
		{
			name: "single token value",
			msg:  "autonomous set-llm p1 decomposition_profile=x",
			path: "/api/autonomous/prds/p1/set_llm",
			want: map[string]string{"decomposition_profile": "x", "actor": "operator"},
		},
		{
			name: "empty token value forwarded",
			msg:  "autonomous set-llm p1 backend=",
			path: "/api/autonomous/prds/p1/set_llm",
			want: map[string]string{"backend": "", "actor": "operator"},
		},
		{
			name: "single positional still supported",
			msg:  "autonomous set-llm p1 opencode",
			path: "/api/autonomous/prds/p1/set_llm",
			want: map[string]string{"backend": "opencode", "actor": "operator"},
		},
		{
			name: "legacy and guardrail tokens coexist",
			msg:  "autonomous set-llm p1 model=q guardrail_backend=ollama guardrail_model=",
			path: "/api/autonomous/prds/p1/set_llm",
			want: map[string]string{
				"model":             "q",
				"guardrail_backend": "ollama",
				"guardrail_model":   "",
				"actor":             "operator",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, got := commSetLLMBody(t, tc.msg)
			if path != tc.path {
				t.Errorf("path=%s want %s", path, tc.path)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("body=%v want %v", got, tc.want)
			}
		})
	}
}
