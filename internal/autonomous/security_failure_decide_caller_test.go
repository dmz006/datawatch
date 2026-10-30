package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// advisorySpy replaces the advisory lookup seam for one test and counts the
// calls that reach it. The returned result is what the lookup answers.
type advisorySpy struct {
	mu     sync.Mutex
	calls  int
	gotIDs []string
	result SecurityFailureAdvisoryResult
}

func (s *advisorySpy) install(t *testing.T, result SecurityFailureAdvisoryResult) {
	t.Helper()
	s.result = result
	restore := setSecurityFailureAdvisoryFn(func(_ context.Context, advisoryID string) SecurityFailureAdvisoryResult {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		s.gotIDs = append(s.gotIDs, advisoryID)
		return s.result
	})
	t.Cleanup(restore)
}

func (s *advisorySpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// decideCallerRun and decideCallerDependency are the shared fixture for the
// caller-path tests: a dependency assessment with one advisory and a run that
// carries a SHA.
var (
	decideCallerRun = SecurityFailureRunIdentity{
		Workflow: "CodeQL",
		RunID:    9100001,
		RunURL:   "https://ci.example/runs/9100001",
		SHA:      "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d",
	}
	decideCallerDependency = SecurityFailureAssessment{
		Kind:        SecurityFailureDependency,
		Dependency:  "example.com/mod",
		AdvisoryIDs: []string{"GHSA-7p2w-4v3x-9q8m"},
	}
)

// The off-mode gate returns before any lookup: neither the registry nor the
// advisory seam is consulted, and the report says so.
func TestDecideSecurityFailureOffConsultsNothing(t *testing.T) {
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{FixedVersion: "9.9.9"})

	// An empty repository path would be a registry error if it were read.
	report, action := DecideSecurityFailure(context.Background(), "", decideCallerDependency, decideCallerRun, SecurityFailureModeOff, true)
	if action != actionReportOnly {
		t.Fatalf("action = %q, want %q", action, actionReportOnly)
	}
	if report.Mode != SecurityFailureModeOff || report.Action != actionReportOnly {
		t.Fatalf("report mode/action = %q/%q, want off/report_only", report.Mode, report.Action)
	}
	if report.RegistryError != "" || report.RegistryFound {
		t.Fatalf("registry was consulted in off mode: found=%v error=%q", report.RegistryFound, report.RegistryError)
	}
	if spy.count() != 0 {
		t.Fatalf("advisory lookup ran %d time(s) in off mode, want 0", spy.count())
	}
	if report.Recommendation != recommendUnknown {
		t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendUnknown)
	}
	if !strings.Contains(report.Note, "off") {
		t.Fatalf("note %q does not say that handling is off", report.Note)
	}
}

// Shadow with a readable registry holding the advisory and no fixed version:
// the registry entry is found, the recommendation is keep, and the would-stop
// action is recorded with the Automaton line still untouched.
func TestDecideSecurityFailureShadowFindsRegistryEntry(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{PublishedDate: "2026-10-27"})

	report, action := DecideSecurityFailure(context.Background(), repo, SecurityFailureAssessment{
		Kind:        SecurityFailureDependency,
		Dependency:  "example.com/example-mod",
		AdvisoryIDs: []string{"GHSA-7p2w-4v3x-9q8m"},
	}, decideCallerRun, SecurityFailureModeShadow, true)

	if spy.count() != 1 || spy.gotIDs[0] != "GHSA-7p2w-4v3x-9q8m" {
		t.Fatalf("lookup calls = %d ids = %v, want one call for the first advisory", spy.count(), spy.gotIDs)
	}
	if !report.RegistryFound {
		t.Fatalf("registry entry not found, error %q", report.RegistryError)
	}
	if report.RegistryID != "osv/GHSA-7p2w-4v3x-9q8m" {
		t.Fatalf("registry id = %q", report.RegistryID)
	}
	if report.RegistryExpires != "2026-11-30" {
		t.Fatalf("registry expires = %q, want the entry's expiry", report.RegistryExpires)
	}
	if report.PublishedAt != "2026-10-27" {
		t.Fatalf("published_at = %q, want the lookup's date carried verbatim", report.PublishedAt)
	}
	if report.Recommendation != recommendKeep {
		t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendKeep)
	}
	if action != actionWouldStop || report.Action != actionWouldStop {
		t.Fatalf("action = %q/%q, want %q", action, report.Action, actionWouldStop)
	}
	if report.Mode != SecurityFailureModeShadow {
		t.Fatalf("mode = %q, want shadow", report.Mode)
	}
}

