package mergepipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Defaults for the Runner's project layout. They match the layout the merge
// helper script uses; a project with another layout sets the Runner fields.
const (
	DefaultMainBranch    = "main"
	DefaultChangelogFile = "CHANGELOG.md"
	DefaultScriptDir     = "scripts"
	// DefaultStepTimeout applies to a step that sets no timeout.
	DefaultStepTimeout = 10 * time.Minute
	unreleasedHeading  = "## [Unreleased]"
	checkVersionScript = "check-merge-version.sh"
	// maxMainMovedRedos bounds how many times a run starts over from the
	// rebase step after the push finds the remote's main moved.
	maxMainMovedRedos = 3
)

// ShipRunner does the steps that leave the machine or change what runs on it:
// tag, push, deploy and the post-ship hook. They only ever run through this
// interface, and only when the pipeline lists them; tests use a fake so no
// test reaches a real remote.
type ShipRunner interface {
	// Tag tags HEAD in dir.
	Tag(ctx context.Context, dir, tag, message string) error
	// Push pushes branch and tag to the project's remote.
	Push(ctx context.Context, dir, branch, tag string) error
	// Deploy restarts the local service on the new build.
	Deploy(ctx context.Context, dir string) error
	// Hook runs a post-ship (or other leaving-the-machine) command step.
	Hook(ctx context.Context, dir string, step Step) (string, error)
	// FileIssue files an issue in repo with title and body, through gh
	// with an argument list (never a shell string) and a --body-file from
	// a temporary file.
	FileIssue(ctx context.Context, dir, repo, title, body string) error
}

// MergeLocker keeps two merge runs from shipping into the same repository at
// once. *autonomous.MergeLocks is the production one.
type MergeLocker interface {
	// Acquire takes the lock for repo. It refuses, with an error and the
	// holder's status, when another run owns the repository.
	Acquire(repo string, h autonomous.MergeLockHolder) (autonomous.MergeLockStatus, error)
	// Release frees repo when runID holds it.
	Release(repo, runID string) error
}

// RunRequest is one merge to run.
type RunRequest struct {
	// Title is the plain-language title of the release.
	Title string
	// Branches are merged in order.
	Branches    []string
	AutomatonID string
}

// RunResult is the outcome of a run.
type RunResult struct {
	Run autonomous.MergePipelineRun
}

// Runner runs a merge through the pipeline the Resolver picks for the
// project. With no project file and no record the pipeline is the built-in
// default, which reproduces the merge helper script step for step.
type Runner struct {
	// Dir is the project directory: the repository root.
	Dir     string
	Project string
	// Resolver picks the pipeline; nil uses the built-in default.
	Resolver *Resolver
	// Conflict wiring, handed to the Merger of each run.
	ConflictSettings func() ConflictSettings
	Model            ConflictModel
	Questions        QuestionRaiser
	// Counter counts applied model resolutions per project and Pending holds
	// proposals awaiting the operator's confirmation; both are handed to the
	// Merger of each run. Nil Counter means no count is kept, so a proposal
	// that needs confirmation is always asked about.
	Counter ConflictCounterStore
	Pending *ConfirmStore
	// Ship runs tag, push, deploy and hook steps. Nil means those steps fail
	// rather than run.
	Ship ShipRunner
	// Recorder receives the run after every step; nil records nothing.
	Recorder autonomous.MergeRunRecorder
	// Release gate wiring, set by the caller from the release approval
	// settings (config.MergePipelineConfig.EffectiveReleaseApproval). With
	// ReleaseApproval empty the gate is not wired and the run ships as
	// before.
	//
	// ReleaseSize is the size of this release: patch, minor, or major. The
	// caller sets it from the place the release-version step learns the
	// version (VersionFiles); empty means the size could not be read and the
	// effective approval resolves to ask rather than shipping.
	ReleaseSize string
	// ReleaseApproval is the effective approval mode for a release of this
	// size: auto, ask, or manual.
	ReleaseApproval string
	// ReleaseAnswers reads the operator's answer to the release question;
	// nil means no answer is readable and an ask-mode run holds until one
	// is.
	ReleaseAnswers ReleaseAnswerReader
	// Locks holds the repository lock for the length of a run; nil means no
	// lock is taken.
	Locks MergeLocker

	MainBranch string // default main
	// PostShip is the operator's autonomous.merge_pipeline.post_ship block.
	// When Enabled is set and the pipeline's push-release step succeeded, its
	// enabled hooks run after the pipeline's own steps, in list order, and
	// never change the run's result.
	PostShip PostShipSettings
	// Notifier receives the notify built-in's message; nil makes that hook
	// fail rather than drop the notification.
	Notifier Notifier
	// Completer marks the Automaton Completed for the mark_completed
	// built-in; nil makes that hook fail rather than skip silently.
	Completer Completer
	// VersionFiles carry `var Version = "X.Y.Z"`; they come from the
	// operator's autonomous.merge_pipeline.version_files setting. None means
	// the release-version step fails rather than guess at a path.
	VersionFiles  []string
	ChangelogFile string // default CHANGELOG.md
	ReadmeFile    string // default README.md; read by validate-release-bump
	ScriptDir     string // relative to Dir; default scripts
	Now           func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) mainBranch() string {
	if r.MainBranch != "" {
		return r.MainBranch
	}
	return DefaultMainBranch
}

func (r *Runner) versionFiles() []string { return r.VersionFiles }

var errNoVersionFiles = errors.New("no version files are configured (autonomous.merge_pipeline.version_files)")

func (r *Runner) changelog() string {
	if r.ChangelogFile != "" {
		return r.ChangelogFile
	}
	return DefaultChangelogFile
}

func (r *Runner) readme() string {
	if r.ReadmeFile != "" {
		return r.ReadmeFile
	}
	return defaultReadmeFile
}

func (r *Runner) scriptDir() string {
	d := r.ScriptDir
	if d == "" {
		d = DefaultScriptDir
	}
	return filepath.Join(r.Dir, d)
}

// runState is one run in progress.
type runState struct {
	r      *Runner
	m      *Merger
	req    RunRequest
	source string
	run    autonomous.MergePipelineRun
	// pushReleaseOK is set when a step with Action ActionPushRelease
	// reported ok; the post-ship hooks run only when it is set.
	pushReleaseOK bool
	// failedSteps counts the steps that failed; a failed check never ships
	// in any mode.
	failedSteps int
	// gate and gateSet hold the release gate verdict, decided at most once,
	// before the first step that leaves the machine.
	gate    gateVerdict
	gateSet bool
	// baseHead is the local main head the current attempt starts from: the
	// head recorded when the lock was acquired, then the head each redo
	// brings local main up to.
	baseHead string
	// observedMain is main's head as the run last saw it. ownCommits holds
	// every commit this run added to main; foreignMain is set when main moved
	// in a way the run did not make (not a fast-forward of what it saw).
	observedMain string
	ownCommits   map[string]bool
	foreignMain  bool
	// tagMade is the local tag this run created, empty when it made none.
	tagMade string
	// redos counts the redos this run has made.
	redos int
	// nothingNew is set when the merge step found every branch already in
	// main. The release steps after it are then skipped: nothing new is
	// released.
	nothingNew bool
}

