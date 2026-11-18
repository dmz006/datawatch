package autonomous

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Verifier pre-start check outcomes.
const (
	PrestartOK      = "ok"
	PrestartNA      = "n/a"
	PrestartProblem = "problem"
)

// prestartTriageSource is the TriageNote.Source of a pre-start block note.
const prestartTriageSource = "verifier_prestart"

// PrestartResult is the outcome of checking one task's verification script.
type PrestartResult struct {
	Outcome string // PrestartOK, PrestartNA or PrestartProblem
	Reason  string // set when Outcome is PrestartProblem
}

// CheckVerifierPrestart checks that script (relative to projectDir) exists as
// a regular file, is executable, starts with a shebang and resolves inside
// projectDir (symlinks and .. included). An empty script is n/a.
func CheckVerifierPrestart(projectDir, script string) PrestartResult {
	script = strings.TrimSpace(script)
	if script == "" {
		return PrestartResult{Outcome: PrestartNA}
	}
	problem := func(format string, a ...any) PrestartResult {
		return PrestartResult{Outcome: PrestartProblem, Reason: fmt.Sprintf(format, a...)}
	}
	root, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return problem("project directory %q cannot be resolved: %v", projectDir, err)
	}
	root, _ = filepath.Abs(root)
	full := script
	if !filepath.IsAbs(full) {
		full = filepath.Join(projectDir, script)
	}
	full = filepath.Clean(full)
	// Lexical escape (.. or an absolute path elsewhere), before touching disk.
	if !withinDir(root, full) && !withinDir(filepath.Clean(projectDir), full) {
		return problem("verification script %q escapes the project directory", script)
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			return problem("verification script %q does not exist", script)
		}
		return problem("verification script %q cannot be resolved: %v", script, err)
	}
	if !withinDir(root, resolved) {
		return problem("verification script %q resolves outside the project directory", script)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return problem("verification script %q cannot be read: %v", script, err)
	}
	if !fi.Mode().IsRegular() {
		return problem("verification script %q is not a regular file", script)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		return problem("verification script %q is not executable", script)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return problem("verification script %q cannot be opened: %v", script, err)
	}
	defer func() { _ = f.Close() }()
	head, _ := bufio.NewReader(f).Peek(2)
	if string(head) != "#!" {
		return problem("verification script %q has no shebang line", script)
	}
	return PrestartResult{Outcome: PrestartOK}
}

func withinDir(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// specCreatePathRe matches a spec instruction that tells the worker to create
// (and make executable) a file; the single token immediately after the match
// is the destination path. "Write it VERBATIM to" is deliberately not in the
// write branch: a spec that carries a verbatim block is a materialization
// case (verifier_materialize.go), not a task-created one.
var specCreatePathRe = regexp.MustCompile(`(?i)(?:\bwrite\s+(?:it|this\s+script|the\s+script|the\s+file|the\s+verification\s+script|a\s+(?:script|file))\s+to\s+|\bcreat\w*\s+(?:this\s+(?:script|file)|the\s+(?:script|file|verification\s+script)|a\s+(?:script|file))(?:\s+(?:at|in|as|to))?\s+|\btouch\s+(?:the\s+(?:script|file)\s+)?|\bchmod\s+(?:\+x|[0-7]{3,4}|[0-7uoa]*x)\s+)`)

// specInstructsCreateScript reports whether the spec tells the worker to
// create the verification script at the task's own path — e.g.
// "Write it to .datawatch/verify/task.sh", "create the script at …" or
// "chmod +x .datawatch/verify/task.sh". Only the full task-relative path or
// its base name, as the single token immediately after a creation
// instruction on the same line, counts. The gate uses it to accept a script
// that is expected to appear only once the task runs; the post-task
// verification is the check that actually sees the written file.
func specInstructsCreateScript(spec, script string) bool {
	script = strings.TrimSpace(script)
	if script == "" || strings.TrimSpace(spec) == "" {
		return false
	}
	cands := []string{script}
	if b := filepath.Base(script); b != script {
		cands = append(cands, b)
	}
	for _, m := range specCreatePathRe.FindAllStringSubmatchIndex(spec, -1) {
		i := m[1]
		for i < len(spec) && (spec[i] == ' ' || spec[i] == '\t') {
			i++
		}
		j := i
		for j < len(spec) && !isSpecTokenSpace(spec[j]) {
			j++
		}
		if j <= i {
			continue
		}
		tok := strings.Trim(spec[i:j], "`\"'()")
		tok = strings.TrimRight(tok, ".,;:")
		tok = strings.Trim(tok, "`\"'")
		for _, c := range cands {
			if tok == c {
				return true
			}
		}
	}
	return false
}

func isSpecTokenSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// notePrestartTaskCreated records the decision for a pre-start pass whose
// script is missing only because the spec instructs the worker to create it,
// mirroring the materialize decision entry.
func (m *Manager) notePrestartTaskCreated(prd *PRD, t *Task) {
	script := strings.TrimSpace(t.VerificationScript)
	note := fmt.Sprintf("task %s: pre-start verifier check: verification script %s is missing but the task spec instructs the worker to create it; the post-task verification checks the written file", t.ID, script)
	d := Decision{At: time.Now(), Kind: "verifier_prestart_task_created", Actor: "autonomous", Note: note}
	if m.store == nil {
		prd.Decisions = append(prd.Decisions, d)
		return
	}
	m.store.WithLock(func() { prd.Decisions = append(prd.Decisions, d) })
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s verifier prestart task-created note: save PRD: %v", prd.ID, t.ID, err)
	}
	log.Printf("[autonomous] prd=%s task=%s prestart accepted: verification script %s is created by the task", prd.ID, t.ID, script)
}

