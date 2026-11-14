package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// Fixtures for the five script-content rules (verify_script_rejects_own_files,
// verify_script_needs_later_task_output, verify_script_sh_for_bash,
// verify_script_pipe_grep_q, verify_script_hex_pattern). Each failing script
// is the shape of a verification script that failed a task this week; the
// corrected script of the same job must not fire.

func s3sWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// s3sRule returns the default-pack rule id, asserting it blocks.
func s3sRule(t *testing.T, id string) Rule {
	t.Helper()
	for _, r := range DefaultDecomposeLintRules() {
		if r.ID == id {
			if r.Severity != config.DecomposeLintSeverityBlock {
				t.Fatalf("%s severity = %q, want block", id, r.Severity)
			}
			return r
		}
	}
	t.Fatalf("rule %s missing from the default pack", id)
	return Rule{}
}

// s3sLint runs one rule over a plan and returns its findings.
func s3sLint(dir string, rule Rule, stories ...Story) []LintFinding {
	return LintTasksIn(dir, stories, []Rule{rule})
}

func s3sOne(task Task) Story { return Story{ID: "s1", Tasks: []Task{task}} }

func TestStability3Script_PackShape(t *testing.T) {
	ids := []string{
		"verify_script_rejects_own_files", "verify_script_needs_later_task_output",
		"verify_script_sh_for_bash", "verify_script_pipe_grep_q", "verify_script_hex_pattern",
	}
	for _, id := range ids {
		s3sRule(t, id)
	}
	block, warn := 0, 0
	for _, r := range DefaultDecomposeLintRules() {
		switch r.Severity {
		case config.DecomposeLintSeverityBlock:
			block++
		case config.DecomposeLintSeverityWarning:
			warn++
		}
	}
	if block != 16 || warn != 5 {
		t.Errorf("pack = %d block / %d warn, want 16 / 5", block, warn)
	}
}

// A script that rejects changelog, README, docs, locales and tests changes
// and a task's own planned test file.
const rejectingScript = `#!/usr/bin/env bash
set -eu
base=$(git merge-base HEAD main)
allowed=(
  internal/autonomous/decompose_lint_verify_script_rules.go
  internal/config/decompose_lint.go
)
for f in $(git diff --name-only "$base"); do
  case " ${allowed[*]} " in
    *" $f "*) ;;
    *) echo "unexpected change: $f"; exit 1 ;;
  esac
done
`

func TestStability3Script_RejectsOwnFiles(t *testing.T) {
	rule := s3sRule(t, "verify_script_rejects_own_files")
	dir := t.TempDir()
	s3sWrite(t, dir, "v/reject.sh", rejectingScript)
	// Corrected: every planned file and the standard set are listed.
	s3sWrite(t, dir, "v/fixed.sh", `#!/usr/bin/env bash
base=$(git merge-base HEAD main)
allowed=(
  internal/autonomous/decompose_lint_verify_script_rules.go
  internal/autonomous/decompose_lint_rules.go
  internal/autonomous/decompose_lint_stability3_script_test.go
  CHANGELOG.md README.md docs/ internal/server/web/locales/ internal/autonomous/x_test.go
)
for f in $(git diff --name-only "$base"); do
  case " ${allowed[*]} " in
    *" $f "*) ;;
    *) echo "unexpected change: $f"; exit 1 ;;
  esac
done
`)
	// Corrected the other way: the shared allowed list does the judging.
	s3sWrite(t, dir, "v/shared.sh", `#!/usr/bin/env bash
source scripts/lib/allowed-files.sh
allowed=(internal/config/decompose_lint.go)
for f in $(git diff --name-only HEAD); do
  allowed_file "$f" || exit 1
done
`)
	// A short list with grep -v, failing on anything else.
	s3sWrite(t, dir, "v/grepv.sh", `#!/usr/bin/env bash
extra=$(git diff --name-only HEAD | grep -v -e internal/config/decompose_lint.go)
[ -n "$extra" ] && exit 1
exit 0
`)
	// No changed-file check at all: an unrelated variable is not a list.
	s3sWrite(t, dir, "v/unrelated.sh", "#!/usr/bin/env bash\nexpected_count=3\ngo test ./...\n")

	planned := []string{
		"internal/autonomous/decompose_lint_verify_script_rules.go",
		"internal/autonomous/decompose_lint_rules.go",
		"internal/autonomous/decompose_lint_stability3_script_test.go",
	}
	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"literal list without own files, changelog, docs, locales, tests", "v/reject.sh", 1},
		{"grep -v short list", "v/grepv.sh", 1},
		{"corrected: own files and standard set listed", "v/fixed.sh", 0},
		{"corrected: shared allowed list", "v/shared.sh", 0},
		{"unrelated variable", "v/unrelated.sh", 0},
		{"missing script skipped", "v/missing.sh", 0},
	} {
		got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: c.script, FilesPlanned: planned}))
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}

	got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: "v/reject.sh", FilesPlanned: planned}))
	f := got[0]
	if f.Task != "t" || f.Script != "v/reject.sh" || !strings.Contains(f.Matched, "allowed=(") ||
		!strings.Contains(f.Message, "v/reject.sh") {
		t.Errorf("finding must name task, script and offending line: %+v", f)
	}
}

