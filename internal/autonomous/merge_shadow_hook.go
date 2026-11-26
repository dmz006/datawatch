// merge_shadow_hook.go — the finish-time shadow merge hook.
//
// When an Automaton reaches the finished state (PRDCompleted), the executor
// calls startMergeShadow. The hook:
//
//   - does nothing when autonomous.merge_shadow.enabled is false, and nothing
//     when the Automaton has no work branch or project directory to merge;
//   - copies what the runner needs out of the Automaton, synchronously, so the
//     background run never reads a record that is still changing;
//   - runs mergeshadow.Runner in its own goroutine, so the finish path never
//     waits on git or on the gates. The run builds a trial merge in a
//     throwaway worktree, runs the gates there and stores the merge plan with
//     applied set to false. The project branch is never touched.
//
// A failed run is logged and dropped; it never changes the Automaton's outcome.
// The comparison of the stored plan with what landed is a separate hook
// (merge_shadow_compare_hook.go).
package autonomous

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
	"github.com/dmz006/datawatch/internal/mergeshadow"
	"github.com/dmz006/datawatch/internal/proclimit"
)

// mergeShadowGitAllowance is the time the run gets for git work on top of the
// gates, so a slow worktree checkout cannot be cut short by the gate budget.
const mergeShadowGitAllowance = 10 * time.Minute

// mergeShadowGateCount is the number of gates a run may wait for: build, lint
// and test, each bounded by the gate timeout.
const mergeShadowGateCount = 3

// mergeShadowRun runs the shadow merge for one Automaton and returns the
// stored plan. It is a variable so the package's tests can replace it: every
// test except the hook's own runs with a no-op, so an Automaton finished by an
// unrelated test does not start real worktrees and gates behind that test's
// temporary directory.
var mergeShadowRun = func(ctx context.Context, in mergeshadow.Input) (*mergeshadow.MergePlan, error) {
	return mergeshadow.Runner{}.Run(ctx, in)
}

// SetMergeShadowConfig sets the autonomous.merge_shadow settings the finish
// hook reads. The daemon calls it at startup with the configured block.
func (m *Manager) SetMergeShadowConfig(c config.MergeShadowConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergeShadow = c
}

// mergeShadowSettings returns the settings set by SetMergeShadowConfig. A
// manager that was never given settings reads the defaults, which are on.
func (m *Manager) mergeShadowSettings() config.MergeShadowConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mergeShadow
}

// startMergeShadow runs the shadow merge for a finished Automaton in the
// background. It returns at once. The returned channel is closed when the
// background run ends; it is nil when no run was started, which is the case
// when the setting is off or the Automaton has nothing to merge. Production
// callers ignore the channel; tests wait on it.
func (m *Manager) startMergeShadow(prd *PRD) <-chan struct{} {
	if prd == nil || m.dataDir == "" {
		return nil
	}
	cfg := m.mergeShadowSettings()
	if !cfg.EffectiveEnabled() {
		return nil
	}
	in, ok := m.mergeShadowInput(prd, cfg)
	if !ok {
		m.recordNoShadowPlan(prd.ID, in, "the Automaton has no work branch or no project folder, so there is no merge to plan")
		return nil
	}
	timeout := mergeShadowGateCount*cfg.EffectiveGateTimeout() + mergeShadowGitAllowance
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		// Every command the shadow starts runs, with everything it starts,
		// under a hard memory limit. Where that cannot be had, the plan's
		// gates, which run the project's own commands, are not run, and
		// datawatch's own git reads run without it.
		limit := proclimit.Host()
		if err := limit.Available(ctx); err != nil {
			limit = proclimit.None(err)
		}
		ctx = proclimit.With(ctx, limit, nil)
		if m.store != nil {
			sel := m.MergeEligibility(ctx, in.AutomatonID)
			in.Selection = &mergeshadow.PlanSelection{Eligible: sel.Eligible, Basis: sel.Basis, Tip: sel.Tip, Reasons: sel.Reasons}
		}
		plan, err := mergeShadowRun(ctx, in)
		if err != nil {
			log.Printf("[autonomous] merge shadow: automaton %s: %v", prd.ID, err)
			m.recordNoShadowPlan(prd.ID, in, err.Error())
			return
		}
		if plan == nil {
			return
		}
		blocking := 0
		for _, f := range plan.Findings {
			if f.Blocking {
				blocking++
			}
		}
		log.Printf("[autonomous] merge shadow: automaton %s: plan stored (release %s, %d conflict(s), %d blocking finding(s))",
			prd.ID, plan.ReleaseSize, len(plan.Conflicts), blocking)
		m.raiseIntermittentDefectAlerts(plan.IntermittentDefects)
	}()
	return done
}