// releaseSteps are the actions a run with nothing new to merge skips. They
// release, or leave the machine with, a version nobody asked for. The
// deploy step is not listed: it is a daemon restart, not a release.
var releaseSteps = map[string]bool{
	ActionSetReleaseVersion:    true,
	ActionInsertChangelog:      true,
	ActionSetReadmeReleaseLine: true,
	ActionCommitReleaseBump:    true,
	ActionTagRelease:           true,
	ActionValidateReleaseBump:  true,
	ActionPushRelease:          true,
}

// nothingNewNote is the detail recorded on a release step a run skipped.
const nothingNewNote = "nothing new was released: every branch was already merged into main"

func newRunID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b)
}

func (r *Runner) resolve() (Resolution, error) {
	if r.Resolver == nil {
		return Resolution{Definition: DefaultDefinition(), Source: SourceDefault, Reason: "no resolver configured; using the built-in pipeline"}, nil
	}
	return r.Resolver.Resolve(r.Project)
}

func (s *runState) record() {
	if s.r.Recorder != nil {
		s.r.Recorder.RecordMergeRun(s.run)
	}
}

// Run merges req.Branches through the resolved pipeline. A failing step stops
// the run unless its on_failure is continue. The returned error is non-nil
// when the run could not start or a step failed and stopped it; the result
// always carries the steps that ran.
func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	res, rerr := r.resolve()
	st := &runState{r: r, req: req, source: string(res.Source)}
	st.run = autonomous.MergePipelineRun{
		ID: newRunID(), Project: r.Project, Branches: append([]string{}, req.Branches...),
		Source: st.source, Reason: redact(res.Reason), Start: r.now(),
		Result: autonomous.MergeRunRunning, Steps: []autonomous.MergeStepRun{},
	}
	finish := func(result string, err error) (RunResult, error) {
		end := r.now()
		st.run.End, st.run.Result = &end, result
		st.record()
		return RunResult{Run: st.run}, err
	}
	if rerr != nil {
		st.run.Reason = redact(rerr.Error())
		return finish(autonomous.MergeRunFailed, fmt.Errorf("no merge pipeline could be chosen: %w", rerr))
	}
	if strings.TrimSpace(req.Title) == "" {
		return finish(autonomous.MergeRunFailed, errors.New("the title must not be empty"))
	}
	if len(req.Branches) == 0 {
		return finish(autonomous.MergeRunFailed, errors.New("at least one branch is required"))
	}
	if r.Locks != nil {
		holder := autonomous.MergeLockHolder{RunID: st.run.ID, AutomatonID: req.AutomatonID, Branches: req.Branches}
		status, err := r.Locks.Acquire(r.Dir, holder)
		if err != nil {
			st.run.Reason = redact(lockRefusal(status, err))
			return finish(autonomous.MergeRunFailed, fmt.Errorf("merge refused: %w", err))
		}
		defer func() {
			if rerr := r.Locks.Release(r.Dir, st.run.ID); rerr != nil {
				log.Printf("[merge-pipeline] release the merge lock on %s: %v", r.Dir, rerr)
			}
		}()
	}
	st.m = &Merger{
		Dir: r.Dir, Pipeline: res.Definition, Settings: r.ConflictSettings, Model: r.Model, Questions: r.Questions,
		VersionFiles: r.VersionFiles, Project: r.Project, Counter: r.Counter, Pending: r.Pending,
	}
	if err := st.startTracking(ctx); err != nil {
		st.run.Reason = redact(err.Error())
		return finish(autonomous.MergeRunFailed, err)
	}
	st.record()
	if err := st.preflight(ctx); err != nil {
		st.run.Reason = redact(err.Error())
		return finish(autonomous.MergeRunFailed, err)
	}

	steps := res.Definition.Steps
	restart := rebaseStepIndex(steps)
	failures := 0
	for i := 0; i < len(steps); {
		step := steps[i]
		if st.nothingNew && releaseSteps[step.Action] {
			_, _, _ = st.recordStep(ctx, st.source, step, DefaultStepTimeout, func(context.Context) (string, string, error) {
				return autonomous.MergeStepSkipped, nothingNewNote, nil
			})
			i++
			continue
		}
		ok, err := st.runStep(ctx, step)
		if oerr := st.observeMain(ctx); oerr != nil {
			st.run.Reason = redact(oerr.Error())
			return finish(autonomous.MergeRunFailed, oerr)
		}
		if ok {
			if step.Action == ActionPushRelease {
				st.pushReleaseOK = true
			}
			i++
			continue
		}
		if step.Action == ActionPushRelease && errors.Is(err, ErrMainMoved) {
			if st.redos >= maxMainMovedRedos {
				reason := fmt.Sprintf("main kept moving: the remote's main moved again after %d redos, so the run stopped", st.redos)
				st.run.Reason = redact(reason)
				return finish(autonomous.MergeRunFailed, fmt.Errorf("%s: %w", reason, err))
			}
			st.redos++
			if rerr := st.recordRedo(ctx, st.redos); rerr != nil {
				st.run.Reason = redact(rerr.Error())
				return finish(autonomous.MergeRunFailed, fmt.Errorf("redo %d of %d: %w", st.redos, maxMainMovedRedos, rerr))
			}
			i = restart
			continue
		}
		failures++
		st.failedSteps++
		if step.EffectiveOnFailure() != OnFailureContinue {
			return finish(autonomous.MergeRunFailed, fmt.Errorf("step %s failed: %w", step.Name, err))
		}
		i++
	}
	// The run reached here, so it is ok or completed_with_failures: that is
	// when the configured post-ship hooks run. They record themselves as
	// steps but never change the result.
	st.runPostShipHooks(ctx)
	if failures > 0 {
		return finish(autonomous.MergeRunCompletedWithFailures, nil)
	}
	return finish(autonomous.MergeRunOK, nil)
}

// lockRefusal is the plain-language reason a run was refused its repository
// lock. When the refusal names a holder, the reason names its run and
// Automaton, so the record shows who is shipping.
func lockRefusal(holder autonomous.MergeLockStatus, err error) string {
	if holder.RunID != "" || holder.AutomatonID != "" {
		return fmt.Sprintf("another merge holds this repository: run %s (Automaton %s)", holder.RunID, holder.AutomatonID)
	}
	return "the merge lock could not be taken: " + err.Error()
}

