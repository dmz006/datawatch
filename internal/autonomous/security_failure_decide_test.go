package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSecurityFailureDecide exercises the pure decide step and the
// caller's off-mode gate with no I/O: no files are read, no command is
// run, and the advisory seam (when consulted at all) is a test spy.
func TestSecurityFailureDecide(t *testing.T) {
	run := SecurityFailureRunIdentity{
		Workflow: "dependency-review",
		RunID:    424242,
		RunURL:   "https://ci.example/runs/424242",
		SHA:      "9f1c2e3a4b5c6d7e8f901234567890abcdef1234",
	}
	assessment := SecurityFailureAssessment{
		Kind:        SecurityFailureDependency,
		Dependency:  "example.com/mod",
		AdvisoryIDs: []string{"GHSA-aaaa-bbbb-cccc"},
	}
	entry := SecurityFailureRiskEntry{
		ID:         "osv/GHSA-aaaa-bbbb-cccc",
		Package:    "example.com/mod",
		FirstAdded: "2026-01-02T00:00:00Z",
		Added:      "2026-01-03T00:00:00Z",
		Expires:    "2026-12-31T00:00:00Z",
	}
	cleanRegistry := SecurityFailureRegistryResult{}
	foundRegistry := SecurityFailureRegistryResult{Found: true, Entry: entry}

	t.Run("upgrade when a fixed version is known, whatever the registry says", func(t *testing.T) {
		lookup := SecurityFailureAdvisoryResult{FixedVersion: "v1.2.3"}
		// Even with the advisory held in the registry — keep would be
		// the answer without a fixed version — the fixed version wins.
		report, action := decideSecurityFailure(assessment, run, foundRegistry, lookup, SecurityFailureModeShadow, true)
		if report.Recommendation != recommendUpgrade {
			t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendUpgrade)
		}
		if report.FixedVersion != "v1.2.3" {
			t.Fatalf("fixed_version = %q, want v1.2.3", report.FixedVersion)
		}
		if action != actionWouldStop {
			t.Fatalf("action = %q, want %q", action, actionWouldStop)
		}
	})

	t.Run("upgrade still when the registry failed", func(t *testing.T) {
		lookup := SecurityFailureAdvisoryResult{FixedVersion: "v1.2.3"}
		registry := SecurityFailureRegistryResult{Error: "open security/accepted-risks.yml: no such file"}
		report, _ := decideSecurityFailure(assessment, run, registry, lookup, SecurityFailureModeShadow, true)
		if report.Recommendation != recommendUpgrade {
			t.Fatalf("recommendation = %q, want %q (a known fixed version wins over a registry failure)", report.Recommendation, recommendUpgrade)
		}
		if report.RegistryError == "" {
			t.Fatal("registry_error not carried")
		}
	})

	t.Run("unknown when the lookup failed and no fixed version", func(t *testing.T) {
		lookup := SecurityFailureAdvisoryResult{ErrText: "gh not found on PATH"}
		report, _ := decideSecurityFailure(assessment, run, cleanRegistry, lookup, SecurityFailureModeShadow, true)
		if report.Recommendation != recommendUnknown {
			t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendUnknown)
		}
		if report.LookupError == "" {
			t.Fatal("lookup_error not carried")
		}
	})

	t.Run("unknown when the registry failed and no fixed version", func(t *testing.T) {
		registry := SecurityFailureRegistryResult{Error: "bad yaml"}
		report, _ := decideSecurityFailure(assessment, run, registry, SecurityFailureAdvisoryResult{}, SecurityFailureModeShadow, true)
		if report.Recommendation != recommendUnknown {
			t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendUnknown)
		}
	})

	t.Run("keep when a readable registry holds an entry and no fixed version", func(t *testing.T) {
		report, _ := decideSecurityFailure(assessment, run, foundRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeShadow, true)
		if report.Recommendation != recommendKeep {
			t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendKeep)
		}
		if !report.RegistryFound {
			t.Fatal("registry_found = false, want true")
		}
		if report.RegistryID != entry.ID || report.RegistryFirstAdded != entry.FirstAdded ||
			report.RegistryAdded != entry.Added || report.RegistryExpires != entry.Expires {
			t.Fatalf("registry fields not carried: %+v", report)
		}
	})

	t.Run("keep_no_entry when a readable registry holds no entry and no fixed version", func(t *testing.T) {
		report, _ := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeShadow, true)
		if report.Recommendation != recommendKeepNoEntry {
			t.Fatalf("recommendation = %q, want %q (the operator adds the entry through the registry; the coordinator writes nothing)", report.Recommendation, recommendKeepNoEntry)
		}
	})

	t.Run("report_only for a not_dependency assessment", func(t *testing.T) {
		na := assessment
		na.Kind = SecurityFailureNotDependency
		na.AdvisoryIDs = nil
		na.Dependency = ""
		// Even in on mode with a linked Automaton: a secret-scan finding
		// is reported, never acted on.
		report, action := decideSecurityFailure(na, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeOn, true)
		if action != actionReportOnly {
			t.Fatalf("action = %q, want %q", action, actionReportOnly)
		}
		if report.Note != "" {
			t.Fatalf("note = %q, want empty", report.Note)
		}
	})

	t.Run("report_only for an unknown assessment", func(t *testing.T) {
		na := assessment
		na.Kind = SecurityFailureUnknown
		na.AdvisoryIDs = nil
		na.Dependency = ""
		_, action := decideSecurityFailure(na, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeOn, true)
		if action != actionReportOnly {
			t.Fatalf("action = %q, want %q", action, actionReportOnly)
		}
	})

	t.Run("report_only with a note when no Automaton line carries the commit", func(t *testing.T) {
		_, action := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeOn, false)
		if action != actionReportOnly {
			t.Fatalf("action = %q, want %q", action, actionReportOnly)
		}
		report, _ := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeOn, false)
		if !strings.Contains(report.Note, "no Automaton line carries this run's commit") {
			t.Fatalf("note = %q, want a note that no Automaton line carries the commit", report.Note)
		}
	})

	t.Run("would_stop in shadow with a linked Automaton, never stopped", func(t *testing.T) {
		report, action := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeShadow, true)
		if action != actionWouldStop {
			t.Fatalf("action = %q, want %q", action, actionWouldStop)
		}
		if report.Action != actionWouldStop {
			t.Fatalf("report action = %q, want %q", report.Action, actionWouldStop)
		}
		if report.Action == actionStopped || action == actionStopped {
			t.Fatal("shadow mode must never stop")
		}
	})

	t.Run("stopped in on mode with a linked Automaton", func(t *testing.T) {
		report, action := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeOn, true)
		if action != actionStopped {
			t.Fatalf("action = %q, want %q", action, actionStopped)
		}
		if report.Action != actionStopped {
			t.Fatalf("report action = %q, want %q", report.Action, actionStopped)
		}
	})

	t.Run("published an hour ago is acted on the same as published a year ago", func(t *testing.T) {
		fresh := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		old := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339)
		for _, mode := range []string{SecurityFailureModeShadow, SecurityFailureModeOn} {
			freshReport, freshAction := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{PublishedDate: fresh, FixedVersion: "v2.0.0"}, mode, true)
			oldReport, oldAction := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{PublishedDate: old, FixedVersion: "v2.0.0"}, mode, true)
			if freshAction != oldAction {
				t.Fatalf("mode %s: action for a fresh advisory = %q, for an old one = %q; there is no age or waiting clock", mode, freshAction, oldAction)
			}
			if freshAction == actionReportOnly {
				t.Fatalf("mode %s: a dependency advisory with a linked Automaton must be acted on, got %q", mode, freshAction)
			}
			// published_at is carried verbatim, never invented.
			if freshReport.PublishedAt != fresh || oldReport.PublishedAt != old {
				t.Fatalf("published_at not carried verbatim: %q / %q", freshReport.PublishedAt, oldReport.PublishedAt)
			}
		}
	})

	t.Run("published_at is empty when unknown, never invented", func(t *testing.T) {
		lookup := SecurityFailureAdvisoryResult{ErrText: "advisories listing returned no advisories"}
		report, _ := decideSecurityFailure(assessment, run, cleanRegistry, lookup, SecurityFailureModeShadow, true)
		if report.PublishedAt != "" {
			t.Fatalf("published_at = %q, want empty when the lookup knew nothing", report.PublishedAt)
		}
	})

	t.Run("the report carries the run identity and the assessment", func(t *testing.T) {
		report, _ := decideSecurityFailure(assessment, run, cleanRegistry, SecurityFailureAdvisoryResult{}, SecurityFailureModeShadow, true)
		if report.Workflow != run.Workflow || report.RunID != run.RunID || report.RunURL != run.RunURL || report.SHA != run.SHA {
			t.Fatalf("run identity not carried: %+v", report)
		}
		if report.Dependency != assessment.Dependency || len(report.AdvisoryIDs) != 1 || report.AdvisoryIDs[0] != "GHSA-aaaa-bbbb-cccc" {
			t.Fatalf("assessment not carried: %+v", report)
		}
		if report.AssessedAt == "" {
			t.Fatal("assessed_at empty")
		}
	})
	// The caller path, not the pure function: in off mode the caller
	// returns report_only before any lookup — the advisory seam is a spy
	// that fails the test if called, and the registry (pointed at a path
	// that cannot exist) never gets consulted, so nothing about it is
	// reported.
	t.Run("off mode caller returns before any lookup", func(t *testing.T) {
		var lookedUp []string
		restore := setSecurityFailureAdvisoryFn(func(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult {
			lookedUp = append(lookedUp, advisoryID)
			return SecurityFailureAdvisoryResult{}
		})
		defer restore()

		report, action := DecideSecurityFailure(context.Background(), "no/such/repo/directory", assessment, run, SecurityFailureModeOff, true)

		if action != actionReportOnly {
			t.Fatalf("action = %q, want %q", action, actionReportOnly)
		}
		if len(lookedUp) != 0 {
			t.Fatalf("advisory lookup was consulted in off mode: %v", lookedUp)
		}
		if report.RegistryError != "" || report.RegistryFound {
			t.Fatalf("registry was consulted in off mode: found=%v error=%q", report.RegistryFound, report.RegistryError)
		}
		if report.LookupError != "" {
			t.Fatalf("lookup error present in off mode: %q", report.LookupError)
		}
		if report.Mode != SecurityFailureModeOff {
			t.Fatalf("mode = %q, want %q", report.Mode, SecurityFailureModeOff)
		}
		if report.Action != actionReportOnly {
			t.Fatalf("report action = %q, want %q", report.Action, actionReportOnly)
		}
	})
}
