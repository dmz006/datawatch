// comm `autonomous judge-policy` and `judge-policy-set` parsing and requests.

package router

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestJudgePolicyCommShowsJudgeSettingsOnly(t *testing.T) {
	got, out := commVerifierPolicyCall(t, "autonomous judge-policy",
		`{"enabled":true,"verifier_policy":{"mode":"balance"},"judge_policy":["qwen"],"judge_timeout_seconds":900}`)
	if got.method != http.MethodGet || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if !strings.Contains(out, "judge_policy") || !strings.Contains(out, "900") || strings.Contains(out, "verifier_policy") {
		t.Errorf("reply should carry the judge settings only: %q", out)
	}
}

func TestJudgePolicyCommSetsBackendsAndTimeout(t *testing.T) {
	got, _ := commVerifierPolicyCall(t, "autonomous judge-policy-set judge_policy=claude-code,qwen judge_timeout_seconds=900", `{}`)
	if got.method != http.MethodPut || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	want := map[string]any{"judge_policy": []any{"claude-code", "qwen"}, "judge_timeout_seconds": float64(900)}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %v, want %v", got.body, want)
	}
}

func TestJudgePolicyCommEmptyPolicyClearsTheList(t *testing.T) {
	got, _ := commVerifierPolicyCall(t, "autonomous judge-policy-set judge_policy=", `{}`)
	want := map[string]any{"judge_policy": []any{}}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %v, want an explicit empty list %v", got.body, want)
	}
}

func TestJudgePolicyCommRejectsBadInputWithoutRequest(t *testing.T) {
	for _, msg := range []string{
		"autonomous judge-policy-set judge_timeout_seconds=-1",
		"autonomous judge-policy-set judge_timeout_seconds=soon",
		"autonomous judge-policy-set verifier_policy=balance",
		"autonomous judge-policy-set",
	} {
		got, out := commVerifierPolicyCall(t, msg, `{}`)
		if got.method != "" {
			t.Errorf("%q sent %s %s; bad input must not reach the daemon", msg, got.method, got.path)
		}
		if !strings.Contains(out, "usage") && !strings.Contains(out, "must be") && !strings.Contains(out, "unknown key") {
			t.Errorf("%q reply %q should explain the problem", msg, out)
		}
	}
}