// preflight requires the main branch checked out and a clean tree, as the
// merge helper script does before it touches anything.
func (s *runState) preflight(ctx context.Context) error {
	out, err := s.m.gitOK(ctx, s.r.Dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if cur := strings.TrimSpace(out); cur != s.r.mainBranch() {
		return fmt.Errorf("current branch is %q; it must be %q to merge into it", cur, s.r.mainBranch())
	}
	out, err = s.m.gitOK(ctx, s.r.Dir, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return errors.New("working tree is not clean; commit or stash your changes first")
	}
	return nil
}

// runStep runs one step, records it and reports whether it did not fail.
func (s *runState) runStep(ctx context.Context, step Step) (bool, error) {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = DefaultStepTimeout
	}
	result, detail, err := s.recordStep(ctx, s.source, step, timeout, func(c context.Context) (string, string, error) {
		return s.execute(c, step)
	})
	if result == autonomous.MergeStepFailed {
		log.Printf("[merge-pipeline] step %s failed: %s", step.Name, firstLine(detail))
		if err == nil {
			err = errors.New(firstLine(detail))
		}
		return false, err
	}
	return true, nil
}

// recordStep appends the step's running record, runs do under the timeout
// and finishes the record with the outcome. Pipeline steps and post-ship
// hook steps share this path.
func (s *runState) recordStep(ctx context.Context, source string, step Step, timeout time.Duration, do func(context.Context) (string, string, error)) (string, string, error) {
	rec := autonomous.MergeStepRun{
		Step: step.Name, Kind: string(step.Kind), Start: s.r.now(), Result: autonomous.MergeStepRunning,
		Source: source, OnFailure: string(step.EffectiveOnFailure()),
	}
	s.run.Steps = append(s.run.Steps, rec)
	idx := len(s.run.Steps) - 1
	s.record()

	hctx, cancel := context.WithTimeout(ctx, timeout)
	result, detail, err := do(hctx)
	cancel()

	end := s.r.now()
	rec.End, rec.Result, rec.Detail = &end, result, redact(detail)
	s.run.Steps[idx] = rec
	s.record()
	return result, rec.Detail, err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// postShipStepSource is recorded on every post-ship hook step.
const postShipStepSource = "post_ship"

// Built-in post-ship hook names, matching the setting vocabulary in
// internal/config.
const (
	builtinRegistryRow   = "registry_row"
	builtinParityIssue   = "parity_issue"
	builtinTag           = "tag"
	builtinNotify        = "notify"
	builtinMarkCompleted = "mark_completed"
)

// runPostShipHooks runs the configured post-ship hooks when the block is on
// and the push-release step succeeded. Each enabled hook is one step run
// through the existing runStep path (kind post_ship_hook, source post_ship,
// on_failure continue): the hook's own on_failure is ignored, a hook failure
// is recorded on its step with its detail redacted, the run result keeps the
// value the pipeline's steps set it, and later hooks still run. Disabled
// hooks produce no step at all.
func (s *runState) runPostShipHooks(ctx context.Context) {
	cfg := s.r.PostShip
	if !cfg.Enabled || !s.pushReleaseOK || len(cfg.Hooks) == 0 {
		return
	}
	version, verr := s.releaseVersionOf()
	for _, hk := range cfg.Hooks {
		if !hk.Enabled {
			continue
		}
		step := Step{Name: hk.Name, Kind: KindPostShipHook, Command: hk.Command, OnFailure: OnFailureContinue}
		var do func(context.Context) (string, string, error)
		if hk.Builtin != "" {
			name := hk.Builtin
			do = func(c context.Context) (string, string, error) {
				detail, err := s.builtinPostShipHook(c, name, version, verr, cfg)
				return hookOutcome(detail, err)
			}
		} else {
			do = func(c context.Context) (string, string, error) { return hookOutcome(s.hook(c, step)) }
		}
		_, _, _ = s.recordStep(ctx, postShipStepSource, step, DefaultStepTimeout, do)
	}
}

// hookOutcome turns a hook's (detail, err) into a step outcome: an error
// keeps the detail on the failed step.
func hookOutcome(detail string, err error) (string, string, error) {
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return autonomous.MergeStepFailed, detail, err
	}
	return autonomous.MergeStepOK, detail, nil
}

// builtinPostShipHook dispatches one built-in hook to the function in
// posthook_builtins.go. A built-in fails when the release version it renders
// cannot be read or its wiring (ship runner, notifier) is missing, rather
// than guess.
func (s *runState) builtinPostShipHook(ctx context.Context, builtin, version string, verr error, cfg PostShipSettings) (string, error) {
	if verr != nil {
		return "", builtinErr("the %s hook cannot run: %s", builtin, verr)
	}
	dir := s.r.Dir
	project := s.r.Project
	switch builtin {
	case builtinRegistryRow:
		return RunRegistryRow(ctx, dir, project, version, cfg)
	case builtinParityIssue:
		return RunParityIssue(ctx, s.r.Ship, dir, project, version, cfg)
	case builtinTag:
		return RunTag(ctx, s.r.Ship, dir, version)
	case builtinNotify:
		return RunNotify(ctx, s.r.Notifier, project, version, cfg)
	case builtinMarkCompleted:
		return s.runMarkCompleted(ctx, version)
	}
	return "", builtinErr("unknown built-in post-ship hook %q", builtin)
}

// execute performs the step and returns its result and a short account. The
// caller bounds it with the step's timeout.
func (s *runState) execute(ctx context.Context, step Step) (string, string, error) {
	if step.RequiresMakeTarget != "" && !makefileHasTarget(s.r.Dir, step.RequiresMakeTarget) {
		return autonomous.MergeStepSkipped, "the project's Makefile has no " + step.RequiresMakeTarget + " target", nil
	}
	// The release gate applies to every step that leaves the machine, and
	// only to those: it is decided at most once per run, before the first
	// such step runs, and its verdict, in the run's plain wording, is
	// recorded on the steps it holds back.
	gate, gerr := gateVerdict{}, error(nil)
	applies := stepLeavesMachine(step)
	if applies {
		gate, gerr = s.gateForLeaving()
		if gerr != nil {
			// Asking the release decision failed; guessing is not an
			// option, so the step fails and the run stops short of the
			// ship.
			return autonomous.MergeStepFailed, "the release gate could not be applied: " + gerr.Error(), gerr
		}
		if gate.holds {
			return autonomous.MergeStepSkipped, gate.skipNote, nil
		}
	}
	var detail string
	var err error
	switch {
	case step.Action != "":
		detail, err = s.action(ctx, step)
	case leavesTheMachine(step.Kind):
		detail, err = s.hook(ctx, step)
	default:
		detail, err = s.command(ctx, step)
	}
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return autonomous.MergeStepFailed, detail, err
	}
	if applies && gate.approvedNote != "" {
		detail = gate.approvedNote + "; " + detail
	}
	return autonomous.MergeStepOK, detail, nil
}

