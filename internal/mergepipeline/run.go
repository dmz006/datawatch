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
	// Ship runs tag, push, deploy and hook steps. Nil means those steps fail
	// rather than run.
	Ship ShipRunner
	// Recorder receives the run after every step; nil records nothing.
	Recorder autonomous.MergeRunRecorder

	MainBranch string // default main
	// VersionFiles carry `var Version = "X.Y.Z"`; they come from the
	// operator's autonomous.merge_pipeline.version_files setting. None means
	// the release-version step fails rather than guess at a path.
	VersionFiles  []string
	ChangelogFile string // default CHANGELOG.md
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
}

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
	st.m = &Merger{Dir: r.Dir, Pipeline: res.Definition, Settings: r.ConflictSettings, Model: r.Model, Questions: r.Questions}
	st.record()
	if err := st.preflight(ctx); err != nil {
		st.run.Reason = redact(err.Error())
		return finish(autonomous.MergeRunFailed, err)
	}

	failures := 0
	for _, step := range res.Definition.Steps {
		ok, err := st.runStep(ctx, step)
		if ok {
			continue
		}
		failures++
		if step.EffectiveOnFailure() != OnFailureContinue {
			return finish(autonomous.MergeRunFailed, fmt.Errorf("step %s failed: %w", step.Name, err))
		}
	}
	if failures > 0 {
		return finish(autonomous.MergeRunCompletedWithFailures, nil)
	}
	return finish(autonomous.MergeRunOK, nil)
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
	rec := autonomous.MergeStepRun{
		Step: step.Name, Kind: string(step.Kind), Start: s.r.now(), Result: autonomous.MergeStepRunning,
		Source: s.source, OnFailure: string(step.EffectiveOnFailure()),
	}
	s.run.Steps = append(s.run.Steps, rec)
	idx := len(s.run.Steps) - 1
	s.record()

	result, detail, err := s.execute(ctx, step)
	end := s.r.now()
	rec.End, rec.Result, rec.Detail = &end, result, redact(detail)
	s.run.Steps[idx] = rec
	s.record()
	if result == autonomous.MergeStepFailed {
		log.Printf("[merge-pipeline] step %s failed: %s", step.Name, firstLine(rec.Detail))
		if err == nil {
			err = errors.New(firstLine(rec.Detail))
		}
		return false, err
	}
	return true, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// execute performs the step and returns its result and a short account.
func (s *runState) execute(ctx context.Context, step Step) (string, string, error) {
	if step.RequiresMakeTarget != "" && !makefileHasTarget(s.r.Dir, step.RequiresMakeTarget) {
		return autonomous.MergeStepSkipped, "the project's Makefile has no " + step.RequiresMakeTarget + " target", nil
	}
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = DefaultStepTimeout
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var detail string
	var err error
	switch {
	case step.Action != "":
		detail, err = s.action(sctx, step)
	case leavesTheMachine(step.Kind):
		detail, err = s.hook(sctx, step)
	default:
		detail, err = s.command(sctx, step)
	}
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return autonomous.MergeStepFailed, detail, err
	}
	return autonomous.MergeStepOK, detail, nil
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
	case ActionCheckBranchVersions:
		return s.checkBranchVersions(ctx)
	case ActionMergeBranches:
		return s.mergeBranches(ctx)
	case ActionSetReleaseVersion:
		cur, err := r.currentVersion()
		if err != nil {
			return "", err
		}
		next, err := nextPatch(cur)
		if err != nil {
			return "", err
		}
		if err := r.writeVersion(next); err != nil {
			return "", err
		}
		return fmt.Sprintf("version %s -> %s", cur, next), nil
	case ActionInsertChangelog:
		v, err := r.currentVersion()
		if err != nil {
			return "", err
		}
		return r.insertChangelog(v, s.req.Title)
	case ActionCommitReleaseBump:
		return s.commitBump(ctx)
	case ActionTagRelease, ActionPushRelease, ActionDeployLatest:
		return s.ship(ctx, step.Action)
	}
	return "", fmt.Errorf("unknown action %q", step.Action)
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
	var merged []string
	for _, b := range s.req.Branches {
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
	return "merged: " + strings.Join(merged, ", "), nil
}

func (s *runState) mergeStopNote(merged []string, failed, why string) string {
	note := fmt.Sprintf("merge of %q stopped: %s", failed, why)
	if len(merged) > 0 {
		note += "\nalready merged and left in place: " + strings.Join(merged, ", ")
	}
	return note
}

func (s *runState) commitBump(ctx context.Context) (string, error) {
	v, err := s.r.currentVersion()
	if err != nil {
		return "", err
	}
	args := append([]string{"add", "--"}, s.r.versionFiles()...)
	args = append(args, s.r.changelog())
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

func (s *runState) ship(ctx context.Context, action string) (string, error) {
	if s.r.Ship == nil {
		return "", errors.New("no ship runner is wired, so the step was not run")
	}
	v, err := s.r.currentVersion()
	if err != nil && action != ActionDeployLatest {
		return "", err
	}
	tag := "v" + v
	switch action {
	case ActionTagRelease:
		return doneOr("tagged "+tag, s.r.Ship.Tag(ctx, s.r.Dir, tag, "Release "+tag))
	case ActionPushRelease:
		return doneOr("pushed "+s.r.mainBranch()+" and "+tag, s.r.Ship.Push(ctx, s.r.Dir, s.r.mainBranch(), tag))
	default:
		return doneOr("deployed the new build", s.r.Ship.Deploy(ctx, s.r.Dir))
	}
}

// doneOr returns the success note, or no note with the error.
func doneOr(note string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return note, nil
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

func nextPatch(v string) (string, error) {
	m := semver3.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("%q is not an X.Y.Z version", v)
	}
	var patch int
	fmt.Sscanf(m[3], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", m[1], m[2], patch+1), nil
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

// Push implements ShipRunner.
func (e ExecShipRunner) Push(ctx context.Context, dir, branch, tag string) error {
	remote := e.Remote
	if remote == "" {
		remote = "origin"
	}
	_, err := e.run(ctx, dir, "git", "push", remote, branch, tag)
	return err
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
