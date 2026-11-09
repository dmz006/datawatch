package main

import (
	"net/http"
	"strings"
	"testing"
)

// mergePipelineReadinessCLIBody is a release readiness reply for a patch
// release that meets the bar, so the ready flag and the zero-missing line print.
const mergePipelineReadinessCLIBody = `{"size":"patch","project":"demo","readiness":{"size":"patch","records":10,"conflicts":2,"pwa_changes":2,"blocking_defects":0,"required":{"records":10,"conflicts":2,"pwa_changes":2},"missing":[],"ready":true},"bar":{"records":10,"conflicts":2,"pwa_changes":2}}`

func TestMergePipelineCLI_ReadinessValidSizeAndProject(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineReadinessCLIBody)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"readiness", "--size", "PATCH", "--project", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/release-readiness" || d.query != "project=demo&size=patch" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	for _, want := range []string{
		"release readiness: patch (project demo)\n",
		"records (plan equal or better): 10 of 10\n",
		"conflicts: 2 of 2\n",
		"PWA changes: 2 of 2\n",
		"blocking defects: 0\n",
		"missing: none\n",
		"ready: true\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMergePipelineCLI_ReadinessZeroCountsAndMissing(t *testing.T) {
	const zero = `{"size":"major","project":"","readiness":{"size":"major","records":0,"conflicts":0,"pwa_changes":0,"blocking_defects":0,"required":{"records":10,"conflicts":2,"pwa_changes":2},"missing":["The major release needs 10 Automata comparison records where the plan is equal or better, but has 0."],"ready":false},"bar":{"records":10,"conflicts":2,"pwa_changes":2}}`
	d := newLaneFillCLIDaemon(t, http.StatusOK, zero)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"readiness", "--size", "major"})
	if err != nil {
		t.Fatal(err)
	}
	if d.query != "size=major" {
		t.Errorf("query=%q, want size=major only", d.query)
	}
	for _, want := range []string{"release readiness: major (all projects)\n", "records (plan equal or better): 0 of 10\n", "missing: The major release needs 10", "ready: false\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMergePipelineCLI_ReadinessRefusesBadSize(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineReadinessCLIBody)
	if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"readiness", "--size", "huge"}); err == nil || !strings.Contains(err.Error(), "patch, minor or major") {
		t.Errorf("err = %v, want the size rule", err)
	}
	if d.calls != 0 {
		t.Errorf("sent %d requests for a bad size, want none", d.calls)
	}
}

func TestMergePipelineCLI_ReadinessRequiresSize(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineReadinessCLIBody)
	if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"readiness"}); err == nil || !strings.Contains(err.Error(), "size") {
		t.Errorf("err = %v, want a missing --size error", err)
	}
	if d.calls != 0 {
		t.Errorf("sent %d requests without --size, want none", d.calls)
	}
}