// stepLeavesMachine reports whether the step runs through the ShipRunner:
// the ship actions, or one of the ship kinds. These are the steps the
// release gate decides about.
func stepLeavesMachine(step Step) bool {
	switch step.Action {
	case ActionTagRelease, ActionPushRelease, ActionDeployLatest:
		return true
	}
	return leavesTheMachine(step.Kind)
}

// leavesTheMachine reports the kinds that only run through the ShipRunner.
func leavesTheMachine(k Kind) bool {
	return k == KindTag || k == KindDeploy || k == KindPostShipHook
}

func (s *runState) stepDir(step Step) (string, error) {
	if step.Dir == "" {
		return s.r.Dir, nil
	}
	if err := checkDir(step.Dir); err != nil {
		return "", err
	}
	return filepath.Join(s.r.Dir, filepath.FromSlash(step.Dir)), nil
}

// command runs a shell command step in the project, with the operator's tmux
// variables removed.
func (s *runState) command(ctx context.Context, step Step) (string, error) {
	if CallsUnscopedTmuxKill(step.Command) {
		return "", errors.New("the command calls an unscoped tmux kill and was not run")
	}
	dir, err := s.stepDir(step)
	if err != nil {
		return "", err
	}
	res, err := s.m.run(ctx, dir, "sh", "-c", step.Command)
	if err != nil {
		return tail(res.Out, 2000), fmt.Errorf("did not finish: %w", err)
	}
	if res.Code != 0 {
		return fmt.Sprintf("exit %d\n%s", res.Code, tail(res.Out, 2000)), fmt.Errorf("exit %d", res.Code)
	}
	return tail(res.Out, 500), nil
}

// hook runs a tag, deploy or post-ship command step through the ShipRunner.
func (s *runState) hook(ctx context.Context, step Step) (string, error) {
	if s.r.Ship == nil {
		return "", errors.New("no ship runner is wired, so the step was not run")
	}
	if CallsUnscopedTmuxKill(step.Command) {
		return "", errors.New("the command calls an unscoped tmux kill and was not run")
	}
	dir, err := s.stepDir(step)
	if err != nil {
		return "", err
	}
	return s.r.Ship.Hook(ctx, dir, step)
}

// action runs a built-in action.
func (s *runState) action(ctx context.Context, step Step) (string, error) {
	r := s.r
	switch step.Action {
	case ActionRebaseOntoMain:
		return s.rebaseOntoMain(ctx)
	case ActionCheckBranchVersions:
		return s.checkBranchVersions(ctx)
	case ActionMergeBranches:
		return s.mergeBranches(ctx)
	case ActionSetReleaseVersion:
		return s.setReleaseVersion()
	case ActionInsertChangelog:
		v, err := s.releaseVersionOf()
		if err != nil {
			return "", err
		}
		return r.insertChangelog(v, s.req.Title)
	case ActionSetReadmeReleaseLine:
		return s.setReadmeReleaseLine()
	case ActionCommitReleaseBump:
		return s.commitBump(ctx)
	case ActionValidateReleaseBump:
		return s.validateReleaseBump(ctx)
	case ActionTagRelease, ActionPushRelease, ActionDeployLatest:
		return s.ship(ctx, step.Action)
	}
	return "", fmt.Errorf("unknown action %q", step.Action)
}

// rebaseOntoMain brings each requested branch onto the local main. The main
// branch is fetched first when the project has a remote; the fetch only
// updates the remote-tracking data and never touches a local branch. Each
// branch not already based on main is rebased in a throwaway worktree and
// then moved to the result. A conflict aborts that rebase, leaves the branch
// where it was and fails the step; branches rebased before it stay rebased.
func (s *runState) rebaseOntoMain(ctx context.Context) (string, error) {
	main := s.r.mainBranch()
	var notes []string
	remote, err := s.fetchMain(ctx, main)
	if err != nil {
		return "", err
	}
	if remote == "" {
		notes = append(notes, "no remote; the main branch was not fetched")
	} else {
		notes = append(notes, "fetched "+main+" from "+remote)
	}
	// fail builds the step's failure: the failure line leads the detail, the
	// account of the fetch and of earlier branches follows it.
	fail := func(line string, err error) (string, error) {
		return strings.Join(append([]string{line}, notes...), "\n"), err
	}
	var rebased, onMain []string
	for _, b := range s.req.Branches {
		if strings.HasPrefix(b, "-") {
			return fail(fmt.Sprintf("invalid branch name %q; nothing was rebased", b), fmt.Errorf("invalid branch name %q", b))
		}
		ref := "refs/heads/" + b
		old, err := s.m.revParse(ctx, s.r.Dir, ref)
		if err != nil {
			return fail(fmt.Sprintf("branch %q does not exist", b), fmt.Errorf("branch %q does not exist: %w", b, err))
		}
		anc, err := s.m.git(ctx, s.r.Dir, "merge-base", "--is-ancestor", "refs/heads/"+main, ref)
		if err != nil {
			return fail(fmt.Sprintf("checking whether %q is on %s failed", b, main), err)
		}
		if anc.Code == 0 {
			onMain = append(onMain, b)
			continue
		}
		if err := s.rebaseBranch(ctx, b, old, main); err != nil {
			line := fmt.Sprintf("rebase of %q onto %s stopped; that branch was left untouched: %s", b, main, firstLine(err.Error()))
			if len(rebased) > 0 {
				notes = append(notes, "already rebased and left in place: "+strings.Join(rebased, ", "))
			}
			return fail(line, fmt.Errorf("rebase of %q onto %s failed: %w", b, main, err))
		}
		rebased = append(rebased, b)
	}
	if len(rebased) > 0 {
		notes = append(notes, "rebased onto "+main+": "+strings.Join(rebased, ", "))
	}
	if len(onMain) > 0 {
		notes = append(notes, "already on "+main+": "+strings.Join(onMain, ", "))
	}
	return strings.Join(notes, "\n"), nil
}

// fetchMain fetches the main branch from the project's remote and returns the
// remote's name. It returns an empty name, and fetches nothing, when the
// project has no remote.
func (s *runState) fetchMain(ctx context.Context, main string) (string, error) {
	out, err := s.m.gitOK(ctx, s.r.Dir, "remote")
	if err != nil {
		return "", err
	}
	names := strings.Fields(out)
	if len(names) == 0 {
		return "", nil
	}
	remote := names[0]
	for _, n := range names {
		if n == "origin" {
			remote = n
		}
	}
	if _, err := s.m.gitOK(ctx, s.r.Dir, "fetch", "--quiet", remote, main); err != nil {
		return "", fmt.Errorf("fetch %s from %s: %w", main, remote, err)
	}
	return remote, nil
}

