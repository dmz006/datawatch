package platformcontext

import (
	"strings"
	"testing"
)

const (
	ctxBlock = `<!-- datawatch:platform-context v1 -->
This session runs on datawatch.
- Sessions: running AI coding sessions.
<!-- /datawatch:platform-context -->`

	rtkBlock = `<!-- rtk-instructions -->
# RTK (Rust Token Killer)

Always prefix commands with rtk.
<!-- /rtk-instructions -->`

	crossBlock = `<!-- cross-session-communication-rule -->
# Cross-Session Communication Rule (datawatch)

Use datawatch's own tools for durable facts.
<!-- /cross-session-communication-rule -->`

	memorySection = `# Memory & Knowledge (datawatch)

Use memory_recall to check similar work.

| Tool | Purpose |
|------|---------|
| memory_recall | Search |`

	guardrailsSection = `# Session Guardrails

Session: a6050001 | Task: add helper

## Constraints
- Work only within: /path/to/project
- When done: output DATAWATCH_COMPLETE`
)

func TestStripDaemonSections_PlatformContextAlone(t *testing.T) {
	in := "# Rules\n\n" + ctxBlock + "\n\nsuffix\n"
	want := "# Rules\n\nsuffix"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_RTKAlone(t *testing.T) {
	in := "# Rules\n\n" + rtkBlock + "\nsuffix\n"
	want := "# Rules\n\nsuffix"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_CrossSessionAlone(t *testing.T) {
	in := "# Rules\n\n" + crossBlock + "\nsuffix\n"
	want := "# Rules\n\nsuffix"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_MemoryAlone(t *testing.T) {
	// Memory section runs to the next "# " line; here the next such line is
	// the guardrails header, so both sections are removed in one file.
	in := "# Rules\n\noperator note\n\n" + memorySection + "\n\n" + guardrailsSection + "\n"
	want := "# Rules\n\noperator note"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_MemoryRunsToNextHashSpace(t *testing.T) {
	// A "# " line after the memory section terminates it.
	in := "# Rules\n\n" + memorySection + "\n\n# Later Header\n\nkept\n"
	want := "# Rules\n\n# Later Header\n\nkept"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_GuardrailsAlone(t *testing.T) {
	// Runs to the next "# " line; "## Constraints" does not terminate it.
	in := "# Rules\n\n" + guardrailsSection + "\n\n# Later Header\n\nkept\n"
	want := "# Rules\n\n# Later Header\n\nkept"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_AllSectionsTogether(t *testing.T) {
	in := strings.Join([]string{
		"# Task",
		"",
		"operator written intro",
		"",
		ctxBlock,
		"",
		"## Real content",
		"",
		"operator detail line",
		"",
		rtkBlock,
		"",
		crossBlock,
		"",
		memorySection,
		"",
		guardrailsSection,
		"",
	}, "\n")
	want := "# Task\n\noperator written intro\n\n## Real content\n\noperator detail line"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_NoSectionsUnchanged(t *testing.T) {
	in := "# Task\n\nJust operator prose.\n\n## Notes\n\n- one\n- two"
	if got := StripDaemonSections(in); got != in {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestStripDaemonSections_UserEditOutsidePreserved(t *testing.T) {
	in := strings.Join([]string{
		"# Task",
		"",
		"operator edited this line",
		"- operator list item",
		"",
		ctxBlock,
		"",
		"operator edited this too",
		"",
		rtkBlock,
	}, "\n")
	want := "# Task\n\noperator edited this line\n- operator list item\n\noperator edited this too"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_WithAndWithoutSectionEqual(t *testing.T) {
	with := "# Rules\n\noperator note\n\n" + rtkBlock + "\n\nlast line\n"
	without := "# Rules\n\noperator note\n\nlast line\n"
	gotWith := StripDaemonSections(with)
	gotWithout := StripDaemonSections(without)
	if gotWith != gotWithout {
		t.Fatalf("with %q != without %q", gotWith, gotWithout)
	}
	if gotWith != "# Rules\n\noperator note\n\nlast line" {
		t.Fatalf("got %q", gotWith)
	}
}

func TestStripDaemonSections_UnterminatedMarkerDropped(t *testing.T) {
	in := "# Rules\n\nstart\n<!-- rtk-instructions -->\nx\n"
	// No end marker: everything from the begin marker on is dropped.
	want := "# Rules\n\nstart"
	if got := StripDaemonSections(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripDaemonSections_Idempotent(t *testing.T) {
	in := strings.Join([]string{
		"# Task",
		"",
		"intro",
		"",
		ctxBlock,
		"",
		rtkBlock,
		"",
		crossBlock,
		"",
		memorySection,
		"",
		guardrailsSection,
		"",
	}, "\n")
	once := StripDaemonSections(in)
	twice := StripDaemonSections(once)
	if once != twice {
		t.Fatalf("not idempotent:\n once=%q\n twice=%q", once, twice)
	}
}
