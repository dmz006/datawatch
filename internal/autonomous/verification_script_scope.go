// Verification-script scope gate. The sibling coverage gate
// (verification_script_coverage.go) enforces that a tooled plan names a
// verification script somewhere; this gate enforces that a script, once
// written, actually verifies the task's own work. The recurring failure
// mode it exists for is a script whose only check is `gofmt -l .` or
// `gofmt -l ./...` over a whole package: that passes or fails on code
// the task never touched — it proves the tree was formatted, not that
// this task's changes are correct. A script that formats/inspects only
// the files the task changed (typically a list produced with
// `git diff --name-only`) is exactly what the prompt asks for, and is
// what this gate accepts. Like the coverage gate it is deliberately
// mechanical: per line, only gofmt invocations are scrutinised, only
// whole-package arguments are rejected, and unreadable/unresolvable
// scripts are skipped (the script may legitimately not exist yet at
// decompose time; the existing runner already skips absolute paths and
// paths escaping the projectDir, so those are skipped here too).
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateVerificationScriptScope walks every story and task with a
// non-empty VerificationScript, reads the script (if it exists under
// projectDir), and rejects any line that runs gofmt over a whole
// package — an argument that is ".", "./..." or a directory rather than
// a .go file list. Variable arguments ("$f", "${files[@]}") and command
// substitutions ("$(git diff --name-only ...)" are file lists and are
// allowed). Scripts that cannot be read are skipped silently (they may
// not be written yet), as are absolute paths and paths that escape
// projectDir (the runner already handles those). Returns nil (including
// when projectDir is empty, mirroring the coverage gate) when nothing
// violates the rule.
func validateVerificationScriptScope(projectDir string, stories []Story) error {
	if projectDir == "" {
		return nil
	}
	const guidance = "a verification script must check only the files the task changes (for example from `git diff --name-only`) and must not run gofmt over a whole package"
	base := filepath.Clean(projectDir)
	for _, s := range stories {
		for _, t := range s.Tasks {
			script := strings.TrimSpace(t.VerificationScript)
			if script == "" {
				continue
			}
			if filepath.IsAbs(script) {
				continue // the runner already rejects these
			}
			full := filepath.Join(base, script)
			if rel, rerr := filepath.Rel(base, full); rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue // escapes projectDir; the runner already skips these
			}
			data, rerr := os.ReadFile(full)
			if rerr != nil {
				continue // the script may not exist yet
			}
			for i, raw := range strings.Split(string(data), "\n") {
				line := strings.TrimSpace(raw)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if bad, found := wholePackageGofmtArg(line); found {
					return fmt.Errorf("story %q, task %q: verification script %s, line %d: %q — %s",
						s.Title, t.Title, script, i+1, bad, guidance)
				}
			}
		}
	}
	return nil
}

// wholePackageGofmtArg reports whether the line contains a gofmt
// invocation with a whole-package argument, returning that argument.
// It inspects every token named `gofmt` (a bare command or a path
// ending in /gofmt), skips flag arguments (leading `-`), and checks the
// remaining arguments against isWholePackageGofmtArg. The line is
// tokenized with quote/parenthesis awareness so substitutions like
// "$(git diff --name-only ...)" stay a single token.
func wholePackageGofmtArg(line string) (arg string, found bool) {
	tokens := tokenizeShellLine(line)
	for i, tk := range tokens {
		if baseName(tk) != "gofmt" {
			continue
		}
		for j := i + 1; j < len(tokens); j++ {
			a := tokens[j]
			if isGofmtArgTerminator(a) {
				break
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			if isWholePackageGofmtArg(a) {
				return a, true
			}
		}
	}
	return "", false
}

// baseName returns the final slash-separated element of a command path
// ("gofmt" for "gofmt" or "/usr/local/bin/gofmt").
func baseName(f string) string {
	if idx := strings.LastIndexByte(f, '/'); idx >= 0 {
		return f[idx+1:]
	}
	return f
}

// tokenizeShellLine splits a shell line into tokens for argument
// inspection: adjacent whitespace separates tokens, quoted regions are
// kept whole (without their quotes), $( ) groups stay a single token
// (including shell metacharacters inside them), and shell metacharacters
// outside a group (";" "|" "&" ">" "<") become standalone tokens. It is
// good enough to find gofmt's arguments; it is not a shell parser.
func tokenizeShellLine(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := byte(0)
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inQuote != 0:
			if c == inQuote || (inQuote == '"' && c == '\\') {
				inQuote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == ' ' || c == '\t':
			if depth == 0 {
				flush()
			}
		case c == ';' || c == '|' || c == '&' || c == '>' || c == '<':
			if depth > 0 {
				cur.WriteByte(c)
			} else {
				flush()
				tokens = append(tokens, string(c))
			}
		case c == '(':
			if inQuote != 0 || depth > 0 {
				cur.WriteByte(c)
			} else {
				depth++
			}
		case c == ')':
			if inQuote != 0 {
				cur.WriteByte(c)
			} else if depth > 0 {
				depth--
			} else {
				cur.WriteByte(c)
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return tokens
}

// isGofmtArgTerminator reports whether a token ends the gofmt
// argument list: a shell metacharacter, a numeric file descriptor of a
// redirect, or a shell keyword.
func isGofmtArgTerminator(f string) bool {
	switch f {
	case ";", "|", "&", ">", "<", "do", "done", "then", "else", "fi", "if", "for", "in", "while", "local", "echo", "exit":
		return true
	}
	if len(f) > 0 {
		for _, r := range f {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// isWholePackageGofmtArg reports whether one non-flag gofmt argument
// names a whole package rather than a file list: "." / "./..." / "..."
// (the current-package idiom), a path ending in `/` (a directory), or a
// bare path with no `.go` suffix (a directory-like package path). Any
// argument containing a `$` — variables such as "$f" or "${files[@]}"
// and command substitutions such as "$(git diff --name-only
// HEAD~1...)" — denotes a file list and is allowed.
func isWholePackageGofmtArg(arg string) bool {
	if strings.Contains(arg, "$") {
		return false
	}
	if arg == "." || arg == "..." || arg == "./..." {
		return true
	}
	if strings.HasSuffix(arg, "/") {
		return true
	}
	if strings.HasSuffix(arg, ".go") || strings.Contains(arg, "*.go") {
		return false
	}
	return true
}
