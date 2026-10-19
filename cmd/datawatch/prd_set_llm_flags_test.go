// prd-set-llm: only the flags actually changed are sent in the body.

package main

import (
	"reflect"
	"testing"
)

func TestCLISetLLM_OnlyChangedFlagsSent(t *testing.T) {
	t.Run("DecompositionProfileOnly", func(t *testing.T) {
		got := runSetLLMCmd(t, newAutonomousPRDSetLLMCmd(), []string{"p1", "--decomposition-profile", "x"})
		want := map[string]string{"decomposition_profile": "x", "actor": "operator"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body=%v want %v", got, want)
		}
		for k := range map[string]bool{"backend": true, "effort": true, "model": true} {
			if _, ok := got[k]; ok {
				t.Errorf("unexpected key %q in body=%v", k, got)
			}
		}
	})
	t.Run("EmptyModelStillSent", func(t *testing.T) {
		got := runSetLLMCmd(t, newAutonomousPRDSetLLMCmd(), []string{"p1", "--model", ""})
		want := map[string]string{"model": "", "actor": "operator"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body=%v want %v", got, want)
		}
	})
	t.Run("BackendAndGuardrailModel", func(t *testing.T) {
		got := runSetLLMCmd(t, newAutonomousPRDSetLLMCmd(), []string{"p1", "--backend", "opencode", "--guardrail-model", "m1"})
		want := map[string]string{"backend": "opencode", "guardrail_model": "m1", "actor": "operator"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body=%v want %v", got, want)
		}
	})
}
