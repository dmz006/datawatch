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
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// NewSupervisorLoopHooks builds the hooks the loop steps need from the
// Manager and the operator's config. settings reads the supervisor_loop block
// live. A nil mgr or cfg yields hooks that all record "not configured".
func NewSupervisorLoopHooks(mgr *autonomous.Manager, cfg *config.Config, settings func() supervisorloop.Settings) autonomous.SupervisorLoopHooks {
	h := autonomous.SupervisorLoopHooks{Settings: settings}
	if mgr == nil || cfg == nil {
		return h
	}
	mergeCfg := func() config.MergePipelineConfig { return cfg.Autonomous.MergePipeline.Clone() }
	// No Server is in scope here, so the data directory comes from the
	// same single source (dataDirPath), with the configured data_dir first.
	dataDir := dataDirPath(cfg.DataDir)
	h.Deploy = func(ctx context.Context) (string, error) { return supervisorLoopDeploy(ctx, mgr, dataDir, mergeCfg()) }
	h.MergeUntilConflict = func(ctx context.Context) (autonomous.SupervisorMergeResult, error) {
		return supervisorLoopMerge(ctx, mgr, dataDir, mergeCfg())
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

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var (
	mergeStopBranch  = regexp.MustCompile(`merge of "([^"]+)" stopped`)
	mergeKeptInPlace = regexp.MustCompile(`already merged and left in place: ([^\n]+)`)
)

// supervisorLoopMerge runs the merge pipeline (mergepipeline.Runner.Run) over
// the completed Automaton branches waiting to merge, the same set GET
// /api/autonomous/merge lists as pending. The pipeline stops at the first
// branch its conflict policy could not settle; that branch is the conflict.
func supervisorLoopMerge(ctx context.Context, mgr *autonomous.Manager, dataDir string, cfg config.MergePipelineConfig) (autonomous.SupervisorMergeResult, error) {
	var out autonomous.SupervisorMergeResult
	repo := mgr.MergeBaseRepo()
	if repo == "" {
		return out, autonomous.ErrSupervisorStepNotConfigured
	}
	var branches []string
	for _, e := range mgr.MergeStatus().Automata {
		if e.State == autonomous.MergeStatePending && e.Branch != "" {
			branches = append(branches, e.Branch)
		}
	}
	if len(branches) == 0 {
		out.Note = "no branches waiting to merge"
		return out, nil
	}
	if !mergeRunMu.TryLock() {
		out.Note = "a merge is already running"
		return out, nil
	}
	defer mergeRunMu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, mergeRunTimeout)
	defer cancel()
	res, err := newMergeRunner(mgr, cfg, repo, filepath.Base(repo), dataDir, mergepipeline.ExecShipRunner{}).Run(runCtx, mergepipeline.RunRequest{
		Title: "supervisor loop merge", Branches: branches,
	})
	if err == nil {
		out.Merged = branches
		return out, nil
	}
	for _, st := range res.Run.Steps {
		if st.Result != autonomous.MergeStepFailed {
			continue
		}
		if m := mergeStopBranch.FindStringSubmatch(st.Detail); m != nil && strings.Contains(err.Error(), "conflict that was not settled") {
			out.Conflict = m[1]
			out.Note = firstLineOf(st.Detail)
			if k := mergeKeptInPlace.FindStringSubmatch(st.Detail); k != nil {
				for _, b := range strings.Split(k[1], ", ") {
					out.Merged = append(out.Merged, strings.TrimSpace(b))
				}
			}
			return out, err
		}
	}
	return out, err
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