func TestStability3Script_NeedsLaterTaskOutput(t *testing.T) {
	rule := s3sRule(t, "verify_script_needs_later_task_output")
	dir := t.TempDir()
	// Requires a file only task "later" creates.
	s3sWrite(t, dir, "v/file.sh", "#!/usr/bin/env bash\ntest -f docs/howto/new-howto.md\n")
	// Requires a phrase in a file the later task creates.
	s3sWrite(t, dir, "v/phrase.sh", "#!/usr/bin/env bash\ngrep -q \"Verification script lint\" docs/howto/new-howto.md\n")
	// Requires a phrase in the shared CHANGELOG that the later task's spec writes.
	s3sWrite(t, dir, "v/changelog.sh", "#!/usr/bin/env bash\ngrep -q 'script content rules' CHANGELOG.md\n")
	s3sWrite(t, dir, "v/own.sh", "#!/usr/bin/env bash\ntest -f internal/a.go\ngrep -q package internal/a.go\n")
	s3sWrite(t, dir, "CHANGELOG.md", "# changelog\n")

	later := Task{ID: "later", Spec: "Write the script content rules entry and the how-to.",
		FilesPlanned: []string{"docs/howto/new-howto.md", "CHANGELOG.md"}}
	for _, c := range []struct {
		name   string
		task   Task
		others []Task
		want   int
	}{
		{"required file made by an independent task", Task{ID: "t", VerificationScript: "v/file.sh"}, []Task{later}, 1},
		{"phrase in a file made by an independent task", Task{ID: "t", VerificationScript: "v/phrase.sh"}, []Task{later}, 1},
		{"phrase a later task's spec writes into a shared file",
			Task{ID: "t", VerificationScript: "v/changelog.sh", Spec: "Add the rules.", FilesPlanned: []string{"CHANGELOG.md"}}, []Task{later}, 1},
		{"corrected: depends_on the creating task", Task{ID: "t", VerificationScript: "v/file.sh", DependsOn: []string{"later"}}, []Task{later}, 0},
		{"corrected: depends through a middle task", Task{ID: "t", VerificationScript: "v/file.sh", DependsOn: []string{"mid"}},
			[]Task{{ID: "mid", DependsOn: []string{"later"}}, later}, 0},
		{"corrected: the task creates the file itself", Task{ID: "t", VerificationScript: "v/file.sh", FilesGenerated: []string{"docs/howto/new-howto.md"}}, []Task{later}, 0},
		{"corrected: nobody else creates it", Task{ID: "t", VerificationScript: "v/file.sh"}, nil, 0},
		{"own file only", Task{ID: "t", VerificationScript: "v/own.sh", FilesPlanned: []string{"internal/a.go"}}, []Task{later}, 0},
	} {
		got := s3sLint(dir, rule, s3sOne(c.task), Story{ID: "s2", Tasks: c.others})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}

	// Story-level depends_on is honoured: every task of a story the task's
	// story depends on is an ancestor.
	dep := Story{ID: "s2", Tasks: []Task{later}}
	owner := Story{ID: "s3", DependsOn: []string{"s2"}, Tasks: []Task{{ID: "t", VerificationScript: "v/file.sh"}}}
	if got := s3sLint(dir, rule, dep, owner); len(got) != 0 {
		t.Errorf("story depends_on not honoured: %+v", got)
	}
	// Without the plan view the predicate never fires.
	m, err := compileMatch(config.DecomposeLintMatch{VerificationScriptNeedsLaterTaskOutput: true}, "r")
	if err != nil {
		t.Fatal(err)
	}
	if m.matchTask(Task{ID: "t", VerificationScript: "v/file.sh"}, dir, nil) {
		t.Error("needs-later-output fired without a plan")
	}

	got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: "v/phrase.sh"}), Story{ID: "s2", Tasks: []Task{later}})
	if len(got) != 1 || got[0].Script != "v/phrase.sh" || !strings.Contains(got[0].Matched, "Verification script lint") {
		t.Errorf("finding must name the script and the offending line: %+v", got)
	}
}

