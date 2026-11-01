package mergepipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// ConflictPolicy says what happens when merging a branch conflicts.
type ConflictPolicy string

// Conflict policies.
const (
	// PolicyStopAndAsk leaves the merge aborted and asks the operator.
	PolicyStopAndAsk ConflictPolicy = "stop_and_ask"
	// PolicyMechanical resolves additive conflicts only; when anything else is
	// left it leaves the merge aborted and asks the operator.
	PolicyMechanical ConflictPolicy = "mechanical"
	// PolicyMechanicalAndModel resolves additive conflicts first and then asks
	// the configured conflict model to propose the rest. A proposal is kept
	// only when the project's full test step passes on it.
	PolicyMechanicalAndModel ConflictPolicy = "mechanical_and_model"
)

// ConflictPolicies lists every valid policy.
func ConflictPolicies() []ConflictPolicy {
	return []ConflictPolicy{PolicyStopAndAsk, PolicyMechanical, PolicyMechanicalAndModel}
}

// Defaults for ConflictSettings.
const (
	DefaultConflictModelTimeout = 5 * time.Minute
	DefaultConflictPromptBytes  = 64 * 1024
	DefaultConflictTestTimeout  = 10 * time.Minute
	DefaultConflictScratchDir   = ".datawatch/conflict-scratch"
	// ConflictQuestionAsker is the asked_by of the question raised for a
	// conflict nobody could settle.
	ConflictQuestionAsker = "merge-conflict"
)

// ConflictSettings are the operator's rules for merge conflicts.
type ConflictSettings struct {
	// Policy is the conflict policy; empty means stop_and_ask.
	Policy ConflictPolicy
	// ModelTimeout bounds the one call to the conflict model.
	ModelTimeout time.Duration
	// MaxPromptBytes caps the prompt sent to the model; a larger one is not
	// sent and the operator is asked instead.
	MaxPromptBytes int
	// TestTimeout bounds a test step that sets no timeout of its own.
	TestTimeout time.Duration
	// ScratchDir is where the scratch worktree for a proposal is made,
	// relative to the project directory.
	ScratchDir string
}

// DefaultConflictSettings returns the operator's answers: stop and ask, a five
// minute model call, a 64 KiB prompt, a ten minute test step.
func DefaultConflictSettings() ConflictSettings {
	return ConflictSettings{
		Policy:         PolicyStopAndAsk,
		ModelTimeout:   DefaultConflictModelTimeout,
		MaxPromptBytes: DefaultConflictPromptBytes,
		TestTimeout:    DefaultConflictTestTimeout,
		ScratchDir:     DefaultConflictScratchDir,
	}
}

// withDefaults fills the zero fields.
func (s ConflictSettings) withDefaults() ConflictSettings {
	d := DefaultConflictSettings()
	if s.Policy == "" {
		s.Policy = d.Policy
	}
	if s.ModelTimeout <= 0 {
		s.ModelTimeout = d.ModelTimeout
	}
	if s.MaxPromptBytes <= 0 {
		s.MaxPromptBytes = d.MaxPromptBytes
	}
	if s.TestTimeout <= 0 {
		s.TestTimeout = d.TestTimeout
	}
	if s.ScratchDir == "" {
		s.ScratchDir = d.ScratchDir
	}
	return s
}

