// decompose_lint_cumulative_test.go — the final_gate_artefact_allowed
// rule: a verification script that enforces the whole changed-file set
// against a finite list (a git diff/status/ls-files listing) and
// exempts the generated artefacts, but carries no FINAL_GATE /
// DW_FINAL_GATE sentinel to skip the check when the final gate re-runs
// it on the cumulative tree. Like the other verify-script stability
// tests the scripts live in a temp project directory and the engine
// reads them; no git, no daemon.
package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDecomposeLintFinalGateArtefactAllowed(t *testing.T) {
	rule := verifyStableRule(t, "final_gate_artefact_allowed")
	if rule.Severity != config.DecomposeLintSeverityBlock {
		t.Fatalf("final_gate_artefact_allowed severity = %q, want block", rule.Severity)
	}

	fileSetBody := "base=$(git merge-base HEAD main)\n" +
		"git diff --name-only \"$base\" | grep -vE 'docs/plans/(drafts|parity-issues|reports)|CHANGELOG' | while read -r f; do\n" +
		"  case \" $allowed \" in *\" $f \"*) ;; *) echo \"unexpected changed file: $f\"; exit 1;; esac\n" +
		"done\n"
	allowed := "allowed='internal/a.go internal/b.go'\n"

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/cumulative.sh", "#!/bin/sh\nset -eu\n"+allowed+fileSetBody)
	verifyStableWrite(t, dir, "v/guarded.sh", "#!/bin/sh\nset -eu\n"+
		"if [ -n \"${DW_FINAL_GATE:-}\" ]; then\n  exit 0\nfi\n"+allowed+fileSetBody)
	verifyStableWrite(t, dir, "v/ownonly.sh", "#!/bin/sh\nset -eu\ntest -s internal/a.go\ntest -s internal/b.go\ngo test ./internal/...\n")
	verifyStableWrite(t, dir, "v/nofileset.sh", "#!/bin/sh\nset -eu\ngo build ./...\ngo test ./...\n")

	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"whole merge-base listing against a finite list with no sentinel fails", "v/cumulative.sh", 1},
		{"the same check guarded by a DW_FINAL_GATE early return passes", "v/guarded.sh", 0},
		{"a script asserting only its own new paths (no whole-listing walk) passes", "v/ownonly.sh", 0},
		{"a script with no file-set assertion passes", "v/nofileset.sh", 0},
		{"a script file that does not exist is skipped", "v/missing.sh", 0},
	} {
		got := verifyStableLint(dir, c.name, rule, Task{ID: "t", VerificationScript: c.script})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
			continue
		}
		if c.want == 1 {
			if got[0].RuleID != "final_gate_artefact_allowed" || got[0].Severity != config.DecomposeLintSeverityBlock {
				t.Errorf("%s: finding %+v, want a block finding for final_gate_artefact_allowed", c.name, got[0])
			}
			if got[0].Script != "v/cumulative.sh" {
				t.Errorf("%s: finding script path = %q, want v/cumulative.sh", c.name, got[0].Script)
			}
		}
	}
}
