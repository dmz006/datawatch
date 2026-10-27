// comm `autonomous judge-policy-set judge_size_threshold_lines=` parsing, the
// show output and the configure help.

package router

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestJudgeSizeSurfaceCommSetsThreshold(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want float64
	}{
		{"autonomous judge-policy-set judge_size_threshold_lines=250", 250},
		{"autonomous judge-policy-set judge_size_threshold_lines=0", 0},
	} {
		got, _ := commVerifierPolicyCall(t, tc.msg, `{}`)
		if got.method != http.MethodPut || got.path != "/api/autonomous/config" {
			t.Fatalf("%q request = %s %s", tc.msg, got.method, got.path)
		}
		want := map[string]any{"judge_size_threshold_lines": tc.want}
		if !reflect.DeepEqual(got.body, want) {
			t.Fatalf("%q body = %v, want %v", tc.msg, got.body, want)
		}
	}
}

func TestJudgeSizeSurfaceCommRejectsBadValue(t *testing.T) {
	for _, msg := range []string{
		"autonomous judge-policy-set judge_size_threshold_lines=-5",
		"autonomous judge-policy-set judge_size_threshold_lines=big",
	} {
		got, out := commVerifierPolicyCall(t, msg, `{}`)
		if got.method != "" {
			t.Errorf("%q sent %s %s; bad input must not reach the daemon", msg, got.method, got.path)
		}
		if !strings.Contains(out, "judge_size_threshold_lines") || !strings.Contains(out, "usage") {
			t.Errorf("%q reply %q should name the key and carry the usage text", msg, out)
		}
	}
}

func TestJudgeSizeSurfaceCommShowReportsKey(t *testing.T) {
	got, out := commVerifierPolicyCall(t, "autonomous judge-policy",
		`{"enabled":true,"judge_policy":["qwen"],"judge_timeout_seconds":900,"judge_size_threshold_lines":250}`)
	if got.method != http.MethodGet || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if !strings.Contains(out, "judge_size_threshold_lines") || !strings.Contains(out, "250") || strings.Contains(out, "enabled") {
		t.Errorf("reply should report the size threshold with the judge settings only: %q", out)
	}
}

func TestJudgeSizeSurfaceConfigureHelpListsKey(t *testing.T) {
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, msg := range []string{"configure help", "configure list"} {
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(out, "autonomous.judge_size_threshold_lines") {
			t.Errorf("%q: help misses the key", msg)
		}
	}
}
