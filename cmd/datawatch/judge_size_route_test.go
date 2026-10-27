package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestJudgeChainBySizeEqualToThresholdUnchanged(t *testing.T) {
	got, rule := judgeSizeRoute([]string{"pi", "claude-code", "ollama"}, 400, 400)
	if want := []string{"pi", "claude-code", "ollama"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !strings.Contains(rule, "at or below") {
		t.Fatalf("rule = %q", rule)
	}
}

func TestJudgeChainBySizeBelowThresholdUnchanged(t *testing.T) {
	got, rule := judgeSizeRoute([]string{"pi", "claude-code"}, 120, 400)
	if want := []string{"pi", "claude-code"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if want := "default order: 120 changed lines is at or below the threshold of 400"; rule != want {
		t.Fatalf("rule = %q want %q", rule, want)
	}
}

func TestJudgeChainBySizeAboveMovesClaudeCodeFirst(t *testing.T) {
	got, rule := judgeSizeRoute([]string{"pi", "opencode", "claude-code", "ollama"}, 1000, 400)
	if want := []string{"claude-code", "pi", "opencode", "ollama"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if want := "size rule: 1000 changed lines is above the threshold of 400, claude-code tried first"; rule != want {
		t.Fatalf("rule = %q want %q", rule, want)
	}
}

func TestJudgeChainBySizeAboveWithoutClaudeCodeUnchanged(t *testing.T) {
	got, rule := judgeSizeRoute([]string{"pi", "ollama"}, 1000, 400)
	if want := []string{"pi", "ollama"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !strings.Contains(rule, "not in the chain") {
		t.Fatalf("rule = %q", rule)
	}
}

func TestJudgeChainBySizeThresholdZeroIsOff(t *testing.T) {
	got, rule := judgeSizeRoute([]string{"pi", "claude-code"}, 1000000, 0)
	if want := []string{"pi", "claude-code"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if want := "default order: size routing is off (threshold 0)"; rule != want {
		t.Fatalf("rule = %q want %q", rule, want)
	}
}

func TestJudgeChainBySizeEmptyChain(t *testing.T) {
	for _, in := range [][]string{nil, {}} {
		for _, lines := range []int{0, 1000} {
			got, _ := judgeSizeRoute(in, lines, 400)
			if got == nil || len(got) != 0 {
				t.Fatalf("got %#v, want empty non-nil slice", got)
			}
		}
	}
}

func TestJudgeChainBySizeClaudeCodeAlreadyFirst(t *testing.T) {
	got, _ := judgeSizeRoute([]string{"claude-code", "pi", "ollama"}, 1000, 400)
	if want := []string{"claude-code", "pi", "ollama"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestJudgeChainBySizeDoesNotMutateInput(t *testing.T) {
	in := []string{"pi", "opencode", "claude-code", "pi"}
	orig := append([]string(nil), in...)
	got, _ := judgeSizeRoute(in, 1000, 400)
	if !reflect.DeepEqual(in, orig) {
		t.Fatalf("input mutated: %v want %v", in, orig)
	}
	got[0] = "changed"
	if !reflect.DeepEqual(in, orig) {
		t.Fatalf("returned slice aliases input: %v", in)
	}
	same, _ := judgeSizeRoute(in, 10, 400)
	same[0] = "changed"
	if !reflect.DeepEqual(in, orig) {
		t.Fatalf("returned slice aliases input on default order: %v", in)
	}
}

func TestJudgeChainBySizeRemovesDuplicates(t *testing.T) {
	cases := []struct {
		lines int
		want  []string
	}{
		{10, []string{"pi", "claude-code", "ollama"}},
		{1000, []string{"claude-code", "pi", "ollama"}},
	}
	for _, c := range cases {
		got, _ := judgeSizeRoute([]string{"pi", "claude-code", "pi", "ollama", "claude-code"}, c.lines, 400)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("lines %d: got %v want %v", c.lines, got, c.want)
		}
	}
}

func TestJudgeChainBySizeRuleNamesCountAndThreshold(t *testing.T) {
	chain := []string{"pi", "claude-code"}
	for _, lines := range []int{399, 400, 401, 7777} {
		_, rule := judgeSizeRoute(chain, lines, 400)
		if !strings.Contains(rule, fmt.Sprint(lines)) || !strings.Contains(rule, "400") {
			t.Fatalf("lines %d: rule %q omits count or threshold", lines, rule)
		}
		if strings.Contains(strings.ToLower(rule), "automaton") {
			t.Fatalf("rule %q names the Automaton object", rule)
		}
	}
}