// rebaseBranch rebases branch onto the local main inside a throwaway
// worktree, then points the branch at the result, but only while it still
// names old. On any failure the rebase is aborted, nothing moves, and the
// worktree is removed.
func (s *runState) rebaseBranch(ctx context.Context, branch, old, main string) error {
	tmp, err := os.MkdirTemp(s.r.Dir, ".datawatch-rebase-")
	if err != nil {
		return err
	}
	defer func() {
		_, _ = s.m.git(context.Background(), s.r.Dir, "worktree", "remove", "--force", tmp)
		_ = os.RemoveAll(tmp)
		_, _ = s.m.git(context.Background(), s.r.Dir, "worktree", "prune")
	}()
	if _, err := s.m.gitOK(ctx, s.r.Dir, "worktree", "add", "--quiet", "--detach", tmp, old); err != nil {
		return err
	}
	res, err := s.m.git(ctx, tmp, "rebase", "refs/heads/"+main)
	if err != nil || res.Code != 0 {
		_, _ = s.m.git(context.Background(), tmp, "rebase", "--abort")
		if err != nil {
			return err
		}
		return fmt.Errorf("the rebase hit a conflict or failed (exit %d)", res.Code)
	}
	head, err := s.m.gitOK(ctx, tmp, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	_, err = s.m.gitOK(ctx, s.r.Dir, "update-ref", "refs/heads/"+branch, strings.TrimSpace(head), old)
	return err
}

func (s *runState) checkBranchVersions(ctx context.Context) (string, error) {
	script := filepath.Join(s.r.scriptDir(), checkVersionScript)
	for _, b := range s.req.Branches {
		if strings.HasPrefix(b, "-") {
			return "", fmt.Errorf("invalid branch name %q", b)
		}
		res, err := s.m.run(ctx, s.r.Dir, "env", "MAIN_BRANCH="+s.r.mainBranch(), "bash", script, b)
		if err != nil {
			return tail(res.Out, 1500), fmt.Errorf("version check of %s did not finish: %w", b, err)
		}
		if res.Code != 0 {
			return fmt.Sprintf("version check failed for %q; nothing was merged\n%s", b, tail(res.Out, 1500)),
				fmt.Errorf("version check failed for %q", b)
		}
	}
	return fmt.Sprintf("%d branch(es) passed the version check", len(s.req.Branches)), nil
}

// mergeBranches merges each branch in order. A branch the conflict policy
// could not settle stops the step; branches already merged stay in place.
func (s *runState) mergeBranches(ctx context.Context) (string, error) {
	var merged, already []string
	s.nothingNew = false
	for _, b := range s.req.Branches {
		in, err := s.branchInMain(ctx, b)
		if err != nil {
			return "", err
		}
		if in {
			already = append(already, b)
			continue
		}
		out, err := s.m.MergeBranch(ctx, MergeRequest{Branch: b, AutomatonID: s.req.AutomatonID})
		if err != nil {
			return s.mergeStopNote(merged, b, err.Error()), fmt.Errorf("merge of %q failed: %w", b, err)
		}
		switch out.Status {
		case StatusMerged, StatusResolvedMechanical, StatusResolvedByModel:
			merged = append(merged, b)
		default:
			note := out.Summary
			if out.QuestionID != "" {
				note += " (operator question " + out.QuestionID + ")"
			}
			return s.mergeStopNote(merged, b, note), fmt.Errorf("merge of %q hit a conflict that was not settled", b)
		}
	}
	if len(merged) == 0 && len(already) > 0 {
		s.nothingNew = true
		return "nothing new: already merged into " + s.r.mainBranch() + ": " + strings.Join(already, ", "), nil
	}
	note := "merged: " + strings.Join(merged, ", ")
	if len(already) > 0 {
		note += "\nalready merged into " + s.r.mainBranch() + ": " + strings.Join(already, ", ")
	}
	return note, nil
}

// branchInMain reports whether branch is already contained in the checked-out
// main: the merge would add nothing.
func (s *runState) branchInMain(ctx context.Context, branch string) (bool, error) {
	res, err := s.m.git(ctx, s.r.Dir, "merge-base", "--is-ancestor", "refs/heads/"+branch, "HEAD")
	if err != nil {
		return false, fmt.Errorf("checking whether %q is on %s failed: %w", branch, s.r.mainBranch(), err)
	}
	switch res.Code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("checking whether %q is on %s failed (exit %d)", branch, s.r.mainBranch(), res.Code)
	}
}

func (s *runState) mergeStopNote(merged []string, failed, why string) string {
	note := fmt.Sprintf("merge of %q stopped: %s", failed, why)
	if len(merged) > 0 {
		note += "\nalready merged and left in place: " + strings.Join(merged, ", ")
	}
	return note
}

func (s *runState) commitBump(ctx context.Context) (string, error) {
	v, err := s.releaseVersionOf()
	if err != nil {
		return "", err
	}
	args := append([]string{"add", "--"}, s.r.versionFiles()...)
	args = append(args, s.r.changelog())
	// The README joins the bump when the project has one: its Current
	// release: line is rewritten by set-readme-release-line.
	if _, err := os.Stat(filepath.Join(s.r.Dir, filepath.FromSlash(s.r.readme()))); err == nil {
		args = append(args, s.r.readme())
	}
	if _, err := s.m.gitOK(ctx, s.r.Dir, args...); err != nil {
		return "", err
	}
	res, err := s.m.git(ctx, s.r.Dir, "diff", "--cached", "--quiet")
	if err != nil {
		return "", err
	}
	if res.Code == 0 {
		return "nothing staged; no bump commit made", nil
	}
	msg := "chore(release): v" + v + " bump for merged branches"
	if _, err := s.m.gitOK(ctx, s.r.Dir, "commit", "-m", msg); err != nil {
		return "", err
	}
	return "committed " + msg, nil
}

// validateReleaseBump runs the release-bump validator over the repository as
// the earlier steps left it: the bump is committed and the release tag is
// made, but nothing is pushed yet. The result is stored on the run record
// whether it passes or not. A failing check fails the step and names every
// check that failed, so the push step never runs.
func (s *runState) validateReleaseBump(ctx context.Context) (string, error) {
	exclude := ""
	if s.run.ReleaseVersion != "" {
		exclude = "v" + s.run.ReleaseVersion
	}
	res := ValidateReleaseBump(ctx, ValidateInput{
		Dir:           s.r.Dir,
		VersionFiles:  s.r.versionFiles(),
		ChangelogFile: s.r.changelog(),
		ReadmeFile:    s.r.readme(),
		ExcludeTag:    exclude,
	})
	s.run.Validator = &res
	var failed []string
	var lines []string
	for _, c := range res.Checks {
		if c.Pass {
			continue
		}
		failed = append(failed, c.Name)
		lines = append(lines, c.Name+": "+c.Detail)
	}
	if len(failed) > 0 {
		detail := "release bump validator failed; nothing was pushed\n" + strings.Join(lines, "\n")
		return detail, fmt.Errorf("release bump validator failed: %s", strings.Join(failed, ", "))
	}
	return fmt.Sprintf("release bump validated: all %d checks passed for v%s", len(res.Checks), res.Version), nil
}

