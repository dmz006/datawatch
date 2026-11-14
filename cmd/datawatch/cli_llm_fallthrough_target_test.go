// CLI surface for the per-LLM fall-through mark: `datawatch llm add|update
// --fallthrough-target`. The flag is sent only when passed, so an update that
// omits it keeps the stored value, and --fallthrough-target=false clears it.

package main

import "testing"

func TestLLMFallthroughTargetFlag(t *testing.T) {
	cases := []struct {
		name    string
		update  bool
		args    []string
		want    bool // value forwarded, when present
		present bool
	}{
		{"add marked", false, []string{"w", "--fallthrough-target"}, true, true},
		{"add plain", false, []string{"w"}, false, false},
		{"update sets", true, []string{"w", "--fallthrough-target"}, true, true},
		{"update clears", true, []string{"w", "--fallthrough-target=false"}, false, true},
		{"update omitted keeps stored", true, []string{"w", "--model", "m"}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := llmDaemon(t)
			c := newLLMAddCmd(tc.update)
			c.SetArgs(tc.args)
			if err := runRunE(t, c); err != nil {
				t.Fatalf("run: %v", err)
			}
			calls := seen()
			if len(calls) != 1 {
				t.Fatalf("calls = %+v, want one", calls)
			}
			got, ok := calls[0].body["fallthrough_target"]
			if ok != tc.present {
				t.Fatalf("fallthrough_target present = %v, want %v (body %v)", ok, tc.present, calls[0].body)
			}
			if ok && got != tc.want {
				t.Fatalf("fallthrough_target = %v, want %v", got, tc.want)
			}
		})
	}
}
