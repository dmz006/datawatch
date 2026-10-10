package autonomous

import (
	"strings"
	"testing"
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
