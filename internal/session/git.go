package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// ProjectGit manages git operations on the user's project directory.
type ProjectGit struct {
	dir string
	// runStart bounds unscoped auto-commit staging: untracked files
	// whose mtime predates it are not considered part of this run
	// (see unscopedChangedFiles). Zero means the run start is unknown.
	runStart time.Time
	// stagingPolicy bounds what an automatic commit may stage: per-file
	// size, ignored cache/build directories, model-weight patterns, the
	// file-count refusal, and the add timeout (see internal/gitguard).
	// NewProjectGit sets gitguard.Default. A zero-value ProjectGit imposes
	// no bounds, so construct through NewProjectGit for automatic commits.
	stagingPolicy gitguard.Policy
	// commitEnv, when set, is appended to the environment of every git
	// commit this ProjectGit runs. It carries the commit token that lets
	// the harness commit through a CommitGuard's hooks; set it only after
	// validation has passed.
	commitEnv []string
}

// NewProjectGit creates a new ProjectGit for the given directory, bounded
// by gitguard.Default.
func NewProjectGit(dir string) *ProjectGit {
	return &ProjectGit{dir: dir, stagingPolicy: gitguard.Default}
}

// SetStagingPolicy replaces the staging policy used by automatic commits.
// Tests use it to tighten limits; future config wiring can call it too.
func (g *ProjectGit) SetStagingPolicy(p gitguard.Policy) {
	g.stagingPolicy = p
}

// SetCommitEnv makes the commit calls of this ProjectGit run git with env
// appended (typically CommitGuard.CommitEnv()). Nil clears it.
func (g *ProjectGit) SetCommitEnv(env []string) {
	g.commitEnv = env
}

// commit runs git commit with the commit environment, if any.
func (g *ProjectGit) commit(args ...string) error {
	return runGitEnv(g.dir, g.commitEnv, append([]string{"commit"}, args...))
}

// IsRepo returns true if dir is inside a git repository.
func (g *ProjectGit) IsRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = g.dir
	return cmd.Run() == nil
}

// Init initializes a git repository in dir.
func (g *ProjectGit) Init() error {
	if err := runGit(g.dir, "init", "-b", "main"); err != nil {
		return runGit(g.dir, "init")
	}
	_ = runGit(g.dir, "config", "user.email", "datawatch@localhost")
	_ = runGit(g.dir, "config", "user.name", "datawatch")
	return nil
}

