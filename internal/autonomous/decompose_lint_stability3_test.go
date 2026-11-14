package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// Fixtures for the three spec-level rules added to the built-in pack
// (read_only_task_writes_file, spec_path_outside_allowed_dirs,
// version_number_in_spec) and the tracker-id pattern fix. They are the
// shapes of the specs that failed decomposition this week. Absolute
// outside paths are assembled from parts so no such path appears in the
// text of this file's own task spec.

func s3Join(parts ...string) string { return strings.Join(parts, "") }

// s3OptPath is a path under the system optional-software directory.
func s3OptPath() string { return s3Join("/", "op", "t", "/tools/bin/", "runner") }

// s3LocalBinPath is a path under the system local-binaries directory.
func s3LocalBinPath() string { return s3Join("/", "usr", "/local", "/bin/", "helper") }

func s3Task(id, title, spec string, readOnly bool, report string) Task {
	return Task{ID: id, Title: title, Spec: spec, ReadOnly: readOnly, ReportPath: report,
		VerificationScript: ".datawatch/verify/" + id + ".sh"}
}

func s3Lint(dir string, tasks ...Task) []LintFinding {
	return LintTasksIn(dir, []Story{{ID: "s1", Tasks: tasks}}, DefaultDecomposeLintRules())
}

// s3Has returns the finding for rule on task, if any.
func s3Has(fs []LintFinding, rule, task string) (LintFinding, bool) {
	for _, f := range fs {
		if f.RuleID == rule && f.Task == task {
			return f, true
		}
	}
	return LintFinding{}, false
}

func TestStability3_PackShape(t *testing.T) {
	want := map[string]string{
		"read_only_task_writes_file":     config.DecomposeLintSeverityBlock,
		"spec_path_outside_allowed_dirs": config.DecomposeLintSeverityBlock,
		"version_number_in_spec":         config.DecomposeLintSeverityWarning,
	}
	rules := DefaultDecomposeLintRules()
	block, warn := 0, 0
	for _, r := range rules {
		if sev, ok := want[r.ID]; ok {
			if r.Severity != sev {
				t.Errorf("%s severity = %q, want %q", r.ID, r.Severity, sev)
			}
			delete(want, r.ID)
		}
		switch r.Severity {
		case config.DecomposeLintSeverityBlock:
			block++
		case config.DecomposeLintSeverityWarning:
			warn++
		}
	}
	for id := range want {
		t.Errorf("rule %s missing from the default pack", id)
	}
	if len(rules) != 21 || block != 16 || warn != 5 {
		t.Errorf("pack = %d rules (%d block, %d warn), want 21 (16 block, 5 warn)", len(rules), block, warn)
	}
	if err := (config.DecomposeLintConfig{Rules: rules}).Validate(); err != nil {
		t.Errorf("default pack does not validate: %v", err)
	}
}