// mergeShadowInput copies the runner's input out of the Automaton. ok is false
// when the Automaton has no work branch or no project directory: there is no
// merge to plan.
func (m *Manager) mergeShadowInput(prd *PRD, cfg config.MergeShadowConfig) (mergeshadow.Input, bool) {
	if prd.Git.Branch == "" || prd.ProjectDir == "" {
		return mergeshadow.Input{Project: ProjectKey(prd), Branch: prd.Git.Branch, BaseRef: prd.Git.BaseBranch}, false
	}
	return mergeshadow.Input{
		ReleaseSize: m.mergeShadowReleaseSize(prd),
		Review:      mergeShadowReview(prd),
		ChangelogDraftPath: strings.ReplaceAll(genartifacts.DefaultPaths()[genartifacts.ChangelogDraft],
			genartifacts.IDPlaceholder, prd.ID),
		CheckRootFiles: true,

		AutomatonID:    prd.ID,
		Project:        ProjectKey(prd),
		RepoPath:       prd.ProjectDir,
		Branch:         prd.Git.Branch,
		BaseRef:        prd.Git.BaseBranch,
		DataDir:        m.dataDir,
		Pipeline:       m.mergePipelineConfig(),
		ChangeRecord:   m.mergeShadowChangeRecord(prd.ID),
		ChangelogTasks: m.mergeShadowChangelogTasks(prd),
		GateTimeout:    cfg.EffectiveGateTimeout(),
	}, true
}

// mergeShadowReleaseSize is the release the Automaton's merge makes, from its
// own release size, else its kind. Empty when it has neither: nothing is
// guessed for the plan.
func (m *Manager) mergeShadowReleaseSize(prd *PRD) mergeshadow.ReleaseSize {
	if prd.ReleaseSize == "" && prd.Kind == "" {
		return ""
	}
	pipeline := m.mergePipelineConfig()
	res := ResolveReleaseSize(ReleaseSizeInput{
		ReleaseSize: prd.ReleaseSize, BreakingChange: prd.BreakingChange, Kind: prd.Kind,
		KindMap: pipeline.EffectiveReleaseSizeMap(ProjectKey(prd)), MajorTrigger: pipeline.EffectiveMajorTrigger(ProjectKey(prd)),
	})
	if size := mergeshadow.ReleaseSize(res.Size); size.Valid() {
		return size
	}
	return ""
}

// mergeShadowReview is the commit the newest passing completion review of the
// Automaton recorded, so a review of an older commit is seen. nil when no
// passing review records a commit.
func mergeShadowReview(prd *PRD) *mergeshadow.ReviewVerdict {
	var anchored []GuardrailVerdict
	for _, v := range prd.Verdicts {
		if v.Outcome == "pass" && v.JudgedCommitSHA != "" {
			anchored = append(anchored, v)
		}
	}
	if v := latestVerdict(anchored, "pass"); v != nil {
		return &mergeshadow.ReviewVerdict{Commit: v.JudgedCommitSHA}
	}
	return nil
}

// recordNoShadowPlan stores, in place of a plan that could not be made, one
// that says why.
func (m *Manager) recordNoShadowPlan(id string, in mergeshadow.Input, why string) {
	plan := &mergeshadow.MergePlan{
		AutomatonID: id, Project: in.Project, Branch: in.Branch, BaseRef: in.BaseRef,
		ReleaseSize: mergeshadow.ReleaseSizeNone, ChangelogSource: mergeshadow.ChangelogSourceMissing, CreatedAt: time.Now().UTC(),
		Findings: []mergeshadow.Finding{{Code: mergeshadow.PlanNotMadeCode, Message: "no merge plan was made: " + why, Blocking: true}},
	}
	if err := mergeshadow.Save(m.dataDir, id, plan); err != nil {
		log.Printf("[autonomous] merge shadow: automaton %s: store why no plan was made: %v", id, err)
	}
}

// mergeShadowChangeRecord returns the Automaton's merged change record, or,
// when none was stored, the merge of its per-task records. nil when there is
// neither: nothing is invented.
func (m *Manager) mergeShadowChangeRecord(automatonID string) *changerecord.ChangeRecord {
	rec, err := changerecord.Load(m.dataDir, automatonID)
	if err == nil {
		return rec
	}
	if !errors.Is(err, os.ErrNotExist) {
		log.Printf("[autonomous] merge shadow: automaton %s: change record: %v", automatonID, err)
	}
	return nil
}

// mergeShadowChangelogTasks lists the Automaton's tasks for the changelog
// reader. Only the changelog entry task's output is read: the others carry no
// changelog text, so their output is not loaded.
func (m *Manager) mergeShadowChangelogTasks(prd *PRD) []mergeshadow.ChangelogTask {
	var out []mergeshadow.ChangelogTask
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			ct := mergeshadow.ChangelogTask{ID: t.ID, Kind: t.Kind}
			if t.Kind == DefectTaskKindChangelog {
				ct.Output = m.mergeShadowTaskOutput(t.SessionID)
			}
			out = append(out, ct)
		}
	}
	return out
}

// mergeShadowTaskOutput reads a task session's recorded output through the
// wired reader. Empty when the session has no output or no reader is wired.
func (m *Manager) mergeShadowTaskOutput(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	m.mu.Lock()
	fn := m.sessionOutputFn
	m.mu.Unlock()
	if fn == nil {
		return ""
	}
	out, ok := fn(sessionID)
	if !ok {
		return ""
	}
	return out
}
