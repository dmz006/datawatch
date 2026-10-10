// BL406 Phase 0 — config bridge tests. Confirms B113's fix: an operator
// who has never touched any scan knob gets scan.DefaultConfig()'s
// all-on intent (not the Go zero-value), while any single explicitly-
// set field — including an explicit disable — is preserved exactly.

package main

import (
	"testing"

	scanpkg "github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/config"
)

func TestScanConfigIsUnset(t *testing.T) {
	if !scanConfigIsUnset(config.ScanConfig{}) {
		t.Fatal("zero-value ScanConfig must be reported as unset")
	}
	if scanConfigIsUnset(config.ScanConfig{Enabled: true}) {
		t.Fatal("Enabled:true must be reported as set")
	}
	if scanConfigIsUnset(config.ScanConfig{SASTEnabled: false, SecretsEnabled: false, DepsEnabled: false, MaxFindings: 5}) {
		t.Fatal("a single non-zero numeric field must be reported as set")
	}
	if scanConfigIsUnset(config.ScanConfig{ProjectRules: []config.ProjectRuleConfig{{ID: "r1"}}}) {
		t.Fatal("a non-empty project_rules list must be reported as set")
	}
}

func TestScanConfigFromYAML_PreservesExplicitDisable(t *testing.T) {
	// B113 regression: an operator who explicitly disabled SAST (while
	// leaving everything else untouched) must never have it silently
	// re-enabled — this is NOT the "unset" case, scanConfigIsUnset must
	// return false and the explicit value must survive conversion.
	c := config.ScanConfig{Enabled: true, SASTEnabled: false, SecretsEnabled: true, DepsEnabled: true}
	if scanConfigIsUnset(c) {
		t.Fatal("explicitly configured ScanConfig must not be reported as unset")
	}
	got := scanConfigFromYAML(c)
	if got.SASTEnabled {
		t.Fatal("explicit SASTEnabled:false must survive conversion, got true")
	}
	if !got.SecretsEnabled || !got.DepsEnabled {
		t.Fatalf("explicit true fields must survive conversion, got %+v", got)
	}
}

func TestScanConfigFromYAML_ProjectRulesConvert(t *testing.T) {
	c := config.ScanConfig{
		ProjectRules: []config.ProjectRuleConfig{{
			ID: "r1", Name: "version-sync", Type: "consistency",
			Granularity: "prd_complete", Pattern: "cmd/datawatch/main.go|internal/server/api.go",
			Severity: "error", Action: "file_upstream_issue", UpstreamTarget: "app",
		}},
	}
	got := scanConfigFromYAML(c)
	if len(got.ProjectRules) != 1 {
		t.Fatalf("got %d project rules, want 1", len(got.ProjectRules))
	}
	r := got.ProjectRules[0]
	if r.ID != "r1" || r.Type != scanpkg.RuleTypeConsistency || r.Granularity != scanpkg.GranularityPRD ||
		r.Action != scanpkg.ActionFileUpstreamIssue || r.UpstreamTarget != "app" {
		t.Fatalf("project rule conversion mismatch: %+v", r)
	}
}

func TestUpstreamReposFromYAML(t *testing.T) {
	got := upstreamReposFromYAML([]config.UpstreamRepoConfig{
		{Name: "app", OwnerRepo: "dmz006/datawatch-app"},
	})
	if len(got) != 1 || got[0].Name != "app" || got[0].OwnerRepo != "dmz006/datawatch-app" {
		t.Fatalf("got %+v", got)
	}
}