func (s *runState) ship(ctx context.Context, action string) (string, error) {
	if s.r.Ship == nil {
		return "", errors.New("no ship runner is wired, so the step was not run")
	}
	if action == ActionDeployLatest {
		return doneOr("deployed the new build", s.r.Ship.Deploy(ctx, s.r.Dir))
	}
	v, err := s.releaseVersionOf()
	if err != nil {
		return "", err
	}
	tag := "v" + v
	switch action {
	case ActionTagRelease:
		return s.tagRelease(ctx, tag)
	default:
		return doneOr("pushed "+s.r.mainBranch()+" and "+tag, s.r.Ship.Push(ctx, s.r.Dir, s.r.mainBranch(), tag))
	}
}

// tagRelease makes the release tag on HEAD, unless it is already there. A tag
// that already points at HEAD is left alone and the step succeeds, so a step
// run twice changes nothing. A tag that points at another commit fails the
// step, naming the tag and that commit, because moving it could rewrite a
// release someone has already published. Only a tag this call creates is
// recorded in tagMade, so a redo never deletes a tag the run did not make.
func (s *runState) tagRelease(ctx context.Context, tag string) (string, error) {
	dir := s.r.Dir
	head, err := s.m.gitOK(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("read HEAD before tagging %s: %w", tag, err)
	}
	head = strings.TrimSpace(head)
	res, err := s.m.git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+tag+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("check for tag %s: %w", tag, err)
	}
	if res.Code == 0 {
		at := strings.TrimSpace(res.Out)
		if at == head {
			return "already tagged " + tag + " at HEAD; nothing to do", nil
		}
		return "", fmt.Errorf("tag %s already exists on commit %s, not HEAD %s; refusing to move it", tag, at, head)
	}
	note, err := doneOr("tagged "+tag, s.r.Ship.Tag(ctx, dir, tag, "Release "+tag))
	if err != nil {
		return "", err
	}
	s.tagMade = tag
	return note, nil
}

// doneOr returns the success note, or no note with the error.
func doneOr(note string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return note, nil
}

// ---- main moved under the run ----

// rebaseStepIndex returns the index of the first rebase-onto-main step, the
// point a redo starts again from. A pipeline with no such step starts over
// from its first step.
func rebaseStepIndex(steps []Step) int {
	for i, s := range steps {
		if s.Action == ActionRebaseOntoMain {
			return i
		}
	}
	return 0
}

// mainHead returns the commit the local main branch points at.
func (s *runState) mainHead(ctx context.Context) (string, error) {
	return s.m.revParse(ctx, s.r.Dir, "refs/heads/"+s.r.mainBranch())
}

// startTracking records local main's head when the run takes the repository
// lock. That head is the base every attempt of the run is measured from.
func (s *runState) startTracking(ctx context.Context) error {
	head, err := s.mainHead(ctx)
	if err != nil {
		return fmt.Errorf("read %s before the run: %w", s.r.mainBranch(), err)
	}
	s.baseHead, s.observedMain = head, head
	s.ownCommits = map[string]bool{}
	return nil
}

// observeMain takes each commit main gained since the run last looked at it
// as this run's own. A move that does not extend the last head it saw marks
// main as moved outside the run, which a redo will not undo.
func (s *runState) observeMain(ctx context.Context) error {
	cur, err := s.mainHead(ctx)
	if err != nil {
		return err
	}
	if cur == s.observedMain {
		return nil
	}
	anc, err := s.m.git(ctx, s.r.Dir, "merge-base", "--is-ancestor", s.observedMain, cur)
	if err != nil {
		return err
	}
	if anc.Code != 0 {
		s.foreignMain = true
		s.observedMain = cur
		return nil
	}
	out, err := s.m.gitOK(ctx, s.r.Dir, "rev-list", s.observedMain+".."+cur)
	if err != nil {
		return err
	}
	for _, c := range strings.Fields(out) {
		s.ownCommits[c] = true
	}
	s.observedMain = cur
	return nil
}

// undoable reports whether local main can be reset to the base: every
// commit above the base must be one this run made.
func (s *runState) undoable(ctx context.Context) error {
	if s.foreignMain {
		return errors.New("main was moved by something other than this run")
	}
	out, err := s.m.gitOK(ctx, s.r.Dir, "rev-list", s.baseHead+"..refs/heads/"+s.r.mainBranch())
	if err != nil {
		return err
	}
	for _, c := range strings.Fields(out) {
		if !s.ownCommits[c] {
			return fmt.Errorf("commit %s on %s was not made by this run", c, s.r.mainBranch())
		}
	}
	return nil
}

// redoFromRebase undoes this run's local merge, bump and tag, brings local
// main up to the remote's main, and clears what the run computed from the old
// main, so the next attempt computes its version from the new one. It leaves
// the run with the synced head as its base.
func (s *runState) redoFromRebase(ctx context.Context) (string, error) {
	main := s.r.mainBranch()
	if err := s.observeMain(ctx); err != nil {
		return "", err
	}
	if err := s.undoable(ctx); err != nil {
		return "", fmt.Errorf("local %s cannot be reset to %s: %w", main, shortSHA(s.baseHead), err)
	}
	var notes []string
	if s.tagMade != "" {
		tag := s.tagMade
		if _, err := s.m.gitOK(ctx, s.r.Dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+tag); err == nil {
			if _, err := s.m.gitOK(ctx, s.r.Dir, "tag", "-d", tag); err != nil {
				return "", fmt.Errorf("delete the local tag %s: %w", tag, err)
			}
		}
		notes = append(notes, "deleted the local tag "+tag)
		s.tagMade = ""
	}
	if _, err := s.m.gitOK(ctx, s.r.Dir, "reset", "--hard", "--quiet", s.baseHead); err != nil {
		return "", fmt.Errorf("reset %s to %s: %w", main, shortSHA(s.baseHead), err)
	}
	notes = append(notes, "reset "+main+" to "+shortSHA(s.baseHead))
	remote, err := s.fetchMain(ctx, main)
	if err != nil {
		return "", err
	}
	if remote == "" {
		return "", errors.New("the project has no remote to bring " + main + " up to")
	}
	if _, err := s.m.gitOK(ctx, s.r.Dir, "merge", "--ff-only", "--quiet", "refs/remotes/"+remote+"/"+main); err != nil {
		return "", fmt.Errorf("local %s cannot fast-forward to %s/%s: %w", main, remote, main, err)
	}
	head, err := s.mainHead(ctx)
	if err != nil {
		return "", err
	}
	s.baseHead, s.observedMain = head, head
	s.ownCommits = map[string]bool{}
	s.foreignMain = false
	s.run.ReleaseVersion = ""
	s.run.Validator = nil
	s.pushReleaseOK = false
	notes = append(notes, "brought "+main+" up to "+remote+"/"+main+" at "+shortSHA(head),
		"the run starts again from the rebase step")
	return strings.Join(notes, "; "), nil
}

