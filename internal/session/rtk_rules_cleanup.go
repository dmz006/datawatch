package session

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Startup cleanup of datawatch blocks that an earlier daemon appended to a
// project's tracked AGENT.md / CLAUDE.md.
//
// Older versions wrote the rtk-instructions block, the cross-session
// communication rule and the memory section into the project's own rules file
// at session start, which left a modified tracked file in the worktree. Those
// blocks now go to the untracked CLAUDE.local.md (see WriteSessionGuardrails).
// CleanupRTKRulesFiles removes leftover blocks only when that is provably
// lossless: the file is tracked, shows a plain unstaged modification, and the
// file without some combination of those blocks is byte-identical to the
// committed version. Anything else is left exactly as it is and raised as one
// alert for the worktree, so the operator decides.

const (
	rtkBeginMarker   = "<!-- rtk-instructions -->"
	rtkEndMarker     = "<!-- /rtk-instructions -->"
	crossBeginMarker = "<!-- cross-session-communication-rule -->"
	crossEndMarker   = "<!-- /cross-session-communication-rule -->"
)

// rtkCleanupFiles are the rules files the cleanup inspects in each directory.
var rtkCleanupFiles = []string{"AGENT.md", "CLAUDE.md"}

// RTKGitRunner runs one git command in dir and returns its stdout. It exists so
// the cleanup can be tested without a real repository.
type RTKGitRunner interface {
	Git(dir string, args ...string) (string, error)
}

// RTKAlertSink receives the operator alert. It has the same shape as the
// session manager's alert function, which the daemon maps onto the alert store.
type RTKAlertSink func(level, title, body string)

// execGitRunner runs the real git binary.
type execGitRunner struct{}

func (execGitRunner) Git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return string(out), err
}

// RTKCleanupResult reports what one cleanup pass did.
type RTKCleanupResult struct {
	// Cleaned lists the files the leftover blocks were removed from.
	Cleaned []string
	// Unsafe lists the files that carry a leftover block but were left untouched.
	Unsafe []string
	// Alerts is the number of alerts raised (at most one per directory).
	Alerts int
}

// removeMarkedBlock removes the block that line-start begin/end markers
// delimit, together with the blank-line separator the append put before it. ok
// is false when the file has no complete block.
func removeMarkedBlock(s, begin, end string) (out string, ok bool) {
	b := -1
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], begin)
		if i < 0 {
			break
		}
		i += from
		if i == 0 || s[i-1] == '\n' {
			b = i
			break
		}
		from = i + len(begin)
	}
	if b < 0 {
		return s, false
	}
	e := strings.Index(s[b:], end)
	if e < 0 {
		return s, false
	}
	stop := b + e + len(end)
	start := b
	if strings.HasSuffix(s[:b], "\n\n") {
		start = b - 2
	}
	return s[:start] + s[stop:], true
}

// removeRTKBlock removes the rtk-instructions block (see removeMarkedBlock).
func removeRTKBlock(s string) (out string, ok bool) {
	return removeMarkedBlock(s, rtkBeginMarker, rtkEndMarker)
}

// removeCrossSessionBlock removes the cross-session communication rule block.
func removeCrossSessionBlock(s string) (out string, ok bool) {
	return removeMarkedBlock(s, crossBeginMarker, crossEndMarker)
}

// removeMemoryBlock removes the memory section exactly as the old append wrote
// it (a blank-line separator plus memoryInstructions()). The section has no
// markers, so only an exact match is removed; a hand-edited copy is not.
func removeMemoryBlock(s string) (out string, ok bool) {
	block := "\n\n" + memoryInstructions()
	i := strings.Index(s, block)
	if i < 0 {
		return s, false
	}
	return s[:i] + s[i+len(block):], true
}

// rtkLeftoverStrippers are the removals the cleanup may combine, one per block
// an old daemon appended.
var rtkLeftoverStrippers = []func(string) (string, bool){
	removeRTKBlock,
	removeCrossSessionBlock,
	removeMemoryBlock,
}

// leftoverCandidates returns every file content reachable by removing a
// non-empty subset of the blocks present in content, the all-blocks removal
// first. A block the committed version itself carries is removed in none of the
// candidates that equal HEAD, so it is kept.
func leftoverCandidates(content string) []string {
	var present []func(string) (string, bool)
	for _, f := range rtkLeftoverStrippers {
		if _, ok := f(content); ok {
			present = append(present, f)
		}
	}
	var out []string
	for mask := (1 << len(present)) - 1; mask >= 1; mask-- {
		cur := content
		for i, f := range present {
			if mask&(1<<i) != 0 {
				cur, _ = f(cur)
			}
		}
		out = append(out, cur)
	}
	return out
}

// hasLeftoverBlock reports whether content carries any block the cleanup knows.
func hasLeftoverBlock(content string) bool {
	if strings.Contains(content, rtkBeginMarker) {
		return true
	}
	for _, f := range rtkLeftoverStrippers {
		if _, ok := f(content); ok {
			return true
		}
	}
	return false
}

