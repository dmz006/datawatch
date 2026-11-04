package autonomous

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

// gateVerifierPrestart runs the pre-start check for t just before the worker
// spawns. off skips; shadow records a finding and lets the task start; on
// marks the task blocked, records a triage entry and returns a non-nil error
// so the worker is not started. An empty script is n/a and never blocks.
func (m *Manager) gateVerifierPrestart(prd *PRD, t *Task, taskDir string) error {
	pol := ChecksPolicyFor(taskDir)
	if pol.PreStartVerifier == config.ChecksModeOff {
		return nil
	}
	res := CheckVerifierPrestart(taskDir, t.VerificationScript)
	if res.Outcome != PrestartProblem {
		return nil
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
