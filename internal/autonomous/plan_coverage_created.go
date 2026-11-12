package autonomous

// Plan-coverage helpers that decide whether a plan's own text creates a
// file (so a FindingUnresolvedFile on that path was self-satisfied,
// not a gap). Pure logic; the only I/O is the read-only os.Stat in
// parentDirCreatable.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// createdFileVerbs are the creation verbs (matched as whole words,
// case-insensitively) that mark a clause as creating the file named in
// that clause. Shared by textCreatesFile and parentDirCreatable.
var createdFileVerbs = map[string]bool{
	"create": true, "creates": true, "created": true, "creating": true,
	"add": true, "adds": true, "added": true, "adding": true,
	"write": true, "writes": true, "wrote": true, "written": true, "writing": true,
	"generate": true, "generates": true, "generated": true, "generating": true,
}

// createdDirectoryWords name what is being created, next to a creation
// verb, in a clause that establishes a parent directory.
var createdDirectoryWords = map[string]bool{
	"directory": true, "folder": true, "dir": true,
	"mkdir": true, "package": true,
}

// createdClauseSplitRe splits text into clauses: on newlines, and on
// . ! ? ; followed by whitespace. A dot inside a file name (for example
// the extension dot of internal/x/plan.go) is not followed by
// whitespace, so it never splits a clause.
var createdClauseSplitRe = regexp.MustCompile(`\n|[.!?;]\s`)

// createdClauseHasVerb reports whether clause contains a whole-word,
// case-insensitive creation verb in an un-negated context. Reuses the
// termBoundaryOK / occurrenceNegated helpers from plan_coverage.go, so
// "do not create internal/x/never.go" and "never add docs/x.md" do not
// count.
func createdClauseHasVerb(clause string) bool {
	low := strings.ToLower(clause)
	for verb := range createdFileVerbs {
		i := 0
		for {
			off := strings.Index(low[i:], verb)
			if off < 0 {
				break
			}
			pos := i + off
			if termBoundaryOK(low, pos, len(verb)) && !occurrenceNegated(low, pos) {
				return true
			}
			i = pos + 1
		}
	}
	return false
}

// textCreatesFile reports whether text contains a clause that names path
// exactly AND, in that same clause, carries an un-negated creation verb
// (either order in the clause) or the phrase "new file".
func textCreatesFile(text, path string) bool {
	if text == "" || path == "" {
		return false
	}
	for _, clause := range createdClauseSplitRe.Split(text, -1) {
		if !strings.Contains(clause, path) {
			continue
		}
		if createdClauseHasVerb(clause) ||
			strings.Contains(strings.ToLower(clause), "new file") {
			return true
		}
	}
	return false
}

// plannedNewFile reports whether path appears in the FilesPlanned list
// of any task or any story in the plan: by exact match, or (when path
// itself has no slash) by base name, so "helper.go" matches a task
// listing "pkg/helper.go".
func plannedNewFile(stories []Story, path string) bool {
	if path == "" {
		return false
	}
	match := func(entry string) bool {
		if entry == path {
			return true
		}
		if !strings.Contains(path, "/") && filepath.Base(entry) == path {
			return true
		}
		return false
	}
	for i := range stories {
		for _, f := range stories[i].FilesPlanned {
			if f != "" && match(f) {
				return true
			}
		}
		for j := range stories[i].Tasks {
			for _, f := range stories[i].Tasks[j].FilesPlanned {
				if f != "" && match(f) {
					return true
				}
			}
		}
	}
	return false
}

// clauseCreatesDirectory reports whether clause names dirText (exact
// substring), carries an un-negated creation verb, and names the thing
// created as a directory/folder/dir/mkdir/package (whole word).
func clauseCreatesDirectory(clause, dirText string) bool {
	low := strings.ToLower(clause)
	if !strings.Contains(low, dirText) {
		return false
	}
	if !createdClauseHasVerb(low) {
		return false
	}
	for word := range createdDirectoryWords {
		i := 0
		for {
			off := strings.Index(low[i:], word)
			if off < 0 {
				break
			}
			pos := i + off
			if termBoundaryOK(low, pos, len(word)) {
				return true
			}
			i = pos + 1
		}
	}
	return false
}

// parentDirCreatable reports whether path can be created:
//
//   - a path with no slash (root level) is always creatable;
//   - the existing-directory case: baseDir joined with the parent of
//     path (the parent itself when path is absolute) is a directory;
//   - the plan-created case: some story or task title or spec has a
//     clause that names the parent directory together with an
//     un-negated creation verb and one of directory/folder/dir/
//     mkdir/package; or any FilesPlanned entry equals the parent with a
//     trailing slash.
//
// Otherwise false: a parent directory that does not exist and that no
// task creates means the path cannot be created. The only I/O in this
// package is the read-only os.Stat used here.
func parentDirCreatable(baseDir string, stories []Story, path string) bool {
	if path == "" || !strings.Contains(path, "/") {
		return true
	}
	parent := filepath.Dir(path)
	// The parent itself when it is absolute, otherwise baseDir joined
	// with it (filepath.Join would mangle an absolute parent).
	dir := parent
	if !filepath.IsAbs(parent) {
		dir = filepath.Join(baseDir, parent)
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return true
	}

	parentText := strings.ToLower(parent)
	parentSlash := parent + "/"
	checkText := func(text string) bool {
		for _, clause := range createdClauseSplitRe.Split(text, -1) {
			if clauseCreatesDirectory(clause, parentText) {
				return true
			}
		}
		return false
	}
	for i := range stories {
		s := &stories[i]
		if checkText(s.Title) || checkText(s.Description) {
			return true
		}
		for _, f := range s.FilesPlanned {
			if f == parentSlash {
				return true
			}
		}
		for j := range s.Tasks {
			t := &s.Tasks[j]
			if checkText(t.Title) || checkText(t.Spec) {
				return true
			}
			for _, f := range t.FilesPlanned {
				if f == parentSlash {
					return true
				}
			}
		}
	}
	return false
}
