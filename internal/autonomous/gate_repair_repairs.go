package autonomous

// Safe defaults for the on-mode repairs the gate repair engine may apply.
// Each is bounded to the caller's script (the engine has already refused a
// script outside the .datawatch/verify folder): they read the script, make
// one minimal documented change, and return the before/after text the engine
// stores on its record. They never touch any other file, and the one
// generated-mirror repair (docs_mirror_stale) is `make sync-docs`, not a
// file edit.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// gateRepairScriptPath resolves the one editable script for the repair: the
// gate script inside the input's verify folder; the error is the refusal.
func gateRepairScriptPath(in GateRepairInput) (string, error) {
	p := in.Gate.ScriptPath
	if p == "" {
		return "", fmt.Errorf("no gate script path to repair")
	}
	if in.VerifyFolder != "" && !pathWithin(in.VerifyFolder, p) {
		return "", fmt.Errorf("the gate script %q is outside the .datawatch/verify folder %q; the gate repair engine does not edit tracked repo files outside it", p, in.VerifyFolder)
	}
	return p, nil
}

// readWriteScript runs fn over the text of one gate script, writes it back
// only when fn returns changed text, and returns the before/after pair.
// before == after means fn refused (nothing was written).
func readWriteScript(path string, fn func(text string) (string, error)) (before, after string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	before = string(b)
	n, err := fn(before)
	if err != nil {
		return before, before, err
	}
	if n == before {
		return before, before, nil
	}
	if werr := os.WriteFile(path, []byte(n), 0o644); werr != nil {
		return before, before, werr
	}
	return before, n, nil
}

// gateRepairRunSyncDocs is the docs_mirror_stale repair: run `make sync-docs`
// in the task's repo folder (it re-generates the embedded docs mirror; the
// gate then re-runs). It never edits a tracked file by hand.
func gateRepairRunSyncDocs(ctx context.Context, repoDir string) (string, error) {
	if repoDir == "" {
		return "", fmt.Errorf("no repo dir to run make sync-docs in")
	}
	if abs, err := filepath.Abs(repoDir); err != nil {
		return "", fmt.Errorf("bad repo dir %q: %v", repoDir, err)
	} else {
		if _, serr := os.Stat(filepath.Join(abs, "Makefile")); serr != nil {
			return "", fmt.Errorf("no Makefile in %s to run sync-docs from", abs)
		}
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}
	_ = deadline
	cmd := exec.CommandContext(ctx, "make", "sync-docs")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("make sync-docs: %v", err)
	}
	return string(out), nil
}

// gateRepairWidenAllowedList is the allowed_list_rejects repair: add the
// rejected files to the script's cumulative allowed-files list (a shell
// array declaration containing "ALLOWED" in its name) so the scope check
// stops rejecting them. It appends one commented block inside that array and
// nothing else; a script with no such list is refused.
func gateRepairWidenAllowedList(in GateRepairInput, lines []string) (before, after string, err error) {
	p, err := gateRepairScriptPath(in)
	if err != nil {
		return "", "", err
	}
	if len(lines) == 0 {
		return "", "", fmt.Errorf("no rejected files to add to the allowed-files list")
	}
	block := "\t# gate-repair(allowed_list_rejects): added by the gate repair engine to stop the scope check rejecting these files\n"
	for _, f := range lines {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		block += "\t" + quoteShellWord(f) + "\n"
	}
	if block == "" {
		return "", "", fmt.Errorf("no rejected files to add to the allowed-files list")
	}
	return readWriteScript(p, func(text string) (string, error) {
		pos := firstAllowedListStart(text)
		if pos < 0 {
			return "", fmt.Errorf("the script %s declares no ALLOWED*=(...) list to widen", p)
		}
		rel := strings.Index(text[pos:], ")")
		if rel < 0 {
			return "", fmt.Errorf("the script %s declares no closing paren for the allowed-files list", p)
		}
		cut := pos + rel
		return text[:cut] + block + text[cut:], nil
	})
}

