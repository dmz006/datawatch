package router

import (
	"net/http"
	"strings"
	"testing"
)

// mergePipelineReadinessBody is a release readiness reply for a minor release
// with no comparisons yet: every count zero, one missing item per short bar.
const mergePipelineReadinessBody = `{"size":"minor","project":"demo","readiness":{"size":"minor","records":0,"conflicts":0,"pwa_changes":0,"blocking_defects":0,"required":{"records":10,"conflicts":2,"pwa_changes":2},"missing":["The minor release needs 10 Automata comparison records where the plan is equal or better, but has 0."],"ready":false},"bar":{"records":10,"conflicts":2,"pwa_changes":2}}`

func TestMergePipelineComm_ReleaseReadinessValidSizeAndProject(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, mergePipelineReadinessBody)
	got := mergePipelineSay(t, port, "autonomous merge-pipeline release-readiness minor demo")
	if rec.method != http.MethodGet || rec.path != "/api/autonomous/release-readiness" || rec.query != "project=demo&size=minor" {
		t.Errorf("sent %s %s?%s", rec.method, rec.path, rec.query)
	}
	for _, want := range []string{
		"release readiness: minor (project demo)",
		"records (plan equal or better): 0 of 10",
		"conflicts: 0 of 2",
		"PWA changes: 0 of 2",
		"blocking defects: 0",
		"missing: The minor release needs 10 Automata comparison records",
		"ready: false",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestMergePipelineComm_ReleaseReadinessReadyWithoutProject(t *testing.T) {
	const ready = `{"size":"patch","project":"","readiness":{"size":"patch","records":10,"conflicts":2,"pwa_changes":2,"blocking_defects":0,"required":{"records":10,"conflicts":2,"pwa_changes":2},"missing":[],"ready":true},"bar":{"records":10,"conflicts":2,"pwa_changes":2}}`
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, ready)
	got := mergePipelineSay(t, port, "autonomous merge-pipeline release-readiness size=patch")
	if rec.query != "size=patch" {
		t.Errorf("query = %q, want size=patch only", rec.query)
	}
	for _, want := range []string{"release readiness: patch (all projects)", "records (plan equal or better): 10 of 10", "missing: none", "ready: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestMergePipelineComm_ReleaseReadinessRefusesBadSize(t *testing.T) {
	for _, msg := range []string{
		"autonomous merge-pipeline release-readiness huge",
		"autonomous merge-pipeline release-readiness",
	} {
		rec, port := mergePipelineCommDaemon(t, http.StatusOK, mergePipelineReadinessBody)
		got := mergePipelineSay(t, port, msg)
		if rec.calls != 0 {
			t.Errorf("%q: sent %d requests to the daemon, want none", msg, rec.calls)
		}
		if !strings.Contains(got, "release-readiness") || (!strings.Contains(got, "patch, minor or major") && !strings.Contains(got, "usage:")) {
			t.Errorf("%q: reply does not name the size rule: %q", msg, got)
		}
	}
}
