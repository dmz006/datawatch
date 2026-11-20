package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateAllowlistStories returns one story whose task names a file in its
// spec that no file list carries: the gate allowlist must still cover
// it. The spec also carries a URL, a shell-substituted token and a
// trailing period to pin that only the literal path is extracted.
func gateAllowlistStories() []Story {
	return []Story{{
		Title: "Engine story",
		Tasks: []Task{{
			Title: "Add the engine",
			Spec:  "Ship internal/core/engine.go. See https://example.com/engine and ${ENGINE_DIR}/notes.md for context, then register it.",
		}},
	}}
}

func TestGateAllowlistSpecNamedFileRejected(t *testing.T) {
	script := "#!/bin/bash\nALLOWED=\"internal/core/util.go scripts/final-gate.sh\"\n"
	f := preflightAllowedFilesFindings("", "scripts/final-gate.sh", []byte(script), gateAllowlistStories())
	if len(f) != 1 {
		t.Fatalf("findings = %+v, want one for the spec-named file", f)
	}
	if f[0].RuleID != RuleVerifyScriptAllowedFiles || f[0].Line != 0 || f[0].Script != "scripts/final-gate.sh" {
		t.Fatalf("finding = %+v", f[0])
	}
	if !strings.Contains(f[0].Message, "internal/core/engine.go") {
		t.Fatalf("message %q must name the spec-named path", f[0].Message)
	}
	if !strings.Contains(f[0].Message, "the spec of task Add the engine") {
		t.Fatalf("message %q must name the spec of the naming task", f[0].Message)
	}
}

func TestGateAllowlistSpecNamedFileAllowed(t *testing.T) {
	script := "#!/bin/bash\nALLOWED=\"internal/core/engine.go internal/core/util.go scripts/final-gate.sh\"\n"
	if f := preflightAllowedFilesFindings("", "scripts/final-gate.sh", []byte(script), gateAllowlistStories()); len(f) != 0 {
		t.Fatalf("findings = %+v, want none", f)
	}
}

// TestGateAllowlistSiblingTestRequired pins the sibling _test.go
// requirement: a .go change that exists in the tree and already carries
// a test file must keep that test file in the allowed set.
func TestGateAllowlistSiblingTestRequired(t *testing.T) {
	dir := t.TempDir()
	mustGateWrite(t, dir, "internal/core/engine.go", "package core\n")
	mustGateWrite(t, dir, "internal/core/engine_test.go", "package core\n")
	stories := []Story{{
		Title: "Engine story",
		Tasks: []Task{{Title: "Add the engine", FilesPlanned: []string{"internal/core/engine.go"}}},
	}}
	// Omitting the sibling test file yields a finding naming it.
	script := "#!/bin/bash\nALLOWED=\"internal/core/engine.go scripts/final-gate.sh\"\n"
	f := preflightAllowedFilesFindings(dir, "scripts/final-gate.sh", []byte(script), stories)
	if len(f) != 1 {
		t.Fatalf("findings = %+v, want one for the sibling test file", f)
	}
	if !strings.Contains(f[0].Message, "internal/core/engine_test.go") {
		t.Fatalf("message %q must name the sibling test file", f[0].Message)
	}
	// Allowing the sibling test file yields none.
	script = "#!/bin/bash\nALLOWED=\"internal/core/engine.go internal/core/engine_test.go scripts/final-gate.sh\"\n"
	if f := preflightAllowedFilesFindings(dir, "scripts/final-gate.sh", []byte(script), stories); len(f) != 0 {
		t.Fatalf("findings = %+v, want none", f)
	}
	// A .go file with no sibling test file on disk needs no test entry.
	mustGateWrite(t, dir, "internal/core/lonely.go", "package core\n")
	stories = []Story{{
		Title: "Engine story",
		Tasks: []Task{{Title: "Add the lonely", FilesPlanned: []string{"internal/core/lonely.go"}}},
	}}
	script = "#!/bin/bash\nALLOWED=\"internal/core/lonely.go scripts/final-gate.sh\"\n"
	if f := preflightAllowedFilesFindings(dir, "scripts/final-gate.sh", []byte(script), stories); len(f) != 0 {
		t.Fatalf("findings = %+v, want none for a Go file without a sibling test", f)
	}
	// The sibling requirement also applies to a spec-named .go file.
	stories = []Story{{
		Title: "Engine story",
		Tasks: []Task{{Title: "Add the engine", Spec: "Ship internal/core/engine.go."}},
	}}
	script = "#!/bin/bash\nALLOWED=\"internal/core/engine.go scripts/final-gate.sh\"\n"
	f = preflightAllowedFilesFindings(dir, "scripts/final-gate.sh", []byte(script), stories)
	if len(f) != 1 || !strings.Contains(f[0].Message, "internal/core/engine_test.go") {
		t.Fatalf("findings = %+v, want the sibling test of the spec-named file", f)
	}
}