// gateVerifierPrestart runs the pre-start check for t just before the worker
// spawns. off skips; shadow records a finding and lets the task start; on
// marks the task blocked, records a triage entry and returns a non-nil error
// so the worker is not started. An empty script is n/a and never blocks.
func (m *Manager) gateVerifierPrestart(prd *PRD, t *Task, taskDir string) error {
	pol := ChecksPolicyFor(taskDir)
	if pol.PreStartVerifier == config.ChecksModeOff {
		return nil
	}
	// Reconstruct the verification script from the verbatim block in the
	// stored spec before the check, so a first start and a restart after
	// the .datawatch/verify folder was removed both find a runnable file.
	m.materializeVerifierScript(prd, t, taskDir)
	res := CheckVerifierPrestart(taskDir, t.VerificationScript)
	if res.Outcome != PrestartProblem {
		return nil
	}
	// A spec that carries no verbatim block but tells the worker to create
	// the script itself ("Write it to <path>", "chmod +x <path>") expects
	// the file to appear during the run; a missing script at start is then
	// an outcome, not a problem. The post-task verification runs the same
	// script once the task is done and fails it if the worker never wrote
	// the file, so nothing is lost by letting the task start.
	if strings.Contains(res.Reason, "does not exist") {
		if _, _, hasVerbatim := parseVerbatimScriptSpec(t.Spec); !hasVerbatim && specInstructsCreateScript(t.Spec, t.VerificationScript) {
			m.notePrestartTaskCreated(prd, t)
			return nil
		}
	}
	note := fmt.Sprintf("task %s: pre-start verifier check: %s", t.ID, res.Reason)
	if !pol.Blocks() {
		shadow := func() {
			prd.Decisions = append(prd.Decisions, Decision{
				At: time.Now(), Kind: "verifier_prestart_shadow", Actor: "autonomous",
				Note: note + " (shadow: task starts anyway)",
			})
		}
		if m.store == nil {
			shadow()
		} else {
			m.store.WithLock(shadow)
			if err := m.store.SavePRD(prd); err != nil {
				log.Printf("[autonomous] %s/%s verifier prestart shadow: save PRD: %v", prd.ID, t.ID, err)
			}
		}
		return nil
	}
	err := fmt.Errorf("verifier_prestart: %s (task not spawned)", res.Reason)
	oldStatus := t.Status
	apply := func() {
		t.Status = TaskBlocked
		t.Error = err.Error()
	}
	if m.store != nil {
		m.store.WithLock(apply)
		if m.store.SaveTask(t) == nil {
			m.emitTaskStatus(prd, t, oldStatus, nil, nil)
		}
		fresh := &TriageRecord{
			PRDID: prd.ID, PRDTitle: prd.Title, Status: string(TaskBlocked),
			TaskID: t.ID, TaskTitle: t.Title, Error: err.Error(),
		}
		if serr := m.SaveTriageRecord(prd.ID, fresh); serr != nil {
			log.Printf("[autonomous] %s/%s verifier prestart triage record: %v", prd.ID, t.ID, serr)
		} else if aerr := m.AppendTriageNote(prd.ID, prestartTriageSource, note); aerr != nil {
			log.Printf("[autonomous] %s/%s verifier prestart triage note: %v", prd.ID, t.ID, aerr)
		}
	} else {
		apply()
	}
	log.Printf("[autonomous] prd=%s task=%s blocked before start: %s", prd.ID, t.ID, res.Reason)
	return err
}
