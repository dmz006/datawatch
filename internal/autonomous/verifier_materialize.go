// verifier_materialize.go — reconstruct a task's verification script from
// the verbatim script block stored in its spec. When a decompose spec says
// "Write it VERBATIM to <repo-relative path>" and then carries the script
// in a fenced block, and the task's VerificationScript names that same
// path, the script can be rewritten from the stored spec — so both a first
// start and a restart after the .datawatch/verify folder was removed have
// a runnable script for the pre-start verifier check.
package autonomous

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// verbatimInstruction matches the spec instruction naming where a fenced
// script block must be written. The trailing run of non-path characters
// (quotes, backticks, a trailing period) is stripped so the captured value
// is a plain path.
var verbatimInstructionRe = regexp.MustCompile(`Write it VERBATIM to\s+([^\s]+)`)

// parseVerbatimScriptSpec scans spec for 'Write it VERBATIM to <path>'
// followed by a fenced script block (``` with an optional language tag) and
// returns the cleaned path and the block's lines. ok is false when the
// instruction is absent, no fenced block follows it before a closing fence
// is reached, or the block is empty.
func parseVerbatimScriptSpec(spec string) (path, content string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(spec, "\r\n", "\n"), "\n")
	for i, line := range lines {
		m := verbatimInstructionRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Strip surrounding quotes/backticks and trailing sentence
		// punctuation without touching a leading dot (.datawatch).
		p := strings.Trim(m[1], "`\"'")
		p = strings.TrimRight(p, ".,;:")
		p = filepath.Clean(p)
		var block []string
		in := false
		closed := false
		for _, l := range lines[i+1:] {
			trimmed := strings.TrimSpace(l)
			if !in {
				if strings.HasPrefix(trimmed, "```") {
					in = true
				}
				continue
			}
			if strings.HasPrefix(trimmed, "```") {
				closed = true
				break
			}
			block = append(block, strings.TrimRight(l, "\t "))
		}
		if !closed {
			continue
		}
		if strings.Join(block, "\n") == "" {
			continue
		}
		return p, strings.Join(block, "\n"), true
	}
	return "", "", false
}

// materializeVerbatimScript is the write step: when the task's
// VerificationScript names relPath (relative to dir), and that file is
// missing, the verbatim block content is written there with mode 0755.
// The path must stay within dir (a traversal or absolute destination is
// rejected) and an existing file is never overwritten. It returns the
// written path relative to dir and true only when a file was written.
func materializeVerbatimScript(dir, script, relPath, content string) (string, bool) {
	script = strings.TrimSpace(script)
	if script == "" {
		return "", false
	}
	root := filepath.Clean(dir)
	rel := filepath.Clean(relPath)
	// The spec instruction is repo-relative: an absolute instruction path is
	// not one (filepath.Join would silently re-anchor it inside dir).
	if filepath.IsAbs(rel) {
		return "", false
	}
	dst := filepath.Join(root, rel)
	if !withinDir(root, dst) {
		return "", false
	}
	// The task's verification script must name this same file.
	want := strings.TrimSpace(script)
	if filepath.IsAbs(want) {
		relWant, err := filepath.Rel(root, filepath.Clean(want))
		if err != nil {
			return "", false
		}
		want = relWant
	}
	if filepath.Clean(want) != rel {
		return "", false
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", false // never overwrite an existing file
	}
	body := strings.TrimRight(content, "\n") + "\n"
	if body == "" || body == "\n" {
		return "", false
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Printf("[autonomous] materialize verification script: mkdir %s: %v", filepath.Dir(dst), err)
		return "", false
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		if os.IsExist(err) { // created concurrently, or a race after Lstat
			return "", false
		}
		log.Printf("[autonomous] materialize verification script: create %s: %v", dst, err)
		return "", false
	}
	if _, werr := f.WriteString(body); werr != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		log.Printf("[autonomous] materialize verification script: write %s: %v", dst, werr)
		return "", false
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(dst)
		log.Printf("[autonomous] materialize verification script: close %s: %v", dst, cerr)
		return "", false
	}
	return rel, true
}

// materializeVerifierScript reconstructs t's verification script from the
// verbatim block in t.Spec before the pre-start check runs, and records a
// decision entry when a script was written. ok is false when there is
// nothing to write (no matching instruction, a traversal or absolute path,
// or an existing file at the destination).
func (m *Manager) materializeVerifierScript(prd *PRD, t *Task, taskDir string) (string, bool) {
	script := strings.TrimSpace(t.VerificationScript)
	if script == "" || t.Spec == "" {
		return "", false
	}
	rel, content, ok := parseVerbatimScriptSpec(t.Spec)
	if !ok {
		return "", false
	}
	written, ok := materializeVerbatimScript(taskDir, script, rel, content)
	if !ok {
		return "", false
	}
	note := "task " + t.ID + ": materialized verification script " + written +
		" from the verbatim block in the task spec"
	d := Decision{At: time.Now(), Kind: "verifier_materialize", Actor: "autonomous", Note: note}
	if m.store == nil {
		prd.Decisions = append(prd.Decisions, d)
	} else {
		m.store.WithLock(func() { prd.Decisions = append(prd.Decisions, d) })
		if err := m.store.SavePRD(prd); err != nil {
			log.Printf("[autonomous] %s/%s materialize verification script: save PRD: %v", prd.ID, t.ID, err)
		}
	}
	log.Printf("[autonomous] prd=%s task=%s materialized verification script %s", prd.ID, t.ID, written)
	return written, true
}
