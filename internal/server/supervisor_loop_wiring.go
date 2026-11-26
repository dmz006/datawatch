// Daemon wiring for the supervisor loop's mechanical steps. The steps live in
// package autonomous; three of them need the merge pipeline (package
// mergepipeline imports autonomous, so autonomous cannot reach it) or the
// operator's lane config, so the daemon supplies them as hooks built here.

package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/planrecords"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// NewSupervisorLoopHooks builds the hooks the loop steps need from the
// Manager and the operator's config. settings reads the supervisor_loop block
// live. A nil mgr or cfg yields hooks that all record "not configured".
func NewSupervisorLoopHooks(mgr *autonomous.Manager, cfg *config.Config, settings func() supervisorloop.Settings, planStore ...func() *planrecords.Store) autonomous.SupervisorLoopHooks {
	h := autonomous.SupervisorLoopHooks{Settings: settings}
	if mgr == nil || cfg == nil {
		return h
	}
	// The merge run and deploy run switch holds the merge and deploy steps.
	h.MergeDeployRunEnabled = func() bool { return cfg.Autonomous.MergeDeployRunEnabled }
	mergeCfg := func() config.MergePipelineConfig { return cfg.Autonomous.MergePipeline.Clone() }
	// No Server is in scope here, so the data directory comes from the
	// same single source (dataDirPath), with the configured data_dir first.
	dataDir := dataDirPath(cfg.DataDir)
	// The daemon's own merge runs gate the registry_row hook the same way
	// the request path does: write-through records the row, off edits the
	// registry file as before.
	// The configured git hosts are read from the live config on every run, so
	// a host added later is seen by the next merge.
	popt := supervisorMergeOption(cfg, planStore...)
	h.Deploy = func(ctx context.Context) (string, error) { return supervisorLoopDeploy(ctx, mgr, dataDir, mergeCfg()) }
	// A configured health check (autonomous.deploy.health_check) runs after a
	// successful deploy and its result goes on the deploy record.
	h.DeployHealth = func(ctx context.Context) (string, string) {
		return supervisorLoopDeployHealth(ctx, mgr, dataDir, mergeCfg(), cfg.Autonomous.Deploy.HealthCheck)
	}
	h.MergeUntilConflict = func(ctx context.Context) (autonomous.SupervisorMergeResult, error) {
		return supervisorLoopMerge(ctx, mgr, dataDir, mergeCfg(), popt)
	}
	h.Lanes = supervisorLaneControl{mgr: mgr}
	return h
}

// supervisorLoopDeploy runs the deploy step the operator's release flow
// defines: the first deploy-kind step with a command in the project's merge
// pipeline (mergepipeline.Resolver picks the pipeline), run through the same
// ShipRunner the pipeline uses. No such step means not configured.
func supervisorLoopDeploy(ctx context.Context, mgr *autonomous.Manager, dataDir string, cfg config.MergePipelineConfig) (string, error) {
	repo := mgr.MergeBaseRepo()
	if repo == "" {
		return "", autonomous.ErrSupervisorStepNotConfigured
	}
	project := filepath.Base(repo)
	runner := newMergeRunner(mgr, cfg, repo, project, dataDir, mergepipeline.ExecShipRunner{})
	res, err := runner.Resolver.Resolve(project)
	if err != nil {
		return "", fmt.Errorf("resolve merge pipeline: %w", err)
	}
	for _, st := range res.Definition.Steps {
		if st.Kind != mergepipeline.KindDeploy || strings.TrimSpace(st.Command) == "" {
			continue
		}
		if mergepipeline.CallsUnscopedTmuxKill(st.Command) {
			return "", errors.New("the deploy command calls an unscoped tmux kill and was not run")
		}
		out, err := runner.Ship.Hook(ctx, repo, st)
		if err != nil {
			return "", err
		}
		return firstLineOf(out), nil
	}
	return "", autonomous.ErrSupervisorStepNotConfigured
}

// supervisorLoopDeployHealth runs the operator's health check command after a
// deploy. No command set means unchecked; exit 0 is passed; anything else is
// failed, with the command's first output line as the detail.
func supervisorLoopDeployHealth(ctx context.Context, mgr *autonomous.Manager, dataDir string, cfg config.MergePipelineConfig, command string) (string, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return autonomous.DeployHealthUnchecked, ""
	}
	if mergepipeline.CallsUnscopedTmuxKill(command) {
		return autonomous.DeployHealthFailed, "the health check calls an unscoped tmux kill and was not run"
	}
	repo := mgr.MergeBaseRepo()
	if repo == "" {
		return autonomous.DeployHealthUnchecked, ""
	}
	runner := newMergeRunner(mgr, cfg, repo, filepath.Base(repo), dataDir, mergepipeline.ExecShipRunner{})
	out, err := runner.Ship.Hook(ctx, repo, mergepipeline.Step{Command: command})
	if err != nil {
		return autonomous.DeployHealthFailed, firstLineOf(err.Error())
	}
	return autonomous.DeployHealthPassed, firstLineOf(out)
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var mergeStopBranch = regexp.MustCompile(`merge of "([^"]+)" stopped`)

