package autonomous

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// VerifyScriptPreflightFinding is one problem found in a verification
// script before it runs.
type VerifyScriptPreflightFinding struct {
	Script  string `json:"script"`  // repo-relative script path
	Line    int    `json:"line"`    // 1-based; 0 when no line applies
	RuleID  string `json:"rule_id"` // e.g. RuleVerifyScriptSyntax
	Message string `json:"message"`
}

// RuleVerifyScriptSyntax marks a shell syntax error found with `<shell> -n`.
const RuleVerifyScriptSyntax = "verify_script_syntax"

// errPreflightInterpreterMissing reports that the interpreter did not resolve.
type errPreflightInterpreterMissing struct{ err error }

func (e errPreflightInterpreterMissing) Error() string { return e.err.Error() }

// preflightRunShellCheck runs `<interpreter> -n` over scriptText and returns
// the combined output. Tests replace it with a fake.
var preflightRunShellCheck = func(interpreter, scriptText string) (string, error) {
	bin, err := exec.LookPath(interpreter)
	if err != nil {
		return "", errPreflightInterpreterMissing{err}
	}
	dir, err := os.MkdirTemp("", "verify-script-preflight-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte(scriptText), 0o600); err != nil {
		return "", err
	}
	out, err := exec.Command(bin, "-n", path).CombinedOutput() // #nosec G204 -- interpreter from a fixed allow-list
	return string(out), err
}

var preflightShells = map[string]bool{"bash": true, "sh": true, "dash": true, "ksh": true, "zsh": true}

var preflightLineRe = regexp.MustCompile(`:\s*(?:line\s+)?(\d+):\s*(.*)`)

// preflightInterpreter returns the shell named on the shebang line, or "".
func preflightInterpreter(text []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(text))
	if !sc.Scan() {
		return ""
	}
	first := strings.TrimSpace(sc.Text())
	if !strings.HasPrefix(first, "#!") {
		return ""
	}
	fields := strings.Fields(strings.TrimPrefix(first, "#!"))
	if len(fields) == 0 {
		return ""
	}
	name := filepath.Base(fields[0])
	if name == "env" {
		name = ""
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") {
				continue
			}
			name = filepath.Base(f)
			break
		}
	}
	if !preflightShells[name] {
		return ""
	}
	return name
}

// preflightSyntaxFindings checks a shell verification script for syntax
// errors. It returns at most one finding.
func preflightSyntaxFindings(script string, text []byte) []VerifyScriptPreflightFinding {
	interp := preflightInterpreter(text)
	if interp == "" {
		return nil
	}
	out, err := preflightRunShellCheck(interp, string(text))
	if err == nil {
		return nil
	}
	if _, ok := err.(errPreflightInterpreterMissing); ok {
		return nil
	}
	if _, ok := err.(*exec.Error); ok {
		return nil
	}
	line, msg := 0, strings.TrimSpace(out)
	for _, l := range strings.Split(out, "\n") {
		if m := preflightLineRe.FindStringSubmatch(l); m != nil {
			line, _ = strconv.Atoi(m[1])
			msg = strings.TrimSpace(m[2])
			break
		}
	}
	if msg == "" {
		msg = err.Error()
	}
	return []VerifyScriptPreflightFinding{{Script: script, Line: line, RuleID: RuleVerifyScriptSyntax, Message: msg}}
}
