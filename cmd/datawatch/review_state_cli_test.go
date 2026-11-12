// Review state on the CLI surface: config-set carries the two new judge flags
// only when they were set (explicit false and an explicit empty backend are
// still sent), the help names both, and prd-get prints a judge timeout as
// "timed out" distinct from a "blocked" verdict.

package main

import (
	"strings"
	"testing"
)

func TestReviewStateCLIBodyHasValueOnlyWhenFlagSet(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags(nil)
	body := map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"judge_strong_backend", "judge_verdict_cache_enabled"} {
		if _, ok := body[k]; ok {
			t.Fatalf("an omitted flag must not send %s: %v", k, body)
		}
	}

	cmd = newAutonomousConfigSetCmd()
	if err := cmd.ParseFlags([]string{"--judge-strong-backend=qwen", "--judge-verdict-cache=false"}); err != nil {
		t.Fatal(err)
	}
	body = map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if body["judge_strong_backend"] != "qwen" {
		t.Fatalf("judge_strong_backend = %v, want qwen: %v", body["judge_strong_backend"], body)
	}
	v, ok := body["judge_verdict_cache_enabled"]
	if !ok || v != false {
		t.Fatalf("an explicit --judge-verdict-cache=false must be sent, got %v", body)
	}

	// An explicit empty backend (resolve to the last judge-policy entry) is
	// still sent, and an explicit true turns the cache back on.
	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--judge-strong-backend=", "--judge-verdict-cache=true"})
	body = map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["judge_strong_backend"]; !ok || v != "" {
		t.Fatalf("an explicit empty --judge-strong-backend must be sent, got %v", body)
	}
	if v, ok := body["judge_verdict_cache_enabled"]; !ok || v != true {
		t.Fatalf("an explicit --judge-verdict-cache=true must be sent, got %v", body)
	}
}

func TestReviewStateCLIHelpListsFlagAndKey(t *testing.T) {
	c := newAutonomousConfigSetCmd()
	help := c.Long + c.UsageString()
	for _, f := range []string{"judge-strong-backend", "judge_strong_backend", "judge-verdict-cache", "judge_verdict_cache_enabled"} {
		if !strings.Contains(help, f) {
			t.Errorf("config-set help lacks %s", f)
		}
	}
}

func TestReviewStateCLIPrdGetLineDistinguishesTimeoutFromBlock(t *testing.T) {
	for _, tc := range []struct {
		json string
		want string
	}{
		{`{"review_state":{"state":"timed_out","cause":"the judge timed out on qwen","action":"retry"}}`,
			"review timed out: the judge timed out on qwen -> retry"},
		{`{"review_state":{"state":"timed_out","cause":"the judge timed out","action":"reroute"}}`,
			"review timed out: the judge timed out -> reroute"},
		{`{"review_state":{"state":"blocked","cause":"the verdict found a blocking issue","action":"fix"}}`,
			"review blocked: the verdict found a blocking issue -> fix"},
		{`{"review_state":{"state":"passed","cause":"","action":""}}`, ""},
		{`{"review_state":{"state":"pending","cause":"awaiting the judges","action":""}}`, ""},
		{`{}`, ""},
		{`not json`, ""},
	} {
		got := prdReviewLine([]byte(tc.json))
		if got != tc.want {
			t.Errorf("prdReviewLine(%s) = %q, want %q", tc.json, got, tc.want)
		}
	}
	// A timeout must read as a timeout, never as a block, and vice versa.
	if line := prdReviewLine([]byte(`{"review_state":{"state":"timed_out","cause":"x","action":"retry"}}`)); strings.Contains(line, "blocked") {
		t.Errorf("timeout line %q must not say blocked", line)
	}
	if line := prdReviewLine([]byte(`{"review_state":{"state":"blocked","cause":"x","action":"fix"}}`)); strings.Contains(line, "timed out") {
		t.Errorf("blocked line %q must not say timed out", line)
	}
}
