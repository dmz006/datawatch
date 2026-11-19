// decompose_lint_script_help.go — the script_invokes_known_flag rule:
// every long flag a verification script uses must be listed in the
// command's own help output.
//
// The predicate (verifyScriptInvokesUnknownFlag) reads the task's
// verification script — and any helper it sources, one level deep — and
// for each long-flag invocation resolves the command on PATH and probes
// it for its own help with `prefix --help` only (never `-h`, never the bare
// prefix), under a bounded timeout, in its own process group, with its
// output capped. A flag that does not
// appear in that help as a whole option fires the rule. Skips, never
// guesses: an unresolvable command, a probe that fails or prints
// nothing, short flags, and a script that does not exist yet all skip.
package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// scriptHelpProbeTimeout bounds one help-probe attempt (one execution of
// the command with its help arguments) so a hung binary cannot hold up
// the lint pass. Unexported so the rule's tests can shorten it.
var scriptHelpProbeTimeout = 5 * time.Second

// scriptHelpProbeCount counts help-probe attempts (not LookPath checks)
// so the rule's tests can assert each prefix is probed at most once per
// lint pass. Guarded by scriptHelpProbeMu.
var scriptHelpProbeCount int

// helpProbeResult is the cached outcome of probing one command prefix
// (the resolved binary plus its remaining words): the help text and
// whether a probe produced it.
type helpProbeResult struct {
	help string
	ok   bool
}

var (
	scriptHelpProbeMu    sync.Mutex
	scriptHelpProbeCache = map[string]helpProbeResult{}
)

// scriptHelpBuiltins are shell builtins and keywords a verification
// script is likely to run; a bare-word command prefix that starts with
// one is never probed (builtins take their own --flags and must not be
// exec'd here).
var scriptHelpBuiltins = map[string]bool{
	"set": true, "cd": true, "echo": true, "printf": true, "export": true,
	"local": true, "read": true, "return": true, "exit": true,
	"source": true, "trap": true, "eval": true, "shift": true,
	"true": true, "false": true, "wait": true,
}

// flagSite is one place the script invokes a long flag: the run of bare
// words immediately in front of it (the command prefix) plus the flag
// name itself (no `--`, no `=` value).
type flagSite struct {
	prefix []string
	flag   string
}

// verifyScriptInvokesUnknownFlag returns the script path and the
// offending invocation (`command … --flag`) when the script (or a
// sourced helper, one level deep) invokes a PATH-resolvable command
// with a long flag the command's own help does not list as a whole
// option. Skips: a script that does not exist yet, a command prefix
// that starts at a shell builtin, a command that does not resolve on
// PATH, a probe that fails or prints nothing within the time bound, and
// every short flag. See the script_invokes_known_flag rule in
// decompose_lint_rules.go.
func verifyScriptInvokesUnknownFlag(dir string, t Task) (path, site string, ok bool) {
	path, content, loaded := verifyScriptContent(dir, t)
	if !loaded {
		return "", "", false
	}
	seen := map[string]bool{}
	for _, fs := range extractFlagSites(content) {
		key := strings.Join(fs.prefix, "\x00") + "\x00--" + fs.flag
		if seen[key] {
			continue
		}
		seen[key] = true
		help, probed := probeCommandHelp(fs.prefix)
		if !probed || helpListsFlag(help, fs.flag) {
			continue
		}
		return path, strings.Join(fs.prefix, " ") + " --" + fs.flag, true
	}
	return "", "", false
}

// extractFlagSites scans a script (already flattened with the helpers it
// sources) for long-flag invocations. A site is a `--flag` token plus
// the maximal run of bare words immediately in front of it; tokens with
// shell metacharacters, `$`, backticks or quotes end that run. The bare
// marker `--`, short flags and `--flag`s whose name is not a plain word
// are not sites, and a run that starts at a shell builtin is not a
// command.
func extractFlagSites(content string) []flagSite {
	var sites []flagSite
	for _, line := range strings.Split(content, "\n") {
		line = stripScriptComment(line)
		toks := strings.Fields(line)
		for i, tok := range toks {
			if tok == "--" || !strings.HasPrefix(tok, "--") || strings.HasPrefix(tok, "---") {
				continue
			}
			flag := tok[2:]
			if eq := strings.IndexByte(flag, '='); eq >= 0 {
				flag = flag[:eq]
			}
			if !isBareWord(flag) {
				continue
			}
			run := make([]string, 0, i)
			for j := i - 1; j >= 0 && isBareWord(toks[j]); j-- {
				run = append([]string{toks[j]}, run...)
			}
			if len(run) == 0 || scriptHelpBuiltins[run[0]] {
				continue
			}
			sites = append(sites, flagSite{prefix: run, flag: flag})
		}
	}
	return sites
}

// stripScriptComment drops a whole-line comment and the first
// ` #`-started trailing comment on a line (shell comment syntax).
func stripScriptComment(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	if i := strings.Index(line, " #"); i >= 0 {
		return line[:i]
	}
	return line
}

// isBareWord reports whether tok is a plain shell word: no shell
// metacharacters, `$`, backticks, quotes, redirections, wildcards or
// glob characters. Anything that needs the shell to interpret it is
// not a bare word.
func isBareWord(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		switch r {
		case ';', '|', '&', '(', ')', '{', '}', '$', '`', '\'',
			'"', '<', '>', '\\', '#', '*', '?', '!':
			return false
		}
	}
	return true
}

// scriptHelpMaxOutputBytes caps the output one help probe keeps. A probe
// that prints more is read up to the cap and the rest is discarded, so a
// streaming command can never grow the daemon's heap.
const scriptHelpMaxOutputBytes = 64 << 10

