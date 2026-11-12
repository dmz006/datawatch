package mergeshadow

import (
	"regexp"
	"strings"
)

// ChangelogTaskKind is the Task.Kind marker that identifies an Automaton's own
// changelog entry task. autonomous marks that task with this kind when it
// builds the shared tasks of a batch, so the reader matches the marker and
// never the task title. The value must equal autonomous.DefectTaskKindChangelog;
// this package cannot import autonomous (autonomous imports it), so the
// equality is checked by a test in the autonomous package.
const ChangelogTaskKind = "defect_changelog"

// ChangelogTask is the part of one Automaton task the changelog reader needs.
// Output is the text the task produced; the reader takes the changelog text
// from it and from nothing else.
type ChangelogTask struct {
	ID     string
	Kind   string
	Output string
}

// ChangelogEntry is the changelog text read from the entry task. Source is the
// entry task id, or ChangelogSourceMissing when no usable entry was found. In
// that case Heading and Body are empty: nothing is invented.
type ChangelogEntry struct {
	Source  string
	Heading string
	Body    string
}

// changelogHeadingRe matches the heading form the CHANGELOG.md entries use for
// a version and date: "## [<version>] - <YYYY-MM-DD>".
var changelogHeadingRe = regexp.MustCompile(`^## \[([^\[\]\s]+)\] - (\d{4}-\d{2}-\d{2})$`)

// ReadChangelogEntry returns the changelog entry produced by the first task
// that carries the changelog marker and whose output holds a well-formed entry.
// A task with no marker, no output, a malformed heading or an empty body is
// skipped. When no task yields an entry, the result has Source set to
// ChangelogSourceMissing and an empty Heading and Body.
//
// The heading is kept as written. The body is every line after the heading up
// to the next "## " line, with only leading and trailing blank lines removed.
func ReadChangelogEntry(tasks []ChangelogTask) ChangelogEntry {
	for _, t := range tasks {
		if t.Kind != ChangelogTaskKind || t.ID == "" {
			continue
		}
		heading, body, ok := parseChangelogEntry(t.Output)
		if !ok {
			continue
		}
		return ChangelogEntry{Source: t.ID, Heading: heading, Body: body}
	}
	return ChangelogEntry{Source: ChangelogSourceMissing}
}

// parseChangelogEntry splits out must start with a well-formed "## [v] - date"
// heading, and it must have a non-empty body after it.
func parseChangelogEntry(out string) (heading, body string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", "", false
	}
	heading = strings.TrimRight(lines[start], " \t")
	if !changelogHeadingRe.MatchString(heading) {
		return "", "", false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	body = trimBlankLines(lines[start+1 : end])
	if body == "" {
		return "", "", false
	}
	return heading, body, true
}

// trimBlankLines joins lines and drops leading and trailing whitespace-only
// lines. Lines inside the body are kept exactly as written.
func trimBlankLines(lines []string) string {
	first, last := 0, len(lines)
	for first < last && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	for last > first && strings.TrimSpace(lines[last-1]) == "" {
		last--
	}
	return strings.Join(lines[first:last], "\n")
}
