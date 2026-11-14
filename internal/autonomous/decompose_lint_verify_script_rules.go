// decompose_lint_verify_script_rules.go — the script-content checks behind
// the verify_script_rejects_own_files, verify_script_needs_later_task_output,
// verify_script_sh_for_bash, verify_script_pipe_grep_q and
// verify_script_hex_pattern decompose-lint rules. Each reads the task's
// verification script (plus the helpers it sources, one level deep — see
// verifyScriptContent) and reports the first offending line; a script that
// does not exist yet, is a command string or lies outside the project is
// skipped, as in the sibling script rules. The needs-later-task-output check
// compares the script against every other task of the plan, so it takes the
// plan-wide view built by newLintPlan.
package autonomous

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// scriptHit is one script-content finding: the repo-relative script path
// and the offending line of it (or of a sourced helper).
type scriptHit struct {
	Path string
	Line string
}

// scriptPredID is a bit per script-content predicate of a compiled match.
type scriptPredID uint8

const (
	scriptRejectsOwnFiles scriptPredID = 1 << iota
	scriptNeedsLaterOutput
	scriptShForBash
	scriptPipeGrepQ
	scriptHexPattern
)

// scriptPredicate binds a predicate bit to its check and to the finding
// text that describes the asserted state.
type scriptPredicate struct {
	id    scriptPredID
	note  string
	check func(dir string, t Task, plan *lintPlan) (scriptHit, bool)
}

// scriptPredicates lists the script-content predicates in evaluation order.
var scriptPredicates = []scriptPredicate{
	{scriptRejectsOwnFiles, "rejects files the task or the standard allowed set writes", verifyScriptRejectsOwnFiles},
	{scriptNeedsLaterOutput, "needs the output of a task it does not depend on", verifyScriptNeedsLaterTaskOutput},
	{scriptShForBash, "bash-only script run with sh", verifyScriptShForBash},
	{scriptPipeGrepQ, "pipe into grep -q", verifyScriptPipeGrepQ},
	{scriptHexPattern, "grep pattern matches CSS hex colours as tracker ids", verifyScriptHexPattern},
}

// lintPlan is the plan-wide view the cross-task checks need: every task
// of every story with its story, and the story dependency edges.
type lintPlan struct {
	tasks     []Task
	storyOf   map[string]string
	storyDeps map[string][]string
	byStory   map[string][]string
	byID      map[string]Task
}

// newLintPlan indexes a story list for plan-wide script checks.
func newLintPlan(stories []Story) *lintPlan {
	p := &lintPlan{
		storyOf:   map[string]string{},
		storyDeps: map[string][]string{},
		byStory:   map[string][]string{},
		byID:      map[string]Task{},
	}
	for _, s := range stories {
		p.storyDeps[s.ID] = s.DependsOn
		for _, t := range s.Tasks {
			p.tasks = append(p.tasks, t)
			p.storyOf[t.ID] = s.ID
			p.byStory[s.ID] = append(p.byStory[s.ID], t.ID)
			p.byID[t.ID] = t
		}
	}
	return p
}

