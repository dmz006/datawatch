// Merge run route: POST /api/autonomous/merge/run.
//
// It is the daemon's entry point to the merge pipeline. The route builds a
// mergepipeline.Runner for the configured worktree base repo, with the Manager
// as the recorder, so every step lands in "pipeline_runs" of
// GET /api/autonomous/merge. With no pipeline file and no daemon record the
// built-in default pipeline runs, which reproduces the merge helper script.
// The Runner lives in package mergepipeline because that package imports
// autonomous, so autonomous cannot import it back.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// mergeRunTimeout bounds one whole merge run.
const mergeRunTimeout = 60 * time.Minute

// mergeLockSource is the optional Manager surface that supplies the
// repository merge lock. A Manager without it runs with no lock.
type mergeLockSource interface {
	MergeLocks() (*autonomous.MergeLocks, error)
}

// unavailableMergeLocks refuses every lock request when the lock store cannot
// be opened, so a run never proceeds unlocked because its store is broken.
type unavailableMergeLocks struct{ err error }

// Acquire implements mergepipeline.MergeLocker by refusing the lock.
func (u unavailableMergeLocks) Acquire(string, autonomous.MergeLockHolder) (autonomous.MergeLockStatus, error) {
	return autonomous.MergeLockStatus{}, fmt.Errorf("merge lock store unavailable: %w", u.err)
}

// Release implements mergepipeline.MergeLocker; nothing is ever held.
func (unavailableMergeLocks) Release(string, string) error { return nil }

// mergeRunner is the Manager surface the run route needs.
type mergeRunner interface {
	autonomous.MergeRunRecorder
	MergeBaseRepo() string
	MergeQuestionStore() *autonomous.QuestionStore
	// AppendTriageNote appends one annotation to the owning Automaton's
	// existing triage record, via the same note API the autonomous panel
	// already uses; the run route adds no note channel of its own.
	AppendTriageNote(prdID, source, text string) error
	// AlertSink is the daemon's operator alert callback, nil when the daemon
	// has not wired one — the same sender autonomous alerts use, so the
	// notify hook introduces no messaging channel of its own.
	AlertSink() func(level, title, body string) string
}

// mergeRunBody is the POST body.
type mergeRunBody struct {
	// Project names the project for per-project settings; empty uses the
	// base name of the worktree base repo.
	Project  string   `json:"project"`
	Title    string   `json:"title"`
	Branches []string `json:"branches"`
	// Automaton names the Automaton the run belongs to (its PRD id). When
	// set, a failed post-ship hook is also written to that Automaton's
	// triage record as a triage note with source "post_ship"; when empty
	// the hook failures live on the merge run record alone.
	Automaton string `json:"automaton,omitempty"`
}

// mergeRunMu allows one merge run at a time.
var mergeRunMu sync.Mutex

