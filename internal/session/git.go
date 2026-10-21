package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProjectGit manages git operations on the user's project directory.
type ProjectGit struct {
	dir string
	// runStart bounds unscoped auto-commit staging: untracked files
	// whose mtime predates it are not considered part of this run
	// (see unscopedChangedFiles). Zero means the run start is unknown.
	runStart time.Time
}

// NewProjectGit creates a new ProjectGit for the given directory.
func NewProjectGit(dir string) *ProjectGit {
	return &ProjectGit{dir: dir}
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
func (g *ProjectGit) stageChanges(scope []string) error {
	if len(scope) > 0 {
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
		args := append([]string{"add", "--"}, inside...)
		if err := runGit(g.dir, args...); err != nil {
			return fmt.Errorf("git add %v: %v", inside, err)
		}
		return nil
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
	if err := runGit(g.dir, append([]string{"add", "--"}, files...)...); err != nil {
		return fmt.Errorf("git add %v: %v", names, err)
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
	return runGit(g.dir, "commit", "-m", snapshotSubject(prdID, taskTitle))
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
	return runGit(g.dir, "commit", "-m", msg)
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
	if err := runGit(g.dir, "commit", "-m", buildCommitMessage(sessionID, summary, task)); err != nil {
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
	if err := runGit(g.dir, "commit", "-F", path); err != nil {
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
	msg := fmt.Sprintf("datawatch %s-session checkpoint for %s: %s",
		kind, sessionID, truncateStr(task, 60))
	return runGit(g.dir, "tag", "-f", "-a", tag, "-m", msg)
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
	if _, err := gitOutput(g.dir, "rev-parse", "--verify", tag); err != nil {
		return fmt.Errorf("pre-session tag not found: %s", tag)
	}
	if !force && g.HasChanges() {
		return fmt.Errorf("uncommitted changes present — pass force=true to discard")
	}
	return runGit(g.dir, "reset", "--hard", tag)
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
	if originURL == "" {
		// Fall back to the configured origin (no token injection — caller
		// presumably has ambient creds).
		return runGit(g.dir, "push", "-u", remoteName, branch)
	}

	// Snapshot the current origin so we can restore it on exit.
	prevURL, _ := gitOutput(g.dir, "remote", "get-url", remoteName)
	prevURL = strings.TrimSpace(prevURL)

	authURL := injectTokenIntoHTTPS(originURL, token)
	if err := runGit(g.dir, "remote", "set-url", remoteName, authURL); err != nil {
		return fmt.Errorf("set credential URL: %w", err)
	}
	defer func() {
		// Restore the canonical (no-token) URL so the credential
		// never persists in .git/config across calls.
		restore := originURL
		if prevURL != "" {
			restore = prevURL
		}
		_ = runGit(g.dir, "remote", "set-url", remoteName, restore)
	}()

	return runGit(g.dir, "push", "-u", remoteName, branch)
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

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v in %s: %w\n%s", args, dir, err, out)
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
