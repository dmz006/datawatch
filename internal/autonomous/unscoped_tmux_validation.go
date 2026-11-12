// Unscoped-tmux decompose gate. A task spec or verification script that
// runs `tmux kill-server` / `tmux kill-session -a` against the operator's
// live tmux server (the TMUX_TMPDIR kill-server incident) is rejected at
// decompose time. The rules mirror scripts/check-no-unscoped-tmux.sh; the
// patterns live here, in one place, for the Go side.
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// tmuxKillRE matches a tmux invocation that kills a server or all sessions.
	tmuxKillRE = regexp.MustCompile(`tmux(\s+[^|;&]*)?\s+(kill-server|kill-session(\s+[^|;&]*)?\s-[A-Za-z]*a)`)
	// tmuxThrowawaySockRE matches an explicit throwaway socket (-S ...).
	tmuxThrowawaySockRE = regexp.MustCompile(`-S\s*["']?\S*(mktemp|TMPDIR|/tmp/|\$S\b|\$\{S\}|\$SOCK\b|\$\{SOCK\}|\$sock\b|\$\{sock\})`)
	// tmuxScrubbedEnvRE matches `env [-flags] -u TMUX`.
	tmuxScrubbedEnvRE    = regexp.MustCompile(`env\s+(-[A-Za-z]+\s+)*-u\s+TMUX(\s|$)`)
	tmuxScrubbedTmpdirRE = regexp.MustCompile(`-u\s+TMUX_TMPDIR`)
)

// unscopedTmuxKill reports whether a single line kills a tmux server /
// all sessions without a throwaway socket or scrubbed environment.
func unscopedTmuxKill(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.Contains(line, "tmux-scope-ok") {
		return false
	}
	if !tmuxKillRE.MatchString(line) {
		return false
	}
	if tmuxThrowawaySockRE.MatchString(line) {
		return false
	}
	if tmuxScrubbedEnvRE.MatchString(line) {
		if strings.Contains(line, "TMUX_TMPDIR") && !tmuxScrubbedTmpdirRE.MatchString(line) {
			return true
		}
		return false
	}
	return true
}

// firstUnscopedTmuxLine returns the first offending line of text, 1-based.
func firstUnscopedTmuxLine(text string) (int, string, bool) {
	for i, raw := range strings.Split(text, "\n") {
		if unscopedTmuxKill(raw) {
			return i + 1, strings.TrimSpace(raw), true
		}
	}
	return 0, "", false
}

// validateNoUnscopedTmux scans story/task titles, descriptions and specs,
// plus any verification_script files that exist under projectDir, for
// unscoped tmux kills.
func validateNoUnscopedTmux(projectDir string, stories []Story) error {
	const guidance = "scope every `tmux kill-server` / `tmux kill-session -a` to a throwaway tmux: `tmux -S \"$(mktemp -d)/sock\" kill-server` or `env -u TMUX -u TMUX_TMPDIR tmux kill-server`; `TMUX_TMPDIR=... tmux kill-server` alone hits the live server"
	for _, s := range stories {
		if n, line, bad := firstUnscopedTmuxLine(s.Title + "\n" + s.Description); bad {
			return fmt.Errorf("story %q, line %d: unscoped tmux kill %q — %s", s.Title, n, line, guidance)
		}
		for _, t := range s.Tasks {
			if n, line, bad := firstUnscopedTmuxLine(t.Title + "\n" + t.Spec); bad {
				return fmt.Errorf("story %q, task %q spec, line %d: unscoped tmux kill %q — %s", s.Title, t.Title, n, line, guidance)
			}
			if projectDir == "" {
				continue
			}
			script := strings.TrimSpace(t.VerificationScript)
			if script == "" || filepath.IsAbs(script) {
				continue
			}
			base := filepath.Clean(projectDir)
			full := filepath.Join(base, script)
			if rel, rerr := filepath.Rel(base, full); rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			data, rerr := os.ReadFile(full)
			if rerr != nil {
				continue // the script may not exist yet
			}
			if n, line, bad := firstUnscopedTmuxLine(string(data)); bad {
				return fmt.Errorf("story %q, task %q: verification script %s, line %d: unscoped tmux kill %q — %s", s.Title, t.Title, script, n, line, guidance)
			}
		}
	}
	return nil
}
