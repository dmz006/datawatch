// verify_script_preflight_shell.go — the interpreter-mismatch check
// behind the verify_script_shell_mismatch preflight rule: a verification
// script that invokes a helper with one shell (e.g. `sh helper.sh`)
// while the helper's shebang names a different one (e.g. `#!/usr/bin/env
// bash`) runs the helper under the wrong interpreter. The check reads
// each called script through readCalled; the production caller wraps
// readProjectFile from decompose_lint_verify_script.go with the task's
// project directory, and tests pass an in-memory map.
package autonomous

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RuleVerifyScriptShellMismatch marks a called script whose shebang names
// a different interpreter than the one invoking it.
const RuleVerifyScriptShellMismatch = "verify_script_shell_mismatch"

// preflightShellMismatchFindings scans scriptRel's text for shell
// invocations — an interpreter name (sh, dash, ksh, zsh or bash), then
// optional flags, then a path token ending in .sh — and compares the
// invoking interpreter to the called script's shebang. Each call whose
// callee declares a different interpreter yields one finding carrying the
// calling script name and the line of the call. A callee that cannot be
// read through readCalled is skipped, as is one whose first line is not a
// recognizable shell shebang; a caller and callee on the same interpreter
// yield no finding.
func preflightShellMismatchFindings(scriptRel string, text []byte, readCalled func(rel string) ([]byte, bool)) []VerifyScriptPreflightFinding {
	var findings []VerifyScriptPreflightFinding
	for i, line := range strings.Split(string(text), "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			continue // comment (incl. the shebang line itself)
		}
		fields := strings.Fields(line)
		for j := 0; j < len(fields); j++ {
			invoker := strings.ToLower(filepath.Base(fields[j]))
			if !preflightShells[invoker] {
				continue
			}
			k := j + 1 // optional flags
			for k < len(fields) && strings.HasPrefix(fields[k], "-") {
				k++
			}
			if k >= len(fields) || !strings.HasSuffix(fields[k], ".sh") {
				continue
			}
			callee := fields[k]
			calleeText, ok := readCalled(callee)
			if !ok {
				continue
			}
			called := preflightInterpreter(calleeText)
			if called == "" || called == invoker {
				continue
			}
			findings = append(findings, VerifyScriptPreflightFinding{
				Script: scriptRel,
				Line:   i + 1,
				RuleID: RuleVerifyScriptShellMismatch,
				Message: fmt.Sprintf(
					"line %d invokes %s with %s, but the script declares %s",
					i+1, callee, invoker, called),
			})
		}
	}
	return findings
}