// recordRedo runs one redo as a recorded step, so the run record names every
// redo and what it did. A redo that cannot finish fails the run.
func (s *runState) recordRedo(ctx context.Context, n int) error {
	step := Step{Name: fmt.Sprintf("main-moved-redo-%d", n), Kind: KindCustom, OnFailure: OnFailureStop}
	result, _, err := s.recordStep(ctx, "redo", step, DefaultStepTimeout, func(c context.Context) (string, string, error) {
		note, err := s.redoFromRebase(c)
		if err != nil {
			return autonomous.MergeStepFailed, err.Error(), err
		}
		return autonomous.MergeStepOK, fmt.Sprintf("redo %d of %d: %s", n, maxMainMovedRedos, note), nil
	})
	if result == autonomous.MergeStepFailed {
		if err == nil {
			err = errors.New("the redo failed")
		}
		return err
	}
	return nil
}

// shortSHA shortens a commit id for a note.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// ---- version and changelog files ----

var versionLine = regexp.MustCompile(`(?m)^var Version = "([^"]*)"`)
var semver3 = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

func (r *Runner) readInside(rel string) (string, []byte, error) {
	if err := checkDir(rel); err != nil {
		return "", nil, fmt.Errorf("%s: %w", rel, err)
	}
	p := filepath.Join(r.Dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(p) // #nosec G304 -- relative path validated, joined under the project
	return p, data, err
}

// currentVersion reads the version from the first version file.
func (r *Runner) currentVersion() (string, error) {
	if len(r.versionFiles()) == 0 {
		return "", errNoVersionFiles
	}
	_, data, err := r.readInside(r.versionFiles()[0])
	if err != nil {
		return "", err
	}
	m := versionLine.FindSubmatch(data)
	if m == nil || !semver3.Match(m[1]) {
		return "", fmt.Errorf("could not read an X.Y.Z version from %s", r.versionFiles()[0])
	}
	return string(m[1]), nil
}

// setReleaseVersion computes the run's release version from main's version
// as it stands now, after the rebase and merge steps, and writes it to every
// version file. It runs once per run; the result is stored on the run record
// and the later steps read it from there.
func (s *runState) setReleaseVersion() (string, error) {
	if s.run.ReleaseVersion != "" {
		// Already computed for this run: when the files carry it, there is
		// nothing to write, and the bytes stay as they are.
		ok, err := s.r.versionFilesCarry(s.run.ReleaseVersion)
		if err != nil {
			return "", err
		}
		if ok {
			return "already at " + s.run.ReleaseVersion + "; version files not written", nil
		}
		return "", fmt.Errorf("the release version was already set to %s for this run", s.run.ReleaseVersion)
	}
	cur, err := s.r.currentVersion()
	if err != nil {
		return "", err
	}
	next, err := NextVersion(cur, ReleaseSize(s.r.ReleaseSize))
	if err != nil {
		return "", err
	}
	if err := s.r.writeVersion(next); err != nil {
		return "", err
	}
	s.run.ReleaseVersion = next
	return fmt.Sprintf("version %s -> %s", cur, next), nil
}

// releaseVersionOf returns the version this run releases: the one the
// set-release-version step computed for the run. A pipeline with no such step
// has no run version, so it reads main's version as it stands, as before.
func (s *runState) releaseVersionOf() (string, error) {
	if s.run.ReleaseVersion != "" {
		return s.run.ReleaseVersion, nil
	}
	return s.r.currentVersion()
}

// versionFilesCarry reports whether every version file carries v on its
// version line. A file that cannot be read or has no version line is an
// error, not a miss.
func (r *Runner) versionFilesCarry(v string) (bool, error) {
	if len(r.versionFiles()) == 0 {
		return false, errNoVersionFiles
	}
	for _, f := range r.versionFiles() {
		_, data, err := r.readInside(f)
		if err != nil {
			return false, err
		}
		m := versionLine.FindSubmatch(data)
		if m == nil {
			return false, fmt.Errorf("no `var Version = \"...\"` line in %s", f)
		}
		if string(m[1]) != v {
			return false, nil
		}
	}
	return true, nil
}

// writeVersion sets the version line in every version file, writing none of
// them unless all carry the line.
func (r *Runner) writeVersion(v string) error {
	type pending struct {
		path string
		data []byte
	}
	var out []pending
	if len(r.versionFiles()) == 0 {
		return errNoVersionFiles
	}
	for _, f := range r.versionFiles() {
		p, data, err := r.readInside(f)
		if err != nil {
			return err
		}
		if !versionLine.Match(data) {
			return fmt.Errorf("no `var Version = \"...\"` line in %s; nothing changed", f)
		}
		out = append(out, pending{p, versionLine.ReplaceAll(data, []byte(`var Version = "`+v+`"`))})
	}
	for _, o := range out {
		if err := os.WriteFile(o.path, o.data, 0o644); err != nil { // #nosec G306 -- source file, normal mode
			return err
		}
	}
	return nil
}

// insertChangelog adds `## vX.Y.Z — title` under the first unreleased heading
// unless a heading for that version is already there.
func (r *Runner) insertChangelog(version, title string) (string, error) {
	p, data, err := r.readInside(r.changelog())
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	prefix := "## v" + version + " "
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return "the changelog already has a heading for v" + version, nil
		}
	}
	at := -1
	for i, l := range lines {
		if l == unreleasedHeading {
			at = i
			break
		}
	}
	if at < 0 {
		return "", fmt.Errorf("no %q line in %s; nothing changed", unreleasedHeading, r.changelog())
	}
	ins := []string{"", "## v" + version + " — " + title, ""}
	lines = append(lines[:at+1], append(ins, lines[at+1:]...)...)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil { // #nosec G306 -- changelog, normal mode
		return "", err
	}
	return "inserted the heading for v" + version, nil
}

// readmeReleaseShape matches the version and date on a Current release: line,
// such as "**Current release: v1.2.3 (2026-11-08).**". Groups are the version
// and the date.
var readmeReleaseShape = regexp.MustCompile(`Current release: v([0-9]+\.[0-9]+\.[0-9]+) \(([0-9]{4}-[0-9]{2}-[0-9]{2})\)`)

