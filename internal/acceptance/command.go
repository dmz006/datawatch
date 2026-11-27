package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxRunOutput   = 8 << 20 // beyond this the output is cut, which is never a pass
	maxEvidence    = 4 << 10
	maxStateFile   = 1 << 20
	checkWaitDelay = 5 * time.Second
)

// Subject is one exact version of what an item is about.
type Subject struct {
	Repo   string `json:"repo,omitempty"`
	Commit string `json:"commit"`
}

// SubjectResolver names the version an effect is at now: "" or "work" for the
// work product, "ref:<name>" for a branch. The request sources supply it.
type SubjectResolver interface {
	Resolve(ctx context.Context, requestID, effect string) (Subject, error)
}

// RunOutcome is one run of a command check. Finished false means the run says
// nothing about the item.
type RunOutcome struct {
	Finished bool
	ExitCode int
	Output   string
	Cut      bool
	Reason   string
}

// Runner runs a saved check on a clean copy of a version.
type Runner interface {
	Run(ctx context.Context, spec CheckSpec, sub Subject, timeout time.Duration) RunOutcome
}

// Repo reads exact versions.
type Repo interface {
	// ObjectID returns the id of a path at a version; ok is false when the
	// version does not have it.
	ObjectID(ctx context.Context, sub Subject, file string) (id string, ok bool, err error)
	// ReadFile returns a regular file's content at a version.
	ReadFile(ctx context.Context, sub Subject, file string) (data []byte, ok bool, err error)
}

// Git implements Runner and Repo with the git command.
type Git struct{}