// TestGateAllowlistGeneratedAndGlobAccepted pins that the standard
// generated files (changelog, parity-issues folder) are always treated
// as covered — a folder exemption covers everything under it — and that
// glob entries cover the doc pages they match.
func TestGateAllowlistGeneratedAndGlobAccepted(t *testing.T) {
	stories := []Story{{
		Title: "Widget story",
		FilesPlanned: []string{
			"internal/widget/widget.go",
			"docs/howto/widget.md",
			"CHANGELOG.md",
			"docs/plans/parity-issues/widget-parity-draft.md",
		},
		Tasks: []Task{{Title: "Add the widget", FilesPlanned: []string{"internal/widget/widget.go"}}},
	}}
	// The script allows the doc page by glob and the generated files by
	// folder prefix, and omits nothing else; it must yield no finding.
	script := "#!/bin/bash\nALLOWED=\"internal/widget/ docs/howto/*.md docs/plans/parity-issues/ scripts/verify.sh\"\n"
	if f := preflightAllowedFilesFindings("", "scripts/verify.sh", []byte(script), stories); len(f) != 0 {
		t.Fatalf("findings = %+v, want none", f)
	}
	// Generated files are never reported as missing even when the script
	// names no generated path at all (not even the folder).
	stories2 := []Story{{
		Title:        "Widget story",
		FilesPlanned: []string{"internal/widget/widget.go", "CHANGELOG.md"},
		Tasks:        []Task{{Title: "Add the widget", FilesPlanned: []string{"internal/widget/widget.go"}}},
	}}
	script = "#!/bin/bash\nALLOWED=\"internal/widget/ scripts/verify.sh\"\n"
	if f := preflightAllowedFilesFindings("", "scripts/verify.sh", []byte(script), stories2); len(f) != 0 {
		t.Fatalf("findings = %+v, want none (generated files are always covered)", f)
	}
}

// TestGateAllowlistGlobTokens pins that glob tokens survive the literal
// token extraction and that a directory glob covers nested pages.
func TestGateAllowlistGlobTokens(t *testing.T) {
	tok := preflightLiteralPathTokens([]byte("ALLOWED=\"docs/* pkg/*.go *.js\"\n"))
	want := map[string]bool{"docs/*": true, "pkg/*.go": true, "*.js": true}
	if len(tok) != len(want) {
		t.Fatalf("tokens = %v, want %v", tok, want)
	}
	for _, p := range []string{"docs/plans/page.md", "pkg/widget.go", "bundle.js"} {
		if !preflightPathCovered(p, tok) {
			t.Fatalf("token %v must cover %q", tok, p)
		}
	}
	if preflightPathCovered("lib/bundle.js", tok) {
		t.Fatalf("*.js must not cover lib/bundle.js (star does not cross a slash)")
	}
}

// TestGateAllowlistSpecPathTokens pins what a spec text contributes to
// the allowlist: literal repo-relative paths in, URLs, absolute paths,
// shell-substituted tokens and bare words out.
func TestGateAllowlistSpecPathTokens(t *testing.T) {
	spec := "Update internal/core/engine.go and pkg/util.js. " +
		"See https://example.com/docs/page.md, /etc/hosts, ~/bin/tool, ${DIR}/notes.md and main-branch for context."
	got := specPathTokens(spec)
	want := []string{"internal/core/engine.go", "pkg/util.js"}
	if len(got) != len(want) {
		t.Fatalf("specPathTokens = %v, want %v", got, want)
	}
	for i, p := range want {
		if got[i] != p {
			t.Fatalf("specPathTokens = %v, want %v", got, want)
		}
	}
}

// mustGateWrite writes content to dir/name, creating parents.
func mustGateWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
