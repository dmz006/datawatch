package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

// scopeFixtureSpec shapes an operator's flattened feature request: one
// paragraph with inline headings in order — FEATURE, READ FIRST,
// SCOPE, CONFIGURABLE, PARITY, TESTS, DOCS and Constraints — so the
// scoped extractor has to pick out the two requested section bodies
// and nothing else.
const scopeFixtureSpec = `FEATURE (operator, 2026-10-30, quoted: "scoped extractor"; item: one): the scoped requirement extractor must extract obligations only from the sections the operator scoped. READ FIRST: the plan coverage gate under internal/autonomous (plan_coverage_gate.go and its tests) and docs/operations.md on the gate, plus the key autonomous.plan_coverage_min_requirement_chars that resolves the floor. SCOPE, EXACTLY: (1) scope must stay small. (2) loose path and config tokens must only count through the sentence that holds them. CONFIGURABLE (operator, 2026-11-01): every behaviour choice must be a configuration key resolved by the caller. PARITY (shared with the fill story, 2026-10-15): the quoted "lane-fill" semantics must stay unchanged for existing callers. TESTS: Go tests under internal/autonomous must cover the heading rule. DOCS: docs/howto/plan-coverage-scoping.md must document the new defaults. Constraints: repository-relative paths only and no new dependencies must be added to go.mod.`

func hasRequirement(reqs []string, want string) bool {
	key := coverageRequirementKey(want)
	for _, r := range reqs {
		if coverageRequirementKey(r) == key {
			return true
		}
	}
	return false
}

func TestPlanCoverageFilters_ScopeFixtureBodiesOnly(t *testing.T) {
	text, found := scopedSpecText(scopeFixtureSpec, []string{"scope", "constraints"})
	if !found {
		t.Fatalf("scopedSpecText found = false, want the two requested sections")
	}

	wantSubs := []string{
		"(1) scope must stay small",
		"loose path and config tokens must only count through the sentence that holds them",
		"repository-relative paths only and no new dependencies must be added to go.mod",
	}
	for _, w := range wantSubs {
		if !strings.Contains(text, w) {
			t.Errorf("scoped text lacks requested-section text %q; got: %s", w, text)
		}
	}

	noneOf := []string{
		"the sections the operator scoped",               // FEATURE body
		"plan_coverage_gate.go",                          // READ FIRST file name
		"docs/operations.md",                             // READ FIRST file name
		"autonomous.plan_coverage_min_requirement_chars", // READ FIRST key token
		"resolved by the caller",                         // CONFIGURABLE body
		`"lane-fill"`,                                    // PARITY's hyphenated quote
		"the heading rule",                               // TESTS body
		"plan-coverage-scoping.md",                       // DOCS file name
		"the new defaults",                               // DOCS body
	}
	for _, w := range noneOf {
		if strings.Contains(text, w) {
			t.Errorf("scoped text must not contain the %q text; got: %s", w, text)
		}
	}

	if got := strings.Count(text, "\n"); got != 1 {
		t.Errorf("scoped text has %d newlines, want exactly 1 joining the two bodies; got: %s", got, text)
	}
}

func TestPlanCoverageFilters_ScopeNoRecognisedHeadingFoundFalse(t *testing.T) {
	specs := []string{
		"Add the exporter flush and ship it by Friday.",
		"## Scope\nthe writer must flush the ledger before every run.", // label without a colon is not a heading
	}
	for _, spec := range specs {
		text, found := scopedSpecText(spec, []string{"scope"})
		if found {
			t.Errorf("scopedSpecText(%q) found = true, want false; text=%q", spec, text)
		}
		if text != "" {
			t.Errorf("scopedSpecText(%q) text = %q, want empty when no section is found", spec, text)
		}
	}
}

