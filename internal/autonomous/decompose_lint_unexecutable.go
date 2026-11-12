// decompose_lint_unexecutable.go — the shared check behind the
// unexecutable_verification_script decompose-lint rule: is a task's
// verification script a runnable file in the project?
package autonomous

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// unexecutableVerificationScriptReason returns why t's verification script
// cannot run, or "" when it can (or there is none). A value with whitespace
// or a shell operator is a command string; any other value is a path,
// checked with CheckVerifierPrestart against dir. A path the task lists in
// its own files is accepted (the task creates it), and with an empty dir
// only command strings are detectable.
func unexecutableVerificationScriptReason(dir string, t Task) string {
	script := strings.TrimSpace(t.VerificationScript)
	if script == "" {
		return ""
	}
	if verificationScriptLooksLikeCommand(script) {
		return "verification script " + strconv.Quote(script) + " is a command string, not a script file"
	}
	clean := filepath.Clean(script)
	for _, list := range [][]string{t.FilesGenerated, t.FilesPlanned} {
		for _, f := range list {
			if filepath.Clean(strings.TrimSpace(f)) == clean {
				return ""
			}
		}
	}
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "" // the base directory is not available: a path cannot be checked
	}
	if res := CheckVerifierPrestart(dir, script); res.Outcome == PrestartProblem {
		return res.Reason
	}
	return ""
}