// stageChanges stages changes for a session commit, scoped to the
// session's write directories when scope is non-empty: only files at or
// under one of those directories enter the index, so untracked or
// unstaged files elsewhere in the same tree (another session's work,
// operator scratch files) are never swept into the session's commit.
//
// Scope entries come from the PRD/task scope namespace. Entries outside
// this repo's directory are skipped with a warning (other PRD write dirs
// live in their own repos or on another host). An empty scope stages
// only the files this run changed (see unscopedChangedFiles), with a
// warning so the reduced staging stays visible in the log — the old
// `git add -A` fallthrough swept pre-existing untracked files into the
// session's commit (2026-10-19 incident) and must never return.
//
// Both branches never add a whole directory tree. A scoped commit used
// to run `git add -- <scope dir>`, which staged everything under a scope
// of the home directory (2026-10-19 and 2026-10-21 bloat incidents).
// Candidates are now enumerated file by file and pass through the staging
// guard (stageCandidates): size cap, ignored cache/build directories, a
// file-count refusal, and a bounded add. Automatic commits in the user's
// home directory are flagged with a warning, since the guard is what keeps
// them from staging the whole home tree.
func (g *ProjectGit) stageChanges(scope []string) error {
	if err := gitguard.RefuseNonProject(g.dir); err != nil {
		return err
	}
	if len(scope) > 0 {
		if home, herr := gitguard.InHome(g.dir, nil); herr == nil && home {
			fmt.Printf("[datawatch] WARNING: automatic commit is running in the user's home directory %s — staging is bounded by the staging guard (per-file size cap, ignored cache/build dirs, file-count limit); narrow the write scope to keep commits small\n", g.dir)
		}
		absDir, err := filepath.Abs(g.dir)
		if err != nil {
			absDir = g.dir
		}
		var inside []string
		for _, d := range scope {
			dd, aerr := filepath.Abs(d)
			if aerr != nil {
				fmt.Printf("[datawatch] warning: bad write scope entry %q, skipping it\n", d)
				continue
			}
			if dd != absDir && !strings.HasPrefix(dd, absDir+string(filepath.Separator)) {
				fmt.Printf("[datawatch] warning: write scope dir %s is outside repo %s — not staged by this commit\n", dd, g.dir)
				continue
			}
			if _, serr := os.Stat(dd); serr != nil {
				fmt.Printf("[datawatch] warning: write scope dir %s does not exist — skipping it\n", dd)
				continue
			}
			inside = append(inside, dd)
		}
		if len(inside) == 0 {
			// Nothing in scope is inside this repo: leave the index as-is;
			// the staged-empty check below skips the commit. Never fall back
			// to add -A here — that would be the exact leak this function
			// exists to close.
			return nil
		}
		candidates, err := g.scopedCandidates(absDir, inside)
		if err != nil {
			return err
		}
		return g.stageCandidates(candidates)
	}
	files := g.unscopedChangedFiles(g.runStart)
	if len(files) == 0 {
		fmt.Printf("[datawatch] warning: no write scope set for this session in %s — staged nothing (no files changed this run detected)\n", g.dir)
		return nil
	}
	names := files
	if len(names) > 20 {
		names = append(append([]string{}, names[:20]...), fmt.Sprintf("…(%d more)", len(files)-20))
	}
	fmt.Printf("[datawatch] warning: no write scope set for this session in %s — staging only the %d file(s) this run changed: %s\n",
		g.dir, len(files), strings.Join(names, ", "))
	return g.stageCandidates(files)
}

// scopedCandidates lists the files a scoped commit may stage: tracked
// modifications, staged changes, and untracked files, filtered to those at
// or under one of inside (absolute scope directories within absDir). The
// filter applies to all three sources, not just untracked files: a tracked
// file outside the scope that was dirtied after the baseline must stay
// out of the commit (see TestCommitSessionWork_ScopedOutOfScopeDirtyTree).
// Paths are relative to g.dir, the directory git runs in. Sorted for
// deterministic logs. A fresh repo with no HEAD yet has no diff output, so
// that diff failure is not an error; the untracked listing must succeed.
func (g *ProjectGit) scopedCandidates(absDir string, inside []string) ([]string, error) {
	prefixes := make([]string, 0, len(inside))
	for _, d := range inside {
		rel, err := filepath.Rel(absDir, d)
		if err != nil {
			continue
		}
		prefixes = append(prefixes, filepath.ToSlash(rel))
	}
	seen := make(map[string]struct{})
	add := func(out string) {
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			if l = strings.TrimSpace(l); l != "" && underAnyDir(l, prefixes) {
				seen[l] = struct{}{}
			}
		}
	}
	if out, err := gitOutput(g.dir, "diff", "--name-only", "--relative", "HEAD"); err == nil {
		add(out)
	}
	if out, err := gitOutput(g.dir, "diff", "--cached", "--name-only", "--relative"); err == nil {
		add(out)
	}
	untracked, err := gitOutput(g.dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list untracked files in %s: %w", g.dir, err)
	}
	add(untracked)
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// underAnyDir reports whether the repo-relative file path is one of dirs or
// lies under one of them. A "." entry matches every file (scope is the repo
// root).
func underAnyDir(file string, dirs []string) bool {
	for _, d := range dirs {
		if d == "." || file == d || strings.HasPrefix(file, d+"/") {
			return true
		}
	}
	return false
}