// Validate reports a setting that cannot be used.
func (s ConflictSettings) Validate() error {
	s = s.withDefaults()
	ok := false
	for _, p := range ConflictPolicies() {
		if s.Policy == p {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("unknown conflict policy %q", s.Policy)
	}
	return checkDir(s.ScratchDir)
}

// ConflictModel proposes how to resolve conflicting hunks. The daemon backs it
// with the model-invocation path the other Automaton reviews use; tests use a
// fake. It returns the model's raw reply.
type ConflictModel interface {
	Propose(ctx context.Context, prompt string) (string, error)
}

// QuestionRaiser raises an operator question. *autonomous.QuestionStore is
// one; a question already open for the same source line is not raised twice.
type QuestionRaiser interface {
	RaiseUnlessOpenSourceLine(q autonomous.Question) (autonomous.Question, bool, error)
}

// ErrNoQuestionChannel means a conflict needed the operator and nothing was
// wired to ask. The merge was still left aborted.
var ErrNoQuestionChannel = errors.New("no operator question channel is wired")

// Status says how a merge attempt ended.
type Status string

// Merge statuses.
const (
	// StatusMerged means the branch merged with no conflict.
	StatusMerged Status = "merged"
	// StatusResolvedMechanical means every conflict was additive and was
	// resolved and committed.
	StatusResolvedMechanical Status = "resolved_mechanical"
	// StatusResolvedByModel means a model proposal passed the test step and
	// was committed.
	StatusResolvedByModel Status = "resolved_by_model"
	// StatusAsked means the merge was left aborted and the operator asked.
	StatusAsked Status = "asked"
)

// Results of a model attempt.
const (
	AttemptAccepted        = "accepted"
	AttemptUnavailable     = "model_unavailable"
	AttemptTimeout         = "timeout"
	AttemptModelError      = "model_error"
	AttemptUnparseable     = "unparseable_reply"
	AttemptPromptTooLarge  = "prompt_too_large"
	AttemptApplyFailed     = "apply_failed"
	AttemptTestsFailed     = "tests_failed"
	AttemptNoTestStep      = "no_test_step"
	AttemptNotApplicable   = "not_applicable"
	AttemptInfrastructure  = "scratch_failed"
	AttemptCommitFailed    = "commit_failed"
	AttemptFastForwardFail = "fast_forward_failed"
)

// ModelAttempt summarises what the model step did.
type ModelAttempt struct {
	Result     string `json:"result"`
	Detail     string `json:"detail,omitempty"`
	TestOutput string `json:"test_output,omitempty"`
	Hunks      int    `json:"hunks"`
}

// Outcome is the result of one merge attempt.
type Outcome struct {
	Status   Status         `json:"status"`
	Policy   string         `json:"policy"`
	Branch   string         `json:"branch"`
	Files    []string       `json:"files,omitempty"` // files that conflicted
	Resolved []ResolvedHunk `json:"resolved,omitempty"`
	// Remaining lists the hunks nothing settled, as "file#index".
	Remaining []string      `json:"remaining,omitempty"`
	Attempt   *ModelAttempt `json:"attempt,omitempty"`
	// QuestionID is the operator question raised, or found already open.
	QuestionID string `json:"question_id,omitempty"`
	Summary    string `json:"summary"`
}

// MergeRequest names the branch to merge.
type MergeRequest struct {
	Branch string
	// Message is the merge commit message; empty uses a default.
	Message string
	// AutomatonID links the question to the Automaton the branch came from.
	AutomatonID string
}

// Merger merges a branch into the project's checked-out branch and settles a
// conflict by the operator's policy. Every git command runs inside Dir, with
// the operator's tmux variables removed.
type Merger struct {
	// Dir is the project directory: the repository root.
	Dir string
	// Pipeline is the project's resolved pipeline; its test steps are the
	// test a model proposal must pass.
	Pipeline Definition
	// Settings is read on every merge; nil uses DefaultConflictSettings.
	Settings func() ConflictSettings
	// Model proposes resolutions; nil means no model is available.
	Model ConflictModel
	// Questions raises the operator question; nil means none can be asked.
	Questions QuestionRaiser
}

func (m *Merger) settings() ConflictSettings {
	if m.Settings == nil {
		return DefaultConflictSettings()
	}
	return m.Settings().withDefaults()
}

// ---- running git and shell commands, only inside the project ----

// scrubbedEnv is the environment of every command run here: the operator's
// tmux variables are removed so nothing run can reach their tmux server, and
// git never prompts.
func scrubbedEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

// within reports whether dir is root or lies under it, following symlinks.
func within(root, dir string) bool {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, d)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

type cmdResult struct {
	Out  string
	Code int
}

// run starts name in dir, which must be the project or lie under it.
func (m *Merger) run(ctx context.Context, dir string, name string, args ...string) (cmdResult, error) {
	if m.Dir == "" {
		return cmdResult{}, errors.New("no project directory")
	}
	if !within(m.Dir, dir) {
		return cmdResult{}, fmt.Errorf("refusing to run %s outside the project directory", name)
	}
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed binaries, arguments built here
	cmd.Dir = dir
	cmd.Env = scrubbedEnv()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := cmdResult{Out: out.String()}
	if err == nil {
		return res, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ctx.Err() == nil {
		res.Code = ee.ExitCode()
		return res, nil
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	return res, err
}

// git runs git in dir with diff3 conflict markers.
func (m *Merger) git(ctx context.Context, dir string, args ...string) (cmdResult, error) {
	return m.run(ctx, dir, "git", append([]string{"-c", "merge.conflictStyle=diff3"}, args...)...)
}

// gitOK runs git and turns a non-zero exit into an error carrying its output.
func (m *Merger) gitOK(ctx context.Context, dir string, args ...string) (string, error) {
	r, err := m.git(ctx, dir, args...)
	if err != nil {
		return r.Out, err
	}
	if r.Code != 0 {
		return r.Out, fmt.Errorf("git %s: exit %d: %s", strings.Join(args, " "), r.Code, strings.TrimSpace(tail(r.Out, 1000)))
	}
	return r.Out, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// unmergedFiles lists the paths git holds as conflicted in dir.
func (m *Merger) unmergedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := m.gitOK(ctx, dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// filePath joins a git-reported path onto dir, refusing one that escapes it.
func filePath(dir, rel string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if r, err := filepath.Rel(dir, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the project", rel)
	}
	return p, nil
}

// ---- the merge ----

// MergeBranch merges req.Branch into the checked-out branch with --no-ff.
// A conflict is not an error: it ends in an Outcome that says whether it was
// settled or the operator was asked. An error means the merge could not be
// attempted or its state could not be cleaned up; whenever a merge was begun
// and not completed it is aborted before MergeBranch returns.
func (m *Merger) MergeBranch(ctx context.Context, req MergeRequest) (Outcome, error) {
	set := m.settings()
	out := Outcome{Policy: string(set.Policy), Branch: req.Branch}
	if err := set.Validate(); err != nil {
		return out, err
	}
	if strings.TrimSpace(req.Branch) == "" || strings.HasPrefix(req.Branch, "-") {
		return out, fmt.Errorf("invalid branch name %q", req.Branch)
	}
	headSHA, err := m.revParse(ctx, m.Dir, "HEAD")
	if err != nil {
		return out, err
	}
	branchSHA, err := m.revParse(ctx, m.Dir, req.Branch+"^{commit}")
	if err != nil {
		return out, fmt.Errorf("branch %s: %w", req.Branch, err)
	}
	msg := req.Message
	if msg == "" {
		msg = "Merge branch '" + req.Branch + "'"
	}

	r, err := m.git(ctx, m.Dir, "merge", "--no-ff", "-m", msg, req.Branch)
	if err != nil {
		m.abortQuietly(m.Dir)
		return out, err
	}
	if r.Code == 0 {
		out.Status = StatusMerged
		out.Summary = "merged " + req.Branch + " with no conflict"
		return out, nil
	}
	files, ferr := m.unmergedFiles(ctx, m.Dir)
	if ferr != nil || len(files) == 0 {
		m.abortQuietly(m.Dir)
		if ferr != nil {
			return out, ferr
		}
		return out, fmt.Errorf("merge of %s failed without a conflict: %s", req.Branch, strings.TrimSpace(tail(r.Out, 1000)))
	}
	out.Files = files

	var resolvedTexts map[string]string
	if set.Policy != PolicyStopAndAsk {
		resolvedTexts, out.Resolved, out.Remaining, err = m.mechanical(ctx, m.Dir, files)
		if err != nil {
			m.abortQuietly(m.Dir)
			return out, err
		}
		if len(out.Remaining) == 0 {
			if cerr := m.commitResolved(ctx, m.Dir, resolvedTexts, msg); cerr != nil {
				m.abortQuietly(m.Dir)
				return out, cerr
			}
			out.Status = StatusResolvedMechanical
			out.Summary = fmt.Sprintf("merged %s; %d additive conflict hunk(s) in %d file(s) resolved by keeping both sides",
				req.Branch, len(out.Resolved), len(resolvedTexts))
			return out, nil
		}
	}

	// Nothing more can be done in the project's own tree: put it back.
	if aerr := m.abort(ctx, m.Dir); aerr != nil {
		return out, aerr
	}
	if after, e := m.revParse(ctx, m.Dir, "HEAD"); e != nil || after != headSHA {
		return out, fmt.Errorf("the project's HEAD moved while aborting the merge of %s", req.Branch)
	}

	if set.Policy == PolicyMechanicalAndModel {
		att := m.proposeWithModel(ctx, set, req, out.Files)
		out.Attempt = &att
		if att.Result == AttemptAccepted {
			out.Status = StatusResolvedByModel
			out.Summary = fmt.Sprintf("merged %s; the conflict model's proposal for %d hunk(s) passed the project's test step and was committed",
				req.Branch, att.Hunks)
			return out, nil
		}
	}

	out.Status = StatusAsked
	out.Summary = conflictSummary(out)
	q, qerr := m.askOperator(req, out, headSHA, branchSHA)
	out.QuestionID = q.ID
	if qerr != nil {
		return out, qerr
	}
	return out, nil
}

func (m *Merger) revParse(ctx context.Context, dir, rev string) (string, error) {
	o, err := m.gitOK(ctx, dir, "rev-parse", "--verify", "--quiet", rev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(o), nil
}

func (m *Merger) abort(ctx context.Context, dir string) error {
	_, err := m.gitOK(ctx, dir, "merge", "--abort")
	if err != nil {
		return fmt.Errorf("abort the merge: %w", err)
	}
	return nil
}

// abortQuietly aborts after a failure, with its own short context so a
// cancelled caller still gets the tree put back.
func (m *Merger) abortQuietly(dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, _ = m.git(ctx, dir, "merge", "--abort")
}

// mechanical runs the additive resolver over every conflicted file in dir. It
// returns the new text of each file it fully settled, the hunks it settled and
// the hunks (or whole files) it could not. A file with any unsettled hunk, or
// with no readable markers, is not written.
func (m *Merger) mechanical(ctx context.Context, dir string, files []string) (map[string]string, []ResolvedHunk, []string, error) {
	texts := map[string]string{}
	var resolved []ResolvedHunk
	var remaining []string
	for _, f := range files {
		p, err := filePath(dir, f)
		if err != nil {
			return nil, nil, nil, err
		}
		data, err := os.ReadFile(p) // #nosec G304 -- path checked to lie inside dir
		if err != nil {
			remaining = append(remaining, f+" (unreadable)")
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			remaining = append(remaining, f+" (binary)")
			continue
		}
		text, res, rest, perr := ResolveAdditive(f, string(data))
		if perr != nil {
			remaining = append(remaining, f+" (no usable conflict markers)")
			continue
		}
		resolved = append(resolved, res...)
		for _, h := range rest {
			remaining = append(remaining, fmt.Sprintf("%s#%d", f, h.Index))
		}
		if len(rest) == 0 {
			texts[f] = text
		}
	}
	if len(remaining) > 0 {
		return texts, resolved, remaining, nil
	}
	return texts, resolved, nil, nil
}

// commitResolved writes the settled files, stages them and commits the merge.
func (m *Merger) commitResolved(ctx context.Context, dir string, texts map[string]string, msg string) error {
	if err := m.stageTexts(ctx, dir, texts); err != nil {
		return err
	}
	_, err := m.gitOK(ctx, dir, "commit", "--no-edit", "-m", msg)
	return err
}

// conflictSummary describes where a conflict stands for the operator.
func conflictSummary(o Outcome) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Merging branch %s conflicted in %d file(s): %s.", o.Branch, len(o.Files), strings.Join(o.Files, ", "))
	switch ConflictPolicy(o.Policy) {
	case PolicyStopAndAsk:
		b.WriteString(" The policy is to stop and ask, so nothing was resolved.")
	default:
		fmt.Fprintf(&b, " %d additive hunk(s) could be resolved by keeping both sides", len(o.Resolved))
		if len(o.Remaining) > 0 {
			fmt.Fprintf(&b, "; %d could not: %s", len(o.Remaining), strings.Join(o.Remaining, ", "))
		}
		b.WriteString(". Nothing was committed.")
	}
	if a := o.Attempt; a != nil {
		fmt.Fprintf(&b, " Model attempt: %s", a.Result)
		if a.Detail != "" {
			b.WriteString(" (" + a.Detail + ")")
		}
		b.WriteString(".")
		if a.TestOutput != "" {
			b.WriteString(" Test output, last part:\n" + a.TestOutput)
		}
	}
	b.WriteString("\nThe merge was left aborted and the branch is unchanged.")
	return b.String()
}

// askOperator raises one operator question for an unsettled conflict. The same
// branch against the same two commits is asked once while its question is open.
func (m *Merger) askOperator(req MergeRequest, o Outcome, headSHA, branchSHA string) (autonomous.Question, error) {
	if m.Questions == nil {
		return autonomous.Question{}, ErrNoQuestionChannel
	}
	sum := sha256.Sum256([]byte("merge-conflict\x00" + req.Branch + "\x00" + headSHA + "\x00" + branchSHA))
	text := o.Summary
	if len(text) > 7000 {
		text = text[:7000] + "\n[shortened]"
	}
	q := autonomous.Question{
		Label:     "decision",
		AskedBy:   ConflictQuestionAsker,
		AskerKind: autonomous.QuestionAskerSystem,
		Context:   autonomous.QuestionContextNone,
		Text:      text + "\nHow do you want to proceed?",
		Options: []autonomous.QuestionOption{
			{ID: "resolve_by_hand", Label: "Resolve it by hand",
				Tradeoffs: "You merge the branch yourself and settle the conflict; the pipeline then continues."},
			{ID: "retry_merge", Label: "Try the merge again",
				Tradeoffs: "Worth it after other branches landed or the conflicting change was reworked."},
			{ID: "leave_branch_out", Label: "Leave this branch out",
				Tradeoffs: "The branch is not merged in this run."},
		},
		Default:          "resolve_by_hand",
		SourceLineSHA256: hex.EncodeToString(sum[:]),
	}
	if req.AutomatonID != "" {
		q.Context = autonomous.QuestionContextPrd
		q.PRDID = req.AutomatonID
	}
	got, _, err := m.Questions.RaiseUnlessOpenSourceLine(q)
	if err != nil {
		return autonomous.Question{}, fmt.Errorf("raise the operator question: %w", err)
	}
	return got, nil
}

// ---- running the project's test step ----

// makefileHasTarget reports whether dir's Makefile defines target.
func makefileHasTarget(dir, target string) bool {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- fixed names under dir
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(l, target+":") {
				return true
			}
		}
		return false
	}
	return false
}

// runTestSteps runs every test step of the pipeline in dir. It passes only if
// at least one step ran and every step that ran succeeded; a step that is a
// built-in action cannot be run here and counts as a failure, since it could
// not be shown to pass. The returned text is the tail of the failing step's
// output, or why nothing could be run.
func (m *Merger) runTestSteps(ctx context.Context, dir string, set ConflictSettings) (passed bool, noStep bool, output string) {
	ran := 0
	for _, s := range m.Pipeline.Steps {
		if s.Kind != KindTest {
			continue
		}
		if s.RequiresMakeTarget != "" && !makefileHasTarget(dir, s.RequiresMakeTarget) {
			continue
		}
		if strings.TrimSpace(s.Command) == "" {
			return false, false, fmt.Sprintf("test step %q is a built-in action and cannot be run on a proposal", s.Name)
		}
		if CallsUnscopedTmuxKill(s.Command) {
			return false, false, fmt.Sprintf("test step %q calls an unscoped tmux kill and was not run", s.Name)
		}
		wd := dir
		if s.Dir != "" {
			if checkDir(s.Dir) != nil {
				return false, false, fmt.Sprintf("test step %q has an unusable dir", s.Name)
			}
			wd = filepath.Join(dir, filepath.FromSlash(s.Dir))
		}
		timeout := s.Timeout
		if timeout <= 0 {
			timeout = set.TestTimeout
		}
		tctx, cancel := context.WithTimeout(ctx, timeout)
		r, err := m.run(tctx, wd, "sh", "-c", s.Command)
		cancel()
		ran++
		if err != nil {
			return false, false, fmt.Sprintf("test step %q did not finish: %v\n%s", s.Name, err, tail(r.Out, 2000))
		}
		if r.Code != 0 {
			return false, false, fmt.Sprintf("test step %q failed (exit %d)\n%s", s.Name, r.Code, tail(r.Out, 2000))
		}
	}
	if ran == 0 {
		return false, true, "the pipeline has no test step that can run here"
	}
	return true, false, ""
}
