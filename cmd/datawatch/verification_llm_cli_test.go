// prd-set-task-verification-llm / prd-set-story-verification-llm /
// prd-set-verification-llm: only set flags are sent, "" clears.

package main

import (
	"reflect"
	"testing"
)

func TestCLISetTaskVerificationLLM_OnlySetFlags(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetTaskVerificationLLMCmd(), []string{"p1", "--task", "t1", "--backend", "ollama"})
	want := map[string]string{"task_id": "t1", "verification_backend": "ollama", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetTaskVerificationLLM_ExplicitEmptyClears(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetTaskVerificationLLMCmd(), []string{"p1", "--task", "t1", "--backend", "", "--model", "qwen3", "--effort", "high"})
	want := map[string]string{"task_id": "t1", "verification_backend": "", "verification_model": "qwen3", "verification_effort": "high", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetStoryVerificationLLM_OnlySetFlags(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetStoryVerificationLLMCmd(), []string{"p1", "--story", "s1", "--model", "m1"})
	want := map[string]string{"story_id": "s1", "verification_model": "m1", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetVerificationLLM_OnlySetFlags(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetVerificationLLMCmd(), []string{"p1", "--effort", ""})
	want := map[string]string{"verification_effort": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetVerificationLLM_NoFlags(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetVerificationLLMCmd(), []string{"p1"})
	want := map[string]string{"actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}
