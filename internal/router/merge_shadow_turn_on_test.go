package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestMergeShadowCommReport(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, `{"projects":[],"turn_on":{"lines":["Turn-on report for demo. Merging is off.","To turn it off: x"]}}`)
	got := mergePipelineSay(t, port, "autonomous merge-shadow report")
	if rec.method != http.MethodGet || rec.path != "/api/autonomous/merge/shadow" {
		t.Errorf("sent %s %s", rec.method, rec.path)
	}
	if !strings.Contains(got, "Turn-on report for demo. Merging is off.\nTo turn it off: x") {
		t.Errorf("reply %q", got)
	}
	_, port = mergePipelineCommDaemon(t, http.StatusOK, `{"projects":[]}`)
	if got := mergePipelineSay(t, port, "autonomous merge-shadow report"); !strings.Contains(got, "No turn-on report") {
		t.Errorf("reply %q", got)
	}
	_, port = mergePipelineCommDaemon(t, http.StatusOK, `{"projects":[],"turn_on":{"merges":[{}],"lines":["Turn-on report for demo.","Matched hand merges in a row: 2 of 5 (not met)."]}}`)
	if got := mergePipelineSay(t, port, "autonomous merge-shadow"); !strings.Contains(got, "2 of 5") {
		t.Errorf("summary %q", got)
	}
}

func TestShippedRecordCommHandChecks(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, `{}`)
	mergePipelineSay(t, port, "autonomous shipped-record a1 commit=abc source=merge_step hand_checks=lint=pass,build=fail")
	if !strings.Contains(rec.body, `"hand_checks":{"build":"fail","lint":"pass"}`) {
		t.Errorf("body %s", rec.body)
	}
	if got := mergePipelineSay(t, port, "autonomous shipped-record a1 commit=abc hand_checks=lint"); !strings.Contains(got, "failed") {
		t.Errorf("reply %q", got)
	}
}
