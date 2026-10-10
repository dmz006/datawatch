package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/git"
)

func TestExtractMarkdownSection(t *testing.T) {
	doc := `# Plan: Example

## Context

Some context.

## Parity surface

| Surface | Status |
|---|---|
| REST | touched |
| PWA | touched, Android N/A (no mobile UI for this) |

## Phases

### Phase 0
stuff`

	got := extractMarkdownSection(doc, "Parity surface")
	if got == "" {
		t.Fatal("expected non-empty section")
	}
	for _, want := range []string{"REST", "PWA", "Android"} {
		if !strings.Contains(got, want) {
			t.Fatalf("extracted section missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Phase 0") {
		t.Fatalf("extraction ran past the next ## heading: %q", got)
	}
}

func TestExtractMarkdownSection_SectionAtEndOfDoc(t *testing.T) {
	doc := "# Plan\n\n## Parity surface\n\nREST only.\n"
	got := extractMarkdownSection(doc, "Parity surface")
	if !strings.Contains(got, "REST") {
		t.Fatalf("expected REST in section at end of doc, got %q", got)
	}
}

func TestExtractMarkdownSection_NoMatch(t *testing.T) {
	doc := "# Plan\n\n## Context\n\nNo parity section here.\n"
	if got := extractMarkdownSection(doc, "Parity surface"); got != "" {
		t.Fatalf("expected empty string when heading absent, got %q", got)
	}
}

func TestExtractMarkdownSection_SubheadingsDoNotTerminate(t *testing.T) {
	doc := "## Parity surface\n\nREST\n\n### a sub-point\n\nPWA\n\n## Next section\n\nnope"
	got := extractMarkdownSection(doc, "Parity surface")
	if !strings.Contains(got, "REST") || !strings.Contains(got, "PWA") {
		t.Fatalf("### subheading should not terminate the section: %q", got)
	}
	if strings.Contains(got, "nope") {
		t.Fatalf("## Next section should terminate it: %q", got)
	}
}

func TestBuildPRDParityContext(t *testing.T) {
	prd := &PRD{
		Spec: "## Parity surface\n\nREST, MCP, PWA.\n\n## Phases\nstuff",
		Story: []Story{
			{ID: "s1", Title: "Story One", Description: "Touches REST and MCP."},
		},
	}
	ctx := buildPRDParityContext(prd)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if !strings.Contains(ctx.ParentSurfaceText, "REST") {
		t.Fatalf("parent surface text missing REST: %q", ctx.ParentSurfaceText)
	}
	if len(ctx.Stories) != 1 || ctx.Stories[0].ID != "s1" || ctx.Stories[0].Description != "Touches REST and MCP." {
		t.Fatalf("story translation wrong: %+v", ctx.Stories)
	}
}

func TestBuildPRDParityContext_Nil(t *testing.T) {
	if buildPRDParityContext(nil) != nil {
		t.Fatal("expected nil context for nil PRD")
	}
}

// --- BL406 Phase 3 — multi-granularity wiring ---

func newGranularityTestManager(t *testing.T, dir string) (*Manager, *PRD) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Scan.ProjectRules = []scan.ProjectRule{
		{ID: "task-rule", Name: "task-level check", Type: scan.RuleTypePresence,
			Granularity: scan.GranularityTask, Severity: scan.SeverityError, Pattern: "nonexistent-task.md"},
		{ID: "story-rule", Name: "story-level check", Type: scan.RuleTypePresence,
			Granularity: scan.GranularityStory, Severity: scan.SeverityError, Pattern: "nonexistent-story.md"},
		{ID: "prd-rule", Name: "prd-level check", Type: scan.RuleTypePresence,
			Granularity: scan.GranularityPRD, Severity: scan.SeverityError, Pattern: "nonexistent-prd.md"},
	}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "prd1", ProjectDir: dir, Spec: "## Parity surface\n\nREST only.\n",
		Story: []Story{{ID: "s1", Title: "Story One", Description: "Touches REST."}}}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return m, prd
}