// newMergeRunner builds the Runner for one project from the operator's
// settings. Nothing here names a repository or path: the project directory is
// the configured worktree base repo, and the version files, conflict policy
// and pipeline sources are autonomous.merge_pipeline settings. dataDir is the
// daemon data directory the record store lives under, resolved by the caller
// (Server.statePath on the request path).
func newMergeRunner(mr mergeRunner, cfg config.MergePipelineConfig, repo, project, dataDir string, ship mergepipeline.ShipRunner) *mergepipeline.Runner {
	var records *mergepipeline.RecordStore
	if rs, err := mergepipeline.NewRecordStore(dataDir); err == nil {
		records = rs
	}
	resolver := &mergepipeline.Resolver{
		Records:    records,
		ProjectDir: func(string) (string, bool) { return repo, true },
		Settings: func(p string) mergepipeline.Settings {
			return mergepipeline.Settings{
				FileName:          cfg.EffectiveFileName(p),
				SourceOrder:       cfg.EffectiveSourceOrder(p),
				FallbackOnBadFile: cfg.EffectiveFallbackOnBadFile(p),
			}
		},
	}
	runner := &mergepipeline.Runner{
		Dir: repo, Project: project, Resolver: resolver,
		VersionFiles: cfg.EffectiveVersionFiles(project),
		Ship:         ship,
		Recorder:     mr,
		PostShip:     postShipSettings(cfg),
		// The notify built-in delivers through the daemon's alert callback —
		// the same sender autonomous alerts use — not a new channel.
		Notifier: &mergeNotifier{sink: mr.AlertSink(), project: project},
		ConflictSettings: func() mergepipeline.ConflictSettings {
			s := mergepipeline.DefaultConflictSettings()
			// The LLM registry is not passed here, so the model policy
			// degrades to stop_and_ask exactly as ResolveConflictPolicy describes.
			policy, _, _ := cfg.ResolveConflictPolicy(project, nil)
			s.Policy = mergepipeline.ConflictPolicy(policy)
			s.Mode = cfg.EffectiveConflictMode(project)
			s.ConfirmFirstN = cfg.EffectiveConflictConfirmFirstN(project)
			return s
		},
	}
	if name := cfg.EffectiveConflictModel(project); name != "" {
		runner.Model = newConflictModel(mr, name)
	}
	// The counter and the pending confirmations live under the daemon data
	// directory. If either file cannot be read, it is left unwired: the count
	// reads as zero and a proposal that needs confirmation is asked about.
	if counter, err := mergepipeline.NewConflictCounter(dataDir); err != nil {
		log.Printf("[merge-pipeline] conflict counter unavailable, confirmations will be asked: %v", err)
	} else {
		runner.Counter = counter
	}
	if pending, err := mergepipeline.NewConfirmStore(dataDir); err != nil {
		log.Printf("[merge-pipeline] pending confirmations unavailable: %v", err)
	} else {
		runner.Pending = pending
	}
	if q := mr.MergeQuestionStore(); q != nil {
		runner.Questions = q
		// The gate reads the operator's answer through the same store that
		// raises its question, so an ask-mode run finds a yes it already
		// raised rather than raising a second one.
		runner.ReleaseAnswers = q
	}
	// Release gate wiring for this runner: the effective approval mode for
	// this release's size, resolved by the caller's settings. The size comes
	// from the same place the release-version step learns the version:
	// the pipeline bumps a patch, so a readable version is a patch release;
	// with no version file the size is unknown and the approval resolves to
	// ask rather than shipping.
	size := config.ReleaseSizePatch
	if len(runner.VersionFiles) == 0 {
		size = ""
	}
	runner.ReleaseSize = size
	runner.ReleaseApproval = cfg.EffectiveReleaseApproval(project, size)
	if src, ok := mr.(mergeLockSource); ok {
		if locks, err := src.MergeLocks(); err != nil {
			runner.Locks = unavailableMergeLocks{err: err}
		} else {
			runner.Locks = locks
		}
	}
	return runner
}

// handleAutonomousMergeRun serves POST /api/autonomous/merge/run. It runs the
// merge synchronously and answers with the recorded run. A step that fails
// answers 200 with "result":"failed" and the steps that ran, so the caller
// sees the record; only a request that could not start is an error status.
func (s *Server) handleAutonomousMergeRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	mr, ok := s.autonomousMgr.(mergeRunner)
	if !ok {
		http.Error(w, "Automaton merge unavailable", http.StatusServiceUnavailable)
		return
	}
	var body mergeRunBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	repo := mr.MergeBaseRepo()
	if repo == "" {
		http.Error(w, autonomous.ErrMergeNoBaseRepo.Error(), http.StatusServiceUnavailable)
		return
	}
	project := body.Project
	if project == "" {
		project = filepath.Base(repo)
	}
	if !mergeRunMu.TryLock() {
		http.Error(w, "a merge is already running", http.StatusConflict)
		return
	}
	defer mergeRunMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), mergeRunTimeout)
	defer cancel()
	res, err := newMergeRunner(mr, s.mergePipelineConfig(), repo, project, s.statePath(), s.mergeShipRunner()).Run(ctx, mergepipeline.RunRequest{
		Title: body.Title, Branches: body.Branches, AutomatonID: body.Automaton,
	})
	if err == nil && body.Automaton != "" {
		writePostShipTriageNotes(mr, body.Automaton, res.Run)
	}
	out := map[string]any{"run": res.Run}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSONOK(w, out)
}

