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
// Output is the text the task produced.
type ChangelogTask struct {
	ID     string
	Kind   string
	Output string
}

// ChangelogDraft is the Automaton's tracked changelog draft file: Path is the
// file's tracked path relative to the project root and Content its text, ""
// when the file is absent. The reader takes precedence for it over any task
// output.
type ChangelogDraft struct {
	Path    string
	Content string
}

// ChangelogEntry is the changelog text the plan used. Source is the draft
// file path when the entry came from the file, the entry task id when it came
// from the task, or ChangelogSourceMissing when no usable entry was found.
// In the missing case Heading and Body are empty: nothing is invented.
type ChangelogEntry struct {
	Source  string
	Heading string
	Body    string
}

// changelogHeadingRe matches the heading form the CHANGELOG.md entries use for
// a version and date: "## [<version>] - <YYYY-MM-DD>".
var changelogHeadingRe = regexp.MustCompile(`^## \[([^\[\]\s]+)\] - (\d{4}-\d{2}-\d{2})$`)

// ReadChangelogEntry returns the changelog entry the plan should use. The
// draft file wins: when it holds a well-formed entry (the same rules the task
// output has, parseChangelogEntry), the heading and body come from the file
// and the Source is the file's path. When the file is absent or does not
// parse, the entry comes from the first task that carries the changelog
// marker and whose output holds a well-formed entry, and the Source is that
// task id. A task with no marker, no output, a malformed heading or an empty
// body is skipped. When neither yields an entry, the result has Source set
// to ChangelogSourceMissing and an empty Heading and Body.
//
// The heading is kept as written. The body is every line after the heading up
// to the next "## " line, with only leading and trailing blank lines removed.
func ReadChangelogEntry(draft ChangelogDraft, tasks []ChangelogTask) ChangelogEntry {
	if draft.Path != "" && draft.Content != "" {
		heading, body, ok := parseChangelogEntry(draft.Content)
		if ok {
			return ChangelogEntry{Source: draft.Path, Heading: heading, Body: body}
		}
	}
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
