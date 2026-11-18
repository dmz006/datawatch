package mergeshadow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/genartifacts"
)

// ChangelogTaskKind is the Task.Kind marker that identifies an Automaton's own
// changelog entry task. autonomous marks that task with this kind when it
// builds the shared tasks of a batch, so the reader matches the marker and
// never the task title. The value must equal autonomous.DefectTaskKindChangelog;
// this package cannot import autonomous (autonomous imports it), so the
// equality is checked by a test in the autonomous package.
const ChangelogTaskKind = "defect_changelog"

// ChangelogTask is the part of one Automaton task the changelog reader needs.
// Output is the text the task produced; when the Automaton's changelog draft
// file does not yield a usable entry, the reader takes the changelog text
// from the first such task.
type ChangelogTask struct {
	ID     string
	Kind   string
	Output string
}

// ChangelogEntry is the changelog text the plan carries. Source names where
// it came from: the path of the Automaton's changelog draft file, when the
// draft was read, the id of the entry task, when its output was read, or
// ChangelogSourceMissing when no usable entry was found anywhere. In that
// last case Heading and Body are empty: nothing is invented.
type ChangelogEntry struct {
	Source  string
	Heading string
	Body    string
}

// changelogHeadingRe matches the heading form the CHANGELOG.md entries use for
// a version and date: "## [<version>] - <YYYY-MM-DD>".
var changelogHeadingRe = regexp.MustCompile(`^## \[([^\[\]\s]+)\] - (\d{4}-\d{2}-\d{2})$`)

// ReadChangelogEntry returns the changelog entry the plan carries. When the
// Automaton's changelog draft file (its tracked artifact path, read from
// repoPath) holds a well-formed entry — the same rules the entry task output
// is held to: a strict version heading plus a non-empty body — its heading and
// body are taken and Source is the draft file path. When the file is absent
// or does not parse, the entry produced by the first task that carries the
// changelog marker and whose output holds a well-formed entry is taken, and
// Source is that task id. A task with no marker, no output, a malformed
// heading or an empty body is skipped. When neither yields an entry, the
// result has Source set to ChangelogSourceMissing and an empty Heading and
// Body: nothing is invented.
//
// The heading is kept as written. The body is every line after the heading up
// to the next "## " line, with only leading and trailing blank lines removed.
func ReadChangelogEntry(automatonID, repoPath string, tasks []ChangelogTask) ChangelogEntry {
	if automatonID != "" && repoPath != "" {
		draftName := changelogDraftPath(automatonID)
		if b, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(draftName))); err == nil {
			if heading, body, ok := parseChangelogEntry(string(b)); ok {
				return ChangelogEntry{Source: draftName, Heading: heading, Body: body}
			}
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

// changelogDraftPath is the tracked artifact path of the Automaton's
// changelog draft file, relative to the project root: the generated-
// artifacts template path of the changelog draft kind, with the Automaton id
// in place of the placeholder.
func changelogDraftPath(automatonID string) string {
	tpl := genartifacts.DefaultPaths()[genartifacts.ChangelogDraft]
	return strings.ReplaceAll(tpl, genartifacts.IDPlaceholder, automatonID)
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
