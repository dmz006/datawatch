package trace

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/git"
)

// SummaryMaxBytes bounds an argument summary in summary mode, further
// limited by tracing.arg_cap_bytes when that is smaller.
const SummaryMaxBytes = 120

// TruncationMarker ends a value that was cut to fit the cap.
const TruncationMarker = "...[truncated]"

var (
	// homePathRE matches home directories on Linux, macOS and Windows,
	// including the user name, plus the trailing path boundary.
	homePathRE = regexp.MustCompile(`(?:/home/[^/\s"'` + "`" + `:]+|/Users/[^/\s"'` + "`" + `:]+|/root|[A-Za-z]:\\Users\\[^\\\s"'` + "`" + `:]+)`)
)

// ScrubPaths rewrites private absolute paths in s: each root (for example
// a project directory) becomes a relative path, and any home directory
// becomes ~. Roots are applied longest first.
func ScrubPaths(s string, roots ...string) string {
	rs := make([]string, 0, len(roots))
	for _, r := range roots {
		r = strings.TrimRight(r, "/")
		if r != "" {
			rs = append(rs, r)
		}
	}
	// longest first so nested roots win
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && len(rs[j]) > len(rs[j-1]); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
	for _, r := range rs {
		s = replaceRoot(s, r)
	}
	return homePathRE.ReplaceAllString(s, "~")
}

// replaceRoot replaces root with a relative form, only at a path boundary.
func replaceRoot(s, root string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, root)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(root)
		if end < len(s) && s[end] != '/' && !isPathStop(s[end]) {
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		b.WriteString(s[:i])
		s = s[end:]
		if strings.HasPrefix(s, "/") {
			s = s[1:]
		} else {
			b.WriteString(".")
		}
	}
}

func isPathStop(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '"', '\'', '`', ':', ',', ')', ';':
		return true
	}
	return false
}

// truncate cuts s to at most max bytes (marker included) on a rune
// boundary.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= len(TruncationMarker) {
		return cutRunes(s, max)
	}
	return cutRunes(s, max-len(TruncationMarker)) + TruncationMarker
}

func cutRunes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// RedactText runs one text through secret redaction (the same detectors as
// git.secret_scan), path scrubbing and truncation to max bytes. Secrets are
// removed before truncation so a cut can never split a secret.
func RedactText(s string, max int, roots ...string) string {
	s, _ = git.RedactSecrets(s)
	s = ScrubPaths(s, roots...)
	return truncate(s, max)
}

// Redact returns a copy of ev that is safe to store. Mode off drops the
// argument summary; summary keeps a short redacted summary; full keeps a
// longer redacted one up to tracing.arg_cap_bytes. Outcome is redacted and
// scrubbed too. Redact must run before an event is stored.
func Redact(ev Event, s Settings) Event {
	return RedactIn(ev, s)
}

// RedactIn is Redact with project roots that are rewritten to relative
// paths before the home-directory scrub.
func RedactIn(ev Event, s Settings, roots ...string) Event {
	s = s.WithDefaults()
	out := ev
	switch s.Mode {
	case ModeOff:
		out.ArgSummary = ""
		out.Outcome = RedactText(ev.Outcome, SummaryMaxBytes, roots...)
		return out
	case ModeFull:
		out.ArgSummary = RedactText(ev.ArgSummary, s.ArgCapBytes, roots...)
		out.Outcome = RedactText(ev.Outcome, s.ArgCapBytes, roots...)
	default:
		limit := SummaryMaxBytes
		if s.ArgCapBytes < limit {
			limit = s.ArgCapBytes
		}
		out.ArgSummary = redactLine(ev.ArgSummary, limit, roots...)
		out.Outcome = redactLine(ev.Outcome, limit, roots...)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// redactLine redacts the whole text first, then keeps only its first line,
// so a multi-line secret is removed before any line is dropped.
func redactLine(s string, max int, roots ...string) string {
	s, _ = git.RedactSecrets(s)
	return truncate(firstLine(ScrubPaths(s, roots...)), max)
}