// mergeShipRunner is the Runner's Ship step for a run: the test override
// when set, the production runner otherwise.
func (s *Server) mergeShipRunner() mergepipeline.ShipRunner {
	if s.mergeShip != nil {
		return s.mergeShip
	}
	return mergepipeline.ExecShipRunner{}
}

// mergeNotifier delivers the notify built-in's message through the daemon's
// alert callback — the same sender autonomous alerts use — so the merge run
// route introduces no messaging channel of its own.
type mergeNotifier struct {
	sink    func(level, title, body string) string
	project string
}

// Notify implements mergepipeline.Notifier.
func (n *mergeNotifier) Notify(_ context.Context, text string) error {
	if n.sink == nil {
		return errors.New("the daemon has no alert sink wired, so the notification was not sent")
	}
	n.sink("info", "shipped "+n.project, text)
	return nil
}

// postShipSettings copies the operator's post-ship hook block into the
// Runner's shape, hook order preserved.
func postShipSettings(cfg config.MergePipelineConfig) mergepipeline.PostShipSettings {
	ps := cfg.EffectivePostShip()
	hooks := make([]mergepipeline.PostShipHook, 0, len(ps.Hooks))
	for i := range ps.Hooks {
		h := &ps.Hooks[i]
		hooks = append(hooks, mergepipeline.PostShipHook{Name: h.Name, Builtin: h.Builtin, Command: h.Command, Enabled: h.Enabled})
	}
	return mergepipeline.PostShipSettings{
		Enabled:             ps.Enabled,
		Hooks:               hooks,
		RegistryFile:        ps.RegistryFile,
		RegistryRowPattern:  ps.RegistryRowPattern,
		CompanionRepository: ps.CompanionRepository,
		IssueTitleTemplate:  ps.IssueTitleTemplate,
		NotifyTemplate:      ps.NotifyTemplate,
	}
}

// postShipFailedSteps are the failed post-ship hooks of a run, in run order.
func postShipFailedSteps(run autonomous.MergePipelineRun) []autonomous.MergeStepRun {
	var failed []autonomous.MergeStepRun
	for _, step := range run.Steps {
		if step.Source == "post_ship" && step.Result == autonomous.MergeStepFailed {
			failed = append(failed, step)
		}
	}
	return failed
}

// writePostShipTriageNotes appends one triage note per failed post-ship hook
// to the owning Automaton's triage record, so whoever reads the Automaton
// sees which hooks failed without opening the merge run record. A note that
// cannot be written (no triage record yet, for instance) is logged and never
// fails the run: the ship itself is already done.
func writePostShipTriageNotes(mr mergeRunner, automaton string, run autonomous.MergePipelineRun) {
	for _, step := range postShipFailedSteps(run) {
		text := fmt.Sprintf("post-ship hook %q failed", step.Step)
		if d := step.Detail; d != "" {
			text += ": " + d
		}
		if err := mr.AppendTriageNote(automaton, "post_ship", text); err != nil {
			log.Printf("[autonomous] automaton %s: triage note for failed post-ship hook %s: %v", automaton, step.Step, err)
		}
	}
}

// mergePipelineConfig returns the operator's autonomous.merge_pipeline block.
func (s *Server) mergePipelineConfig() config.MergePipelineConfig {
	if s.cfg == nil {
		return config.MergePipelineConfig{}
	}
	return s.cfg.Autonomous.MergePipeline.Clone()
}