// stageCandidates runs candidates through the staging policy and adds the
// survivors to the index. A candidate set the policy refuses (more files
// than MaxStagedFiles) returns an error and stages nothing: it stops with a
// clear message rather than adding part of a huge tree. Skipped files are
// reported with the reason, so an operator can see why a changed file did
// not reach the commit.
func (g *ProjectGit) stageCandidates(candidates []string) error {
	kept, skipped := g.stagingPolicy.Keep(g.dir, candidates)
	if reason := g.stagingPolicy.RefuseReason(len(kept)); reason != "" {
		return fmt.Errorf("%s: %s", g.dir, reason)
	}
	if len(skipped) > 0 {
		fmt.Printf("[datawatch] warning: %s: staged %d, skipped %d (%s) — see staging guard\n",
			g.dir, len(kept), len(skipped), skipSummary(skipped))
	}
	if len(kept) == 0 {
		return nil
	}
	return g.addBounded(kept)
}

// skipSummary counts skipped candidates by reason, for example
// "ignore-dir=3, size=1". Reasons are sorted so the log line is stable.
func skipSummary(skipped []gitguard.Skip) string {
	counts := make(map[string]int)
	for _, s := range skipped {
		counts[s.Reason]++
	}
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		parts = append(parts, fmt.Sprintf("%s=%d", r, counts[r]))
	}
	return strings.Join(parts, ", ")
}

// addPathsChunk caps how many paths one git add invocation receives, so a
// very long candidate list cannot exceed the OS argument limit.
const addPathsChunk = 1000

// addBounded stages paths with git add, all invocations sharing one deadline
// of the policy's AddTimeout (zero means no deadline). If the add times out
// or fails, the interrupted git process may leave temporary pack files behind,
// so addBounded removes them with gitguard.CleanTempPacks and returns the
// error. The success path never cleans: committed packs have .idx and .rev
// siblings, and removing them is only for failure and interruption debris.
func (g *ProjectGit) addBounded(paths []string) error {
	ctx := context.Background()
	if g.stagingPolicy.AddTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.stagingPolicy.AddTimeout)
		defer cancel()
	}
	for start := 0; start < len(paths); start += addPathsChunk {
		end := min(start+addPathsChunk, len(paths))
		cmd := exec.CommandContext(ctx, "git", append([]string{"add", "--"}, paths[start:end]...)...)
		cmd.Dir = g.dir
		out, err := cmd.CombinedOutput()
		if err == nil {
			continue
		}
		cause := err
		if ctx.Err() != nil {
			cause = fmt.Errorf("timed out after %s: %w", g.stagingPolicy.AddTimeout, ctx.Err())
		}
		removed, cerr := gitguard.CleanTempPacks(g.dir)
		if cerr != nil {
			fmt.Printf("[datawatch] warning: cleaning interrupted pack files in %s: %v\n", g.dir, cerr)
		} else if len(removed) > 0 {
			fmt.Printf("[datawatch] removed %d interrupted pack file(s) in %s\n", len(removed), g.dir)
		}
		return fmt.Errorf("git add in %s: %w\n%s", g.dir, cause, out)
	}
	return nil
}

// SetRunStart records when this run started so an empty-scope (unscoped)
// auto-commit stages only the files changed since then. See
// unscopedChangedFiles. Zero (the default) means the run start is
// unknown: every untracked file then counts as run output.
func (g *ProjectGit) SetRunStart(t time.Time) {
	g.runStart = t
}

