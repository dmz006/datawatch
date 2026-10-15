// BL447 — per-task project directory override.

package autonomous

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
