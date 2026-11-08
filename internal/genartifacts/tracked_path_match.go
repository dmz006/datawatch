package genartifacts

import (
	"path"
	"regexp"
	"strings"
)

// idPattern matches one non-empty run of the characters safeID keeps
// (ASCII letters, digits, '-', '_', '.'), never a slash.
const idPattern = `[A-Za-z0-9._-]+`

// trackedPathPattern builds an anchored regular expression for a tracked
// kind's default path: every split of the template on IDPlaceholder is
// literal-quoted with regexp.QuoteMeta, and each placeholder segment is
// matched by idPattern, so the whole path must match. A template that
// carries no placeholder cannot be for a specific automaton and yields an
// empty pattern.
func trackedPathPattern(tpl string) string {
	parts := strings.Split(tpl, IDPlaceholder)
	if len(parts) < 2 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`^`)
	for i, part := range parts {
		if i > 0 {
			sb.WriteString(idPattern)
		}
		sb.WriteString(regexp.QuoteMeta(part))
	}
	sb.WriteString(`$`)
	return sb.String()
}

// MatchesTrackedPath reports whether rel, a repository-relative path with
// forward slashes, is the tracked path of one of the generated kinds for
// some automaton id.
//
// A match is judged against the kind's entry of DefaultPaths() read at call
// time, with the IDPlaceholder segment replaced by one non-empty run of the
// characters safeID keeps (ASCII letters, digits, '-', '_', '.'), never a
// slash. An empty path, an absolute path, a path containing "..", and a
// path not in clean forward-slash form (path.Clean(rel) != rel) all yield
// false.
func MatchesTrackedPath(rel string) bool {
	if rel == "" || rel == "." {
		return false
	}
	if path.Clean(rel) != rel || strings.Contains(rel, "..") {
		return false
	}
	for _, kind := range Kinds() {
		pattern := trackedPathPattern(DefaultPaths()[kind])
		if pattern == "" {
			continue
		}
		if regexp.MustCompile(pattern).MatchString(rel) {
			return true
		}
	}
	return false
}