// A dependency with no advisory identifier never reaches the lookup, and a
// registry with no entry for the dependency reads as keep_no_entry.
func TestDecideSecurityFailureNoAdvisoryNoLookup(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{FixedVersion: "1.0.0"})

	report, action := DecideSecurityFailure(context.Background(), repo, SecurityFailureAssessment{
		Kind:       SecurityFailureDependency,
		Dependency: "example.com/unknown",
	}, decideCallerRun, SecurityFailureModeOn, true)

	if spy.count() != 0 {
		t.Fatalf("lookup ran %d time(s) with no advisory id, want 0", spy.count())
	}
	if report.RegistryFound || report.RegistryError != "" {
		t.Fatalf("registry: found=%v error=%q, want consulted with no entry", report.RegistryFound, report.RegistryError)
	}
	if report.Recommendation != recommendKeepNoEntry {
		t.Fatalf("recommendation = %q, want %q", report.Recommendation, recommendKeepNoEntry)
	}
	if action != actionStopped {
		t.Fatalf("on mode with a linked Automaton: action = %q, want %q", action, actionStopped)
	}
}

// A missing registry is carried as the registry error, and the lookup still
// answers: a known fixed version gives upgrade whatever the registry says.
func TestDecideSecurityFailureMissingRegistryStillUpgrades(t *testing.T) {
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{FixedVersion: "1.4.2"})

	report, action := DecideSecurityFailure(context.Background(), t.TempDir(), decideCallerDependency, decideCallerRun, SecurityFailureModeShadow, true)

	if report.RegistryError == "" {
		t.Fatal("a missing registry must be carried as registry_error")
	}
	if report.RegistryFound {
		t.Fatal("a missing registry cannot hold an entry")
	}
	if spy.count() != 1 {
		t.Fatalf("lookup calls = %d, want 1 (the registry failure does not skip the lookup)", spy.count())
	}
	if report.Recommendation != recommendUpgrade || report.FixedVersion != "1.4.2" {
		t.Fatalf("recommendation = %q fixed = %q, want upgrade to 1.4.2", report.Recommendation, report.FixedVersion)
	}
	if action != actionWouldStop {
		t.Fatalf("action = %q, want %q", action, actionWouldStop)
	}
}

// An empty repository path is refused by the registry reader; the caller
// carries that refusal and never reads the working directory.
func TestDecideSecurityFailureEmptyRepoPathRefused(t *testing.T) {
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{})

	report, _ := DecideSecurityFailure(context.Background(), "  ", decideCallerDependency, decideCallerRun, SecurityFailureModeShadow, true)
	if !strings.Contains(report.RegistryError, "repository path is required") {
		t.Fatalf("registry_error = %q, want the empty-path refusal", report.RegistryError)
	}
}

// A lookup failure is carried as the lookup error and the recommendation
// falls back to unknown: nothing is invented.
func TestDecideSecurityFailureLookupErrorIsUnknown(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{ErrText: "gh api advisories/GHSA-7p2w-4v3x-9q8m: network down"})

	report, _ := DecideSecurityFailure(context.Background(), repo, decideCallerDependency, decideCallerRun, SecurityFailureModeShadow, true)
	if !strings.Contains(report.LookupError, "network down") {
		t.Fatalf("lookup_error = %q, want the lookup failure carried", report.LookupError)
	}
	if report.FixedVersion != "" || report.PublishedAt != "" {
		t.Fatalf("lookup failed but fixed=%q published=%q were invented", report.FixedVersion, report.PublishedAt)
	}
}

// In on mode a run whose Automaton line does not carry the run's commit is
// report_only with a note that says so; it is never stopped.
func TestDecideSecurityFailureOnWithoutLinkedAutomatonIsReportOnly(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	var spy advisorySpy
	spy.install(t, SecurityFailureAdvisoryResult{FixedVersion: "2.0.0"})

	report, action := DecideSecurityFailure(context.Background(), repo, decideCallerDependency, decideCallerRun, SecurityFailureModeOn, false)
	if action != actionReportOnly || report.Action != actionReportOnly {
		t.Fatalf("action = %q/%q, want %q (no Automaton line carries the commit)", action, report.Action, actionReportOnly)
	}
	if report.Note == "" {
		t.Fatal("report_only without a linked Automaton must say why in note")
	}
	if report.Recommendation != recommendUpgrade {
		t.Fatalf("recommendation = %q, want upgrade: the report still carries the recommendation", report.Recommendation)
	}
}