func TestInvokeScanGuardrail_ProjectRules_TaskGranularity_OnlyFiresTaskRule(t *testing.T) {
	dir := t.TempDir()
	m, prd := newGranularityTestManager(t, dir)
	entry := GuardrailEntry{Name: "project-rules-scan", Type: "scan", ScanType: "project-rules"}
	inv := GuardrailInvocation{PRDID: prd.ID, Level: "task", UnitID: "s1t1", ProjectDir: dir}

	v, err := m.invokeScanGuardrail(entry, inv)
	if err != nil {
		t.Fatal(err)
	}
	if v.Outcome != "block" {
		t.Fatalf("expected the task-granularity rule to fire (block), got outcome=%q summary=%q", v.Outcome, v.Summary)
	}
}

func TestInvokeScanGuardrail_ProjectRules_StoryGranularity_OnlyFiresStoryRule(t *testing.T) {
	dir := t.TempDir()
	m, prd := newGranularityTestManager(t, dir)
	entry := GuardrailEntry{Name: "project-rules-scan", Type: "scan", ScanType: "project-rules"}
	inv := GuardrailInvocation{PRDID: prd.ID, Level: "story", UnitID: "s1", ProjectDir: dir}

	v, err := m.invokeScanGuardrail(entry, inv)
	if err != nil {
		t.Fatal(err)
	}
	if v.Outcome != "block" {
		t.Fatalf("expected the story-granularity rule to fire (block), got outcome=%q summary=%q", v.Outcome, v.Summary)
	}
}

func TestInvokeScanGuardrail_ProjectRules_TaskLevel_DoesNotFireStoryOrPRDRule(t *testing.T) {
	// Only the task-rule's own pattern ("nonexistent-task.md") should be
	// checked at task granularity; story/prd rules target different
	// (also-missing) filenames, so if they fired too the finding count
	// (and thus the block reason) would differ. invokeScanGuardrail only
	// returns a verdict, not the raw finding count, so assert indirectly:
	// create the task-rule's target file so ONLY it would pass, and
	// confirm the outcome flips to pass -- proving no other granularity's
	// (still-missing) rule is contributing a finding.
	dir := t.TempDir()
	m, prd := newGranularityTestManager(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "nonexistent-task.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := GuardrailEntry{Name: "project-rules-scan", Type: "scan", ScanType: "project-rules"}
	inv := GuardrailInvocation{PRDID: prd.ID, Level: "task", UnitID: "s1t1", ProjectDir: dir}

	v, err := m.invokeScanGuardrail(entry, inv)
	if err != nil {
		t.Fatal(err)
	}
	if v.Outcome != "pass" {
		t.Fatalf("expected pass once the task-rule's own target exists (proving story/prd rules aren't also being checked at task granularity), got outcome=%q summary=%q", v.Outcome, v.Summary)
	}
}

func TestRunPRDCompletionProjectRulesCheck_PRDGranularity_Blocks(t *testing.T) {
	dir := t.TempDir()
	m, prd := newGranularityTestManager(t, dir)

	blocked, err := m.runPRDCompletionProjectRulesCheck(prd)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("expected the prd-granularity rule's missing file to block PRD completion")
	}
	if len(prd.Decisions) != 1 || prd.Decisions[0].Kind != "project_rules_block" {
		t.Fatalf("expected a project_rules_block decision recorded, got %+v", prd.Decisions)
	}

	// Satisfy it and confirm it stops blocking.
	if err := os.WriteFile(filepath.Join(dir, "nonexistent-prd.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked, err = m.runPRDCompletionProjectRulesCheck(prd)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected PRD completion to no longer be blocked once the prd-rule's target exists")
	}
}

