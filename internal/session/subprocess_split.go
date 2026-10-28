package session

import (
	"errors"
	"fmt"
	"strings"
)

// splitSubprocessTask turns a subprocess session's task into the program and
// arguments the helper runs directly. No shell is involved anywhere: the task
// is read with shell-style quoting only, and anything that would need a shell
// to mean what it says is refused, not passed through as literal text.
//
// Accepted: words separated by spaces or tabs; 'single quotes' (literal);
// "double quotes" (backslash escapes only \" and \\); a backslash outside
// quotes escapes the next character; adjacent quoted and unquoted pieces join
// into one word; "" is an empty argument. A word that starts with an unquoted
// ~ followed by / or the end of the word gets home in its place (when home is
// not empty), so ~/jobs/run.sh works; no other expansion exists.
//
// Refused, with the reason in the error: an unquoted | & ; < > ( ) $ or `, a $
// or ` inside double quotes (they would expand in a shell), an unquoted
// newline (a script, not a command), an unterminated quote or trailing
// backslash, and a task with no program. A task that needs a pipe, a list or an
// expansion names the program that provides it, for example
// bash -c 'make test && make lint', or runs a script file.
func splitSubprocessTask(task, home string) ([]string, error) {
	var (
		words   []string
		cur     strings.Builder
		inWord  bool
		runes   = []rune(task)
		refused = func(r rune, where string) error {
			return fmt.Errorf("subprocess task uses shell syntax (%q %s); a task is one program with arguments, "+
				"use bash -c '...' or a script file to get a shell", r, where)
		}
	)
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case ' ', '\t':
			flush()
		case '\n', '\r':
			return nil, refused(r, "outside quotes: a newline starts a second command")
		case '|', '&', ';', '<', '>', '(', ')', '$', '`':
			return nil, refused(r, "outside quotes")
		case '\\':
			i++
			if i >= len(runes) {
				return nil, errors.New("subprocess task ends with a backslash")
			}
			cur.WriteRune(runes[i])
			inWord = true
		case '\'':
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '\'' {
					end = j
					break
				}
			}
			if end < 0 {
				return nil, errors.New("subprocess task has an unterminated single quote")
			}
			cur.WriteString(string(runes[i+1 : end]))
			inWord = true
			i = end
		case '"':
			closed := false
			for i++; i < len(runes); i++ {
				c := runes[i]
				if c == '"' {
					closed = true
					break
				}
				if c == '$' || c == '`' {
					return nil, refused(c, "inside double quotes")
				}
				if c == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
					i++
					c = runes[i]
				}
				cur.WriteRune(c)
			}
			if !closed {
				return nil, errors.New("subprocess task has an unterminated double quote")
			}
			inWord = true
		case '~':
			if !inWord && home != "" && (i+1 == len(runes) || strings.ContainsRune("/ \t", runes[i+1])) {
				cur.WriteString(home)
			} else {
				cur.WriteRune(r)
			}
			inWord = true
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	if len(words) == 0 || words[0] == "" {
		return nil, errors.New("subprocess task has no program to run")
	}
	return words, nil
}
