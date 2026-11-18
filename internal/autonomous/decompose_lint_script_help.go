// decompose_lint_script_help.go — the script_invokes_known_flag rule:
// every long flag a verification script uses must be listed in the
// command's own help output.
//
// The predicate (verifyScriptInvokesUnknownFlag) reads the task's
// verification script — and any helper it sources, one level deep — and
// for each long-flag invocation resolves the command on PATH and probes
// it for its own help (`prefix --help`, then `prefix -h`, then the bare
// prefix), each attempt under a bounded timeout. A flag that does not
// appear in that help as a whole option fires the rule. Skips, never
// guesses: an unresolvable command, a probe that fails or prints
// nothing, short flags, and a script that does not exist yet all skip.
package autonomous

import (
	"context"
	"os/exec"
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

// probeCommandHelp resolves the command prefix (its first word on PATH)
// and returns the command's own help text: `prefix --help` first, then
// `prefix -h`, then the bare prefix. Each attempt runs under
// scriptHelpProbeTimeout; an attempt that fails or prints nothing
// passes to the next, and a command whose every attempt fails or prints
// nothing is skipped (skipped on uncertainty never fires). Results are
// cached per command prefix so each prefix is probed at most once.
func probeCommandHelp(prefix []string) (string, bool) {
	bin, err := exec.LookPath(prefix[0])
	if err != nil {
		return "", false
	}
	key := bin + "\x00" + strings.Join(prefix[1:], "\x1f")
	scriptHelpProbeMu.Lock()
	if res, ok := scriptHelpProbeCache[key]; ok {
		scriptHelpProbeMu.Unlock()
		return res.help, res.ok
	}
	scriptHelpProbeMu.Unlock()

	attempts := [][]string{{"--help"}, {"-h"}, {}}
	var help string
	probed := false
	for _, extra := range attempts {
		args := append(append([]string{}, prefix[1:]...), extra...)
		ctx, cancel := context.WithTimeout(context.Background(), scriptHelpProbeTimeout)
		out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
		cancel()
		scriptHelpProbeMu.Lock()
		scriptHelpProbeCount++
		scriptHelpProbeMu.Unlock()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			help, probed = string(out), true
			break
		}
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