func TestRunPRDCompletionProjectRulesCheck_NoOpWhenNoPRDGranularityRules(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Scan.ProjectRules = []scan.ProjectRule{
		{ID: "task-only", Type: scan.RuleTypePresence, Granularity: scan.GranularityTask, Pattern: "nope.md"},
	}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "prd2", ProjectDir: dir}
	blocked, err := m.runPRDCompletionProjectRulesCheck(prd)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected no-op (not blocked) when no rule is declared at prd_complete granularity")
	}
}

// --- BL406 Phase 4 — upstream issue-filing action ---

type fakeIssueProvider struct {
	git.GitLab // embed the stub for every method this test doesn't care about
	calls      []git.IssueOptions
}

func (f *fakeIssueProvider) CreateIssue(_ context.Context, opts git.IssueOptions) (string, error) {
	f.calls = append(f.calls, opts)
	return "https://github.com/" + opts.Repo + "/issues/1", nil
}

func TestFireUpstreamIssueActions_CallsCreateIssueWithRightRepoTitleBody(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeIssueProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }
	m.cfg.UpstreamRepos = []scan.UpstreamRepo{{Name: "app", OwnerRepo: "dmz006/datawatch-app"}}

	rule := scan.ProjectRule{ID: "r1", Name: "parity gap", Action: scan.ActionFileUpstreamIssue, UpstreamTarget: "app"}
	finding := scan.Finding{Scanner: "project-rules", RuleID: "r1", Message: "story X dropped the Android surface"}

	m.fireUpstreamIssueActions([]scan.ProjectRule{rule}, []scan.Finding{finding})

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 CreateIssue call, got %d", len(fake.calls))
	}
	got := fake.calls[0]
	if got.Repo != "dmz006/datawatch-app" {
		t.Errorf("Repo = %q, want dmz006/datawatch-app", got.Repo)
	}
	if got.Title != "project rule: parity gap" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Body != "story X dropped the Android surface" {
		t.Errorf("Body = %q", got.Body)
	}
}

func TestFireUpstreamIssueActions_SkipsRulesWithoutTheAction(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeIssueProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	rule := scan.ProjectRule{ID: "r1", Name: "no action here"} // Action left empty
	finding := scan.Finding{Scanner: "project-rules", RuleID: "r1", Message: "x"}
	m.fireUpstreamIssueActions([]scan.ProjectRule{rule}, []scan.Finding{finding})

	if len(fake.calls) != 0 {
		t.Fatalf("expected no CreateIssue calls for a rule with no file_upstream_issue action, got %d", len(fake.calls))
	}
}

func TestFireUpstreamIssueActions_UnknownTargetLogsAndSkips(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeIssueProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }
	// No upstream_repos configured at all.

	rule := scan.ProjectRule{ID: "r1", Name: "x", Action: scan.ActionFileUpstreamIssue, UpstreamTarget: "nonexistent"}
	finding := scan.Finding{Scanner: "project-rules", RuleID: "r1", Message: "x"}
	m.fireUpstreamIssueActions([]scan.ProjectRule{rule}, []scan.Finding{finding})

	if len(fake.calls) != 0 {
		t.Fatalf("expected no CreateIssue call when UpstreamTarget doesn't resolve, got %d", len(fake.calls))
	}
}

func TestFireUpstreamIssueActions_OnlyOnePerRuleAcrossMultipleFindings(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeIssueProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }
	m.cfg.UpstreamRepos = []scan.UpstreamRepo{{Name: "app", OwnerRepo: "dmz006/datawatch-app"}}

	rule := scan.ProjectRule{ID: "r1", Name: "x", Action: scan.ActionFileUpstreamIssue, UpstreamTarget: "app"}
	findings := []scan.Finding{
		{Scanner: "project-rules", RuleID: "r1", Message: "first"},
		{Scanner: "project-rules", RuleID: "r1", Message: "second"},
	}
	m.fireUpstreamIssueActions([]scan.ProjectRule{rule}, findings)

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 CreateIssue call for 2 findings on the same rule, got %d", len(fake.calls))
	}
}