// ancestors returns the IDs of every task t depends on, directly or
// through other tasks, and every task of the stories its story depends on.
func (p *lintPlan) ancestors(t Task) map[string]bool {
	out := map[string]bool{}
	var queue []string
	push := func(id string) {
		if id != "" && !out[id] {
			out[id] = true
			queue = append(queue, id)
		}
	}
	seenStory := map[string]bool{}
	var addStory func(sid string)
	addStory = func(sid string) {
		if seenStory[sid] {
			return
		}
		seenStory[sid] = true
		for _, id := range p.byStory[sid] {
			push(id)
		}
		for _, d := range p.storyDeps[sid] {
			addStory(d)
		}
	}
	for _, id := range t.DependsOn {
		push(id)
	}
	if sid := p.storyOf[t.ID]; sid != "" {
		for _, d := range p.storyDeps[sid] {
			addStory(d)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		o, ok := p.byID[id]
		if !ok {
			continue
		}
		for _, d := range o.DependsOn {
			push(d)
		}
		if sid := p.storyOf[id]; sid != "" {
			for _, d := range p.storyDeps[sid] {
				addStory(d)
			}
		}
	}
	return out
}

// scriptCodeLines returns the trimmed, non-blank, non-comment lines.
func scriptCodeLines(content string) []string {
	var out []string
	for _, l := range strings.Split(content, "\n") {
		s := strings.TrimSpace(l)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// normPlanFile cleans a planned/generated file for comparison.
func normPlanFile(f string) string {
	f = strings.TrimSpace(f)
	if f == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(f))
}

// taskOwnFiles is the set of files t plans or generates, plus its script.
func taskOwnFiles(t Task) map[string]bool {
	own := map[string]bool{}
	for _, list := range [][]string{t.FilesPlanned, t.FilesGenerated, {t.VerificationScript}} {
		for _, f := range list {
			if n := normPlanFile(f); n != "" {
				own[n] = true
			}
		}
	}
	return own
}

// --- (a) verify_script_rejects_own_files ---------------------------------

var (
	// allowedAssignRe: an assignment of an allowed-file list variable.
	allowedAssignRe = regexp.MustCompile(`(?i)^(?:(?:local|declare|readonly|export)(?:\s+-[a-zA-Z]+)?\s+)?\w*(?:allowed|permitted|allowlist|whitelist|own_files|task_files|only_files|expected_files)\w*=(.*)$`)
	// fileLikeRe: text that names files (a path or a name.ext).
	fileLikeRe = regexp.MustCompile(`[\w.*-]+/|[\w*-]+\.[A-Za-z0-9*]{1,6}\b`)
	// rejectPipeRe: a changed-file listing filtered by grep -v.
	rejectPipeRe = regexp.MustCompile(`(?i)git\s+(?:diff|status|ls-files|diff-tree)\b[^\n]*\|\s*(?:e|f)?grep\s+(?:-\w+\s+)*-\w*v\w*\b(.*)$`)
	// caseFileRe: a case over a changed-file variable.
	caseFileRe = regexp.MustCompile(`(?i)^case\s+["']?\$\{?(?:f|p|file|path|changed|name|entry|line)\w*\}?["']?\s+in\b`)
	// failRe: a failing construct.
	failRe = regexp.MustCompile(`(?i)\bexit\b|\breturn\s+[1-9]|\bfail\w*\b|\bdie\b|\[\[?\s+-n\b|\btest\s+-n\b|\bgrep\s+-q\s+\.`)
	// sharedAllowedRe: a reference to the shared allowed list (a sourced
	// helper, an allowed-list file or an allowed-list function).
	sharedAllowedRe = regexp.MustCompile(`(?i)(?:^|[\s;(])(?:source|\.)\s+["']?[^\s"';]*allowed[^\s"';]*|[\w./-]*/[\w.-]*allowed[\w.-]*\.(?:sh|txt|ya?ml|json)|\b(?:is_allowed\w*|check_allowed\w*|allowed_files_check)\b`)
)

// standardAllowedWords is the standard allowed set: a literal list must
// name each (changelog, README, docs, locales, tests).
var standardAllowedWords = []string{"changelog", "readme", "docs", "locale", "test"}

// listTokens splits list text into path-ish tokens.
func listTokens(list string) []string {
	return strings.FieldsFunc(list, func(r rune) bool {
		return strings.ContainsRune(" \t\n|()\"'`;,[]{}=", r)
	})
}

// listCovers reports whether the literal list names file: verbatim, by a
// glob token, or by a directory prefix token.
func listCovers(list, file string) bool {
	if strings.Contains(list, file) {
		return true
	}
	for _, tok := range listTokens(list) {
		switch {
		case strings.Contains(tok, "*"):
			re := "^" + strings.ReplaceAll(regexp.QuoteMeta(tok), `\*`, ".*") + "$"
			if ok, _ := regexp.MatchString(re, file); ok {
				return true
			}
		case strings.HasSuffix(tok, "/") && strings.HasPrefix(file, tok):
			return true
		}
	}
	return false
}

// allowedListsIn extracts the literal allowed-file lists of a script as
// (text, offending line) pairs: allowed-list assignments, changed-file
// listings filtered by grep -v that then fail, and case blocks over a
// changed-file variable with a failing catch-all.
func allowedListsIn(content string) [][2]string {
	var raw []string
	for _, l := range strings.Split(content, "\n") {
		raw = append(raw, strings.TrimSpace(l))
	}
	var out [][2]string
	fileCheck := verifyFileCheckRe.MatchString(content)
	for i := 0; i < len(raw); i++ {
		l := raw[i]
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if m := allowedAssignRe.FindStringSubmatch(l); m != nil && fileCheck {
			text := m[1]
			if open := openerOf(text); open != "" {
				for j := i + 1; j < len(raw); j++ {
					text += " " + raw[j]
					if strings.Contains(raw[j], open) {
						break
					}
				}
			}
			if fileLikeRe.MatchString(text) {
				out = append(out, [2]string{text, l})
			}
			continue
		}
		if m := rejectPipeRe.FindStringSubmatch(l); m != nil {
			tail := l
			for j := i + 1; j < len(raw) && j <= i+3; j++ {
				tail += "\n" + raw[j]
			}
			if failRe.MatchString(tail) && fileLikeRe.MatchString(m[1]) {
				out = append(out, [2]string{m[1], l})
			}
			continue
		}
		if caseFileRe.MatchString(l) {
			var patterns []string
			failing := false
			for j := i + 1; j < len(raw) && !strings.HasPrefix(raw[j], "esac"); j++ {
				if strings.HasPrefix(raw[j], "*)") {
					failing = failRe.MatchString(raw[j]) || (j+1 < len(raw) && failRe.MatchString(raw[j+1]))
					break
				}
				patterns = append(patterns, raw[j])
			}
			text := strings.Join(patterns, " ")
			if failing && fileLikeRe.MatchString(text) {
				out = append(out, [2]string{text, l})
			}
		}
	}
	return out
}

// openerOf returns the closer of a multi-line value opened on one line
// (an array or an unclosed quote), or "".
func openerOf(val string) string {
	switch {
	case strings.HasPrefix(val, "(") && !strings.Contains(val, ")"):
		return ")"
	case strings.HasPrefix(val, "'") && strings.Count(val, "'")%2 == 1:
		return "'"
	case strings.HasPrefix(val, `"`) && strings.Count(val, `"`)%2 == 1:
		return `"`
	}
	return ""
}

// verifyScriptRejectsOwnFiles returns the script and offending line when
// its allowed-file list is built only from literals (no shared allowed
// list) and omits a file the task plans or a word of the standard allowed
// set (changelog, README, docs, locales, tests).
func verifyScriptRejectsOwnFiles(dir string, t Task, _ *lintPlan) (scriptHit, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || sharedAllowedRe.MatchString(content) {
		return scriptHit{}, false
	}
	for _, pair := range allowedListsIn(content) {
		list := pair[0]
		low := strings.ToLower(list)
		missing := false
		for _, f := range t.FilesPlanned {
			if n := normPlanFile(f); n != "" && !listCovers(list, n) {
				missing = true
			}
		}
		for _, w := range standardAllowedWords {
			if !strings.Contains(low, w) {
				missing = true
			}
		}
		if missing {
			return scriptHit{Path: path, Line: pair[1]}, true
		}
	}
	return scriptHit{}, false
}

// --- (b) verify_script_needs_later_task_output ---------------------------

var (
	pathTestRe = regexp.MustCompile(`(?:^|[\s;&|(!])(?:\[\[?|test)\s+-[fesdr]\s+["']?([^\s"'\]);&|$` + "`" + `]+)`)
	grepWordRe = regexp.MustCompile(`(?:^|[\s;&|(!` + "`" + `])(?:e|f)?grep\b(.*)$`)
)

// shellTokens splits a command tail into tokens (quotes honoured, quotes
// removed) up to the first shell operator.
func shellTokens(s string) []string {
	var toks []string
	var cur strings.Builder
	var quote rune
	has := false
	flush := func() {
		if has {
			toks = append(toks, cur.String())
		}
		cur.Reset()
		has = false
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, has = r, true
		case r == ' ' || r == '\t':
			flush()
		case r == '|' || r == ';' || r == '&' || r == ')' || r == '>' || r == '<':
			flush()
			return toks
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	return toks
}

// grepArgs splits a grep tail into its pattern and file operands.
func grepArgs(tail string) (pattern string, files []string) {
	toks := shellTokens(tail)
	patSet := false
	for i := 0; i < len(toks); i++ {
		tk := toks[i]
		switch {
		case tk == "-e" && i+1 < len(toks):
			if !patSet {
				pattern, patSet = toks[i+1], true
			}
			i++
		case tk == "-f" || tk == "-m" || tk == "-A" || tk == "-B" || tk == "-C" || tk == "--include" || tk == "--exclude":
			i++
		case strings.HasPrefix(tk, "-") && tk != "-":
		case !patSet:
			pattern, patSet = tk, true
		default:
			files = append(files, tk)
		}
	}
	return pattern, files
}

// scriptRequirement is one file or phrase a script requires.
type scriptRequirement struct {
	file   string
	phrase string
	line   string
}

// scriptRequirements extracts the path tests and grep targets of a script.
func scriptRequirements(content string) []scriptRequirement {
	var out []scriptRequirement
	for _, l := range scriptCodeLines(content) {
		for _, m := range pathTestRe.FindAllStringSubmatch(l, -1) {
			out = append(out, scriptRequirement{file: m[1], line: l})
		}
		if m := grepWordRe.FindStringSubmatch(l); m != nil {
			pat, files := grepArgs(m[1])
			for _, f := range files {
				out = append(out, scriptRequirement{file: f, phrase: pat, line: l})
			}
		}
	}
	return out
}

// plainFile reports whether f is a literal repo-relative file name (no
// variable, glob, absolute or parent path).
func plainFile(f string) bool {
	if f == "" || strings.ContainsAny(f, "$*?[`~") || filepath.IsAbs(f) || strings.HasPrefix(f, "..") {
		return false
	}
	return true
}

// verifyScriptNeedsLaterTaskOutput returns the script and line when it
// requires a file (path test, grep target) or a phrase only a task it does
// not depend on creates. Needs the plan; with none it never fires.
func verifyScriptNeedsLaterTaskOutput(dir string, t Task, plan *lintPlan) (scriptHit, bool) {
	if plan == nil {
		return scriptHit{}, false
	}
	path, content, ok := verifyScriptContent(dir, t)
	if !ok {
		return scriptHit{}, false
	}
	own := taskOwnFiles(t)
	anc := plan.ancestors(t)
	for _, rq := range scriptRequirements(content) {
		if !plainFile(rq.file) {
			continue
		}
		f := normPlanFile(rq.file)
		existing, onDisk := readProjectFile(dir, f)
		if onDisk && (rq.phrase == "" || strings.Contains(existing, rq.phrase)) {
			continue
		}
		for _, o := range plan.tasks {
			if o.ID == t.ID || anc[o.ID] {
				continue
			}
			creates := false
			for _, list := range [][]string{o.FilesPlanned, o.FilesGenerated} {
				for _, of := range list {
					if normPlanFile(of) == f {
						creates = true
					}
				}
			}
			if !creates {
				continue
			}
			switch {
			case !own[f] && !onDisk:
				return scriptHit{Path: path, Line: rq.line}, true
			case own[f] && rq.phrase != "" && len(rq.phrase) >= 6 &&
				strings.Contains(o.Spec, rq.phrase) && !strings.Contains(t.Spec, rq.phrase):
				return scriptHit{Path: path, Line: rq.line}, true
			}
		}
	}
	return scriptHit{}, false
}

// --- (c) verify_script_sh_for_bash ---------------------------------------

var (
	shShebangRe   = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+(?:-\S+\s+)*)?(?:/\S*/)?(?:sh|dash|ash)(?:\s|$)`)
	bashShebangRe = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+(?:-\S+\s+)*)?(?:/\S*/)?bash(?:\s|$)`)
	// bashOnlyRe: bash-only syntax: [[ ]], arrays, process substitution,
	// pipefail, mapfile.
	bashOnlyRe = regexp.MustCompile(`(?:^|[\s;&|(!])\[\[\s|(?:^|[\s;&|(])(?:declare|local|typeset)\s+-[aA]\b|(?:^|[\s;&|(])\w+=\(|(?:^|[\s<>=])[<>]\(|\$\{\w+\[[@*0-9]|\bpipefail\b|(?:^|[\s;&|(])(?:mapfile|readarray)\b`)
	// runWithShRe: a line running a script with sh.
	runWithShRe = regexp.MustCompile(`(?:^|[\s;&|(` + "`" + `])sh\s+(?:-[A-Za-z]+\s+)*["']?([^\s"';&|)` + "`" + `]+)`)
)

// firstBashOnlyLine returns the first code line using bash-only syntax.
func firstBashOnlyLine(content string) string {
	for _, l := range scriptCodeLines(content) {
		if bashOnlyRe.MatchString(l) {
			return l
		}
	}
	return ""
}

// scriptFirstLine returns the first line of s.
func scriptFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// isBashScript reports whether text declares a bash shebang or uses
// bash-only syntax.
func isBashScript(text string) bool {
	return bashShebangRe.MatchString(scriptFirstLine(text)) || firstBashOnlyLine(text) != ""
}

// verifyScriptShForBash returns the script and line when the script uses
// bash-only syntax under an sh shebang, or the spec (or a script line)
// runs a bash script with sh.
func verifyScriptShForBash(dir string, t Task, _ *lintPlan) (scriptHit, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok {
		return scriptHit{}, false
	}
	if shShebangRe.MatchString(scriptFirstLine(content)) {
		if l := firstBashOnlyLine(content); l != "" {
			return scriptHit{Path: path, Line: l}, true
		}
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	check := func(line string) bool {
		for _, m := range runWithShRe.FindAllStringSubmatch(line, -1) {
			target := filepath.ToSlash(filepath.Clean(m[1]))
			if target == clean {
				if isBashScript(content) {
					return true
				}
				continue
			}
			if strings.HasSuffix(target, ".sh") && plainFile(target) {
				if text, ok := readProjectFile(dir, target); ok && isBashScript(text) {
					return true
				}
			}
		}
		return false
	}
	for _, l := range strings.Split(t.Spec, "\n") {
		if check(l) {
			return scriptHit{Path: path, Line: strings.TrimSpace(l)}, true
		}
	}
	for _, l := range scriptCodeLines(content) {
		if check(l) {
			return scriptHit{Path: path, Line: l}, true
		}
	}
	return scriptHit{}, false
}

// --- (d) verify_script_pipe_grep_q ---------------------------------------

// pipeGrepQRe: a single pipe (not ||) into grep with -q / --quiet.
var pipeGrepQRe = regexp.MustCompile(`(?:^|[^|])\|\s*(?:e|f)?grep\b(?:\s+-[A-Za-z0-9]+)*?\s+(?:-[A-Za-z0-9]*q[A-Za-z0-9]*|--quiet|--silent)\b`)

// verifyScriptPipeGrepQ returns the script and line of the first pipe into
// grep -q.
func verifyScriptPipeGrepQ(dir string, t Task, _ *lintPlan) (scriptHit, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok {
		return scriptHit{}, false
	}
	for _, l := range scriptCodeLines(content) {
		if pipeGrepQRe.MatchString(l) {
			return scriptHit{Path: path, Line: l}, true
		}
	}
	return scriptHit{}, false
}

// --- (e) verify_script_hex_pattern ---------------------------------------

var (
	hexGrepRe = regexp.MustCompile(`\b(?:e|f)?grep\b|\brg\b|\bag\b`)
	// bareHashDigitsRe: #<digits> as a pattern (\d, [0-9] or [[:digit:]]
	// under a quantifier).
	bareHashDigitsRe = regexp.MustCompile(`#(?:\\d|\[0-9\]|\[\[:digit:\]\])(?:\+|\*|\{\d+,?\d*\})`)
	// trackerWordBeforeRe: a tracker word shortly before the #.
	trackerWordBeforeRe = regexp.MustCompile(`(?i)\b(?:issues?|pr|pull request|tickets?|bugs?|fix(?:es|ed)?|close[sd]?|resolve[sd]?|refs?)[^#]{0,20}$`)
)

// verifyScriptHexPattern returns the script and line of the first grep
// pattern with a bare #<digits> run, which also matches CSS hex colours.
func verifyScriptHexPattern(dir string, t Task, _ *lintPlan) (scriptHit, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok {
		return scriptHit{}, false
	}
	for _, l := range scriptCodeLines(content) {
		if !hexGrepRe.MatchString(l) {
			continue
		}
		for _, loc := range bareHashDigitsRe.FindAllStringIndex(l, -1) {
			if !trackerWordBeforeRe.MatchString(l[:loc[0]]) {
				return scriptHit{Path: path, Line: l}, true
			}
		}
	}
	return scriptHit{}, false
}

// scriptPredsOf folds a match's script-content assertions into the bit set
// the compiled match carries.
func scriptPredsOf(m config.DecomposeLintMatch) scriptPredID {
	var ids scriptPredID
	if m.VerificationScriptRejectsOwnFiles {
		ids |= scriptRejectsOwnFiles
	}
	if m.VerificationScriptNeedsLaterTaskOutput {
		ids |= scriptNeedsLaterOutput
	}
	if m.VerificationScriptShForBash {
		ids |= scriptShForBash
	}
	if m.VerificationScriptPipeGrepQ {
		ids |= scriptPipeGrepQ
	}
	if m.VerificationScriptHexPattern {
		ids |= scriptHexPattern
	}
	return ids
}