func TestStability3_ReadOnlyTaskWritesFile(t *testing.T) {
	const rule = "read_only_task_writes_file"
	report := ".datawatch/reports/audit.md"
	badSpec := "Audit the lint pack against the plans.\nWrite the findings summary to docs/plans/2026-11-12-lint-audit.md when done.\nDo not change any source file."

	fail := []struct {
		name string
		task Task
		line string
	}{
		{"flag", s3Task("ro-flag", "Audit lint pack", badSpec, true, report),
			"Write the findings summary to docs/plans/2026-11-12-lint-audit.md when done."},
		{"marker", s3Task("ro-marker", "Audit lint pack", "READ-ONLY: audit the pack.\nCreate a new file docs/plans/audit-notes.md with the result.", false, report),
			"Create a new file docs/plans/audit-notes.md with the result."},
		{"file under a directory", s3Task("ro-dir", "Audit", "Check the rules.\nSave a file under docs/plans/ listing every gap.", true, report),
			"Save a file under docs/plans/ listing every gap."},
		{"report path declared but spec names another file", s3Task("ro-other", "Audit", "Record the verdict in "+report+".\nAlso edit internal/autonomous/decompose_lint.go to add a comment.", true, report),
			"Also edit internal/autonomous/decompose_lint.go to add a comment."},
		{"no report path at all", s3Task("ro-noreport", "Audit", "Write the audit to docs/plans/audit.md.", true, ""),
			"Write the audit to docs/plans/audit.md."},
	}
	for _, c := range fail {
		t.Run("fail/"+c.name, func(t *testing.T) {
			f, ok := s3Has(s3Lint("", c.task), rule, c.task.ID)
			if !ok {
				t.Fatalf("%s did not fire on %s", rule, c.task.ID)
			}
			if f.Severity != config.DecomposeLintSeverityBlock {
				t.Errorf("severity = %q, want block", f.Severity)
			}
			if f.Matched != c.line {
				t.Errorf("matched = %q, want the offending line %q", f.Matched, c.line)
			}
			if !strings.Contains(f.Message, ".datawatch/reports") {
				t.Errorf("message %q must point at a report path under .datawatch/reports", f.Message)
			}
		})
	}

	pass := []struct {
		name string
		task Task
	}{
		{"writes only its report path", s3Task("ok-report", "Audit", "READ-ONLY: audit the pack.\nWrite the verdict to "+report+".", true, report)},
		{"report path with a space in it", s3Task("ok-space", "Audit", "Write the verdict and the evidence to one report file, .datawatch/reports/task templates-final-gate.txt (slugged).", true, ".datawatch/reports/task templates-final-gate.txt")},
		{"negated write", s3Task("ok-negated", "Audit", "Do not write docs/plans/audit.md and never create any file.\nRecord the verdict in "+report+".", true, report)},
		{"only reads", s3Task("ok-reads", "Audit", "Read docs/plans/README.md and internal/autonomous/decompose_lint.go.\nReport which rules are missing.", true, report)},
		{"not read-only", s3Task("ok-writer", "Write the audit", "Write the findings summary to docs/plans/2026-11-12-lint-audit.md.", false, "")},
	}
	for _, c := range pass {
		t.Run("pass/"+c.name, func(t *testing.T) {
			if f, ok := s3Has(s3Lint("", c.task), rule, c.task.ID); ok {
				t.Fatalf("%s fired on a clean task: %+v", rule, f)
			}
		})
	}
}

func TestStability3_SpecPathOutsideAllowedDirs(t *testing.T) {
	const rule = "spec_path_outside_allowed_dirs"
	dir := "/home/ci/project"

	// The two real failing spec lines: a path under the optional-software
	// directory and one under the local-binaries directory.
	optLine := "Run " + s3OptPath() + " --check against the tree."
	binLine := "Copy the built binary to " + s3LocalBinPath() + " when it passes."
	cases := []struct {
		name string
		spec string
		line string
	}{
		{"optional-software directory", "Build the tool.\n" + optLine + "\nThen stop.", optLine},
		{"local-binaries directory", binLine, binLine},
		{"another project's tree", "Read /home/ci/other/README.md first.", "Read /home/ci/other/README.md first."},
		{"scratch directory", "Write the output to /tmp/out.txt.", "Write the output to /tmp/out.txt."},
	}
	for _, c := range cases {
		t.Run("fail/"+c.name, func(t *testing.T) {
			task := s3Task("abs-"+strings.ReplaceAll(c.name, " ", "-"), "Run tool", c.spec, false, "")
			f, ok := s3Has(s3Lint(dir, task), rule, task.ID)
			if !ok {
				t.Fatalf("%s did not fire for %q", rule, c.spec)
			}
			if f.Severity != config.DecomposeLintSeverityBlock {
				t.Errorf("severity = %q, want block", f.Severity)
			}
			if f.Matched != c.line {
				t.Errorf("matched = %q, want the offending line %q", f.Matched, c.line)
			}
		})
	}

	// Without a known project directory nothing is inside the project,
	// so a system path is still flagged.
	task := s3Task("abs-nodir", "Run tool", optLine, false, "")
	if _, ok := s3Has(s3Lint("", task), rule, task.ID); !ok {
		t.Errorf("%s must fire with an empty project dir", rule)
	}

	passes := []struct{ name, spec string }{
		{"inside the project tree", "Read " + dir + "/internal/x.go and " + dir + "."},
		{"repo-relative path", "Read internal/autonomous/decompose_lint.go and docs/howto/README.md."},
		{"relative path that contains a root name", "Edit docs/etc/notes.md and src/usr/main.go."},
		{"URL", "Fetch https://example.com/usr/local/bin/tool for the docs."},
		{"shebang line", "Start the script with #!" + s3Join("/usr", "/bin/env") + " bash."},
		{"dev null", "Redirect the noise to /dev/null."},
		{"REST route", "Call GET /api/autonomous/prds and check the reply."},
		{"root name only as a prefix", "Name it /usrfoo-style."},
	}
	for _, c := range passes {
		t.Run("pass/"+c.name, func(t *testing.T) {
			task := s3Task("abs-ok", "Run tool", c.spec, false, "")
			if f, ok := s3Has(s3Lint(dir, task), rule, task.ID); ok {
				t.Fatalf("%s fired on %q: %+v", rule, c.spec, f)
			}
		})
	}
}