func TestStability3Script_ShForBash(t *testing.T) {
	rule := s3sRule(t, "verify_script_sh_for_bash")
	dir := t.TempDir()
	s3sWrite(t, dir, "v/sh_dbl.sh", "#!/bin/sh\nif [[ -f a.go ]]; then echo ok; fi\n")
	s3sWrite(t, dir, "v/sh_arr.sh", "#!/bin/sh\nfiles=(a.go b.go)\necho \"${files[@]}\"\n")
	s3sWrite(t, dir, "v/sh_proc.sh", "#!/usr/bin/env sh\ndiff <(echo a) <(echo b)\n")
	s3sWrite(t, dir, "v/sh_pipefail.sh", "#!/bin/sh\nset -euo pipefail\ntrue\n")
	s3sWrite(t, dir, "v/bash_dbl.sh", "#!/usr/bin/env bash\nif [[ -f a.go ]]; then echo ok; fi\n")
	s3sWrite(t, dir, "v/sh_posix.sh", "#!/bin/sh\nif [ -f a.go ]; then echo ok; fi\ngrep -q '[[:space:]]' a.go\n")
	s3sWrite(t, dir, "v/sh_comment.sh", "#!/bin/sh\n# [[ is bash-only; set -o pipefail too\ntest -f a.go\n")
	s3sWrite(t, dir, "v/runner.sh", "#!/bin/bash\nsh v/bash_dbl.sh\n")

	for _, c := range []struct {
		name string
		task Task
		want int
	}{
		{"[[ ]] under an sh shebang", Task{ID: "t", VerificationScript: "v/sh_dbl.sh"}, 1},
		{"array under an sh shebang", Task{ID: "t", VerificationScript: "v/sh_arr.sh"}, 1},
		{"process substitution under env sh", Task{ID: "t", VerificationScript: "v/sh_proc.sh"}, 1},
		{"pipefail under an sh shebang", Task{ID: "t", VerificationScript: "v/sh_pipefail.sh"}, 1},
		{"spec runs a bash script with sh", Task{ID: "t", VerificationScript: "v/bash_dbl.sh",
			Spec: "Verify with:\nsh v/bash_dbl.sh"}, 1},
		{"script line runs a bash helper with sh", Task{ID: "t", VerificationScript: "v/runner.sh"}, 1},
		{"corrected: bash shebang", Task{ID: "t", VerificationScript: "v/bash_dbl.sh"}, 0},
		{"corrected: spec runs it with bash", Task{ID: "t", VerificationScript: "v/bash_dbl.sh", Spec: "Verify with:\nbash v/bash_dbl.sh"}, 0},
		{"posix script under sh", Task{ID: "t", VerificationScript: "v/sh_posix.sh"}, 0},
		{"bash-only words only in a comment", Task{ID: "t", VerificationScript: "v/sh_comment.sh"}, 0},
		{"posix script run with sh", Task{ID: "t", VerificationScript: "v/sh_posix.sh", Spec: "sh v/sh_posix.sh"}, 0},
	} {
		got := s3sLint(dir, rule, s3sOne(c.task))
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}

	got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: "v/sh_dbl.sh"}))
	if got[0].Script != "v/sh_dbl.sh" || !strings.Contains(got[0].Matched, "[[ -f a.go ]]") {
		t.Errorf("finding must name the script and the offending line: %+v", got[0])
	}
}

func TestStability3Script_PipeGrepQ(t *testing.T) {
	rule := s3sRule(t, "verify_script_pipe_grep_q")
	dir := t.TempDir()
	s3sWrite(t, dir, "v/pipe.sh", "#!/usr/bin/env bash\nset -euo pipefail\ngit diff --name-only HEAD | grep -q decompose_lint.go\n")
	s3sWrite(t, dir, "v/pipe_e.sh", "#!/usr/bin/env bash\nset -o pipefail\ncat x | grep -E -q 'a|b'\n")
	s3sWrite(t, dir, "v/pipe_long.sh", "#!/usr/bin/env bash\ncat x | grep --quiet a\n")
	s3sWrite(t, dir, "v/captured.sh", "#!/usr/bin/env bash\nset -euo pipefail\nout=$(git diff --name-only HEAD)\ngrep -q decompose_lint.go <<<\"$out\"\n")
	s3sWrite(t, dir, "v/file_arg.sh", "#!/usr/bin/env bash\ngrep -q decompose_lint.go CHANGELOG.md\n")
	s3sWrite(t, dir, "v/devnull.sh", "#!/usr/bin/env bash\ncat x | grep a >/dev/null\ntrue || grep -q a x\n")
	s3sWrite(t, dir, "v/comment.sh", "#!/usr/bin/env bash\n# do not use: cat x | grep -q a\ntrue\n")

	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"pipe into grep -q", "v/pipe.sh", 1},
		{"pipe into grep -E -q", "v/pipe_e.sh", 1},
		{"pipe into grep --quiet", "v/pipe_long.sh", 1},
		{"corrected: capture then here-string", "v/captured.sh", 0},
		{"grep -q on a file", "v/file_arg.sh", 0},
		{"no -q and || before grep -q", "v/devnull.sh", 0},
		{"pattern only in a comment", "v/comment.sh", 0},
	} {
		got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: c.script}))
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}
	got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: "v/pipe.sh"}))
	if got[0].Script != "v/pipe.sh" || !strings.Contains(got[0].Matched, "| grep -q decompose_lint.go") {
		t.Errorf("finding must name the script and the offending line: %+v", got[0])
	}
}

