package autonomous

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The two assertion-stability predicates:
//
//   - verifyScriptUnknownFlag: the script passes a long flag to an
//     installed command (or one of its subcommands) whose own `--help`
//     output does not list it — a flag the script's author invented.
//     Commands the environment cannot find are not checked; help that
//     cannot be read is not checked (both are skipped silently, each
//     probe bounded by a few-second timeout). The rule is a stability
//     rule because a script asserting against an option the installed
//     binary does not have fails in a way no fix text can anticipate
//     until the flag is real.
//
//   - verifyScriptTextNotInSpec: the script asserts a markdown
//     heading (a `# …` line, grepped or compared) or a multi-word
//     output phrase that does not appear verbatim in the task spec —
//     an expectation stated nowhere. The task spec is the contract;
//     the fix is to state the exact heading or format in the spec,
//     word for word. Text the script generates itself (echoed or
//     written on a non-checking line before it is checked) is not an
//     assertion against it and is not flagged.
//
// Both are content-level predicates: they read the same
// script-plus-followed-helper content as the other verify-script rules.

// scriptHelpTimeout bounds a single help probe run.
const scriptHelpTimeout = 5 * time.Second

// scriptHelpMaxProbes caps the number of probe invocations (command -v
// resolutions and --help runs combined) per lint evaluation, so a long
// script cannot make the lint unboundedly slow.
const scriptHelpMaxProbes = 16

// scriptHelpProbe runs one shell line in the execution environment and
// returns its combined output. The decompose-lint engine itself is
// pure (in-memory in, findings out); this is the single injectable
// escape hatch the unknown-flag predicate uses to consult command help
// text. Tests replace it with a deterministic fake.
var scriptHelpProbe = defaultScriptHelpProbe

func defaultScriptHelpProbe(ctx context.Context, shellLine string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, scriptHelpTimeout)
	defer cancel()
	// Bounded like the help-text probe (runBoundedProbe): own process group
	// killed whole on timeout, capped output, scratch working directory.
	out, err := runBoundedProbe(ctx, "sh", "-c", shellLine)
	if err != nil {
		// A non-zero exit with output (some commands report their
		// help that way) is still usable help text; a failure with no
		// output (command not found, timeout) is the skip signal.
		if len(out) == 0 {
			return "", err
		}
	}
	return string(out), nil
}

var (
	// scriptToolNameRe: the only command candidates this predicate
	// ever probes — bare, PATH-resolvable names (no slashes, no
	// metacharacters). Relative or absolute binary paths are
	// deliberately not probed: their `--help` is unknown ground.
	scriptToolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	scriptLongFlagRe = regexp.MustCompile(`--[A-Za-z][A-Za-z0-9-]*`)
	scriptSubWordRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	scriptAssignRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// scriptShellBuiltins are shell keywords, builtins and the shell
// families themselves: probing `<word> --help` for them would either
// error, print unrelated help, or execute something.
var scriptShellBuiltins = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"for": true, "until": true, "while": true, "do": true, "done": true,
	"case": true, "esac": true, "function": true, "select": true,
	"return": true, "exit": true, "set": true, "shift": true, "local": true,
	"readonly": true, "unset": true, "export": true, "source": true, "eval": true,
	"exec": true, "read": true, "cd": true, "pushd": true, "popd": true,
	"true": true, "false": true, "echo": true, "printf": true, "test": true,
	"time": true, "wait": true, "trap": true, "alias": true, "unalias": true,
	"break": true, "continue": true, ":": true,
	"env": true, "sudo": true, "command": true, "timeout": true, "nice": true,
	"sh": true, "bash": true, "dash": true, "zsh": true, "ksh": true,
}

type scriptInvocation struct {
	cmd   string
	sub   string
	flags []string
}

// scriptCommandInvocations extracts the tool invocations worth probing
// from shell script text: one entry per simple-command segment that
// passes at least one long flag, as a bare PATH-resolvable name.
func scriptCommandInvocations(content string) []scriptInvocation {
	var out []scriptInvocation
	for _, line := range strings.Split(content, "\n") {
		line = cutScriptComment(line)
		for _, seg := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ';' || r == '|' || r == '&'
		}) {
			inv, ok := scriptInvocationFromTokens(strings.Fields(seg))
			if ok {
				out = append(out, inv)
			}
		}
	}
	return out
}