// scriptHelpWaitDelay is how long a probe waits, after its context ends, for
// the child's output pipes to close before they are closed from this side.
const scriptHelpWaitDelay = 2 * time.Second

// cappedBuffer keeps at most max bytes and silently discards the rest. Write
// reports the full length so the child is never failed by a short write.
type cappedBuffer struct {
	buf []byte
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - len(c.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		c.buf = append(c.buf, p[:room]...)
	}
	return len(p), nil
}

// runBoundedProbe runs one help probe the bounded way: in a fresh scratch
// working directory, in its own process group that is killed whole when ctx
// ends, with the pipe wait bounded by scriptHelpWaitDelay and the combined
// output capped at scriptHelpMaxOutputBytes. It returns what the child
// printed (up to the cap) and the run error.
func runBoundedProbe(ctx context.Context, name string, args ...string) ([]byte, error) {
	scratch, derr := os.MkdirTemp("", "datawatch-help-probe-")
	if derr != nil {
		return nil, derr
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = scratch
	out := &cappedBuffer{max: scriptHelpMaxOutputBytes}
	cmd.Stdout = out
	cmd.Stderr = out
	configureGateProcessGroup(cmd)
	cmd.WaitDelay = scriptHelpWaitDelay
	err := cmd.Run()
	return out.buf, err
}

// isDaemonCommand reports whether name (a bare command name or a resolved
// path) is the running daemon's own executable, by the name "datawatch", by
// the executable's base name, or by file identity. A help probe must never
// run it: the daemon's own subcommands act on the live daemon.
func isDaemonCommand(name string) bool {
	if name == "" {
		return false
	}
	base := filepath.Base(name)
	if base == "datawatch" {
		return true
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	if base == filepath.Base(self) {
		return true
	}
	a, aerr := os.Stat(name)
	b, berr := os.Stat(self)
	return aerr == nil && berr == nil && os.SameFile(a, b)
}

// probeCommandHelp resolves the command prefix (its first word on PATH)
// and returns the command's own help text from `prefix --help`, the only
// form it ever runs. Only a bare command name plus at most one plain
// subcommand word is probed; a longer prefix, a path, the daemon's own
// executable, and a command whose help fails or prints nothing are all
// skipped (skipped on uncertainty never fires). The probe runs under
// scriptHelpProbeTimeout in its own process group with its output capped
// (see runBoundedProbe). Results are cached per command prefix so each
// prefix is probed at most once.
func probeCommandHelp(prefix []string) (string, bool) {
	if len(prefix) == 0 || len(prefix) > 2 || !scriptToolNameRe.MatchString(prefix[0]) ||
		(len(prefix) == 2 && !scriptSubWordRe.MatchString(prefix[1])) {
		return "", false
	}
	bin, err := exec.LookPath(prefix[0])
	if err != nil || isDaemonCommand(prefix[0]) || isDaemonCommand(bin) {
		return "", false
	}
	key := bin + "\x00" + strings.Join(prefix[1:], "\x1f")
	scriptHelpProbeMu.Lock()
	if res, ok := scriptHelpProbeCache[key]; ok {
		scriptHelpProbeMu.Unlock()
		return res.help, res.ok
	}
	scriptHelpProbeMu.Unlock()

	var help string
	probed := false
	args := append(append([]string{}, prefix[1:]...), "--help")
	ctx, cancel := context.WithTimeout(context.Background(), scriptHelpProbeTimeout)
	out, err := runBoundedProbe(ctx, bin, args...)
	cancel()
	scriptHelpProbeMu.Lock()
	scriptHelpProbeCount++
	scriptHelpProbeMu.Unlock()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		help, probed = string(out), true
	}
	res := helpProbeResult{help: help, ok: probed}
	scriptHelpProbeMu.Lock()
	scriptHelpProbeCache[key] = res
	scriptHelpProbeMu.Unlock()
	return help, probed
}

// helpFlagTokenCutset is the punctuation stripped from each help-text
// token before comparing it against an option name: the quotes and
// brackets usage lines wrap options in (for example `[--verbose]`).
const helpFlagTokenCutset = "\"'`[]\u003c\u003e(){}"

// helpListsFlag reports whether the help text lists --flag as a whole
// option: the token `--flag` (or its `--flag=` form) standing alone —
// whitespace- or comma-delimited, with any wrapping brackets or quotes
// stripped — so a longer flag that merely starts the same way
// (`--output`) does not count as listing `--out`. Wrapped forms like
// `[--verbose]` and comma-separated lists like `--out, --in` are
// listed.
func helpListsFlag(help, flag string) bool {
	if flag == "" {
		return false
	}
	want := "--" + flag
	for _, part := range strings.FieldsFunc(help, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	}) {
		tok := strings.Trim(part, helpFlagTokenCutset)
		if tok == want || strings.HasPrefix(tok, want+"=") {
			return true
		}
	}
	return false
}

// resetScriptHelpProbes clears the probe cache and counter (test seam
// for the script_invokes_known_flag rule's tests, so each test sees a
// clean per-prefix cache and an exact probe count).
func resetScriptHelpProbes() {
	scriptHelpProbeMu.Lock()
	scriptHelpProbeCount = 0
	scriptHelpProbeCache = map[string]helpProbeResult{}
	scriptHelpProbeMu.Unlock()
}

// scriptHelpProbeAttempts returns how many help-probe attempts have been
// made (test seam).
func scriptHelpProbeAttempts() int {
	scriptHelpProbeMu.Lock()
	defer scriptHelpProbeMu.Unlock()
	return scriptHelpProbeCount
}
