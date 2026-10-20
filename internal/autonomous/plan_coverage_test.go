package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

// findUnmatched returns the verdict (and whether present) of a
// requirement (matched case-insensitively by its key form) inside a
// report's Unmatched list.
func findUnmatched(t *testing.T, report *PlanCoverageReport, wantRequirement string) (CoverageVerdict, bool) {
	t.Helper()
	want := coverageRequirementKey(wantRequirement)
	for _, u := range report.Unmatched {
		if coverageRequirementKey(u.Requirement) == want {
			return u.Verdict, true
		}
	}
	return "", false
}

func TestCheckPlanCoverageDroppedNumberedRequirementIsMissing(t *testing.T) {
	spec := "1. Implement retry logic for the http client\n2. Send a heartbeat message every 30 seconds"

	stories := []Story{
		{
			Title:       "Heartbeat",
			Description: "Send a heartbeat message every 30 seconds",
			Tasks: []Task{
				{Title: "Emit heartbeat", Spec: "Send a heartbeat message every 30 seconds"},
			},
		},
	}

	report := CheckPlanCoverage(spec, stories)

	got, found := findUnmatched(t, report, "Implement retry logic for the http client")
	if !found {
		t.Fatalf("dropped numbered requirement not in Unmatched list; report=%+v", report)
	}
	if got != CoverageMissing {
		t.Fatalf("dropped numbered requirement: got verdict %q, want missing", got)
	}
	if report.Missing != 1 {
		t.Errorf("Missing = %d, want 1 (report=%+v)", report.Missing, report)
	}
	if report.Covered != 1 {
		t.Errorf("Covered = %d, want 1 — the heartbeat requirement is fully in the plan (report=%+v)", report.Covered, report)
	}
	if report.Weakened != 0 {
		t.Errorf("Weakened = %d, want 0 (report=%+v)", report.Weakened, report)
	}
	if report.Missing+report.Weakened != len(report.Unmatched) {
		t.Errorf("Unmatched length %d, want it to equal Missing+Weakened = %d", len(report.Unmatched), report.Missing+report.Weakened)
	}
}

func TestCheckPlanCoverageFullCoverageHasZeroMissingAndUnmatched(t *testing.T) {
	spec := "The exporter must flush pending metrics on shutdown.\nIt should write the summary report to reports/daily.txt.\nAdd tests for the flush logic."

	stories := []Story{
		{
			Title:       "Exporter shutdown behavior",
			Description: "The exporter must flush pending metrics on shutdown, and the flush logic is covered by dedicated tests. Add tests for the flush logic.",
			Tasks: []Task{
				{
					Title: "Daily report writer",
					Spec:  "On shutdown the writer should write the summary report to reports/daily.txt.",
				},
			},
		},
	}

	report := CheckPlanCoverage(spec, stories)

	if report.Missing != 0 {
		t.Errorf("Missing = %d, want 0; unmatched=%+v", report.Missing, report.Unmatched)
	}
	if report.Weakened != 0 {
		t.Errorf("Weakened = %d, want 0; unmatched=%+v", report.Weakened, report.Unmatched)
	}
	if want := len(ExtractRequirements(spec)); report.Covered != want {
		t.Errorf("Covered = %d, want every extracted requirement covered (%d)", report.Covered, want)
	}
	if want := len(ExtractRequirements(spec)); want == 0 {
		t.Fatalf("test is broken: spec produced no extractable requirements")
	}
	if len(report.Unmatched) != 0 {
		t.Errorf("Unmatched = %+v, want empty", report.Unmatched)
	}
}

func TestCheckPlanCoverageEmptySpecYieldsZeroCountReport(t *testing.T) {
	spec := "please review the release notes"

	if reqs := ExtractRequirements(spec); len(reqs) != 0 {
		t.Fatalf("ExtractRequirements(%q) = %v, want no extractable requirements", spec, reqs)
	}

	report := CheckPlanCoverage(spec, []Story{{Title: "Notes", Tasks: []Task{{Title: "Review", Spec: "Read the release notes"}}}})
	if report == nil {
		t.Fatal("CheckPlanCoverage returned nil report; it must not fail on a requirement-free spec")
	}
	if report.Covered != 0 || report.Missing != 0 || report.Weakened != 0 {
		t.Errorf("report = %+v, want all counts zero for a spec with no extractable requirements", report)
	}
	if len(report.Unmatched) != 0 {
		t.Errorf("Unmatched = %+v, want empty", report.Unmatched)
	}

	// The zero-count path also holds for an entirely empty spec.
	empty := CheckPlanCoverage("", nil)
	if empty.Covered != 0 || empty.Missing != 0 || empty.Weakened != 0 || len(empty.Unmatched) != 0 {
		t.Errorf("empty spec report = %+v, want all counts zero", empty)
	}
}

func TestCheckPlanCoverageNegatedTermIsWeakened(t *testing.T) {
	spec := "The exporter must flush pending metrics on shutdown."

	stories := []Story{
		{
			Title:       "Exporter shutdown",
			Description: "The exporter does not flush pending metrics on shutdown; this is deferred.",
		},
	}

	report := CheckPlanCoverage(spec, stories)

	got, found := findUnmatched(t, report, "The exporter must flush pending metrics on shutdown")
	if !found {
		t.Fatalf("negated requirement not in Unmatched list; report=%+v", report)
	}
	if got != CoverageWeakened {
		t.Fatalf("negated requirement: got verdict %q, want weakened (present but negated)", got)
	}
	if report.Weakened != 1 || report.Missing != 0 || report.Covered != 0 {
		t.Errorf("report = %+v, want Weakened=1 and nothing else", report)
	}
}

func TestExtractRequirementsOrderAndDedup(t *testing.T) {
	spec := strings.Join([]string{
		"(1) Exporter must flush metrics before exit",
		"(2) Add tests for the flush logic",
		"Config key planning_retry_count is required.",
		"",
		"Exporter must flush metrics before exit", // duplicate of (1), should not resurface
	}, "\n")

	got := ExtractRequirements(spec)

	want := []string{
		"Exporter must flush metrics before exit",
		"Add tests for the flush logic",
		"Config key planning_retry_count is required.",
		"planning_retry_count",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractRequirements:\n got  %v\n want %v", got, want)
	}
}

func TestCheckPlanCoverageIsDeterministic(t *testing.T) {
	spec := strings.Join([]string{
		"1. The importer must validate checksums before commit",
		"2. Support the config key retry_backoff_ms",
		"Add tests for the checksum validation.",
	}, "\n")
	stories := []Story{
		{
			Title: "Import validation",
			Tasks: []Task{
				{
					Title: "Checksum gate",
					Spec:  "The importer must validate checksums before commit, and honor retry_backoff_ms.",
				},
			},
		},
	}

	first := CheckPlanCoverage(spec, stories)
	second := CheckPlanCoverage(spec, stories)

	if first.Missing+first.Weakened != len(first.Unmatched) || second.Missing+second.Weakened != len(second.Unmatched) {
		t.Fatalf("report invariants violated: first=%+v second=%+v", first, second)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same spec+plan produced different reports:\nfirst  %+v\nsecond %+v", first, second)
	}
}
