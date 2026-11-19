// verify_script_preflight_pipefail.go — the pipefail + `grep -q`-in-a-pipe
// check behind the verify_script_pipefail_grep_q preflight rule: a
// verification script that enables `pipefail` and then filters a pipeline
// with a quiet grep (e.g. `cmd | grep -q pattern`) can fail on a broken
// pipe after grep exits at its first match, which reads as a false
// no-match. The check is purely textual; it never executes the script or
// any pipeline in it.
package autonomous

import (
	"strings"
)

// RuleVerifyScriptPipefailGrepQuiet marks a piped `grep -q` line in a
// script that enables pipefail.
const RuleVerifyScriptPipefailGrepQuiet = "verify_script_pipefail_grep_q"

// pipefailGrepQuietMessage tells the author why the line is flagged and
// how to write the check instead.
const pipefailGrepQuietMessage = "under pipefail, `grep -q` exits at the first match and closes the pipe, so the upstream command's broken pipe reads as a false no-match; write the pipeline's output to a file and test that file with grep -q"

// pipefailEnabled reports whether any non-comment line of text enables the
// pipefail option with a set command, such as `set -euo pipefail` or
// `set -o pipefail`.
func pipefailEnabled(text []byte) bool {
	for _, raw := range strings.Split(string(text), "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(raw)
		if len(fields) == 0 || fields[0] != "set" {
			continue
		}
		for _, opt := range fields[1:] {
			if opt == "pipefail" {
				return true
			}
		}
	}
	return false
}

// grepQuietArg reports whether an argument cluster passed to grep carries
// the quiet flag: a single `-q`, a mixed cluster such as `-qE`, `-Eq` or
// `-qx`, or the long form `--quiet`.
func grepQuietArg(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}
	body := strings.TrimLeft(arg, "-")
	if body == "quiet" {
		return true
	}
	for i := 0; i < len(body); i++ {
		if body[i] == 'q' {
			return true
		}
	}
	return false
}

// pipedGrepQuiet reports whether line pipes something into a grep command
// whose options include the quiet flag, e.g. `seq 1 1000 | grep -q 42`,
// `cmd | grep -qE pat`, `a | cmd | grep -qx pat` or `a |& grep -q pat`.
// A grep that reads a file argument on an unjoined line does not count,
// nor does a pipeline whose segments never end in a quiet grep.
func pipedGrepQuiet(raw string) bool {
	segments := strings.Split(raw, "|")
	if len(segments) < 2 {
		return false
	}
	for _, seg := range segments[1:] {
		fields := strings.Fields(seg)
		if fields[0] == "&" && len(fields) > 1 { // `|&` form
			fields = fields[1:]
		}
		if len(fields) == 0 || fields[0] != "grep" {
			continue
		}
		for _, arg := range fields[1:] {
			if !grepQuietArg(arg) {
				continue
			}
			return true
		}
	}
	return false
}

// preflightPipefailGrepFindings reports each non-comment line of scriptRel
// that pipes into a grep carrying the quiet flag, but only when the script
// enables pipefail. Each flagged line yields one finding with the script
// name, that 1-based line and a message explaining the false no-match and
// telling the author to write the output to a file and test that file with
// grep. A script without pipefail yields none, as does a `grep -q` that
// reads a file argument with no pipe. The check is textual only; it never
// executes the script or any pipe in it.
func preflightPipefailGrepFindings(scriptRel string, text []byte) []VerifyScriptPreflightFinding {
	if !pipefailEnabled(text) {
		return nil
	}
	var findings []VerifyScriptPreflightFinding
	for i, raw := range strings.Split(string(text), "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !pipedGrepQuiet(raw) {
			continue
		}
		findings = append(findings, VerifyScriptPreflightFinding{
			Script:  scriptRel,
			Line:    i + 1,
			RuleID:  RuleVerifyScriptPipefailGrepQuiet,
			Message: pipefailGrepQuietMessage,
		})
	}
	return findings
}
