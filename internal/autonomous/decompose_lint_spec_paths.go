// decompose_lint_spec_paths.go — the spec-line checks behind the
// read_only_task_writes_file and spec_path_outside_allowed_dirs
// decompose-lint rules. Both look at individual spec lines and return the
// offending line, so a finding can quote it (see excerptFor).
package autonomous

import (
	"path"
	"regexp"
	"strings"
)

var (
	// roWriteVerbRE is a verb that changes a file.
	roWriteVerbRE = regexp.MustCompile(`(?i)\b(?:write|writes|writing|create|creates|creating|edit|edits|editing|modify|modifies|modifying|update|updates|updating|append|appends|appending|overwrite|overwrites|overwriting|save|saves|saving|generate|generates|generating|produce|produces|producing)\b`)
	// roPathRE is a file target after such a verb: a path ending in a file
	// extension, a bare file name with a common extension, or a path
	// under a common repo directory (a directory counts: "a file under
	// docs/plans").
	roPathRE = regexp.MustCompile("(?:[A-Za-z0-9_.~-]+/)+[A-Za-z0-9_.-]*[A-Za-z0-9_-]\\.[A-Za-z0-9]{1,8}\\b" +
		"|\\b[A-Za-z0-9_-]+\\.(?:md|go|js|ts|json|ya?ml|toml|txt|sh|py|html|css|sql|csv|log)\\b" +
		"|\\b(?:docs|internal|cmd|src|pkg|scripts|tests?|examples|templates|charts)/[A-Za-z0-9_./-]*")
	// roNegationRE ends a line prefix that negates the verb that follows
	// ("do not write", "never create", "without editing").
	roNegationRE = regexp.MustCompile(`(?i)\b(?:not|never|no|without|don't|dont|cannot|can't|mustn't|avoid|nothing)\b[^\n.;]{0,40}$`)

	// specAbsPathRE is an absolute path under a system root. The caller
	// checks the characters around each hit (an absolute path starts a
	// token; it is not the tail of a URL or of a relative path).
	specAbsPathRE = regexp.MustCompile(`/(?:usr|opt|etc|var|home|Users|root|srv|mnt|media|tmp|bin|sbin|lib|lib32|lib64|proc|sys|boot|snap|private|Library|Applications|run|nix|dev)(?:/[A-Za-z0-9._+@%~-]+)*/?`)
)

// normSpecPath trims quoting and trailing punctuation from a path token
// and cleans it (a leading ./ is dropped).
func normSpecPath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "\"'`()[]<>,;:.")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// readOnlyTaskIsReadOnly reports whether t is read-only by its flag or
// by the READ-ONLY: marker opening its spec (the same two forms the
// read_only_unverifiable rule uses).
func readOnlyTaskIsReadOnly(t Task) bool {
	return t.ReadOnly || specIsReadOnly(t.Spec)
}

// readOnlyWriteLine returns the first spec line of a read-only task that
// tells it to write, create or edit a file other than its report path,
// or "" when the task is not read-only or has no such line. A line whose
// verb is negated ("do not write ...") is not an instruction to write.
func readOnlyWriteLine(t Task) string {
	if !readOnlyTaskIsReadOnly(t) {
		return ""
	}
	report := ""
	if rp := strings.TrimSpace(t.ReportPath); rp != "" {
		report = normSpecPath(rp)
	}
	usableReport := taskHasUsableReportPath(t)
	for _, line := range strings.Split(t.Spec, "\n") {
		// A line that names the report location is the one allowed write
		// (a report path with a space in it splits into path-like pieces
		// that must not be read as stray files).
		if usableReport && (strings.Contains(line, ".datawatch/reports/") || (report != "" && strings.Contains(line, report))) {
			continue
		}
		for _, v := range roWriteVerbRE.FindAllStringIndex(line, -1) {
			if roNegationRE.MatchString(line[:v[0]]) {
				continue
			}
			for _, tok := range roPathRE.FindAllString(line[v[1]:], -1) {
				p := normSpecPath(tok)
				if p == "" || (report != "" && p == report) {
					continue
				}
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

// specAbsolutePathOutside returns the first spec line that names an
// absolute path outside the project directory, with the path itself, or
// "" when there is none. Paths inside dir are ignored (an empty dir has
// no project tree, so none is inside it), as are repo-relative paths,
// the tail of a URL, a shebang interpreter and /dev paths.
func specAbsolutePathOutside(spec, dir string) (line, p string) {
	root := ""
	if d := strings.TrimSpace(dir); d != "" {
		root = path.Clean(d)
	}
	for _, l := range strings.Split(spec, "\n") {
		for _, loc := range specAbsPathRE.FindAllStringIndex(l, -1) {
			if loc[0] > 0 {
				prev := l[loc[0]-1]
				// A token start: not the tail of a relative path, a URL
				// host, a longer absolute path or a ../ prefix.
				if prev == '/' || prev == '.' || prev == '-' || prev == '_' || prev == '~' ||
					(prev >= '0' && prev <= '9') || (prev >= 'A' && prev <= 'Z') || (prev >= 'a' && prev <= 'z') {
					continue
				}
				if prev == '!' && loc[0] > 1 && l[loc[0]-2] == '#' {
					continue // shebang
				}
			}
			if loc[1] < len(l) {
				// The root name must end where the match ends ("/usrfoo").
				next := l[loc[1]]
				if (next >= '0' && next <= '9') || (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') {
					continue
				}
			}
			cand := normSpecPath(l[loc[0]:loc[1]])
			if cand == "" || cand == "/usr/bin/env" || cand == "/dev" || strings.HasPrefix(cand, "/dev/") {
				continue
			}
			if root != "" && (cand == root || strings.HasPrefix(cand, root+"/")) {
				continue
			}
			return strings.TrimSpace(l), cand
		}
	}
	return "", ""
}

// quoteSpecLine clips a spec line to the excerpt limit, keeping anchor
// (the offending token) visible when the line is long.
func quoteSpecLine(line, anchor string) string {
	line = strings.TrimSpace(line)
	r := []rune(line)
	if len(r) <= maxExcerptLen {
		return line
	}
	if i := strings.Index(line, anchor); anchor != "" && i > 0 {
		start := len([]rune(line[:i])) - 40
		if start < 0 {
			start = 0
		}
		if start > 0 {
			line = "…" + string(r[start:])
		}
	}
	return clipExcerpt(line)
}
