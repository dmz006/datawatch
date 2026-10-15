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
	return renderScopeBlock(prd.ReadDirs, prd.ProjectDir, EffectiveWriteDirs(prd))
}

func renderScopeBlock(readDirs []string, projectDir string, writeDirs []string) string {
	var b strings.Builder
	b.WriteString("SCOPE (hard boundary):\n")
	fmt.Fprintf(&b, "- Project directory: %s\n", projectDir)
	fmt.Fprintf(&b, "- You may WRITE only under: %s\n", strings.Join(writeDirs, ", "))
	if ro := readOnlyDirs(readDirs, writeDirs); len(ro) > 0 {
		fmt.Fprintf(&b, "- You may READ (never modify) under: %s\n", strings.Join(ro, ", "))
	}
	b.WriteString("- Do not read, write or list anything outside these directories.\n")
	b.WriteString("- All output files, notes and checkpoints go under the project directory, using paths relative to it.\n")
	return b.String()
}

func readOnlyDirs(readDirs, writeDirs []string) []string {
	var out []string
	for _, d := range cleanDirs(readDirs) {
		if !PathInDirs(d, writeDirs) {
			out = append(out, d)
		}
	}
	return out
}

var absPathRe = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])(/[A-Za-z0-9_.-]+/[^\s"'` + "`" + `),;:]*)`)

var fsRoots = map[string]bool{"home": true, "etc": true, "var": true, "usr": true, "opt": true, "tmp": true, "root": true, "mnt": true, "srv": true}

// execDirs are read-only executable directories. A spec that names a binary
// directly inside one (e.g. /usr/bin/time) is invoking a tool, not claiming
// file access, so the spec-text lint skips it unless a write verb precedes it
// (BL421).
var execDirs = map[string]bool{
	"/bin": true, "/sbin": true,
	"/usr/bin": true, "/usr/sbin": true,
	"/usr/local/bin":    true,
	"/opt/homebrew/bin": true,
}

// writeVerbRe matches words that turn a following path into a write target.
var writeVerbRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(?:write|writes|writing|create|creates|overwrite|overwrites|modify|modifies|edit|edits|delete|deletes|remove|removes|rm|mv|cp|install|installs|chmod|chown|tee|truncate)(?:$|[^A-Za-z])|>`)

// writeWindowWords is how many words before a path are checked for a write verb.
const writeWindowWords = 4

// isBinaryMention reports whether p names an executable directly inside an
// execDir and the text just before it (pre) carries no write verb.
func isBinaryMention(p, pre string) bool {
	if !execDirs[filepath.Dir(filepath.Clean(p))] {
		return false
	}
	words := strings.Fields(pre)
	if len(words) > writeWindowWords {
		words = words[len(words)-writeWindowWords:]
	}
	return !writeVerbRe.MatchString(strings.Join(words, " "))
}

func firstSegment(p string) string {
	p = strings.TrimPrefix(p, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i]
	}
	return p
}

// LintTaskScope returns human-readable problems with a task spec/file list
// that reference paths outside the PRD's declared directories.
//
// Planned files are write claims and are always checked against the writable
// directories. Spec prose is checked against the readable directories, except
// that merely naming a system binary (a path directly inside an execDirs
// entry, e.g. "fall back to /usr/bin/time -v") is not an access claim and is
// skipped, unless a write verb in the few words before it makes it a write
// target (BL421).
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
	for _, m := range absPathRe.FindAllStringSubmatchIndex(spec, -1) {
		p := strings.TrimRight(spec[m[2]:m[3]], ".")
		if seg := firstSegment(p); !fsRoots[seg] && seg != firstSegment(prd.ProjectDir) {
			continue
		}
		if isBinaryMention(p, spec[:m[2]]) {
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

// EffectiveTaskWriteDirs returns the directories a task's worker may modify.
// Declared PRD WriteDirs always win; otherwise a task ProjectDir override
// narrows/moves the write scope to that dir; otherwise it is exactly
// EffectiveWriteDirs(prd). BL447.
func EffectiveTaskWriteDirs(prd *PRD, t *Task) []string {
	if len(prd.WriteDirs) > 0 {
		return EffectiveWriteDirs(prd)
	}
	if dir := ResolveTaskProjectDir(prd, t); dir != "" && dir != prd.ProjectDir {
		return cleanDirs([]string{dir})
	}
	return EffectiveWriteDirs(prd)
}

// EffectiveTaskReadDirs is EffectiveReadDirs with the task-effective write dirs.
func EffectiveTaskReadDirs(prd *PRD, t *Task) []string {
	return cleanDirs(append(append([]string{}, prd.ReadDirs...), EffectiveTaskWriteDirs(prd, t)...))
}

// TaskScopeBlock renders ScopeBlock for one task, using the task's resolved
// project dir and task-effective write dirs. Identical to ScopeBlock(prd)
// when the task has no ProjectDir override.
func TaskScopeBlock(prd *PRD, t *Task) string {
	return renderScopeBlock(prd.ReadDirs, ResolveTaskProjectDir(prd, t), EffectiveTaskWriteDirs(prd, t))
}

// ValidateTaskProjectDir checks a per-task ProjectDir override: it must be an
// absolute path, not the filesystem root or a top-level system dir, and must
// sit inside the PRD's declared WriteDirs when there are any.
func ValidateTaskProjectDir(prd *PRD, dir string) error {
	if err := ValidateScopeDirs([]string{dir}); err != nil {
		return err
	}
	clean := filepath.Clean(strings.TrimSpace(dir))
	if clean == "/" || (filepath.Dir(clean) == "/" && fsRoots[strings.TrimPrefix(clean, "/")]) {
		return fmt.Errorf("directory %q is a filesystem root or system directory", dir)
	}
	if prd != nil && len(prd.WriteDirs) > 0 && !PathInDirs(clean, EffectiveWriteDirs(prd)) {
		return fmt.Errorf("directory %q is outside the PRD write dirs %s", dir, strings.Join(EffectiveWriteDirs(prd), ", "))
	}
	return nil
}
