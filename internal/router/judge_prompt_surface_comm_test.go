// comm `autonomous judge-policy-set` parsing for the judge prompt delivery
// keys, the show output and the configure help.

package router

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

var judgePromptCommKeys = []string{
	"judge_prompt_file_threshold_lines",
	"judge_delivery_wait_per_thousand_lines_seconds",
}

func TestJudgePromptSurfaceCommSetsBothValues(t *testing.T) {
	for _, key := range judgePromptCommKeys {
		for _, tc := range []struct {
			val  string
			want float64
		}{{"250", 250}, {"0", 0}} {
			msg := "autonomous judge-policy-set " + key + "=" + tc.val
			got, _ := commVerifierPolicyCall(t, msg, `{}`)
			if got.method != http.MethodPut || got.path != "/api/autonomous/config" {
				t.Fatalf("%q request = %s %s", msg, got.method, got.path)
			}
			want := map[string]any{key: tc.want}
			if !reflect.DeepEqual(got.body, want) {
				t.Fatalf("%q body = %v, want %v", msg, got.body, want)
			}
		}
	}
}

func TestJudgePromptSurfaceCommRejectsBadValue(t *testing.T) {
	for _, key := range judgePromptCommKeys {
		for _, v := range []string{"-5", "big"} {
			msg := "autonomous judge-policy-set " + key + "=" + v
			got, out := commVerifierPolicyCall(t, msg, `{}`)
			if got.method != "" {
				t.Errorf("%q sent %s %s; bad input must not reach the daemon", msg, got.method, got.path)
			}
			if !strings.Contains(out, key) || !strings.Contains(out, "usage") {
				t.Errorf("%q reply %q should name the key and carry the usage text", msg, out)
			}
		}
	}
}

func TestJudgePromptSurfaceCommShowReportsKeys(t *testing.T) {
	got, out := commVerifierPolicyCall(t, "autonomous judge-policy",
		`{"enabled":true,"judge_prompt_file_threshold_lines":321,"judge_delivery_wait_per_thousand_lines_seconds":77}`)
	if got.method != http.MethodGet || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	for _, want := range append([]string{"321", "77"}, judgePromptCommKeys...) {
		if !strings.Contains(out, want) {
			t.Errorf("reply should contain %q: %q", want, out)
		}
	}
	if strings.Contains(out, `"enabled"`) {
		t.Errorf("reply should carry the judge settings only: %q", out)
	}
}

func TestJudgePromptSurfaceConfigureHelpListsKeys(t *testing.T) {
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, msg := range []string{"configure help", "configure list"} {
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		for _, key := range judgePromptCommKeys {
			if !strings.Contains(out, "autonomous."+key) {
				t.Errorf("%q: help misses %s", msg, key)
			}
		}
	}
}