func TestStability3_VersionNumberInSpec(t *testing.T) {
	const rule = "version_number_in_spec"
	fail := []struct{ name, title, spec, line string }{
		{"python version in the spec", "Add the parser", "Set up the venv.\nRequires Python 3.11 or newer for match statements.\nThen run the tests.", "Requires Python 3.11 or newer for match statements."},
		{"library pin", "Add the client", "Pin requests==2.31.0 in the requirements file.", "Pin requests==2.31.0 in the requirements file."},
		{"go toolchain", "Raise the toolchain", "Raise the module to go 1.23.4 in go.mod.", "Raise the module to go 1.23.4 in go.mod."},
		{"version in the title", "Port the script to Python 3.12", "Port it.", "Port the script to Python 3.12"},
	}
	for _, c := range fail {
		t.Run("fail/"+c.name, func(t *testing.T) {
			task := s3Task("ver-"+strings.ReplaceAll(c.name, " ", "-"), c.title, c.spec, false, "")
			f, ok := s3Has(s3Lint("", task), rule, task.ID)
			if !ok {
				t.Fatalf("%s did not fire (title %q, spec %q)", rule, c.title, c.spec)
			}
			if f.Severity != config.DecomposeLintSeverityWarning {
				t.Errorf("severity = %q, want warn", f.Severity)
			}
			if f.Matched != c.line {
				t.Errorf("matched = %q, want the offending line %q", f.Matched, c.line)
			}
			if !strings.Contains(strings.ToLower(f.Message), "unambiguous") {
				t.Errorf("message %q must tell the planner to keep it unambiguous", f.Message)
			}
		})
	}
	pass := []struct{ name, title, spec string }{
		{"no version at all", "Add the parser", "Parse the file and return the rows."},
		{"plain numbers", "Add the parser", "Handle 3.5 seconds of latency and 1.2 MB files."},
		{"a bare ordinal", "Tune the pool", "Use 4 workers and retry 2 times."},
	}
	for _, c := range pass {
		t.Run("pass/"+c.name, func(t *testing.T) {
			task := s3Task("ver-ok", c.title, c.spec, false, "")
			if f, ok := s3Has(s3Lint("", task), rule, task.ID); ok {
				t.Fatalf("%s fired on a clean task: %+v", rule, f)
			}
		})
	}
}

func TestStability3_TrackerIDIgnoresHexColours(t *testing.T) {
	const rule = "names_tracker_id"
	hex := []string{
		"Set the accent colour to #123456 in style.css.",
		"Use background: #abc123; for the badge.",
		"Colours: #123456, #abc123, #0a0b0c and #ffffff.",
		"fill=\"#123456\" and stroke=\"#abc123\"",
		"colour#123456",
	}
	for _, spec := range hex {
		task := s3Task("hex", "Restyle the badge", spec, false, "")
		if f, ok := s3Has(s3Lint("", task), rule, "hex"); ok {
			t.Errorf("hex-colour spec %q triggered %s: %+v", spec, rule, f)
		}
		// The spec lint compiles the same pattern.
		for _, sf := range LintSpecText(spec) {
			if sf.Rule == SpecRuleTrackerID {
				t.Errorf("hex-colour spec %q triggered the spec lint tracker rule: %+v", spec, sf)
			}
		}
	}
	// A hex colour in the title is not a tracker id either.
	if _, ok := s3Has(s3Lint("", s3Task("hex-title", "Make the badge #123456", "Restyle it.", false, "")), rule, "hex-title"); ok {
		t.Errorf("hex colour in the title triggered %s", rule)
	}

	real := []struct{ name, spec, hit string }{
		{"prefixed id", "Fix the crash described in BL123 first.", "BL123"},
		{"hyphenated id", "Follow up on GH-45 afterwards.", "GH-45"},
		{"hash id", "Closes GH#172 once merged.", "GH#172"},
		{"issue number", "Implements the change from issue #4521.", "issue #4521"},
		{"fixes number", "This fixes #987 for good.", "fixes #987"},
	}
	for _, c := range real {
		t.Run("real/"+c.name, func(t *testing.T) {
			task := s3Task("real", "Do the work", c.spec, false, "")
			f, ok := s3Has(s3Lint("", task), rule, "real")
			if !ok {
				t.Fatalf("%s did not fire on %q", rule, c.spec)
			}
			if f.Matched != c.spec {
				t.Errorf("matched = %q, want the spec line %q", f.Matched, c.spec)
			}
			found := false
			for _, sf := range LintSpecText(c.spec) {
				if sf.Rule == SpecRuleTrackerID && strings.Contains(sf.Phrase, c.hit) {
					found = true
				}
			}
			if !found {
				t.Errorf("spec lint did not flag %q in %q", c.hit, c.spec)
			}
		})
	}
}

