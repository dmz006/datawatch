// decompose_lint_artefact_test.go — the artefact_files_allowed rule: a
// verification script that does a changed-file-set check must exempt the
// generated artefacts (changelog draft, parity draft, gate report) the
// daemon writes per Automaton, or the final gate's cumulative re-run can
// only fail. Like the other verify-script stability tests the scripts
// live in a temp project directory and the engine reads them; no git,
// no daemon.
package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDecomposeLintArtefactFilesAllowed(t *testing.T) {
	rule := verifyStableRule(t, "artefact_files_allowed")
	if rule.Severity != config.DecomposeLintSeverityBlock {
		t.Fatalf("artefact_files_allowed severity = %q, want block", rule.Severity)
	}
	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/fileset.sh", "#!/bin/sh\ngit diff --name-only HEAD | grep -qx a.go\n")
	verifyStableWrite(t, dir, "v/exempt.sh", "#!/bin/sh\ngit diff --name-only HEAD | grep -vE 'docs/plans/(drafts|parity-issues|reports)|CHANGELOG' | grep -qx a.go\n")
	verifyStableWrite(t, dir, "v/nofileset.sh", "#!/bin/sh\ngo test ./...\n")

	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"file-set check without the artefact exemptions fails", "v/fileset.sh", 1},
		{"the generated-artefact exemptions present passes", "v/exempt.sh", 0},
		{"a script without a file-set check passes", "v/nofileset.sh", 0},
		{"a script file that does not exist is skipped", "v/missing.sh", 0},
	} {
		got := verifyStableLint(dir, c.name, rule, Task{ID: "t", VerificationScript: c.script})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
			continue
		}
		if c.want == 1 {
			if got[0].RuleID != "artefact_files_allowed" || got[0].Severity != config.DecomposeLintSeverityBlock {
				t.Errorf("%s: finding %+v, want a block finding for artefact_files_allowed", c.name, got[0])
			}
			if got[0].Script != "v/fileset.sh" {
				t.Errorf("%s: finding script path = %q, want v/fileset.sh", c.name, got[0].Script)
			}
		}
	}
}
