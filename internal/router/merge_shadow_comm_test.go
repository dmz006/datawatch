package router

import (
	"net/http"
	"strings"
	"testing"
)

const mergeShadowCommView = `{"projects":[
{"project":"a","plans":[],"comparisons":[{"automaton_id":"x"},{"automaton_id":"y"}],"counts":{"patch":{"matched":1,"different":1},"minor":{"matched":0,"different":0},"major":{"matched":0,"different":0},"none":{"matched":0,"different":0}},"streak":{"length":2,"total":2,"threshold":3,"met":false}},
{"project":"b","plans":[],"comparisons":[{"automaton_id":"z"}],"counts":{"patch":{"matched":0,"different":0},"minor":{"matched":1,"different":0},"major":{"matched":0,"different":0},"none":{"matched":0,"different":0}},"streak":{"length":4,"total":4,"threshold":3,"met":true}}]}`

func TestMergeShadowComm_SummaryLine(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, mergeShadowCommView)
	got := mergePipelineSay(t, port, "autonomous merge-shadow")
	if rec.method != http.MethodGet || rec.path != "/api/autonomous/merge/shadow" {
		t.Errorf("sent %s %s", rec.method, rec.path)
	}
	want := "Merge shadow: patch 1 matched, 1 different; minor 1 matched, 0 different; major 0 matched, 0 different; none 0 matched, 0 different; streak: a 2/3 not met, b 4/3 met"
	if !strings.Contains(got, want) {
		t.Errorf("reply lacks %q: %q", want, got)
	}
}

// TestMergeShadowComm_NoStreakFields is the legacy shape: projects that carry
// no streak field must not change the summary line at all.
func TestMergeShadowComm_NoStreakFields(t *testing.T) {
	body := `{"projects":[{"project":"a","plans":[],"comparisons":[{"automaton_id":"x"}],"counts":{"patch":{"matched":0,"different":1},"minor":{"matched":0,"different":0},"major":{"matched":0,"different":0},"none":{"matched":0,"different":0}}}]}`
	_, port := mergePipelineCommDaemon(t, http.StatusOK, body)
	got := mergePipelineSay(t, port, "autonomous merge-shadow")
	if strings.Contains(got, "streak") {
		t.Errorf("reply without streak fields must not mention streaks: %q", got)
	}
}

func TestMergeShadowComm_EmptyReply(t *testing.T) {
	for _, body := range []string{`{"projects":[]}`, `{"projects":[{"project":"a","plans":[],"comparisons":[],"counts":{}}]}`} {
		_, port := mergePipelineCommDaemon(t, http.StatusOK, body)
		got := mergePipelineSay(t, port, "autonomous merge-shadow")
		if !strings.Contains(got, "No merge comparisons yet") {
			t.Errorf("body %s: reply %q", body, got)
		}
	}
}

func TestMergeShadowComm_ErrorAndAlias(t *testing.T) {
	_, port := mergePipelineCommDaemon(t, http.StatusInternalServerError, "boom")
	if got := mergePipelineSay(t, port, "autonomous merge_shadow"); !strings.Contains(got, "failed") {
		t.Errorf("reply %q", got)
	}
	if v := MergeShadowCommVerbs(); len(v) != 1 || v[0] != "merge-shadow" {
		t.Errorf("verbs %v", v)
	}
}
