package config

import (
	"reflect"
	"testing"
)

func TestNormalizeJudgeTimeoutSeconds_DefaultIsThirtyMinutes(t *testing.T) {
	if JudgeTimeoutDefaultSeconds != 1800 {
		t.Fatalf("JudgeTimeoutDefaultSeconds = %d, want 1800 (30 minutes)", JudgeTimeoutDefaultSeconds)
	}
	for _, v := range []int{0, -1, -900} {
		if got := NormalizeJudgeTimeoutSeconds(v); got != 1800 {
			t.Errorf("NormalizeJudgeTimeoutSeconds(%d) = %d, want 1800", v, got)
		}
	}
	if got := NormalizeJudgeTimeoutSeconds(900); got != 900 {
		t.Errorf("NormalizeJudgeTimeoutSeconds(900) = %d, want 900", got)
	}
}

func TestNormalizeJudgePolicy_TrimsDedupesAndKeepsOrder(t *testing.T) {
	got := NormalizeJudgePolicy([]string{" claude-code ", "", "qwen", "claude-code", "  "})
	if want := []string{"claude-code", "qwen"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeJudgePolicy = %v, want %v", got, want)
	}
}

// An empty judge policy must be an empty list, not nil, so the JSON surfaces
// render [] and a clear round-trips.
func TestNormalizeJudgePolicy_EmptyIsEmptyListNotNil(t *testing.T) {
	for _, in := range [][]string{nil, {}, {"  "}} {
		got := NormalizeJudgePolicy(in)
		if got == nil || len(got) != 0 {
			t.Fatalf("NormalizeJudgePolicy(%v) = %#v, want empty non-nil list", in, got)
		}
	}
}
