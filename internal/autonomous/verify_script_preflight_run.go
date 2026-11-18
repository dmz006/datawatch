package autonomous

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// RunVerifyScriptPreflight checks every verification script the plan names
// (on a task or on a story) before any of them runs, and returns the problems
// as block lint findings owned by the naming story and task. Command strings
// and scripts that cannot be read under dir are skipped.
func RunVerifyScriptPreflight(dir string, stories []Story) []LintFinding {
	if dir == "" {
		return nil
	}
	readCalled := func(rel string) ([]byte, bool) {
		s, ok := readProjectFile(dir, rel)
		return []byte(s), ok
	}
	var out []LintFinding
	check := func(script, storyID, taskID string) {
		script = strings.TrimSpace(script)
		if script == "" || verificationScriptLooksLikeCommand(script) {
			return
		}
		content, ok := readProjectFile(dir, script)
		if !ok {
			return
		}
		text := []byte(content)
		var fs []VerifyScriptPreflightFinding
		fs = append(fs, preflightSyntaxFindings(script, text)...)
		fs = append(fs, preflightShellMismatchFindings(script, text, readCalled)...)
		fs = append(fs, preflightPipefailGrepFindings(script, text)...)
		fs = append(fs, preflightAllowedFilesFindings(script, text, stories)...)
		for _, f := range fs {
			out = append(out, LintFinding{
				RuleID:   f.RuleID,
				Severity: config.DecomposeLintSeverityBlock,
				Message:  fmt.Sprintf("%s:%d %s", f.Script, f.Line, f.Message),
				Story:    storyID,
				Task:     taskID,
				Script:   script,
			})
		}
	}
	for _, s := range stories {
		check(s.VerificationScript, s.ID, "")
		for _, t := range s.Tasks {
			check(t.VerificationScript, s.ID, t.ID)
		}
	}
	return out
}
