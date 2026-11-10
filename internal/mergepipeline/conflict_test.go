package mergepipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// gitIn runs git in dir for test setup, with the tmux variables removed.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = scrubbedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// conflictRepo makes a temporary repository whose main and "feature" branches
// both changed file: base on the common commit, mainText on main, featText on
// feature. HEAD is left on main.
func conflictRepo(t *testing.T, file, base, mainText, featText string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, file, base)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, file, featText)
	gitIn(t, dir, "commit", "-q", "-am", "feature change")
	gitIn(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, file, mainText)
	gitIn(t, dir, "commit", "-q", "-am", "main change")
	return dir
}

func assertAborted(t *testing.T, dir, wantHead, file, wantText string) {
	t.Helper()
	if head := gitIn(t, dir, "rev-parse", "HEAD"); head != wantHead {
		t.Fatalf("HEAD moved: %s, want %s", head, wantHead)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err == nil {
		t.Fatal("a merge is still in progress")
	}
	if st := gitIn(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("the tree is not clean:\n%s", st)
	}
	if got := readFile(t, dir, file); got != wantText {
		t.Fatalf("%s = %q, want %q", file, got, wantText)
	}
	if wt := gitIn(t, dir, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("a scratch worktree was left behind:\n%s", wt)
	}
}

type fakeModel struct {
	reply   string
	err     error
	block   bool
	prompts []string
}

func (f *fakeModel) Propose(ctx context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if f.block {
		time.Sleep(2 * time.Second)
		return "", nil
	}
	return f.reply, f.err
}

func newQuestions(t *testing.T) *autonomous.QuestionStore {
	t.Helper()
	s, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func openConflictQuestions(s *autonomous.QuestionStore) []autonomous.Question {
	return s.List(autonomous.QuestionFilter{State: autonomous.QuestionOpen, Asker: ConflictQuestionAsker, Limit: 100})
}

func testPipeline(command string) Definition {
	return Definition{Steps: []Step{{Name: "tests", Kind: KindTest, Command: command, Timeout: time.Minute}}}
}

// merger returns a merger whose model proposals are applied (mode on), so the
// commit path is what the test exercises.
func merger(dir string, policy ConflictPolicy, model ConflictModel, qs QuestionRaiser, command string) *Merger {
	return mergerInMode(dir, policy, ConflictModeOn, model, qs, command)
}

func mergerInMode(dir string, policy ConflictPolicy, mode string, model ConflictModel, qs QuestionRaiser, command string) *Merger {
	return &Merger{
		Dir:       dir,
		Pipeline:  testPipeline(command),
		Settings:  func() ConflictSettings { return ConflictSettings{Policy: policy, Mode: mode} },
		Model:     model,
		Questions: qs,
	}
}

func TestDefaultConflictPolicyIsStopAndAsk(t *testing.T) {
	if p := DefaultConflictSettings().Policy; p != PolicyStopAndAsk {
		t.Fatalf("default policy = %q", p)
	}
	if err := (ConflictSettings{Policy: "guess"}).Validate(); err == nil {
		t.Fatal("an unknown policy must be rejected")
	}
	for _, p := range ConflictPolicies() {
		if err := (ConflictSettings{Policy: p}).Validate(); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if err := (ConflictSettings{ScratchDir: "../out"}).Validate(); err == nil {
		t.Fatal("a scratch dir outside the project must be rejected")
	}
}

func TestMergeWithoutConflictJustMerges(t *testing.T) {
	// The two branches change distant lines, so git merges them itself.
	gap := "\n\n\n\n\n\n"
	dir := conflictRepo(t, "a.txt", "a"+gap+"z\n", "M"+gap+"z\n", "a"+gap+"Z\n")
	o, err := merger(dir, PolicyStopAndAsk, nil, nil, "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil || o.Status != StatusMerged {
		t.Fatalf("outcome = %+v, err = %v", o, err)
	}
	if got := readFile(t, dir, "a.txt"); got != "M"+gap+"Z\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if n := gitIn(t, dir, "rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(n)) != 3 {
		t.Fatalf("expected a --no-ff merge commit, got %q", n)
	}
}

// TestMechanicalClassRoutes gives each mechanically settled class its own
// fixture repository and checks the merge comes out resolved_mechanical with
// the expected content; a code file with an overlapping edit must stay asked.
func TestMechanicalClassRoutes(t *testing.T) {
	// makefileWith commits a Makefile on main, after the conflict fixture, so
	// the feature branch never touches it.
	makefileWith := func(recipe string) func(t *testing.T, dir string) {
		return func(t *testing.T, dir string) {
			writeFile(t, dir, "Makefile", "docs-index:\n\t"+recipe+"\n")
			gitIn(t, dir, "add", "Makefile")
			gitIn(t, dir, "commit", "-q", "-m", "makefile")
		}
	}
	cases := []struct {
		name       string
		file       string
		base       string
		mainText   string
		featText   string
		setup      func(t *testing.T, dir string)
		wantStatus Status
		wantText   string
	}{
		{
			name:       "changelog",
			file:       "CHANGELOG.md",
			base:       "# Changes\n",
			mainText:   "# Changes\n- m\n",
			featText:   "# Changes\n- f\n",
			wantStatus: StatusResolvedMechanical,
			wantText:   "# Changes\n- m\n- f\n",
		},
		{
			name:       "version line",
			file:       "internal/server/api.go",
			base:       "var Version = \"1.2.3\"\n",
			mainText:   "var Version = \"1.2.4\"\n",
			featText:   "var Version = \"1.3.0\"\n",
			wantStatus: StatusResolvedMechanical,
			wantText:   "var Version = \"1.2.4\"\n",
		},
		{
			name:       "readme line",
			file:       "README.md",
			base:       "# X\nLatest release: v2.0.0\n",
			mainText:   "# X\nLatest release: v2.0.1\n",
			featText:   "# X\nLatest release: v2.1.0\n",
			wantStatus: StatusResolvedMechanical,
			wantText:   "# X\nLatest release: v2.0.1\n",
		},
		{
			name:       "locale",
			file:       "internal/server/web/locales/en.json",
			base:       "{\"a\":\"A\"}\n",
			mainText:   "{\"a\":\"A\",\"m\":\"M\"}\n",
			featText:   "{\"a\":\"A\",\"f\":\"F\"}\n",
			wantStatus: StatusResolvedMechanical,
			wantText:   "{\n  \"a\": \"A\",\n  \"f\": \"F\",\n  \"m\": \"M\"\n}\n",
		},
		{
			name:       "generated index rebuilt by docs-index",
			file:       "internal/docsindex/assets/index.json",
			base:       "base index\n",
			mainText:   "main index\n",
			featText:   "feature index\n",
			setup:      makefileWith("printf 'regenerated\\n' > internal/docsindex/assets/index.json"),
			wantStatus: StatusResolvedMechanical,
			wantText:   "regenerated\n",
		},
		{
			name:       "generated index whose docs-index fails stays asked",
			file:       "internal/docsindex/assets/index.json",
			base:       "base index\n",
			mainText:   "main index\n",
			featText:   "feature index\n",
			setup:      makefileWith("exit 1"),
			wantStatus: StatusAsked,
			wantText:   "main index\n",
		},
		{
			name:       "code overlapping edit stays asked",
			file:       "cfg.txt",
			base:       "x=1\n",
			mainText:   "x=2\n",
			featText:   "x=3\n",
			wantStatus: StatusAsked,
			wantText:   "x=2\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := conflictRepo(t, c.file, c.base, c.mainText, c.featText)
			if c.setup != nil {
				c.setup(t, dir)
			}
			head := gitIn(t, dir, "rev-parse", "HEAD")
			m := merger(dir, PolicyMechanical, nil, newQuestions(t), "true")
			m.VersionFiles = []string{"internal/server/api.go"}
			o, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
			if err != nil {
				t.Fatal(err)
			}
			if o.Status != c.wantStatus {
				t.Fatalf("status = %q, want %q; outcome %+v", o.Status, c.wantStatus, o)
			}
			if c.wantStatus == StatusResolvedMechanical {
				if len(o.Remaining) != 0 {
					t.Fatalf("remaining = %v", o.Remaining)
				}
				if got := readFile(t, dir, c.file); got != c.wantText {
					t.Fatalf("%s = %q, want %q", c.file, got, c.wantText)
				}
				if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err == nil {
					t.Fatal("a merge is still in progress")
				}
				if st := gitIn(t, dir, "status", "--porcelain"); st != "" {
					t.Fatalf("tree not clean after the merge commit:\n%s", st)
				}
				return
			}
			assertAborted(t, dir, head, c.file, c.wantText)
		})
	}
}

func TestMechanicalResolvesAppendAppend(t *testing.T) {
	dir := conflictRepo(t, "list.txt", "a\n", "a\nm\n", "a\nf\n")
	qs := newQuestions(t)
	o, err := merger(dir, PolicyMechanical, nil, qs, "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusResolvedMechanical || len(o.Resolved) != 1 || len(o.Remaining) != 0 {
		t.Fatalf("outcome = %+v", o)
	}
	if got := readFile(t, dir, "list.txt"); got != "a\nm\nf\n" {
		t.Fatalf("list.txt = %q", got)
	}
	if st := gitIn(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("tree not clean after the merge commit:\n%s", st)
	}
	if n := gitIn(t, dir, "rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(n)) != 3 {
		t.Fatalf("expected a merge commit, got %q", n)
	}
	if len(openConflictQuestions(qs)) != 0 {
		t.Fatal("a resolved merge must not ask the operator")
	}
}

func TestMechanicalDoesNotTreatOverlappingEditAsAdditive(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	qs := newQuestions(t)
	o, err := merger(dir, PolicyMechanical, nil, qs, "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusAsked || len(o.Resolved) != 0 || len(o.Remaining) != 1 {
		t.Fatalf("outcome = %+v", o)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
	if got := openConflictQuestions(qs); len(got) != 1 || o.QuestionID != got[0].ID {
		t.Fatalf("questions = %v, outcome question %q", got, o.QuestionID)
	}
}

func TestMechanicalAsksForTheRestAndCommitsNothing(t *testing.T) {
	// One file is purely additive, another is an overlapping edit: nothing is
	// committed, because the merge as a whole cannot be finished.
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "T")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "list.txt", "a\n")
	writeFile(t, dir, "cfg.txt", "x=1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "list.txt", "a\nf\n")
	writeFile(t, dir, "cfg.txt", "x=3\n")
	gitIn(t, dir, "commit", "-q", "-am", "feature")
	gitIn(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "list.txt", "a\nm\n")
	writeFile(t, dir, "cfg.txt", "x=2\n")
	gitIn(t, dir, "commit", "-q", "-am", "main")
	head := gitIn(t, dir, "rev-parse", "HEAD")

	o, err := merger(dir, PolicyMechanical, nil, newQuestions(t), "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusAsked || len(o.Resolved) != 1 || o.Resolved[0].File != "list.txt" || len(o.Remaining) != 1 || o.Remaining[0] != "cfg.txt#1" {
		t.Fatalf("outcome = %+v", o)
	}
	if !strings.Contains(o.Summary, "cfg.txt#1") {
		t.Fatalf("the summary must name what is left: %s", o.Summary)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
}

func TestStopAndAskResolvesNothing(t *testing.T) {
	dir := conflictRepo(t, "list.txt", "a\n", "a\nm\n", "a\nf\n") // additive, but the policy says ask
	head := gitIn(t, dir, "rev-parse", "HEAD")
	qs := newQuestions(t)
	o, err := merger(dir, PolicyStopAndAsk, &fakeModel{reply: "{}"}, qs, "true").MergeBranch(context.Background(),
		MergeRequest{Branch: "feature", AutomatonID: "d1b60001"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusAsked || len(o.Resolved) != 0 || o.Attempt != nil {
		t.Fatalf("outcome = %+v", o)
	}
	assertAborted(t, dir, head, "list.txt", "a\nm\n")
	qsOpen := openConflictQuestions(qs)
	if len(qsOpen) != 1 {
		t.Fatalf("questions = %v", qsOpen)
	}
	q := qsOpen[0]
	if q.PRDID != "d1b60001" || q.Context != autonomous.QuestionContextPrd || q.AskerKind != autonomous.QuestionAskerSystem {
		t.Fatalf("question = %+v", q)
	}
	if strings.Contains(strings.ToLower(q.Text), "prd") {
		t.Fatalf("the question must not say PRD: %s", q.Text)
	}
}

func TestAskingTwiceForTheSameConflictRaisesOneQuestion(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	qs := newQuestions(t)
	m := merger(dir, PolicyStopAndAsk, nil, qs, "true")
	a, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if a.QuestionID == "" || a.QuestionID != b.QuestionID || len(openConflictQuestions(qs)) != 1 {
		t.Fatalf("question ids %q and %q, open %d", a.QuestionID, b.QuestionID, len(openConflictQuestions(qs)))
	}
}

func TestAskWithoutAChannelStillLeavesTheMergeAborted(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	o, err := merger(dir, PolicyStopAndAsk, nil, nil, "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != ErrNoQuestionChannel || o.Status != StatusAsked {
		t.Fatalf("err = %v, outcome = %+v", err, o)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
}

const overlapResolved = `{"resolutions":[{"file":"cfg.txt","hunk":1,"text":"x=4\n"}]}`

func TestModelProposalThatPassesTestsIsCommitted(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	commitBuildTarget(t, dir)
	fm := &fakeModel{reply: "```json\n" + overlapResolved + "\n```"}
	o, err := merger(dir, PolicyMechanicalAndModel, fm, newQuestions(t), `grep -qx 'x=4' cfg.txt`).MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusResolvedByModel || o.Attempt == nil || o.Attempt.Result != AttemptAccepted || o.Attempt.Hunks != 1 {
		t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
	}
	if got := readFile(t, dir, "cfg.txt"); got != "x=4\n" {
		t.Fatalf("cfg.txt = %q", got)
	}
	if n := gitIn(t, dir, "rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(n)) != 3 {
		t.Fatalf("expected a merge commit, got %q", n)
	}
	if st := gitIn(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("tree not clean:\n%s", st)
	}
	if wt := gitIn(t, dir, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("scratch worktree left behind:\n%s", wt)
	}
	// The prompt carried base, ours and theirs, as data.
	p := fm.prompts[0]
	for _, want := range []string{"x=1", "x=2", "x=3", "<user_data", "cfg.txt"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestModelOnlyGetsWhatMechanicalCouldNotSettle(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "T")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "list.txt", "a\n")
	writeFile(t, dir, "cfg.txt", "x=1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "list.txt", "a\nf\n")
	writeFile(t, dir, "cfg.txt", "x=3\n")
	gitIn(t, dir, "commit", "-q", "-am", "feature")
	gitIn(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "list.txt", "a\nm\n")
	writeFile(t, dir, "cfg.txt", "x=2\n")
	gitIn(t, dir, "commit", "-q", "-am", "main")
	commitBuildTarget(t, dir)

	fm := &fakeModel{reply: overlapResolved}
	o, err := merger(dir, PolicyMechanicalAndModel, fm, newQuestions(t), "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil || o.Status != StatusResolvedByModel {
		t.Fatalf("outcome = %+v err = %v", o, err)
	}
	if strings.Contains(fm.prompts[0], "list.txt") {
		t.Fatalf("an additive file must not be sent to the model:\n%s", fm.prompts[0])
	}
	if readFile(t, dir, "list.txt") != "a\nm\nf\n" || readFile(t, dir, "cfg.txt") != "x=4\n" {
		t.Fatalf("files = %q %q", readFile(t, dir, "list.txt"), readFile(t, dir, "cfg.txt"))
	}
}

func TestModelProposalThatFailsTestsIsDiscarded(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	commitBuildTarget(t, dir)
	head := gitIn(t, dir, "rev-parse", "HEAD")
	qs := newQuestions(t)
	o, err := merger(dir, PolicyMechanicalAndModel, &fakeModel{reply: overlapResolved}, qs, `echo "boom: wanted x=5"; grep -qx 'x=5' cfg.txt`).
		MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptTestsFailed {
		t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
	}
	if !strings.Contains(o.Attempt.TestOutput, "boom") || !strings.Contains(o.Summary, "tests_failed") {
		t.Fatalf("the operator must see the attempt: %+v / %s", o.Attempt, o.Summary)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
	if len(openConflictQuestions(qs)) != 1 {
		t.Fatal("the operator must be asked")
	}
}

func TestModelRepliesThatCannotBeUsedAreDiscarded(t *testing.T) {
	cases := map[string]string{
		"malformed":        "I think you should keep both lines.",
		"empty object":     `{}`,
		"unknown hunk":     `{"resolutions":[{"file":"cfg.txt","hunk":9,"text":"x\n"}]}`,
		"other file":       `{"resolutions":[{"file":"other.txt","hunk":1,"text":"x\n"}]}`,
		"still conflicted": `{"resolutions":[{"file":"cfg.txt","hunk":1,"text":"<<<<<<< a\nx\n>>>>>>> b\n"}]}`,
		"unknown field":    `{"resolutions":[{"file":"cfg.txt","hunk":1,"text":"x\n","why":"y"}]}`,
		"two objects":      overlapResolved + overlapResolved,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
			head := gitIn(t, dir, "rev-parse", "HEAD")
			o, err := merger(dir, PolicyMechanicalAndModel, &fakeModel{reply: reply}, newQuestions(t), "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
			if err != nil {
				t.Fatal(err)
			}
			if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptUnparseable {
				t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
			}
			assertAborted(t, dir, head, "cfg.txt", "x=2\n")
		})
	}
}

func TestUnavailableModelAsksTheOperator(t *testing.T) {
	for name, model := range map[string]ConflictModel{
		"error":    &fakeModel{err: os.ErrNotExist},
		"no model": nil,
	} {
		t.Run(name, func(t *testing.T) {
			dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
			head := gitIn(t, dir, "rev-parse", "HEAD")
			qs := newQuestions(t)
			o, err := merger(dir, PolicyMechanicalAndModel, model, qs, "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
			if err != nil {
				t.Fatal(err)
			}
			if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptUnavailable {
				t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
			}
			assertAborted(t, dir, head, "cfg.txt", "x=2\n")
			if len(openConflictQuestions(qs)) != 1 {
				t.Fatal("the operator must be asked")
			}
		})
	}
}

func TestSlowModelTimesOut(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	m := merger(dir, PolicyMechanicalAndModel, &fakeModel{block: true}, newQuestions(t), "true")
	m.Settings = func() ConflictSettings {
		return ConflictSettings{Policy: PolicyMechanicalAndModel, ModelTimeout: 50 * time.Millisecond}
	}
	start := time.Now()
	o, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("the merge waited %s for a model past its timeout", time.Since(start))
	}
	if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptTimeout {
		t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
}

func TestProposalWithNoRunnableTestStepIsNeverCommitted(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	commitBuildTarget(t, dir)
	head := gitIn(t, dir, "rev-parse", "HEAD")
	m := merger(dir, PolicyMechanicalAndModel, &fakeModel{reply: overlapResolved}, newQuestions(t), "true")
	m.Pipeline = Definition{Steps: []Step{{Name: "lint", Kind: KindLint, Command: "true"}}}
	o, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptTestsFailed || !strings.Contains(o.Attempt.TestOutput, "no test step") {
		t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
}

func TestOversizedPromptIsNotSent(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	fm := &fakeModel{reply: overlapResolved}
	m := merger(dir, PolicyMechanicalAndModel, fm, newQuestions(t), "true")
	m.Settings = func() ConflictSettings {
		return ConflictSettings{Policy: PolicyMechanicalAndModel, MaxPromptBytes: 50}
	}
	o, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil || o.Attempt == nil || o.Attempt.Result != AttemptPromptTooLarge || len(fm.prompts) != 0 {
		t.Fatalf("outcome = %+v err = %v prompts = %d", o, err, len(fm.prompts))
	}
}

func TestCommandsRunOnlyInsideTheProjectWithoutTheOperatorsTmux(t *testing.T) {
	dir := t.TempDir()
	m := &Merger{Dir: dir}
	if _, err := m.run(context.Background(), t.TempDir(), "true"); err == nil {
		t.Fatal("a command outside the project directory must be refused")
	}
	if _, err := m.run(context.Background(), filepath.Join(dir, ".."), "true"); err == nil {
		t.Fatal("the parent of the project directory must be refused")
	}
	t.Setenv("TMUX", "/tmp/operator-tmux,1,0")
	t.Setenv("TMUX_TMPDIR", "/tmp/operator")
	r, err := m.run(context.Background(), dir, "sh", "-c", `printf '[%s][%s]' "$TMUX" "$TMUX_TMPDIR"`)
	if err != nil || r.Out != "[][]" {
		t.Fatalf("out = %q, err = %v", r.Out, err)
	}
}

func TestRefusesAnUnsafeBranchName(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	for _, b := range []string{"", "--abort", "-x"} {
		if _, err := merger(dir, PolicyStopAndAsk, nil, nil, "true").MergeBranch(context.Background(), MergeRequest{Branch: b}); err == nil {
			t.Fatalf("branch %q must be refused", b)
		}
	}
}

// TestConflictShadow covers the two modes of a passing model proposal: shadow
// records it and leaves the project alone; on applies it.
func TestConflictShadow(t *testing.T) {
	t.Run("shadow records the proposal and leaves HEAD at its pre-merge SHA", func(t *testing.T) {
		dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
		commitBuildTarget(t, dir)
		head := gitIn(t, dir, "rev-parse", "HEAD")
		qs := newQuestions(t)
		fm := &fakeModel{reply: overlapResolved}
		o, err := mergerInMode(dir, PolicyMechanicalAndModel, "", fm, qs, `grep -qx 'x=4' cfg.txt`).
			MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
		if err != nil {
			t.Fatal(err)
		}
		if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptShadowComputed {
			t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
		}
		if o.Attempt.Mode != ConflictModeShadow {
			t.Fatalf("attempt mode = %q, want shadow", o.Attempt.Mode)
		}
		if got := o.Attempt.Proposal["cfg.txt"]; got != "x=4\n" {
			t.Fatalf("recorded proposal = %v, want cfg.txt x=4", o.Attempt.Proposal)
		}
		assertAborted(t, dir, head, "cfg.txt", "x=2\n")
		if len(openConflictQuestions(qs)) != 1 {
			t.Fatal("in shadow mode the operator must still be asked")
		}
	})

	t.Run("on applies the proposal", func(t *testing.T) {
		dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
		commitBuildTarget(t, dir)
		head := gitIn(t, dir, "rev-parse", "HEAD")
		o, err := mergerInMode(dir, PolicyMechanicalAndModel, ConflictModeOn, &fakeModel{reply: overlapResolved},
			newQuestions(t), `grep -qx 'x=4' cfg.txt`).MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
		if err != nil {
			t.Fatal(err)
		}
		if o.Status != StatusResolvedByModel || o.Attempt == nil || o.Attempt.Result != AttemptAccepted {
			t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
		}
		if o.Attempt.Proposal != nil {
			t.Fatalf("on mode must not record a shadow proposal: %v", o.Attempt.Proposal)
		}
		if gitIn(t, dir, "rev-parse", "HEAD") == head {
			t.Fatal("on mode must move HEAD onto the proposal")
		}
		if got := readFile(t, dir, "cfg.txt"); got != "x=4\n" {
			t.Fatalf("cfg.txt = %q", got)
		}
	})
}

func TestConflictModeValidation(t *testing.T) {
	for mode, wantErr := range map[string]bool{"": false, ConflictModeShadow: false, ConflictModeOn: false, "maybe": true, "ON": true} {
		s := ConflictSettings{Policy: PolicyMechanicalAndModel, Mode: mode}
		if err := s.Validate(); (err != nil) != wantErr {
			t.Fatalf("mode %q: Validate() = %v, want error %v", mode, err, wantErr)
		}
	}
	if got := (ConflictSettings{}).withDefaults().Mode; got != ConflictModeShadow {
		t.Fatalf("default mode = %q, want shadow", got)
	}
}

// TestModelProposalFailingTestsIsDiscarded is a fixture: the model's resolution
// (x=5) breaks the project's test step (which needs x=4). The project must be
// left exactly as it was and the attempt must say tests_failed.
func TestModelProposalFailingTestsIsDiscarded(t *testing.T) {
	dir := conflictRepo(t, "cfg.txt", "x=1\n", "x=2\n", "x=3\n")
	commitBuildTarget(t, dir)
	head := gitIn(t, dir, "rev-parse", "HEAD")
	breaking := `{"resolutions":[{"file":"cfg.txt","hunk":1,"text":"x=5\n"}]}`
	qs := newQuestions(t)
	o, err := mergerInMode(dir, PolicyMechanicalAndModel, ConflictModeOn, &fakeModel{reply: breaking}, qs,
		`grep -qx 'x=4' cfg.txt`).MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptTestsFailed {
		t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
	}
	if o.Attempt.Proposal != nil {
		t.Fatalf("a failing proposal must not be recorded: %v", o.Attempt.Proposal)
	}
	assertAborted(t, dir, head, "cfg.txt", "x=2\n")
}