func scriptInvocationFromTokens(toks []string) (scriptInvocation, bool) {
	i := 0
	for i < len(toks) {
		switch toks[i] {
		case "env", "sudo", "time", "command":
			i++
			continue
		case "timeout": // timeout <duration> <cmd>…
			if i+1 < len(toks) {
				i += 2
				continue
			}
		case "nice":
			return scriptInvocation{}, false
		}
		if scriptAssignRe.MatchString(toks[i]) { // VAR=… <cmd>…
			i++
			continue
		}
		break
	}
	if i >= len(toks) {
		return scriptInvocation{}, false
	}
	name := toks[i]
	if !scriptToolNameRe.MatchString(name) || scriptShellBuiltins[name] {
		return scriptInvocation{}, false
	}
	inv := scriptInvocation{cmd: name}
	for _, tok := range toks[i+1:] {
		switch {
		case tok == "--" || tok == "--help" || tok == "--version":
			// meta flags and the option-terminator are never asserted
			continue
		case strings.HasPrefix(tok, "--"):
			if m := scriptLongFlagRe.FindString(tok); m != "" {
				inv.flags = append(inv.flags, strings.SplitN(m, "=", 2)[0])
			}
		case !strings.HasPrefix(tok, "-") && !strings.HasPrefix(tok, "$") && !strings.HasPrefix(tok, "/") && scriptSubWordRe.MatchString(tok) && inv.sub == "":
			inv.sub = tok
		}
	}
	if len(inv.flags) == 0 {
		return scriptInvocation{}, false
	}
	return inv, true
}

// cutScriptComment drops an unquoted `# …` comment from a shell line.
func cutScriptComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' || line[i-1] == ';' {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return line
}

// scriptHelpCache remembers probe outcomes for the duration of one lint
// evaluation so a repeated command is probed at most once per form.
type scriptHelpCache struct {
	probes   int
	known    map[string]map[string]bool
	unusable map[string]bool
}

func newScriptHelpCache() *scriptHelpCache {
	return &scriptHelpCache{known: map[string]map[string]bool{}, unusable: map[string]bool{}}
}

// flagsHelpFor reports whether all of the invocation's long flags are
// listed by the command's own `--help` (falling back to
// `<cmd> <sub> --help` for a flagged subcommand), or an error when the
// command or its help is unavailable — which the caller skips.
func (c *scriptHelpCache) flagsHelpFor(ctx context.Context, inv scriptInvocation) (bool, error) {
	probe := func(form, line string) (map[string]bool, error) {
		// Never probe the running daemon's own executable.
		if isDaemonCommand(inv.cmd) {
			return nil, fmt.Errorf("command %q is the daemon executable", inv.cmd)
		}
		if f, ok := c.known[form]; ok {
			return f, nil
		}
		if c.unusable[form] || c.unusable["cmd_"+inv.cmd] {
			return nil, fmt.Errorf("unavailable")
		}
		if c.probes >= scriptHelpMaxProbes {
			return nil, fmt.Errorf("probe budget exhausted")
		}
		c.probes++
		// resolve first: a command the environment cannot find is
		// skipped silently.
		if name, err := scriptHelpProbe(ctx, "command -v "+inv.cmd); err != nil || strings.TrimSpace(name) == "" {
			c.unusable["cmd_"+inv.cmd] = true
			return nil, fmt.Errorf("command %q not installed", inv.cmd)
		}
		// then read its help: unreadable help is skipped silently.
		c.probes++
		help, err := scriptHelpProbe(ctx, line)
		if err != nil && strings.TrimSpace(help) == "" {
			c.unusable[form] = true
			return nil, fmt.Errorf("help unreadable")
		}
		flags := scriptHelpFlagSet(help)
		c.known[form] = flags
		return flags, nil
	}

	main, err := probe("main", inv.cmd+" --help")
	if err != nil {
		return false, err
	}
	if scriptHelpFlagsCover(main, inv.flags) {
		return true, nil
	}
	if inv.sub == "" {
		return false, nil
	}
	sub, err := probe("sub", inv.cmd+" "+inv.sub+" --help")
	if err != nil {
		return false, nil
	}
	return scriptHelpFlagsCover(sub, inv.flags), nil
}

// scriptHelpFlagsCover reports whether every probed flag is listed in
// the help flag set.
func scriptHelpFlagsCover(help map[string]bool, flags []string) bool {
	for _, f := range flags {
		if !help[f] {
			return false
		}
	}
	return true
}

// scriptHelpFlagSet extracts the long options a help text advertises.
var scriptHelpFlagListRe = regexp.MustCompile(`--[A-Za-z][A-Za-z0-9-]+`)

func scriptHelpFlagSet(help string) map[string]bool {
	set := map[string]bool{}
	for _, m := range scriptHelpFlagListRe.FindAllString(help, -1) {
		set[strings.SplitN(m, "=", 2)[0]] = true
	}
	return set
}

