// decompose_lint_coverage.go — plan-level coverage checks. They are
// built-in checks beside the rule pack (not entries of it), so they read
// the disabled rule ids themselves.
package autonomous

import (
	"path"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// planCoverageCheck is one plan-level check: an id and a run function
// over the stories, the PRD kind and the disabled rule ids.
type planCoverageCheck struct {
	id  string
	run func(stories []Story, prdKind string, disabled []string) []LintFinding
}

// planCoverageChecks is the registry of plan-level checks.
var planCoverageChecks = []planCoverageCheck{
	{id: ruleSmokeTaskRequired, run: checkSmokeTaskRequired},
}

const ruleSmokeTaskRequired = "smoke_task_required"

const smokeTaskRequiredMessage = "smoke_task_required: the plan changes code but has no task that adds or updates a smoke test and names its run command; add a section to scripts/release-smoke.sh or a story under scripts/test-stories and name the run command."

// RunPlanCoverageChecks runs every registered check not disabled. The
// plan-level exemption reason comes from prd (may be nil).
func RunPlanCoverageChecks(prd *PRD, stories []Story, disabled []string) []LintFinding {
	kind, reason := "", ""
	if prd != nil {
		kind, reason = prd.Kind, strings.TrimSpace(prd.CoverageExemptionReason)
	}
	var out []LintFinding
	for _, c := range planCoverageChecks {
		if idDisabled(c.id, disabled) {
			continue
		}
		if reason != "" && c.id == ruleSmokeTaskRequired {
			if first := firstCodeChangingTask(stories); first != nil {
				out = append(out, exemptFinding(c.id, first.story, first.task, reason))
			}
			continue
		}
		out = append(out, c.run(stories, kind, disabled)...)
	}
	return out
}

func idDisabled(id string, disabled []string) bool {
	for _, d := range disabled {
		if strings.TrimSpace(d) == id {
			return true
		}
	}
	return false
}

func exemptFinding(id, story, task, reason string) LintFinding {
	return LintFinding{
		RuleID:   id,
		Severity: config.DecomposeLintSeverityWarning,
		Message:  id + ": exempted by a written reason",
		Story:    story,
		Task:     task,
		Matched:  "exempt: " + reason,
	}
}

type taskRef struct{ story, task string }

var templateTitleRe = regexp.MustCompile(`(?i)final[ _-]?gate|version note|changelog entry|parity issue draft`)

func isTemplateTask(t Task) bool {
	return t.Kind == TaskKindFinalGate || t.Kind == TaskKindSmoke || templateTitleRe.MatchString(t.Title)
}

func isCodeFile(f string) bool {
	f = path.Clean(strings.ReplaceAll(strings.TrimSpace(f), "\\", "/"))
	switch path.Ext(f) {
	case ".go":
		return !strings.HasSuffix(f, "_test.go")
	case ".js", ".html", ".css":
		const web = "internal/server/web/"
		if !strings.HasPrefix(f, web) {
			return false
		}
		rest := strings.TrimPrefix(f, web)
		for _, seg := range strings.Split(rest, "/") {
			switch strings.ToLower(seg) {
			case "locales", "docs", "test", "tests":
				return false
			}
		}
		base := path.Base(f)
		return !strings.Contains(base, ".test.") && !strings.HasSuffix(base, "_test.js")
	}
	return false
}

func isCodeChangingTask(t Task) bool {
	if t.ReadOnly || isTemplateTask(t) {
		return false
	}
	for _, f := range t.FilesPlanned {
		if isCodeFile(f) {
			return true
		}
	}
	return false
}

func firstCodeChangingTask(stories []Story) *taskRef {
	for _, s := range stories {
		for _, t := range s.Tasks {
			if isCodeChangingTask(t) {
				return &taskRef{s.ID, t.ID}
			}
		}
	}
	return nil
}

func isCoverageSmokeTask(t Task) bool {
	if t.ReadOnly {
		return false
	}
	spec := t.Spec
	target := strings.Contains(spec, "release-smoke.sh") // names a section of the script
	for _, f := range t.FilesPlanned {
		f = path.Clean(strings.TrimSpace(f))
		if f == "scripts/release-smoke.sh" || strings.HasPrefix(f, "scripts/test-stories/") {
			target = true
		}
	}
	if !target {
		return false
	}
	return strings.Contains(spec, "scripts/release-smoke.sh") ||
		(strings.Contains(spec, "run-tests.sh") && regexp.MustCompile(`run-tests\.sh[^\n]*--story`).MatchString(spec))
}

func checkSmokeTaskRequired(stories []Story, _ string, _ []string) []LintFinding {
	first := firstCodeChangingTask(stories)
	if first == nil {
		return nil
	}
	for _, s := range stories {
		for _, t := range s.Tasks {
			if isCoverageSmokeTask(t) {
				return nil
			}
		}
	}
	// A task-level exemption on any code-changing task records itself.
	for _, s := range stories {
		for _, t := range s.Tasks {
			if r := strings.TrimSpace(t.CoverageExemptionReason); r != "" && isCodeChangingTask(t) {
				return []LintFinding{exemptFinding(ruleSmokeTaskRequired, s.ID, t.ID, r)}
			}
		}
	}
	return []LintFinding{{
		RuleID:   ruleSmokeTaskRequired,
		Severity: config.DecomposeLintSeverityBlock,
		Message:  smokeTaskRequiredMessage,
		Story:    first.story,
		Task:     first.task,
	}}
}