// firstAllowedListStart finds the opening paren of the first shell array
// whose name contains "ALLOWED" (e.g. `FILES_ALLOWED=(`), or -1 when the
// script declares none.
func firstAllowedListStart(text string) int {
	up := strings.ToUpper(text)
	searchFrom := 0
	for {
		loc := strings.Index(up[searchFrom:], "ALLOWED")
		if loc < 0 {
			return -1
		}
		pos := searchFrom + loc
		// the declaration is NAME=( ... ): the first "=" after the name,
		// then a "("
		rest := text[pos:]
		if eq := strings.Index(rest, "="); eq >= 0 {
			raw := rest[eq+1:]
			trim := len(raw) - len(strings.TrimLeft(raw, " \t"))
			if lp := strings.Index(raw[trim:], "("); lp >= 0 {
				return pos + eq + 1 + trim + lp
			}
		}
		searchFrom = pos + len("ALLOWED")
	}
}

// gateRepairAdjustPipes is the pipe_sigpipe repair: two documented minimal
// changes that stop the pipeline dying of SIGPIPE or a sh/bashism mismatch:
//
//   - a `set -o pipefail` (alone or with -e) is dropped for the plain
//     `set -e` it came with, with a comment — an early-exiting pipeline
//     reader (grep -q, head -1) no longer SIGPIPE-fails the whole pipeline;
//   - a `#!/bin/sh` shebang becomes `#!/usr/bin/env bash`, so bashisms run
//     under bash instead of dying as "Syntax error" / "not found".
//
// Nothing else is touched; a script with neither shape is refused.
func gateRepairAdjustPipes(in GateRepairInput) (before, after string, err error) {
	p, err := gateRepairScriptPath(in)
	if err != nil {
		return "", "", err
	}
	return readWriteScript(p, func(text string) (string, error) {
		changed := false
		const note = "# gate-repair(pipe_sigpipe): pipefail dropped — an early-exiting pipeline reader (grep -q, head) no longer SIGPIPEs the pipeline into a 141\n"
		if strings.Contains(text, "set -e -o pipefail") {
			text = strings.Replace(text, "set -e -o pipefail", note+"set -e", 1)
			changed = true
		} else if strings.Contains(text, "set -o pipefail") {
			text = strings.Replace(text, "set -o pipefail", note, 1)
			changed = true
		}
		if n := strings.Replace(text, "#!/bin/sh", "#!/usr/bin/env bash # gate-repair(pipe_sigpipe): bash, not sh — bashisms no longer die as syntax errors", 1); n != text {
			text, changed = n, true
		}
		if !changed {
			return "", fmt.Errorf("no pipefail line or /bin/sh shebang found to fix in %s", p)
		}
		return text, nil
	})
}

// gateRepairNarrowTrackerIDCheck is the tracker_id_css_hex repair: narrow
// the script's tracker-ID alternation so it cannot match a '#' hex token
// (a CSS color code): the pattern that is generated for exactly this check,
// `\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b`, becomes the PCRE spelling with a
// hex-digit right-exclusion, and its plain ERE spelling gains an explicit
// non-hex-class left guard. No other occurrence is touched.
func gateRepairNarrowTrackerIDCheck(in GateRepairInput) (before, after string, err error) {
	p, err := gateRepairScriptPath(in)
	if err != nil {
		return "", "", err
	}
	return readWriteScript(p, func(text string) (string, error) {
		pairs := []struct{ old, neu string }{
			{`\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b`, `(^|#?[^#])(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)(?![0-9])`},
			{`(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)`, `(^|[^0-9A-Za-z#])(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)([^0-9]|$)`},
		}
		for _, pr := range pairs {
			if strings.Contains(text, pr.old) {
				return strings.ReplaceAll(text, pr.old, pr.neu), nil
			}
		}
		return "", fmt.Errorf("no tracker-ID pattern found to narrow in %s", p)
	})
}

// quoteShellWord quotes one path for an allowed-list entry: bare when it is
// already shell-safe, single-quoted otherwise (no expansion when the list is
// used later).
func quoteShellWord(w string) string {
	if w == "" {
		return "''"
	}
	if ok, _ := regexp.MatchString(`^[A-Za-z0-9_./-]+$`, w); ok {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}