func TestStability3Script_HexPattern(t *testing.T) {
	rule := s3sRule(t, "verify_script_hex_pattern")
	dir := t.TempDir()
	s3sWrite(t, dir, "v/bare.sh", "#!/usr/bin/env bash\n! grep -rEq '#[0-9]{3,}' docs/\n")
	s3sWrite(t, dir, "v/digit_class.sh", "#!/usr/bin/env bash\ngrep -E '(^|[^&])#\\d+' docs/x.md\n")
	s3sWrite(t, dir, "v/with_word.sh", "#!/usr/bin/env bash\n! grep -rEq '(issues?|fix(es|ed)?|closes?)[[:space:]]+#[0-9]{3,}' docs/\n")
	s3sWrite(t, dir, "v/prefixed.sh", "#!/usr/bin/env bash\n! grep -rEq '\\b(BL|GH)[#-]?[0-9]{2,}\\b' docs/\n")
	s3sWrite(t, dir, "v/nogrep.sh", "#!/usr/bin/env bash\necho '#123456'\n")

	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"bare #digits grep pattern", "v/bare.sh", 1},
		{"bare #\\d+ pattern", "v/digit_class.sh", 1},
		{"corrected: tracker word before the #", "v/with_word.sh", 0},
		{"corrected: prefixed id form", "v/prefixed.sh", 0},
		{"no grep on the line", "v/nogrep.sh", 0},
	} {
		got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: c.script}))
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}
	got := s3sLint(dir, rule, s3sOne(Task{ID: "t", VerificationScript: "v/bare.sh"}))
	if got[0].Script != "v/bare.sh" || !strings.Contains(got[0].Matched, "#[0-9]{3,}") {
		t.Errorf("finding must name the script and the offending line: %+v", got[0])
	}
}

// The whole default pack fires each new rule through LintTasksInDir (the
// plan-wide path), and the engine plumbing handles nested matches.
func TestStability3Script_DefaultPackAndNesting(t *testing.T) {
	dir := t.TempDir()
	s3sWrite(t, dir, "v/bad.sh", "#!/bin/sh\nif [[ -f a ]]; then cat x | grep -q y; fi\n")
	fs := LintTasksIn(dir, []Story{s3sOne(Task{ID: "t", VerificationScript: "v/bad.sh", FilesPlanned: []string{"a.go"}})}, DefaultDecomposeLintRules())
	for _, id := range []string{"verify_script_sh_for_bash", "verify_script_pipe_grep_q"} {
		if _, ok := s3Has(fs, id, "t"); !ok {
			t.Errorf("%s did not fire through the default pack: %+v", id, fs)
		}
	}

	m, err := compileMatch(config.DecomposeLintMatch{
		AnyOf: []config.DecomposeLintMatch{{VerificationScriptPipeGrepQ: true}},
		AllOf: []config.DecomposeLintMatch{{Kind: "x"}},
	}, "r")
	if err != nil {
		t.Fatal(err)
	}
	if !m.usesScriptContent() {
		t.Error("nested script predicate not reported as script content")
	}
	if got := verifyScriptFindingPathIn(m, dir, Task{VerificationScript: "v/bad.sh"}, nil); got != "v/bad.sh" {
		t.Errorf("finding path = %q", got)
	}
	if got := verifyScriptFindingPath(m, dir, Task{VerificationScript: "v/none.sh"}); got != "v/none.sh" {
		t.Errorf("finding path for a missing script = %q", got)
	}
}

func TestStability3Script_Helpers(t *testing.T) {
	if !listCovers("a/ docs/* b.go", "docs/x/y.md") || !listCovers("a/ b.go", "a/z.go") || listCovers("b.go", "c.go") {
		t.Error("listCovers")
	}
	if openerOf("(a b") != ")" || openerOf("'a") != "'" || openerOf(`"a`) != `"` || openerOf("(a)") != "" {
		t.Error("openerOf")
	}
	pat, files := grepArgs(` -E -m 1 -e "foo bar" a.md b.md | wc -l`)
	if pat != "foo bar" || len(files) != 2 {
		t.Errorf("grepArgs = %q %v", pat, files)
	}
	if plainFile("$X/a") || plainFile("/etc/x") || plainFile("../x") || plainFile("a*") || !plainFile("a/b.md") {
		t.Error("plainFile")
	}
	if normPlanFile("  ") != "" || normPlanFile("./a/../b.go") != "b.go" {
		t.Error("normPlanFile")
	}
}