// unscopedChangedFiles returns the set of files this run changed in the
// working tree — the union of modified tracked files
// (`git diff --name-only HEAD`), staged changes
// (`git diff --cached --name-only`), and new untracked files
// (`git ls-files --others --exclude-standard`). When since is non-zero,
// untracked files whose mtime predates since are excluded: they existed
// before the run and may be another session's (or the operator's) work,
// exactly the leak the scoped-staging fix closes for empty scopes. Paths
// are repo-relative and sorted for deterministic logs. Returns an empty
// slice when git reports nothing (including when git itself fails).
func (g *ProjectGit) unscopedChangedFiles(since time.Time) []string {
	seen := make(map[string]struct{})
	var untracked []string
	if out, err := gitOutput(g.dir, "diff", "--name-only", "HEAD"); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				seen[l] = struct{}{}
			}
		}
	}
	if out, err := gitOutput(g.dir, "diff", "--cached", "--name-only"); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				seen[l] = struct{}{}
			}
		}
	}
	if out, err := gitOutput(g.dir, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				untracked = append(untracked, l)
			}
		}
	}
	for _, rel := range untracked {
		if since.IsZero() {
			seen[rel] = struct{}{}
			continue
		}
		// mtime bounds what counts as run output. Files that no longer
		// exist are skipped — staging a missing path would fail the add.
		st, err := os.Stat(filepath.Join(g.dir, rel))
		if err != nil {
			continue
		}
		if st.ModTime().After(since) {
			seen[rel] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// stagedOnly reports whether the index holds any staged changes relative
// to HEAD (staged additions of previously-untracked files count; pure
// working-tree dirt that was intentionally NOT staged — i.e. out-of-scope
// changes after a scoped stage — does not). This is the right empty-tree
// guard after a scoped stage: "git status --porcelain" would see the
// out-of-scope dirt and lead to a failed empty commit.
func (g *ProjectGit) stagedOnly() bool {
	return runGit(g.dir, "diff", "--cached", "--quiet") != nil
}

// PreSessionCommit creates a snapshot commit before the session starts.
// prdID and taskTitle name the snapshot; the subject comes from
// snapshotSubject, e.g. "snapshot before task <title> (PRD <prdShortID>)",
// and never contains the worker prompt. scope are the session's effective
// write directories. nil/empty preserves baseline behavior — the whole
// dirty tree is staged, with a warning: the pre-session snapshot is
// designed to capture pre-existing dirt (the pinned Start() test
// contract), so the run's later completion commit diffs only against that
// baseline. Completion auto-commits bound unscoped staging to the run via
// SetRunStart instead.
func (g *ProjectGit) PreSessionCommit(prdID, taskTitle string, scope []string) error {
	if err := g.stageChanges(scope); err != nil {
		return err
	}
	if !g.stagedOnly() {
		return nil // nothing to commit
	}
	return g.commit("-m", snapshotSubject(prdID, taskTitle))
}

// PostSessionCommit creates a final commit after the session ends.
// scope are the session's effective write directories; nil/empty stages
// only the files changed since the run start (SetRunStart) with a
// warning.
func (g *ProjectGit) PostSessionCommit(sessionID, task string, state State, scope []string) error {
	if err := g.stageChanges(scope); err != nil {
		return err
	}
	if !g.stagedOnly() {
		return nil
	}
	msg := fmt.Sprintf("session[%s](%s): %s", sessionID, state, truncateStr(task, 60))
	return g.commit("-m", msg)
}

// CommitSessionWork stages changes (scoped to the session's write
// directories) and commits them with a message built from the session
// summary (falling back to the task title). Returns the new HEAD sha,
// or ("", nil) when there is nothing to commit.
//
// scope are the session's effective write directories (PRD.WriteDirs /
// task ProjectDir override, resolved by the autonomous executor). Nil or
// empty stages only the files changed since the run start
// (SetRunStart), with a warning.
func (g *ProjectGit) CommitSessionWork(sessionID, task, summary string, scope []string) (string, error) {
	if err := g.stageChanges(scope); err != nil {
		return "", err
	}
	if !g.stagedOnly() {
		return "", nil
	}
	if err := g.commit("-m", buildCommitMessage(sessionID, summary, task)); err != nil {
		return "", err
	}
	return g.headSHA()
}

// TaskCommitLabel is the bare task identity an automatic task commit is
// named after. Prompt is the full worker prompt: it is only ever used to
// recognise prompt text, never written as a subject.
type TaskCommitLabel struct {
	PRDID        string
	TaskTitle    string
	Prompt       string
	Summary      string
	Verification string
}

// CommitTaskWork stages changes exactly as CommitSessionWork does and commits
// them under a subject built from the task title ("task(<prd>): <title>")
// rather than from the worker prompt. The body lists the staged files and the
// verification result; a non-empty Summary that is not prompt text is
// appended as the final line. Returns the new HEAD sha, or ("", nil) when
// nothing is staged.
func (g *ProjectGit) CommitTaskWork(sessionID string, label TaskCommitLabel, scope []string) (string, error) {
	if err := g.stageChanges(scope); err != nil {
		return "", err
	}
	if !g.stagedOnly() {
		return "", nil
	}
	names, err := gitOutput(g.dir, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	files := strings.Split(strings.TrimSpace(names), "\n")

	msg := taskCommitSubject(label.PRDID, label.TaskTitle, label.Prompt) + "\n\n" +
		commitBody(files, label.Verification)
	if s := taskCommitSummaryLine(label.Summary, label.Prompt); s != "" {
		msg += "\n\n" + s
	}

	path, err := writeCommitMessageFile(msg)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(path) }()
	if err := g.commit("-F", path); err != nil {
		return "", fmt.Errorf("session %s: %w", sessionID, err)
	}
	return g.headSHA()
}

// taskCommitSummaryLine collapses summary to one line for a task commit body.
// It returns "" when the summary is empty or is prompt text.
func taskCommitSummaryLine(summary, prompt string) string {
	line := strings.Join(strings.Fields(summary), " ")
	if line == "" || isPromptLike(line, prompt) {
		return ""
	}
	return capRunes(line, commitSubjectMaxRunes)
}

// writeCommitMessageFile writes msg to a new temporary file in the OS temp
// directory and returns its path. The caller removes it.
func writeCommitMessageFile(msg string) (string, error) {
	f, err := os.CreateTemp("", "datawatch-commit-*.txt")
	if err != nil {
		return "", fmt.Errorf("commit message file: %w", err)
	}
	if _, err := f.WriteString(msg); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("commit message file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("commit message file: %w", err)
	}
	return f.Name(), nil
}

// headSHA returns the sha of HEAD, trimmed.
func (g *ProjectGit) headSHA() (string, error) {
	sha, err := gitOutput(g.dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// buildCommitMessage builds the commit subject for CommitSessionWork:
// summary trimmed, collapsed to its first line, capped at 200 chars. If that
// is empty it falls back to "session work", with the session ID appended in
// parentheses when sessionID is non-empty. The task prompt is never used as
// the subject; the result passes through refusePromptSubject as a safety net.
// Never empty.
func buildCommitMessage(sessionID, summary, task string) string {
	msg := strings.TrimSpace(summary)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if msg == "" {
		msg = commitFallbackTitle
		if sessionID != "" {
			msg += " (" + sessionID + ")"
		}
	}
	return refusePromptSubject(msg, task, commitFallbackTitle)
}

// TagCheckpoint (BL29) creates an annotated tag at HEAD pointing at
// the pre-/post-session state. Tag name pattern:
//
//	datawatch-{kind}-{sessionID}    (kind: "pre" | "post")
//
// kind must be "pre" or "post". Idempotent — re-tagging is allowed
// (uses git tag -f) so a session restart can update the marker.
func (g *ProjectGit) TagCheckpoint(kind, sessionID, task string) error {
	if !g.IsRepo() {
		return nil
	}
	if kind != "pre" && kind != "post" {
		return fmt.Errorf("invalid checkpoint kind %q (want pre|post)", kind)
	}
	tag := fmt.Sprintf("datawatch-%s-%s", kind, sessionID)
	if err := validateGitRef("tag", tag); err != nil {
		return err
	}
	msg := fmt.Sprintf("datawatch %s-session checkpoint for %s: %s",
		kind, sessionID, truncateStr(task, 60))
	return runGitEnv(g.dir, nil, []string{"tag", "-f", "-a", "-m", msg, "--"}, tag)
}

// Rollback (BL29) hard-resets the working tree to the pre-session
// checkpoint tag. Returns an error if the pre-tag doesn't exist or
// the working tree has uncommitted changes (operator must commit /
// stash / accept loss explicitly via `force=true`).
func (g *ProjectGit) Rollback(sessionID string, force bool) error {
	if !g.IsRepo() {
		return fmt.Errorf("not a git repo: %s", g.dir)
	}
	tag := "datawatch-pre-" + sessionID
	if err := validateGitRef("tag", tag); err != nil {
		return err
	}
	if _, err := gitOutputOperands(g.dir, []string{"rev-parse", "--verify"}, tag); err != nil {
		return fmt.Errorf("pre-session tag not found: %s", tag)
	}
	if !force && g.HasChanges() {
		return fmt.Errorf("uncommitted changes present — pass force=true to discard")
	}
	return runGitEnv(g.dir, nil, []string{"reset", "--hard"}, tag)
}

// HasChanges returns true if there are uncommitted changes in dir.
func (g *ProjectGit) HasChanges() bool {
	out, err := gitOutput(g.dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// DiffStat (BL10) parses `git diff --stat HEAD~1..HEAD` and returns
// a structured summary of files changed + insertions + deletions. If
// HEAD~1 doesn't exist (initial commit) or the dir is not a repo,
// returns a zero-valued DiffStat with err nil so callers can treat
// "no diff" identically to "no changes".
func (g *ProjectGit) DiffStat() (DiffStat, error) {
	if !g.IsRepo() {
		return DiffStat{}, nil
	}
	out, err := gitOutput(g.dir, "diff", "--shortstat", "HEAD~1..HEAD")
	if err != nil {
		// Likely no parent commit (initial). Treat as empty.
		return DiffStat{}, nil
	}
	return parseShortstat(strings.TrimSpace(out)), nil
}

// DiffStat is a structured summary of `git diff --shortstat`.
//
// Example shortstat: " 3 files changed, 47 insertions(+), 12 deletions(-)"
type DiffStat struct {
	Files      int    `json:"files"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Summary    string `json:"summary"` // human-readable, e.g. "3 files, +47/-12"
}

// IsZero reports whether the diff summarizes no change.
func (d DiffStat) IsZero() bool {
	return d.Files == 0 && d.Insertions == 0 && d.Deletions == 0
}

// DiffNames (Phase 4 follow-up, v5.26.67) returns the list of files
// changed in HEAD~1..HEAD. Companion to DiffStat — operator-asked
// post-session hook that populates Task.FilesTouched on autonomous
// spawns. Returns nil when not a repo or when HEAD~1 doesn't exist.
// Capped at 50 entries to keep the autonomous PRD record bounded
// (per the Phase 4 design's 5KB-per-story budget).
func (g *ProjectGit) DiffNames() []string {
	if !g.IsRepo() {
		return nil
	}
	out, err := gitOutput(g.dir, "diff", "--name-only", "HEAD~1..HEAD")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l)
		if len(files) >= 50 {
			break
		}
	}
	return files
}

// TouchedFiles (B102) returns files modified or created since the given time,
// working for both git and non-git project directories. Capped at 50 entries.
//
// For git repos: unions tracked-modified (git diff --name-only HEAD) with new
// untracked files (git ls-files --others --exclude-standard). This catches
// uncommitted edits and brand-new files that git diff HEAD~1..HEAD misses.
//
// For non-git directories (or when git fails): walks the directory tree and
// returns files whose ModTime is after since, skipping dotfiles and
// vendor/node_modules directories.
func (g *ProjectGit) TouchedFiles(since time.Time) []string {
	seen := make(map[string]struct{})
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && len(seen) < 50 {
			seen[name] = struct{}{}
		}
	}

	if g.IsRepo() {
		// Tracked modified/staged vs HEAD — git returns repo-relative paths;
		// join with g.dir so the caller gets absolute paths.
		if out, err := gitOutput(g.dir, "diff", "--name-only", "HEAD"); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
				l = strings.TrimSpace(l)
				if l != "" {
					add(filepath.Join(g.dir, l))
				}
			}
		}
		// New untracked files (not yet in git index)
		if out, err := gitOutput(g.dir, "ls-files", "--others", "--exclude-standard"); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
				l = strings.TrimSpace(l)
				if l != "" {
					add(filepath.Join(g.dir, l))
				}
			}
		}
		if len(seen) > 0 {
			out := make([]string, 0, len(seen))
			for f := range seen {
				out = append(out, f)
			}
			return out
		}
		// Fall through to find-newer if git returned nothing (possible on fresh clone)
	}

	// Non-git or git returned nothing: walk and find files modified after since.
	// Use the absolute path from filepath.Walk directly so results are always absolute.
	skipDirs := map[string]bool{
		"node_modules": true, "vendor": true, ".git": true,
		".hg": true, ".svn": true,
	}
	_ = filepath.Walk(g.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(seen) >= 50 {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] || (info.Name() != "." && strings.HasPrefix(info.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		if info.ModTime().After(since) {
			add(path)
		}
		return nil
	})

	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	return out
}

func parseShortstat(line string) DiffStat {
	// Tokens we care about: "<n> file[s] changed", "<n> insertion[s]", "<n> deletion[s]".
	out := DiffStat{}
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		var n int
		if _, err := fmt.Sscanf(part, "%d", &n); err != nil {
			continue
		}
		switch {
		case strings.Contains(part, "file"):
			out.Files = n
		case strings.Contains(part, "insertion"):
			out.Insertions = n
		case strings.Contains(part, "deletion"):
			out.Deletions = n
		}
	}
	if !out.IsZero() {
		out.Summary = fmt.Sprintf("%d file(s), +%d/-%d",
			out.Files, out.Insertions, out.Deletions)
	}
	return out
}

// CurrentBranch returns the name of the currently checked-out branch
// (or "HEAD" for a detached HEAD). Empty + error when not a repo.
func (g *ProjectGit) CurrentBranch() (string, error) {
	out, err := gitOutput(g.dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

// PushBranch pushes branch to remote, ephemerally re-injecting
// originURL with token-based HTTPS basic-auth so the credential
// never lands in .git/config. Origin URL is restored on return.
//
// F10 S5.4 — used by the post-session PR hook to push the worker's
// commits back to the project repo before opening a PR.
//
// remoteName is typically "origin"; pass empty for "origin".
func (g *ProjectGit) PushBranch(remoteName, branch, originURL, token string) error {
	if remoteName == "" {
		remoteName = "origin"
	}
	if branch == "" {
		return fmt.Errorf("PushBranch: branch required")
	}
	if err := validateGitRemoteName(remoteName); err != nil {
		return err
	}
	if err := validateGitRef("branch", branch); err != nil {
		return err
	}
	if originURL != "" {
		if err := validateGitURL(originURL); err != nil {
			return err
		}
	}
	if originURL == "" {
		// Fall back to the configured origin (no token injection — caller
		// presumably has ambient creds).
		return runGitEnv(g.dir, nil, []string{"push", "-u", "--"}, remoteName, branch)
	}

	// Snapshot the current origin so we can restore it on exit.
	prevURL, _ := gitOutputOperands(g.dir, []string{"remote", "get-url", "--"}, remoteName)
	prevURL = strings.TrimSpace(prevURL)

	authURL := injectTokenIntoHTTPS(originURL, token)
	if err := runGitEnv(g.dir, nil, []string{"remote", "set-url", "--"}, remoteName, authURL); err != nil {
		return fmt.Errorf("set credential URL: %w", err)
	}
	defer func() {
		// Restore the canonical (no-token) URL so the credential
		// never persists in .git/config across calls.
		restore := originURL
		if prevURL != "" {
			restore = prevURL
		}
		_ = runGitEnv(g.dir, nil, []string{"remote", "set-url", "--"}, remoteName, restore)
	}()

	return runGitEnv(g.dir, nil, []string{"push", "-u", "--"}, remoteName, branch)
}

// injectTokenIntoHTTPS adds an HTTPS basic-auth header to an https://
// URL with x-access-token as username. Returns rawURL unchanged when
// token is empty or scheme isn't http/https. Mirrors
// internal/agents/worker_clone.go's helper but lives here so the
// session package doesn't need to depend on agents.
func injectTokenIntoHTTPS(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	const httpsPrefix = "https://"
	const httpPrefix = "http://"
	prefix := ""
	switch {
	case strings.HasPrefix(rawURL, httpsPrefix):
		prefix = httpsPrefix
	case strings.HasPrefix(rawURL, httpPrefix):
		prefix = httpPrefix
	default:
		return rawURL
	}
	rest := rawURL[len(prefix):]
	// Skip if already has user info — operator-supplied creds win.
	if i := strings.Index(rest, "@"); i >= 0 && i < strings.Index(rest+"/", "/") {
		return rawURL
	}
	return prefix + "x-access-token:" + token + "@" + rest
}

// checkGitOperands refuses any operand (a ref, remote name, URL or path) that
// is empty, begins with a dash so git would read it as an option, or holds a
// NUL or other control character. Operands travel apart from the fixed
// subcommand and flags so that every value from outside this file passes
// through this check before it reaches git's argv. Where git accepts it the
// caller places "--" ahead of the operands as well.
func checkGitOperands(operands []string) error {
	for _, o := range operands {
		if strings.ContainsRune(o, 0) {
			return fmt.Errorf("invalid git operand: NUL byte")
		}
		if err := checkGitArg("operand", o); err != nil {
			return err
		}
	}
	return nil
}

// checkGitFixedArgs refuses a NUL byte in the subcommand and flag arguments.
func checkGitFixedArgs(args []string) error {
	for _, a := range args {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("invalid git argument: NUL byte")
		}
	}
	return nil
}

// runGit runs git with a fixed argument list (subcommand, flags and literal
// values only). Values from outside the package go through runGitEnv as
// operands.
func runGit(dir string, args ...string) error {
	return runGitEnv(dir, nil, args)
}

// runGitEnv is runGit with extra environment entries appended to the process
// environment, and with operands (refs, remotes, URLs, paths) validated by
// checkGitOperands and appended after args.
func runGitEnv(dir string, env []string, args []string, operands ...string) error {
	if err := checkGitFixedArgs(args); err != nil {
		return err
	}
	if err := checkGitOperands(operands); err != nil {
		return err
	}
	argv := append(append([]string{}, args...), operands...)
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v in %s: %w\n%s", argv, dir, err, out)
	}
	return nil
}

// gitOutput runs git with a fixed argument list and returns its stdout.
func gitOutput(dir string, args ...string) (string, error) {
	return gitOutputOperands(dir, args)
}

// gitOutputOperands is gitOutput with operands validated by checkGitOperands
// and appended after args.
func gitOutputOperands(dir string, args []string, operands ...string) (string, error) {
	if err := checkGitFixedArgs(args); err != nil {
		return "", err
	}
	if err := checkGitOperands(operands); err != nil {
		return "", err
	}
	argv := append(append([]string{}, args...), operands...)
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
