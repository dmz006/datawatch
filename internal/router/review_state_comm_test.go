// Review state on the comm surface: judge-policy-set accepts the two new keys,
// judge-policy (show) reports them, and the PRD get reply carries a
// "timed out" status line distinct from a block.

package router

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestReviewStateCommPolicySetSendsKeys(t *testing.T) {
	got, _ := commVerifierPolicyCall(t,
		"autonomous judge-policy-set judge_strong_backend=claude-code judge_verdict_cache_enabled=false",
		`{}`)
	if got.method != http.MethodPut || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	want := map[string]any{"judge_strong_backend": "claude-code", "judge_verdict_cache_enabled": false}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %v, want %v", got.body, want)
	}
}

func TestReviewStateCommPolicySetDashSpelling(t *testing.T) {
	got, _ := commVerifierPolicyCall(t,
		"autonomous judge-policy-set judge-strong-backend=qwen judge-verdict-cache-enabled=true",
		`{}`)
	want := map[string]any{"judge_strong_backend": "qwen", "judge_verdict_cache_enabled": true}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %v, want %v", got.body, want)
	}
}

func TestReviewStateCommPolicySetRejectsBadCacheValue(t *testing.T) {
	got, out := commVerifierPolicyCall(t, "autonomous judge-policy-set judge_verdict_cache_enabled=maybe", `{}`)
	if got.method != "" {
		t.Fatalf("bad input must not reach the daemon: %s %s", got.method, got.path)
	}
	if !strings.Contains(out, "judge_verdict_cache_enabled") || !strings.Contains(out, "usage") {
		t.Errorf("reply should name the key and carry the usage text: %q", out)
	}
}

func TestReviewStateCommPolicyShowReportsKeys(t *testing.T) {
	_, out := commVerifierPolicyCall(t, "autonomous judge-policy",
		`{"judge_policy":["qwen"],"judge_strong_backend":"claude-code","judge_verdict_cache_enabled":false}`)
	if !strings.Contains(out, "judge_strong_backend") || !strings.Contains(out, "claude-code") ||
		!strings.Contains(out, "judge_verdict_cache_enabled") || !strings.Contains(out, "false") {
		t.Errorf("reply should report both keys with their values: %q", out)
	}
}

func TestReviewStateCommStatusLineDistinguishesTimeoutFromBlock(t *testing.T) {
	for _, tc := range []struct {
		body string
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
		got := reviewStateStatusLine(tc.body)
		if got != tc.want {
			t.Errorf("reviewStateStatusLine(%s) = %q, want %q", tc.body, got, tc.want)
		}
	}
	if line := reviewStateStatusLine(`{"review_state":{"state":"timed_out","cause":"x","action":"retry"}}`); strings.Contains(line, "blocked") {
		t.Errorf("timeout line %q must not say blocked", line)
	}
	if line := reviewStateStatusLine(`{"review_state":{"state":"blocked","cause":"x","action":"fix"}}`); strings.Contains(line, "timed out") {
		t.Errorf("blocked line %q must not say timed out", line)
	}
}