func TestPlanCoverageFilters_ScopeCommaQualifierAndParenthetical(t *testing.T) {
	spec := `**APPLIES TO (repo, 2026-11-01), EXACTLY:** the writer must flush the ledger before every run.`
	text, found := scopedSpecText(spec, []string{"applies"})
	if !found {
		t.Fatalf("heading with parenthetical and comma qualifier not recognised; text=%q", text)
	}
	if !strings.HasPrefix(text, "the writer must flush the ledger before every run") {
		t.Errorf("body = %q, want it to start with the sentence after the heading colon", text)
	}
}

func TestPlanCoverageFilters_ScopeSectionNamesCaseInsensitive(t *testing.T) {
	text, found := scopedSpecText(scopeFixtureSpec, []string{"SCOPE", "CONSTRAINTS"})
	if !found {
		t.Fatalf("sections matched case-insensitively should still be found")
	}
	if !strings.Contains(text, "scope must stay small") || !strings.Contains(text, "go.mod") {
		t.Errorf("both requested bodies should be present; got: %s", text)
	}

	// And the reverse direction: lowercase headings in the spec are
	// matched by uppercase requests.
	other := "scope: the importer must rotate credentials nightly."
	text, found = scopedSpecText(other, []string{"Scope"})
	if !found || text != "the importer must rotate credentials nightly." {
		t.Errorf("lowercase heading with %q: found=%v text=%q", "Scope", found, text)
	}
}

func TestPlanCoverageFilters_ScopeExcludesNonScopedTokens(t *testing.T) {
	reqs := ExtractRequirementsScoped(scopeFixtureSpec, []string{"scope", "constraints"}, 0)

	wantReqs := []string{
		"loose path and config tokens must only count through the sentence that holds them",
		"repository-relative paths only and no new dependencies must be added to go.mod.",
	}
	for _, w := range wantReqs {
		if !hasRequirement(reqs, w) {
			t.Errorf("scoped requirements lack %q; got: %v", w, reqs)
		}
	}

	none := []string{
		"internal/autonomous (plan_coverage_gate.go and its tests)",
		"plan_coverage_gate.go",
		"docs/operations.md",
		"autonomous.plan_coverage_min_requirement_chars",
		"lane-fill",
		"docs/howto/plan-coverage-scoping.md",
		"the heading rule",
	}
	for _, w := range none {
		for _, r := range reqs {
			if strings.Contains(strings.ToLower(r), strings.ToLower(w)) {
				t.Errorf("scoped requirements must not carry the non-scoped token %q; requirement: %q", w, r)
			}
		}
	}
}

func TestPlanCoverageFilters_ScopeMinCharsDropsShortKeepsLong(t *testing.T) {
	sections := []string{"scope", "constraints"}

	short := ExtractRequirementsScoped(scopeFixtureSpec, sections, 0)
	if !hasRequirement(short, "scope must stay small") {
		t.Errorf("minChars=0 should keep the short obligation sentence; got: %v", short)
	}

	capped := ExtractRequirementsScoped(scopeFixtureSpec, sections, 30)
	if hasRequirement(capped, "scope must stay small") {
		t.Errorf("minChars=30 should drop the short obligation sentence; got: %v", capped)
	}
	longWant := "loose path and config tokens must only count through the sentence that holds them"
	if !hasRequirement(capped, longWant) {
		t.Errorf("minChars=30 should keep the longer obligation sentence; got: %v", capped)
	}
}

func TestPlanCoverageFilters_ScopeUnscopedExtractionUnchanged(t *testing.T) {
	spec := strings.Join([]string{
		"(1) Exporter must flush metrics before exit",
		"(2) Add tests for the flush logic",
		"Config key planning_retry_count is required.",
	}, "\n")

	want := []string{
		"Exporter must flush metrics before exit",
		"Add tests for the flush logic",
		"Config key planning_retry_count is required.",
		"planning_retry_count",
	}
	got := ExtractRequirements(spec)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractRequirements on an unscoped spec:\n got  %v\n want %v", got, want)
	}
	if !reflect.DeepEqual(ExtractRequirements(spec), extractRequirementsFrom(spec, true)) {
		t.Fatalf("ExtractRequirements must be exactly extractRequirementsFrom(spec, true)")
	}
}
