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

// patternVerbRe matches the words that mark a quoted span as a search pattern
// (e.g. grep for '/home/x/.ssh') rather than a path the worker must touch.
var patternVerbRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(?:grep|pattern|patterns|scan|search|match|matches|forbidden)(?:$|[^A-Za-z])`)

// patternWindowWords is how many words before an opening quote are checked for a pattern verb.
const patternWindowWords = 5

// isPlaceholderPath reports whether p is template or glob syntax rather than a
// concrete location: an angle-bracket or curly-brace placeholder, a glob star,
// or a segment that is a shell variable ($NAME) or tilde-user form (~name).
func isPlaceholderPath(p string) bool {
	if strings.ContainsAny(p, "<>{}*") {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, "$") || strings.HasPrefix(seg, "~") {
			return true
		}
	}
	return false
}

// isPatternQuoted reports whether the token starting at pos sits inside a
// single-, double- or backtick-quoted span whose opening quote follows a
// pattern verb on the same line. A quote only opens when it does not follow a
// word character, so the apostrophe in "don't" is not taken for a quote.
func isPatternQuoted(spec string, pos int) bool {
	lineStart := strings.LastIndex(spec[:pos], "\n") + 1
	open := -1
	var quote byte
	for i := lineStart; i < pos; i++ {
		c := spec[i]
		if open < 0 {
			if isQuoteByte(c) && (i == lineStart || !isWordByte(spec[i-1])) {
				open, quote = i, c
			}
			continue
		}
		if c == quote && (i+1 >= len(spec) || !isWordByte(spec[i+1])) {
			open = -1
		}
	}
	if open < 0 {
		return false
	}
	return patternVerbRe.MatchString(lastWords(spec[lineStart:open], patternWindowWords))
}

func isQuoteByte(c byte) bool {
	return c == '\'' || c == '"' || c == '`'
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// lastWords returns the final n whitespace-separated words of s, joined by spaces.
func lastWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) > n {
		words = words[len(words)-n:]
	}
	return strings.Join(words, " ")
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
// target (BL421). Placeholder or glob syntax (/home/<user>, /home/{x}, $NAME,
// ~name, *) and paths inside a quoted pattern after a search verb (grep for
// '/home/x') are not access claims either.
func LintTaskScope(prd *PRD, label, spec string, files []string) []string {
	return lintScope(prd.ProjectDir, EffectiveReadDirs(prd), EffectiveWriteDirs(prd), label, spec, files)
}

// LintTaskScopeFor is LintTaskScope judged against the task's own resolved
// project dir and task-effective read/write dirs. Identical to
// LintTaskScope(prd, ...) when the task has no ProjectDir override. BL447.
func LintTaskScopeFor(prd *PRD, t *Task, label, spec string, files []string) []string {
	return lintScope(ResolveTaskProjectDir(prd, t), EffectiveTaskReadDirs(prd, t), EffectiveTaskWriteDirs(prd, t), label, spec, files)
}

func lintScope(projectDir string, read, write []string, label, spec string, files []string) []string {
	var out []string
	for _, f := range files {
		switch {
		case filepath.IsAbs(f):
			if !PathInDirs(f, write) {
				out = append(out, fmt.Sprintf("%s: planned file %s is outside the writable directories; remove it from the task's files or widen the PRD write dirs", label, f))
			}
		case strings.HasPrefix(filepath.Clean(f), ".."):
			out = append(out, fmt.Sprintf("%s: planned file %s escapes the project directory", label, f))
		}
	}
	for _, m := range absPathRe.FindAllStringSubmatchIndex(spec, -1) {
		p := strings.TrimRight(spec[m[2]:m[3]], ".")
		if seg := firstSegment(p); !fsRoots[seg] && seg != firstSegment(projectDir) {
			continue
		}
		if isPlaceholderPath(p) || isPatternQuoted(spec, m[2]) {
			continue
		}
		if isBinaryMention(p, spec[:m[2]]) {
			continue
		}
		if !PathInDirs(p, read) {
			line := strings.Count(spec[:m[2]], "\n") + 1
			out = append(out, fmt.Sprintf("%s: spec line %d references %s outside the allowed directories; edit the task spec to remove or quote it, or widen the PRD read dirs if the task must read it", label, line, p))
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
			out = append(out, LintTaskScopeFor(prd, &t, "task '"+t.Title+"'", t.Spec, t.FilesPlanned)...) // BL447
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

// scopeLintErrorPrefix is the Task.Error prefix the executor writes when the
// pre-spawn scope lint rejects a task (see executor.go, "scope: %s (task not
// spawned)").
const scopeLintErrorPrefix = "scope:"

// isScopeLintFailure reports whether t failed only because the pre-spawn scope
// lint rejected its spec, so the operator can fix the spec in place.
func isScopeLintFailure(t *Task) bool {
	return t != nil && t.Status == TaskFailed && strings.HasPrefix(t.Error, scopeLintErrorPrefix)
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
