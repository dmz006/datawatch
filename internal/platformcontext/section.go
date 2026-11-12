package platformcontext

import (
	"strconv"
	"strings"
)

const (
	beginPrefix = "<!-- datawatch:platform-context v"
	beginSuffix = " -->"
	endLine     = "<!-- /datawatch:platform-context -->"
)

// findSection locates a marked block. It returns the start of the begin line,
// the end of the begin line (after its newline), the start of the end line and
// the offset just past the end line and its newline. ok is false when no
// complete block exists.
func findSection(s string) (bStart, bodyStart, eStart, eEnd int, ok bool) {
	pos := 0
	for pos <= len(s) {
		i := strings.Index(s[pos:], beginPrefix)
		if i < 0 {
			return 0, 0, 0, 0, false
		}
		i += pos
		if i != 0 && s[i-1] != '\n' {
			pos = i + len(beginPrefix)
			continue
		}
		nl := strings.IndexByte(s[i:], '\n')
		if nl < 0 {
			return 0, 0, 0, 0, false
		}
		line := strings.TrimRight(s[i:i+nl], "\r")
		if !strings.HasSuffix(line, beginSuffix) {
			pos = i + len(beginPrefix)
			continue
		}
		bodyStart = i + nl + 1
		for p := bodyStart; p <= len(s); {
			e := strings.IndexByte(s[p:], '\n')
			var ln string
			next := len(s)
			if e < 0 {
				ln = s[p:]
			} else {
				ln = s[p : p+e]
				next = p + e + 1
			}
			if strings.TrimRight(ln, "\r") == endLine {
				return i, bodyStart, p, next, true
			}
			if e < 0 {
				break
			}
			p = next
		}
		return 0, 0, 0, 0, false
	}
	return 0, 0, 0, 0, false
}

func beginLine(version int) string {
	return beginPrefix + strconv.Itoa(version) + beginSuffix + "\n"
}

func block(body string, version int) string {
	return beginLine(version) + strings.TrimRight(body, "\n") + "\n" + endLine + "\n"
}

// UpsertSection inserts or updates the marked platform context section in
// existing. Text outside the markers is never modified.
func UpsertSection(existing string, body string, version int) string {
	want := block(body, version)
	bStart, _, _, eEnd, ok := findSection(existing)
	if !ok {
		if existing == "" {
			return want
		}
		sep := ""
		switch {
		case strings.HasSuffix(existing, "\n\n"):
		case strings.HasSuffix(existing, "\n"):
			sep = "\n"
		default:
			sep = "\n\n"
		}
		return existing + sep + want
	}
	cur := existing[bStart:eEnd]
	if cur == want {
		return existing
	}
	// A final section without a trailing newline keeps that property.
	if eEnd == len(existing) && !strings.HasSuffix(cur, "\n") {
		want = strings.TrimSuffix(want, "\n")
	}
	return existing[:bStart] + want + existing[eEnd:]
}

// RemoveSection deletes the marked block and the blank line before it. Text
// outside the block is otherwise unchanged.
func RemoveSection(existing string) string {
	bStart, _, _, eEnd, ok := findSection(existing)
	if !ok {
		return existing
	}
	head := existing[:bStart]
	if strings.HasSuffix(head, "\n\n") {
		head = head[:len(head)-1]
	}
	return head + existing[eEnd:]
}