// TestStability3_FindingNamesTaskAndLine: a finding carries the task and
// quotes the offending spec line, and the shared renderers print both.
func TestStability3_FindingNamesTaskAndLine(t *testing.T) {
	dir := "/home/ci/project"
	optLine := "Run " + s3OptPath() + " --check."
	tasks := []Task{
		s3Task("t-ro", "Audit", "READ-ONLY: audit.\nWrite the notes to docs/plans/notes.md.", false, ".datawatch/reports/a.md"),
		s3Task("t-abs", "Run", "Setup first.\n"+optLine+"\nDone.", false, ""),
		s3Task("t-ver", "Pin", "Intro.\nUse Python 3.11 here.", false, ""),
		s3Task("t-trk", "Track", "Context.\nSee BL123 for details, ok.", false, ""),
	}
	cases := []struct{ rule, task, line string }{
		{"read_only_task_writes_file", "t-ro", "Write the notes to docs/plans/notes.md."},
		{"spec_path_outside_allowed_dirs", "t-abs", optLine},
		{"version_number_in_spec", "t-ver", "Use Python 3.11 here."},
		{"names_tracker_id", "t-trk", "See BL123 for details, ok."},
	}
	findings := s3Lint(dir, tasks...)
	for _, c := range cases {
		f, ok := s3Has(findings, c.rule, c.task)
		if !ok {
			t.Errorf("%s did not fire on %s", c.rule, c.task)
			continue
		}
		if f.Task != c.task || f.Story != "s1" {
			t.Errorf("%s finding names story %q task %q, want s1/%s", c.rule, f.Story, f.Task, c.task)
		}
		if f.Matched != c.line {
			t.Errorf("%s matched = %q, want the whole line %q", c.rule, f.Matched, c.line)
		}
		rendered := LintFindingLine(f)
		if !strings.Contains(rendered, c.task) || !strings.Contains(rendered, c.line) || !strings.Contains(rendered, c.rule) {
			t.Errorf("%s rendered line %q must name rule, task %q and the line %q", c.rule, rendered, c.task, c.line)
		}
		feedback := decomposeLintFeedback([]LintFinding{f})
		if !strings.Contains(feedback, c.task) || !strings.Contains(feedback, c.line) {
			t.Errorf("%s retry feedback %q must name task %q and the line %q", c.rule, feedback, c.task, c.line)
		}
	}
}

// TestStability3_LongLineClippedAroundHit: a long spec line is clipped to
// the excerpt limit and still shows the offending token.
func TestStability3_LongLineClippedAroundHit(t *testing.T) {
	filler := strings.Repeat("word ", 80)
	line := filler + "then copy to " + s3LocalBinPath() + " and " + filler
	task := s3Task("long", "Copy", line, false, "")
	f, ok := s3Has(s3Lint("/home/ci/project", task), "spec_path_outside_allowed_dirs", "long")
	if !ok {
		t.Fatal("rule did not fire on a long line")
	}
	if n := len([]rune(f.Matched)); n > maxExcerptLen {
		t.Errorf("excerpt is %d runes, want <= %d", n, maxExcerptLen)
	}
	if !strings.Contains(f.Matched, s3LocalBinPath()) {
		t.Errorf("clipped excerpt %q lost the offending path", f.Matched)
	}
}
