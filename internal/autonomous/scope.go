package autonomous

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// EffectiveWriteDirs returns the directories workers may modify: the PRD's
// WriteDirs, or its ProjectDir when none are declared.
func EffectiveWriteDirs(prd *PRD) []string {
	if len(prd.WriteDirs) > 0 {
		return cleanDirs(prd.WriteDirs)
	}
	if prd.ProjectDir != "" {
		return cleanDirs([]string{prd.ProjectDir})
	}
	return nil
}

// EffectiveReadDirs returns every directory workers may read: ReadDirs plus
// all write dirs.
func EffectiveReadDirs(prd *PRD) []string {
	return cleanDirs(append(append([]string{}, prd.ReadDirs...), EffectiveWriteDirs(prd)...))
}

func cleanDirs(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, d := range in {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		d = filepath.Clean(d)
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// PathInDirs reports whether an absolute path is at or under one of dirs.
func PathInDirs(p string, dirs []string) bool {
	p = filepath.Clean(p)
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ValidateScopeDirs requires every entry to be an absolute path.
func ValidateScopeDirs(dirs []string) error {
	for _, d := range dirs {
		if !filepath.IsAbs(strings.TrimSpace(d)) {
			return fmt.Errorf("directory %q must be an absolute path", d)
		}
	}
	return nil
}

// ScopeBlock renders the boundary contract shown to the planner and workers.
func ScopeBlock(prd *PRD) string {
	var b strings.Builder
	b.WriteString("SCOPE (hard boundary):\n")
	fmt.Fprintf(&b, "- Project directory: %s\n", prd.ProjectDir)
	fmt.Fprintf(&b, "- You may WRITE only under: %s\n", strings.Join(EffectiveWriteDirs(prd), ", "))
	if ro := onlyRead(prd); len(ro) > 0 {
		fmt.Fprintf(&b, "- You may READ (never modify) under: %s\n", strings.Join(ro, ", "))
	}
	b.WriteString("- Do not read, write or list anything outside these directories.\n")
	b.WriteString("- All output files, notes and checkpoints go under the project directory, using paths relative to it.\n")
	return b.String()
}

func onlyRead(prd *PRD) []string {
	var out []string
	for _, d := range cleanDirs(prd.ReadDirs) {
		if !PathInDirs(d, EffectiveWriteDirs(prd)) {
			out = append(out, d)
		}
	}
	return out
}

var absPathRe = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])(/[A-Za-z0-9_.-]+/[^\s"'` + "`" + `),;:]*)`)

var fsRoots = map[string]bool{"home": true, "etc": true, "var": true, "usr": true, "opt": true, "tmp": true, "root": true, "mnt": true, "srv": true}

func firstSegment(p string) string {
	p = strings.TrimPrefix(p, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i]
	}
	return p
}

// LintTaskScope returns human-readable problems with a task spec/file list
// that reference paths outside the PRD's declared directories.
func LintTaskScope(prd *PRD, label, spec string, files []string) []string {
	read := EffectiveReadDirs(prd)
	write := EffectiveWriteDirs(prd)
	var out []string
	for _, f := range files {
		switch {
		case filepath.IsAbs(f):
			if !PathInDirs(f, write) {
				out = append(out, fmt.Sprintf("%s: planned file %s is outside the writable directories", label, f))
			}
		case strings.HasPrefix(filepath.Clean(f), ".."):
			out = append(out, fmt.Sprintf("%s: planned file %s escapes the project directory", label, f))
		}
	}
	for _, m := range absPathRe.FindAllStringSubmatch(spec, -1) {
		p := strings.TrimRight(m[1], ".")
		if seg := firstSegment(p); !fsRoots[seg] && seg != firstSegment(prd.ProjectDir) {
			continue
		}
		if !PathInDirs(p, read) {
			out = append(out, fmt.Sprintf("%s: spec references %s outside the allowed directories", label, p))
		}
	}
	return out
}

// LintPlanScope lints every story and task of a decomposed plan.
func LintPlanScope(prd *PRD, stories []Story) []string {
	var out []string
	for _, s := range stories {
		out = append(out, LintTaskScope(prd, "story '"+s.Title+"'", s.Description, s.FilesPlanned)...)
		for _, t := range s.Tasks {
			out = append(out, LintTaskScope(prd, "task '"+t.Title+"'", t.Spec, t.FilesPlanned)...)
		}
	}
	return out
}