// verifyScriptUnknownFlag returns the script path, a detail naming the
// first unknown flag, and whether the rule fires. Commands that are
// not installed, or whose help is unreadable, are skipped silently.
func verifyScriptUnknownFlag(dir string, t Task) (path, detail string, ok bool) {
	path, content, readable := verifyScriptContent(dir, t)
	if !readable {
		return "", "", false
	}
	cache := newScriptHelpCache()
	ctx := context.Background()
	for _, inv := range scriptCommandInvocations(content) {
		covered, err := cache.flagsHelpFor(ctx, inv)
		if err != nil {
			continue // skip: uninstalled or unreadable
		}
		if covered {
			continue
		}
		main := cache.known["main"]
		sub := cache.known["sub"]
		for _, f := range inv.flags {
			if main != nil && main[f] {
				continue
			}
			if sub != nil && sub[f] {
				continue
			}
			what := inv.cmd + " --help"
			if inv.sub != "" {
				what = fmt.Sprintf("%s %s --help (and %s --help)", inv.cmd, inv.sub, inv.cmd)
			}
			return path, fmt.Sprintf("passes %s to %s, which is not listed by %s", f, inv.cmd, what), true
		}
	}
	return "", "", false
}

var (
	// verifyHeadingLiteralRe: a quoted literal starting with an
	// optional `^` anchor, one to six `#` and a space — a markdown
	// heading assertion (the whole `#` run plus text is captured, so
	// `## Results` is checked as `## Results`). A shebang (`#!`) has
	// no space after the `#` and is excluded.
	verifyHeadingLiteralRe = regexp.MustCompile(`["'](\^?#{1,6}[ \t]+[A-Za-z][^\n"']{2,90})`)
	// verifyFormatLiteralRe: a quoted phrase — an output-format
	// assertion. The character class deliberately excludes both quote
	// characters and `#`, so a heading literal never matches here.
	verifyFormatLiteralRe = regexp.MustCompile(`["']([A-Za-z][A-Za-z0-9_ .,;:()/-]{3,99})["']`)
	// verifyCheckLineRe: a line that performs a check against text —
	// the grep family, awk, sed, or a test/[[/==/eq comparison. A
	// literal on any other line is text the script generates, not an
	// assertion against it, and is not flagged.
	verifyCheckLineRe = regexp.MustCompile(`(?i)\b(grep|egrep|fgrep|awk|sed|test)\b|\[\[|==|\bif\b[^\n]*==|\beq\b`)
	scriptAlphaWordRe = regexp.MustCompile(`^[A-Za-z]+$`)
)

// verifyScriptTextNotInSpec returns the script path, a detail naming
// the first heading or phrase the spec does not state, and whether the
// rule fires.
func verifyScriptTextNotInSpec(dir string, t Task) (path, detail string, ok bool) {
	path, content, readable := verifyScriptContent(dir, t)
	if !readable {
		return "", "", false
	}
	for _, cl := range scriptTextClaims(content) {
		if strings.Contains(t.Spec, cl) {
			continue
		}
		return path, fmt.Sprintf("asserts %q, which the task spec does not state", cl), true
	}
	return "", "", false
}

// scriptTextClaims collects the headings and phrases the script
// asserts (check lines only), in first-seen line order, deduplicated.
func scriptTextClaims(content string) []string {
	var claims []string
	seen := map[string]bool{}
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			return
		}
		seen[text] = true
		claims = append(claims, text)
	}
	for _, line := range strings.Split(content, "\n") {
		if !verifyCheckLineRe.MatchString(line) {
			continue
		}
		for _, m := range verifyHeadingLiteralRe.FindAllStringSubmatchIndex(line, -1) {
			// a `^` anchor is a grep artifact, not part of the heading
			add(strings.TrimPrefix(line[m[2]:m[3]], "^"))
		}
		for _, m := range verifyFormatLiteralRe.FindAllStringSubmatchIndex(line, -1) {
			if text, valid := scriptPhraseCandidate(line[m[2]:m[3]]); valid {
				add(text)
			}
		}
	}
	return claims
}

// scriptPhraseCandidate validates a format-phrase literal: three or
// more words (no flags, not path-shaped), at least two pure-alphabetic
// words.
func scriptPhraseCandidate(cand string) (string, bool) {
	text := strings.TrimSpace(cand)
	words := strings.Fields(text)
	if len(words) < 3 {
		return "", false
	}
	alpha := 0
	for _, w := range words {
		if strings.HasPrefix(w, "-") {
			return "", false
		}
		if strings.Contains(w, "/") && len(words) < 4 {
			return "", false // path-shaped literal
		}
		if scriptAlphaWordRe.MatchString(w) {
			alpha++
		}
	}
	if alpha < 2 {
		return "", false
	}
	return text, true
}