// setReadmeReleaseLine rewrites the version and date on the README's Current
// release: line to the run's release version and the runner's date. Nothing
// else in the line or the file changes. A line that already names the version
// is left alone, so a repeated run leaves the file as the earlier run did.
func (s *runState) setReadmeReleaseLine() (string, error) {
	v, err := s.releaseVersionOf()
	if err != nil {
		return "", err
	}
	name := s.r.readme()
	p, data, err := s.r.readInside(name)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	at := -1
	for i, l := range lines {
		if _, ok := releaseLineRest(l); ok {
			at = i
			break
		}
	}
	if at < 0 {
		return "", fmt.Errorf("no %s line in %s; nothing changed", currentReleasePrefix, name)
	}
	loc := readmeReleaseShape.FindStringSubmatchIndex(lines[at])
	if loc == nil {
		return "", fmt.Errorf("the %s line in %s has no vX.Y.Z (YYYY-MM-DD) to rewrite; nothing changed", currentReleasePrefix, name)
	}
	line := lines[at]
	if line[loc[2]:loc[3]] == v {
		return "the " + currentReleasePrefix + " line in " + name + " already names v" + v, nil
	}
	date := s.r.now().Format("2006-01-02")
	lines[at] = line[:loc[2]] + v + line[loc[3]:loc[4]] + date + line[loc[5]:]
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil { // #nosec G306 -- README, normal mode
		return "", err
	}
	return "set the " + currentReleasePrefix + " line in " + name + " to v" + v + " (" + date + ")", nil
}

// ---- keeping secrets out of records and logs ----

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization|token|secret|passw(or)?d|api[_-]?key|credential)(["']?\s*[:=]\s*|\s+)(bearer\s+)?[^\s"',;]+`),
	regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`),
	regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})\b`),
}

// redact removes anything that looks like a credential from s.
func redact(s string) string {
	s = secretPatterns[0].ReplaceAllString(s, "${1}${3}[redacted]")
	s = secretPatterns[1].ReplaceAllString(s, "${1}[redacted]@")
	s = secretPatterns[2].ReplaceAllString(s, "[redacted]")
	return s
}

// ---- the production ShipRunner ----

// ExecShipRunner is the production ShipRunner: it runs git and shell
// commands in the project with the operator's tmux variables removed. Deploy
// runs DeployCommand, which the operator sets; there is no built-in one.
type ExecShipRunner struct {
	// Remote is the remote to push to; empty means origin.
	Remote string
	// DeployCommand is the shell command that restarts the service; empty
	// makes Deploy fail rather than guess.
	DeployCommand string
}

func (e ExecShipRunner) run(ctx context.Context, dir, name string, args ...string) (string, error) {
	m := &Merger{Dir: dir}
	res, err := m.run(ctx, dir, name, args...)
	if err != nil {
		return redact(tail(res.Out, 1500)), err
	}
	if res.Code != 0 {
		return redact(tail(res.Out, 1500)), fmt.Errorf("%s exited %d", name, res.Code)
	}
	return redact(tail(res.Out, 500)), nil
}

// Tag implements ShipRunner.
func (e ExecShipRunner) Tag(ctx context.Context, dir, tag, message string) error {
	_, err := e.run(ctx, dir, "git", "tag", "-a", tag, "-m", message)
	return err
}

// ErrMainMoved is returned by Push when the remote's branch has commits that
// HEAD does not contain. Nothing is pushed then: the push would have to
// rewrite the remote, and the merge run must be rebased onto the new head.
var ErrMainMoved = errors.New("remote main moved")

// Push implements ShipRunner. It fetches the remote's branch first and
// refuses with ErrMainMoved unless that branch is an ancestor of HEAD. The
// push itself never forces, so it can only fast-forward the remote. A project
// with no remote skips the guard and pushes as it always has.
func (e ExecShipRunner) Push(ctx context.Context, dir, branch, tag string) error {
	m := &Merger{Dir: dir}
	remote := e.Remote
	if remote == "" {
		remote = "origin"
	}
	if hasRemote, err := e.hasRemotes(ctx, m, dir); err != nil {
		return err
	} else if hasRemote {
		if err := e.checkFastForward(ctx, m, dir, remote, branch); err != nil {
			return err
		}
	}
	args := []string{"push", remote, branch}
	if tag != "" {
		args = append(args, tag)
	}
	_, err := e.run(ctx, dir, "git", args...)
	return err
}

// hasRemotes reports whether the repository at dir has any remote.
func (e ExecShipRunner) hasRemotes(ctx context.Context, m *Merger, dir string) (bool, error) {
	out, err := m.gitOK(ctx, dir, "remote")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// checkFastForward fetches branch from remote and returns ErrMainMoved,
// naming both heads, unless the fetched head is an ancestor of HEAD. A
// remote with no such branch has nothing to overwrite, so it passes.
func (e ExecShipRunner) checkFastForward(ctx context.Context, m *Merger, dir, remote, branch string) error {
	ls, err := m.gitOK(ctx, dir, "ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("list %s on %s: %w", branch, remote, err)
	}
	fields := strings.Fields(ls)
	if len(fields) == 0 {
		return nil
	}
	remoteHead := fields[0]
	if _, err := m.gitOK(ctx, dir, "fetch", "--quiet", remote, branch); err != nil {
		return fmt.Errorf("fetch %s from %s: %w", branch, remote, err)
	}
	head, err := m.gitOK(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	head = strings.TrimSpace(head)
	res, err := m.git(ctx, dir, "merge-base", "--is-ancestor", remoteHead, head)
	if err != nil {
		return err
	}
	switch res.Code {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%w: %s on %s is at %s, which HEAD (%s) does not contain; nothing was pushed",
			ErrMainMoved, branch, remote, remoteHead, head)
	default:
		return fmt.Errorf("check %s against HEAD: exit %d: %s", branch, res.Code, strings.TrimSpace(tail(res.Out, 500)))
	}
}

// Deploy implements ShipRunner.
func (e ExecShipRunner) Deploy(ctx context.Context, dir string) error {
	if strings.TrimSpace(e.DeployCommand) == "" {
		return errors.New("no deploy command is configured")
	}
	if CallsUnscopedTmuxKill(e.DeployCommand) {
		return errors.New("the deploy command calls an unscoped tmux kill")
	}
	_, err := e.run(ctx, dir, "sh", "-c", e.DeployCommand)
	return err
}

// Hook implements ShipRunner.
func (e ExecShipRunner) Hook(ctx context.Context, dir string, step Step) (string, error) {
	return e.run(ctx, dir, "sh", "-c", step.Command)
}

// FileIssue implements ShipRunner. It runs `gh issue create` with an
// argument list, never a shell string, and passes the body through a
// temporary file with --body-file.
func (e ExecShipRunner) FileIssue(ctx context.Context, dir, repo, title, body string) error {
	tmp, err := os.CreateTemp(dir, "issue-body-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, werr := tmp.WriteString(body); werr != nil {
		_ = tmp.Close()
		return werr
	}
	if cerr := tmp.Close(); cerr != nil {
		return cerr
	}
	_, err = e.run(ctx, dir, "gh", "issue", "create", "--repo", repo, "--title", title, "--body-file", name) // #nosec G204 -- fixed binary, arguments built here
	return err
}