// rtkRemovalSafe decides whether name's leftover blocks may be removed from
// dir. It returns the cleaned content when safe. Otherwise it returns a reason,
// and quiet is true when the file is only a non-event (untracked or unmodified)
// and carries no RTK marker, so no alert is warranted.
func rtkRemovalSafe(runner RTKGitRunner, dir, name, content string) (cleaned string, reason string, quiet bool) {
	hasRTK := strings.Contains(content, rtkBeginMarker)
	if _, err := runner.Git(dir, "ls-files", "--error-unmatch", "--", name); err != nil {
		return "", "the file is not tracked by git", !hasRTK
	}
	status, err := runner.Git(dir, "status", "--porcelain", "--", name)
	if err != nil {
		return "", "git status failed: " + err.Error(), false
	}
	line := strings.TrimRight(status, "\r\n")
	if strings.Contains(line, "\n") {
		return "", "git status reports more than one entry for the file", false
	}
	if line == "" {
		return "", "the file has no plain unstaged modification (the block may be committed, or other changes are staged)", !hasRTK
	}
	if len(line) < 2 || line[0] != ' ' || line[1] != 'M' {
		return "", "the file has no plain unstaged modification (the block may be committed, or other changes are staged)", false
	}
	head, err := runner.Git(dir, "show", "HEAD:"+name)
	if err != nil {
		return "", "git show HEAD:" + name + " failed: " + err.Error(), false
	}
	if hasRTK {
		if _, ok := removeRTKBlock(content); !ok {
			return "", "the rtk-instructions block is incomplete", false
		}
	}
	for _, cand := range leftoverCandidates(content) {
		if cand == head {
			return cand, "", false
		}
	}
	return "", "removing the datawatch blocks would not restore the committed version (the file has other uncommitted changes)", false
}

// CleanupRTKRulesFiles inspects AGENT.md and CLAUDE.md in each directory and
// removes leftover rtk-instructions, cross-session and memory blocks only in
// the safe case described at the top of this file. For every directory that has at least one file it could
// not clean safely it raises exactly one alert through alert (nil = no alert)
// that names those files; the files are not modified. Missing directories and
// files, and files without the block, are skipped silently.
func CleanupRTKRulesFiles(dirs []string, runner RTKGitRunner, alert RTKAlertSink) RTKCleanupResult {
	if runner == nil {
		runner = execGitRunner{}
	}
	var res RTKCleanupResult
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true

		var unsafe []string
		var reasons []string
		for _, name := range rtkCleanupFiles {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(data)
			if !hasLeftoverBlock(content) {
				continue
			}
			cleaned, reason, quiet := rtkRemovalSafe(runner, dir, name, content)
			if quiet {
				continue
			}
			if reason == "" {
				mode := os.FileMode(0o644)
				if info, serr := os.Stat(path); serr == nil {
					mode = info.Mode().Perm()
				}
				if werr := os.WriteFile(path, []byte(cleaned), mode); werr != nil {
					reason = "write failed: " + werr.Error()
				} else {
					res.Cleaned = append(res.Cleaned, path)
					log.Printf("[session] rtk-cleanup: removed the leftover datawatch blocks from %s (restored the committed version)", path)
					continue
				}
			}
			res.Unsafe = append(res.Unsafe, path)
			unsafe = append(unsafe, name)
			reasons = append(reasons, fmt.Sprintf("%s: %s", name, reason))
		}

		if len(unsafe) == 0 {
			continue
		}
		log.Printf("[session] rtk-cleanup: left %s untouched in %s: %s", strings.Join(unsafe, ", "), dir, strings.Join(reasons, "; "))
		if alert != nil {
			alert("warn", "Datawatch block left in "+strings.Join(unsafe, " and "),
				fmt.Sprintf("%s still contains an old datawatch block (RTK, cross-session or memory instructions) that datawatch no longer writes. It was not removed automatically because removal is not provably lossless — %s. Remove the block by hand if you do not want it.",
					dir, strings.Join(reasons, "; ")))
			res.Alerts++
		}
	}
	return res
}

// CleanupRTKRulesFiles runs the startup cleanup over the distinct project
// directories of the stored sessions, using the real git binary and the
// manager's alert sink. It does nothing when rtk.write_rules_files is on,
// because the blocks are then intentionally present.
func (m *Manager) CleanupRTKRulesFiles() RTKCleanupResult {
	if m.cfg != nil && m.cfg.RTK.WriteRulesFiles {
		return RTKCleanupResult{}
	}
	var dirs []string
	for _, s := range m.store.List() {
		if s != nil && s.ProjectDir != "" {
			dirs = append(dirs, s.ProjectDir)
		}
	}
	return CleanupRTKRulesFiles(dirs, execGitRunner{}, RTKAlertSink(m.pushAlertFn))
}