// supervisorLoopMerge runs the merge pipeline (mergepipeline.Runner.Run) over
// what the merge selection rule picks among the branches GET
// /api/autonomous/merge lists as pending (GET .../merge?selection=true).
func supervisorLoopMerge(ctx context.Context, mgr *autonomous.Manager, dataDir string, cfg config.MergePipelineConfig, opts ...mergeRunnerOption) (autonomous.SupervisorMergeResult, error) {
	if mgr.MergeBaseRepo() == "" {
		return autonomous.SupervisorMergeResult{}, autonomous.ErrSupervisorStepNotConfigured
	}
	// What was pushed and not recorded is recorded first, so it is not
	// listed as waiting.
	reconcileMergePushes(ctx, mgr, dataDir, cfg)
	return supervisorLoopMergeSelected(ctx, mgr, dataDir, cfg, mgr.MergeSelection(ctx, ""), opts...)
}

// supervisorLoopMergeSelected runs one pipeline run per eligible Automaton. It
// stops at the first branch the conflict policy could not settle, at the first
// failure, and after a run whose push did not happen, so nothing is merged on
// top of an unpushed merge. Each excluded candidate is reported with why.
func supervisorLoopMergeSelected(ctx context.Context, mgr *autonomous.Manager, dataDir string, cfg config.MergePipelineConfig, sel autonomous.MergeSelection, opts ...mergeRunnerOption) (autonomous.SupervisorMergeResult, error) {
	var out autonomous.SupervisorMergeResult
	repo := mgr.MergeBaseRepo()
	for _, e := range sel.Excluded {
		out.Excluded = append(out.Excluded, fmt.Sprintf("%s (%s): %s", e.AutomatonID, e.Branch, strings.Join(e.Reasons, "; ")))
	}
	if len(sel.Eligible) == 0 {
		out.Note = "no branches waiting to merge"
		if len(sel.Excluded) > 0 {
			out.Note = "no branch waiting to merge is eligible"
		}
		return out, nil
	}
	if !mergeRunMu.TryLock() {
		out.Note = "a merge is already running"
		return out, nil
	}
	defer mergeRunMu.Unlock()

	for _, e := range sel.Eligible {
		runCtx, cancel := context.WithTimeout(ctx, mergeRunTimeout)
		res, err := newMergeRunner(mgr, cfg, repo, filepath.Base(repo), dataDir, mergepipeline.ExecShipRunner{}, opts...).Run(runCtx, mergepipeline.RunRequest{
			Title: supervisorMergeTitle(mgr, e.AutomatonID), Branches: []string{e.Branch}, AutomatonID: e.AutomatonID,
		})
		cancel()
		if err == nil {
			out.Merged = append(out.Merged, e.Branch)
			if !res.Pushed {
				out.NotMerged = append(out.NotMerged, e.Branch)
				out.Note = "stopped after " + e.Branch + ": its push did not happen, so nothing more is merged on top of it"
				return out, nil
			}
			continue
		}
		for _, st := range res.Run.Steps {
			if st.Result != autonomous.MergeStepFailed {
				continue
			}
			if m := mergeStopBranch.FindStringSubmatch(st.Detail); m != nil && strings.Contains(err.Error(), "conflict that was not settled") {
				out.Conflict = m[1]
				out.Note = firstLineOf(st.Detail)
				return out, err
			}
		}
		return out, err
	}
	return out, nil
}

// supervisorMergeOption is what the daemon adds to each runner of a loop
// merge: the plan record store, the git hosts as configured now, and the
// merge run and deploy run switch, asked again before the push.
func supervisorMergeOption(cfg *config.Config, planStore ...func() *planrecords.Store) mergeRunnerOption {
	return func(r *mergepipeline.Runner) {
		if len(planStore) > 0 && planStore[0] != nil {
			withPlanRecords(planStore[0](), cfg.Autonomous.PlanRecords.EffectiveWriteThrough())(r)
		}
		withGitHosts(cfg.Git.Hosts)(r)
		withMergeSwitch(func() bool { return cfg.Autonomous.MergeDeployRunEnabled })(r)
	}
}

// supervisorMergeTitle is the release title of a loop merge: the Automaton's
// own, so the changelog says what shipped.
func supervisorMergeTitle(mgr *autonomous.Manager, id string) string {
	if f, ok := mgr.MergeReleaseFacts(id, nil, ""); ok && f.Title != "" {
		return f.Title
	}
	return "supervisor loop merge"
}

// supervisorLaneControl moves the Claude lane count (autonomous.lane_fill
// claude_lanes) at runtime. The loop has no demand signal of its own, so the
// wanted count is the running count: the step's effect is to pull a count that
// sits outside the operator's supervisor_loop.lane_count bounds back inside
// them, never to push it past them.
type supervisorLaneControl struct{ mgr *autonomous.Manager }

func (l supervisorLaneControl) Current() int { return l.mgr.Config().LaneFill.EffectiveClaudeLanes() }
func (l supervisorLaneControl) Wanted() int  { return l.Current() }
func (l supervisorLaneControl) SetCount(n int) error {
	cfg := l.mgr.Config()
	cfg.LaneFill.ClaudeLanes = n
	l.mgr.SetConfig(cfg)
	return nil
}

// RegisterSupervisorWake registers the built-in 'supervisor-wake' set from the
// live supervisor_loop.wake settings. It returns nil, registering nothing,
// while the wake path is off.
func RegisterSupervisorWake(mgr *autonomous.Manager, settings supervisorloop.Settings, clock supervisorloop.WakeClock,
	session func() string, send func(sessionID, text string) error, rec *supervisorloop.WakeRecord) *autonomous.SupervisorWakeSet {
	if mgr == nil {
		return nil
	}
	return autonomous.RegisterSupervisorWake(autonomous.SupervisorWakeConfig{
		Subscriber: mgr, Settings: settings.Wake, Session: session, SendInput: send, Clock: clock, Record: rec,
	})
}
