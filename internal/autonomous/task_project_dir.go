// BL447 — per-task project directory override.

package autonomous

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// ResolveTaskProjectDir returns the directory a task works and is verified
// in: the task's ProjectDir override (cleaned) when set, else the PRD's
// ProjectDir. Nil-safe for both arguments. This is the single resolution
// path — callers without a Manager use it directly.
func ResolveTaskProjectDir(prd *PRD, task *Task) string {
	if task != nil && task.ProjectDir != "" {
		return filepath.Clean(task.ProjectDir)
	}
	if prd == nil {
		return ""
	}
	return prd.ProjectDir
}

// resolveTaskProjectDir delegates to ResolveTaskProjectDir.
func (m *Manager) resolveTaskProjectDir(prd *PRD, task *Task) string {
	return ResolveTaskProjectDir(prd, task)
}

// SetTaskProjectDir overrides a task's working/verification directory
// before approval. An empty dir clears the override (falls back to
// PRD.ProjectDir). Same lock rule as SetTaskLLM.
func (m *Manager) SetTaskProjectDir(prdID, taskID, dir, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept project dir overrides", prdID, prd.Status)
	}
	dir = strings.TrimSpace(dir)
	if dir != "" {
		if err := ValidateTaskProjectDir(prd, dir); err != nil {
			return nil, err
		}
		dir = filepath.Clean(dir)
	}
	var task *Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				task = &prd.Story[si].Tasks[ti]
				break
			}
		}
		if task != nil {
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	task.ProjectDir = dir
	task.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_project_dir", Actor: actor,
		Note: fmt.Sprintf("task=%s project_dir=%s", taskID, dir),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// runQualityGateTests is pipeline.RunTests; a var so tests can observe the
// directory each quality-gate run happens in. The last argument is the
// allowed project roots the run's directory is checked against.
var runQualityGateTests = pipeline.RunTests

// qualityGateBaselines caches BL367 quality-gate baselines per directory
// for one Run (BL447). The PRD-level baseline captured in prd.ProjectDir
// only applies to tasks that resolve to that dir; any other resolved task
// dir gets its own baseline captured lazily on first use. A nil receiver
// (gates off) reports no baseline.
type qualityGateBaselines struct {
	mu     sync.Mutex
	cfg    pipeline.QualityGateConfig
	prdDir string
	byDir  map[string]pipeline.TestResult
	// vetByDir caches the dir's vet baseline: the packages failing at
	// baseline time. vetBadDir marks dirs where vet itself could not
	// run (unusable = warn-only, never a regression).
	vetByDir  map[string][]string
	vetBadDir map[string]bool
	// onInvalid is invoked when a lazily captured per-dir baseline
	// classifies as unusable; the executor wires it to the PRD-level invalid
	// marking and warning. nil is a no-op.
	onInvalid func(dir, reason string)
	enabled   bool
	// roots are the allowed project roots a lazily captured baseline's
	// directory is checked against.
	roots []string
}

func newQualityGateBaselines(cfg pipeline.QualityGateConfig, prdDir string, prdBaseline *pipeline.TestResult, roots []string) *qualityGateBaselines {
	b := &qualityGateBaselines{cfg: cfg, prdDir: prdDir, byDir: map[string]pipeline.TestResult{},
		vetByDir: map[string][]string{}, vetBadDir: map[string]bool{},
		enabled: cfg.Enabled && cfg.TestCommand != "", roots: roots}
	if prdBaseline != nil {
		b.byDir[prdDir] = *prdBaseline
	}
	return b
}

// vetForDir returns the set of packages failing `go vet` at baseline time
// in dir (captured once per dir, through the same runQualityGateTests seam
// as the test baselines, and cached for the rest of the Run). ok is false
// when vet itself could not execute in that dir: callers log a warning and
// skip the vet comparison, and never count an unusable vet as a regression.
func (b *qualityGateBaselines) vetForDir(dir string) (failing []string, ok bool) {
	if b == nil || !b.enabled || dir == "" {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if pkgs, cached := b.vetByDir[dir]; cached {
		return pkgs, true
	}
	if b.vetBadDir[dir] {
		return nil, false
	}
	r := runQualityGateTests("go vet ./...", dir, b.cfg.EffectiveTimeout(), b.roots)
	if r.ExitCode == -1 {
		b.vetBadDir[dir] = true
		log.Printf("[autonomous] WARNING: quality-gate vet baseline unusable in %s (vet could not run) - vet comparison for this dir warns only", dir)
		return nil, false
	}
	pkgs := pipeline.VetFailuresFromOutput(r.Output)
	b.vetByDir[dir] = pkgs
	if len(pkgs) > 0 {
		log.Printf("[autonomous] quality-gate vet baseline for dir %s: %d failing package(s): %s", dir, len(pkgs), strings.Join(pkgs, ", "))
	}
	return pkgs, true
}

// forDir returns the baseline comparable with a test run in dir. For
// prd.ProjectDir that is the pre-run baseline (absent when it was never
// captured, preserving pre-BL447 behaviour); any other dir is captured
// now and cached.
func (b *qualityGateBaselines) forDir(dir string) (pipeline.TestResult, bool) {
	if b == nil || !b.enabled || dir == "" {
		return pipeline.TestResult{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if r, ok := b.byDir[dir]; ok {
		return r, true
	}
	if dir == b.prdDir {
		return pipeline.TestResult{}, false
	}
	r := runQualityGateTests(b.cfg.TestCommand, dir, b.cfg.EffectiveTimeout(), b.roots)
	log.Printf("[autonomous] quality-gate baseline for task dir %s: %d pass / %d fail", dir, r.PassCount, r.FailCount)
	b.byDir[dir] = r
	if valid, reason := pipeline.ClassifyBaseline(r); !valid && b.onInvalid != nil {
		b.onInvalid(dir, reason)
	}
	return r, true
}

// storyEvidenceDir is the directory story-level verification-script
// evidence runs in. A story spans tasks, so it stays on prd.ProjectDir
// unless every task in the story resolves to the same override dir — then
// that shared dir is the only place the story's work exists (BL447).
func storyEvidenceDir(prd *PRD, s *Story) string {
	if s == nil || len(s.Tasks) == 0 {
		return prd.ProjectDir
	}
	dir := ResolveTaskProjectDir(prd, &s.Tasks[0])
	for i := 1; i < len(s.Tasks); i++ {
		if ResolveTaskProjectDir(prd, &s.Tasks[i]) != dir {
			return prd.ProjectDir
		}
	}
	return dir
}