func git(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
	full := append([]string{"-C", repo, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "--literal-pathspecs"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) // #nosec G204 -- fixed binary, arguments are not shell-interpreted
	cmd.Env = append(cleanEnv(os.Environ()), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// cleanEnv drops what could point a check at the live system or the working
// tree: tmux, datawatch credentials, git redirections and relative PATH
// entries.
func cleanEnv(in []string) []string {
	out := make([]string, 0, len(in))
	for _, kv := range in {
		name, val, _ := strings.Cut(kv, "=")
		if name == "TMUX" || name == "TMUX_TMPDIR" || strings.HasPrefix(name, "DATAWATCH_") || strings.HasPrefix(name, "GIT_") {
			continue
		}
		if name == "PATH" {
			var keep []string
			for _, p := range filepath.SplitList(val) {
				if filepath.IsAbs(p) {
					keep = append(keep, p)
				}
			}
			kv = "PATH=" + strings.Join(keep, string(os.PathListSeparator))
		}
		out = append(out, kv)
	}
	return out
}

// Version names the commit a repository's ref is at now, with the same
// restrictions as every other read here. A request's resolver uses it.
func (Git) Version(ctx context.Context, repo, ref string) (Subject, error) {
	commit, err := exactCommit(ctx, Subject{Repo: repo, Commit: ref})
	if err != nil {
		return Subject{}, err
	}
	return Subject{Repo: repo, Commit: commit}, nil
}

// repoPath refuses anything that is not a plain path inside the repository.
func repoPath(file string) (string, error) {
	c := path.Clean(file)
	if file == "" || path.IsAbs(c) || c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%q is not a path inside the repository", file)
	}
	return c, nil
}

func exactCommit(ctx context.Context, sub Subject) (string, error) {
	if sub.Repo == "" || sub.Commit == "" || strings.HasPrefix(sub.Commit, "-") {
		return "", errors.New("no exact version of the subject")
	}
	out, err := git(ctx, sub.Repo, nil, "rev-parse", "--verify", "--quiet", sub.Commit+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func lsTree(ctx context.Context, sub Subject, file string) (mode, id string, ok bool, err error) {
	p, err := repoPath(file)
	if err != nil {
		return "", "", false, err
	}
	commit, err := exactCommit(ctx, sub)
	if err != nil {
		return "", "", false, err
	}
	out, err := git(ctx, sub.Repo, nil, "ls-tree", "-z", "--full-tree", commit, "--", p)
	if err != nil {
		return "", "", false, err
	}
	meta, _, found := strings.Cut(string(out), "\t")
	fields := strings.Fields(meta)
	if !found || len(fields) != 3 {
		return "", "", false, nil
	}
	return fields[0], fields[2], true, nil
}

// ObjectID implements Repo.
func (Git) ObjectID(ctx context.Context, sub Subject, file string) (string, bool, error) {
	_, id, ok, err := lsTree(ctx, sub, file)
	return id, ok, err
}

// ReadFile implements Repo. A link or an oversized file is an error: datawatch
// does not judge content it did not read whole.
func (Git) ReadFile(ctx context.Context, sub Subject, file string) ([]byte, bool, error) {
	mode, id, ok, err := lsTree(ctx, sub, file)
	if err != nil || !ok {
		return nil, false, err
	}
	if mode != "100644" && mode != "100755" {
		return nil, false, fmt.Errorf("%q is not a regular file at that version", file)
	}
	out, err := git(ctx, sub.Repo, nil, "cat-file", "blob", id)
	if err != nil {
		return nil, false, err
	}
	if len(out) > maxStateFile {
		return nil, false, fmt.Errorf("%q is too large to read whole", file)
	}
	return out, true, nil
}

// materialize writes the tracked files of one version into dir, without
// touching the repository, its index or its worktrees.
func materialize(ctx context.Context, sub Subject, work, dir string) error {
	commit, err := exactCommit(ctx, sub)
	if err != nil {
		return err
	}
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(work, "index")}
	if _, err := git(ctx, sub.Repo, env, "read-tree", commit); err != nil {
		return err
	}
	_, err = git(ctx, sub.Repo, env, "checkout-index", "--all", "--force", "--prefix="+dir+string(os.PathSeparator))
	return err
}

type cappedBuffer struct {
	bytes.Buffer
	cut bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := maxRunOutput - c.Len(); room < len(p) {
		c.cut = true
		p = p[:max(room, 0)]
	}
	_, _ = c.Buffer.Write(p)
	return n, nil
}

// Run implements Runner: a clean copy of the exact version, with the saved
// script kept outside it.
func (Git) Run(ctx context.Context, spec CheckSpec, sub Subject, timeout time.Duration) RunOutcome {
	fail := func(format string, a ...any) RunOutcome {
		return RunOutcome{ExitCode: -1, Reason: fmt.Sprintf(format, a...)}
	}
	if spec.Script == "" && spec.Command == "" {
		return fail("the list holds no saved copy of this check")
	}
	work, err := os.MkdirTemp("", "dw-acceptance-")
	if err != nil {
		return fail("could not prepare a clean copy: %v", err)
	}
	defer os.RemoveAll(work) //nolint:errcheck
	tree := filepath.Join(work, "tree")
	if err := os.Mkdir(tree, 0o700); err != nil {
		return fail("could not prepare a clean copy: %v", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := materialize(runCtx, sub, work, tree); err != nil {
		return fail("could not prepare a clean copy: %v", err)
	}
	argv := []string{"sh", "-c", spec.Command}
	if spec.Script != "" {
		saved := filepath.Join(work, "check.sh")
		if err := os.WriteFile(saved, []byte(spec.Script), 0o600); err != nil {
			return fail("could not write the saved check: %v", err)
		}
		argv = []string{"bash", saved}
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) // #nosec G204 -- saved copy of an operator-registered or plan-approved check
	cmd.Dir = tree
	cmd.Env = cleanEnv(os.Environ())
	setProcessGroup(cmd)
	cmd.WaitDelay = checkWaitDelay
	buf := &cappedBuffer{}
	cmd.Stdout, cmd.Stderr = buf, buf
	runErr := cmd.Run()
	killProcessGroup(cmd) // nothing the check started outlives it
	out := RunOutcome{Output: buf.String(), Cut: buf.cut}
	var exit *exec.ExitError
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		out.ExitCode, out.Reason = -1, fmt.Sprintf("the check ran past its %s limit", timeout)
	case ctx.Err() != nil:
		out.ExitCode, out.Reason = -1, "the check was cancelled before it finished"
	case runErr == nil:
		out.Finished = true
	case errors.As(runErr, &exit):
		out.ExitCode = exit.ExitCode()
		switch {
		case out.ExitCode < 0:
			out.Reason = "the check was killed"
		case out.ExitCode == 126 || out.ExitCode == 127:
			out.Reason = fmt.Sprintf("exit %d: a tool the check needs could not run", out.ExitCode)
		case out.ExitCode > 128:
			out.Reason = fmt.Sprintf("exit %d: the check was ended by a signal", out.ExitCode)
		default:
			out.Finished = true
		}
	default:
		out.ExitCode, out.Reason = -1, fmt.Sprintf("the check could not start: %v", runErr)
	}
	return out
}

var notRunWords = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(skip|skips|skipped|skipping|todo|xfail|xfailed|deselected|ignored|not run|` +
	`no tests? (to run|ran|run|found|collected)|no test files|collected 0 (items|tests)|(ran|running) 0 tests|0 tests ran)($|[^a-z0-9_])`)

// lineShows is the proof form of lineSeen: a line has to start with the marker,
// after spaces, and say nothing after it about being skipped. "not ok 1" and
// "would run: ok 1" do not show "ok 1". A failure or an already passing start
// run that only needs to be noticed uses lineSeen.
func lineShows(out, marker string) bool {
	marker = strings.TrimLeft(marker, " \t")
	if marker == "" {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimLeft(strings.TrimRight(line, "\r"), " \t")
		rest, found := strings.CutPrefix(line, marker)
		if found && endsAWord(marker, rest) && !notRunWords.MatchString(rest) {
			return true
		}
	}
	return false
}

func wordByte(b byte) bool {
	return b == '_' || b == '-' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// endsAWord reports whether marker, followed by rest, does not stop in the
// middle of a longer name.
func endsAWord(marker, rest string) bool {
	return rest == "" || !wordByte(marker[len(marker)-1]) || !wordByte(rest[0])
}

// lineSeen reports whether a line of out holds marker as a whole word group:
// "PASS: TestA" is not found in "PASS: TestAll" or "PASS: TestA/sub".
func lineSeen(out, marker string) bool {
	if marker == "" {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		for from := 0; ; {
			i := strings.Index(line[from:], marker)
			if i < 0 {
				break
			}
			end := from + i + len(marker)
			if endsAWord(marker, line[end:]) {
				return true
			}
			from += i + 1
		}
	}
	return false
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-n:], "")
}
