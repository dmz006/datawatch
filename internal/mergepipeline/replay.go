// The shadow of one hand merge. datawatch builds the same merge with the same
// engine as a real run, in shadow mode, from the hand merge's own base and
// branch tip.
//
// The engine works in a disposable copy of the project that has a bare
// "remote" of its own, both at the hand merge's base. The pipeline's check
// commands come from the work being merged and are not sandboxed; in the copy
// neither the project's remote nor its folder appears in any git setting, so a
// command that pushes, tags or moves a ref reaches only the copy, where it is
// seen and reported. The project and its remote are only read, before and
// after, so a change that reaches them all the same is reported too.
package mergepipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// ReplayInput is one hand merge to shadow.
type ReplayInput struct {
	// Replay carries the identity: key, Automaton, project, branch, and the
	// hand merge's commit, release and time. The rest is filled in.
	Replay mergeshadow.Replay
	// Repo is the project's repository. It is only read.
	Repo string
	// WorkDir is a folder of this shadow's own, made for the copy of the
	// project and removed afterwards. It must lie outside Repo.
	WorkDir string
	// BranchHead is the commit the Automaton's branch points at now.
	BranchHead string
	// Title is the release title, as a real run is given it.
	Title string
	// Select answers the merge selection rule for the commit the hand merge
	// merged.
	Select func(ctx context.Context, tip string) *mergeshadow.ReplaySelection
	// Runner builds the merge engine for the copy, the way a real run builds
	// it for the project.
	Runner func(copyDir, scratchRoot string) *Runner
	// Settings is added to the settings fingerprint: the project's merge
	// settings as they are written.
	Settings []byte
}

// maxHandHistory bounds how far back along main the hand merge is looked for.
const maxHandHistory = 200

// maxCompareBytes is the largest file read to see whether two versions differ
// only in what may differ.
const maxCompareBytes = 8 << 20

// ShadowHandMerge builds the same merge as a hand merge with the merge engine
// in shadow mode and returns what it saw. It changes nothing in the project or
// on its remote. A shadow that cannot be run returns a replay that says why.
func ShadowHandMerge(ctx context.Context, in ReplayInput) *mergeshadow.Replay {
	rec := in.Replay
	rec.State, rec.Error = mergeshadow.ReplayDone, ""
	rec.Hand.Conflicts = []string{}
	rec.Daemon = mergeshadow.DaemonMerge{Checks: []mergeshadow.ReplayCheck{}, Conflicts: []string{}}
	rec.Content = mergeshadow.ReplayContent{Differences: []string{}}
	rec.Leak = mergeshadow.ReplayLeak{PrivatePatterns: -1}
	rec.SideEffects = mergeshadow.ReplaySideEffects{Found: []string{}}
	// Every git step of the shadow is held to a limit.
	if _, ok := git.ProcessLimitOf(ctx); !ok {
		ctx = git.WithProcessLimit(ctx, git.ShadowProcessLimit)
	}
	if err := shadowHandMerge(ctx, in, &rec); err != nil {
		rec.State, rec.Error = mergeshadow.ReplayError, redact(err.Error())
	}
	rec.UpdatedAt = time.Now().UTC()
	return &rec
}

func shadowHandMerge(ctx context.Context, in ReplayInput, rec *mergeshadow.Replay) (err error) {
	if in.Repo == "" || in.WorkDir == "" || in.Runner == nil || in.Select == nil {
		return errors.New("the shadow is not fully wired")
	}
	repo, err := filepath.Abs(in.Repo)
	if err != nil {
		return err
	}
	work, err := filepath.Abs(in.WorkDir)
	if err != nil {
		return err
	}
	if rel, rerr := filepath.Rel(repo, work); rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("the shadow's folder must lie outside the project")
	}
	src := &Merger{Dir: repo}
	if err := readHandMerge(ctx, src, rec, in.BranchHead); err != nil {
		return err
	}
	rec.Selection = in.Select(ctx, rec.Hand.Tip)

	if err := os.RemoveAll(work); err != nil {
		return fmt.Errorf("clear the shadow's folder: %w", err)
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		return fmt.Errorf("make the shadow's folder: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil && err == nil {
			rec.SideEffects.Found = append(rec.SideEffects.Found, "the shadow's folder could not be removed: "+rerr.Error())
		}
	}()
	c, err := makeShadowCopy(ctx, src, work, rec)
	if err != nil {
		return fmt.Errorf("make the shadow's copy of the project: %w", err)
	}

	runner := in.Runner(c.dir, filepath.Join(work, "scratch"))
	if err := c.handFacts(ctx, src, rec, runner.VersionFiles); err != nil {
		return err
	}
	mainBranch := runner.mainBranch()
	// A shadow run asks, sends, records and calls nothing. The engine drops
	// these itself too.
	runner.Shadow, runner.Isolate, runner.Dir, runner.ScratchRoot = true, true, c.dir, filepath.Join(work, "scratch")
	runner.Ship, runner.Model, runner.Questions, runner.ReleaseAnswers, runner.Notifier = nil, nil, nil, nil, nil
	runner.Landing, runner.Completer, runner.Counter, runner.Pending, runner.PlanRecords = nil, nil, nil, nil, nil
	runner.PostShip, runner.StillAllowed, runner.Selection, runner.Locks = PostShipSettings{}, nil, nil, nil
	runner.Hosts, runner.RemoteURL = nil, ""
	res, err := runner.resolve()
	if err != nil {
		return fmt.Errorf("no merge pipeline could be chosen: %w", err)
	}
	def := res.Definition
	if rec.SettingsHash, err = SettingsFingerprint(runner, in.Settings); err != nil {
		return err
	}
	if err := c.copyPrivateFiles(ctx, repo, def.Shadow); err != nil {
		return err
	}

	copyBefore, err := c.snapshot(ctx, mainBranch)
	if err != nil {
		return fmt.Errorf("read the copy before the run: %w", err)
	}
	realBefore, realErr := readReal(ctx, src)

	out, runErr := runner.Run(ctx, RunRequest{Title: in.Title, Branches: []string{rec.Branch}, AutomatonID: rec.AutomatonID})
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("the shadow did not finish: %w", cerr)
	}
	daemonSide(rec, runner, def, out, runErr)
	leakSide(rec, out.Run)

	c.sideEffects(ctx, rec, mainBranch, copyBefore)
	realSideEffects(ctx, src, rec, c, work, realBefore, realErr)
	leftoverProcesses(rec, out.Run.ID)

	candidate := rec.Daemon.Candidate
	if candidate != "" {
		if err := compareTrees(ctx, c.git, c.dir, def, runner, rec); err != nil {
			return fmt.Errorf("compare the two trees: %w", err)
		}
	} else {
		candidate = rec.Hand.Commit
	}
	rec.CheckFiles = def.checkFiles()
	if rec.ChecksHash, err = CheckFilesFingerprint(ctx, c.dir, candidate, rec.CheckFiles); err != nil {
		return err
	}
	return nil
}

// ---- what the hand merge did ----

func lines(s string) []string { return strings.Fields(s) }

// readHandMerge reads, from the commit the shipped record names, where the
// hand merge started and which branch commit it merged. Along main's first
// parents, the merge that brought the branch in is the oldest commit that
// holds the branch's head; the commit before it on main is the base.
func readHandMerge(ctx context.Context, src *Merger, rec *mergeshadow.Replay, branchHead string) error {
	h := &rec.Hand
	commit, err := src.revParse(ctx, src.Dir, h.Commit+"^{commit}")
	if err != nil || strings.HasPrefix(h.Commit, "-") {
		return fmt.Errorf("the recorded commit %q is not in the repository", h.Commit)
	}
	h.Commit = commit
	if _, err := src.revParse(ctx, src.Dir, branchHead+"^{commit}"); err != nil || strings.HasPrefix(branchHead, "-") {
		return fmt.Errorf("the head of branch %s is not in the repository", rec.Branch)
	}
	holds := func(c string) (bool, error) {
		r, err := src.git(ctx, src.Dir, "merge-base", "--is-ancestor", branchHead, c)
		if err != nil {
			return false, err
		}
		if r.Code > 1 {
			return false, fmt.Errorf("compare %s and %s: %s", shortSHA(branchHead), shortSHA(c), strings.TrimSpace(tail(r.Out, 300)))
		}
		return r.Code == 0, nil
	}
	out, err := src.gitOK(ctx, src.Dir, "rev-list", "--first-parent", "--max-count="+strconv.Itoa(maxHandHistory), commit)
	if err != nil {
		return err
	}
	chain := lines(out)
	merge, ended := "", false
	for _, c := range chain {
		ok, err := holds(c)
		if err != nil {
			return err
		}
		if !ok {
			ended = true
			break
		}
		merge = c
	}
	if merge == "" {
		return fmt.Errorf("the recorded commit %s does not contain the head of branch %s", shortSHA(commit), rec.Branch)
	}
	if !ended {
		return fmt.Errorf("no merge of branch %s was found in the last %d commits before %s", rec.Branch, len(chain), shortSHA(commit))
	}
	out, err = src.gitOK(ctx, src.Dir, "rev-list", "--parents", "--max-count=1", merge)
	if err != nil {
		return err
	}
	ids := lines(out)
	switch {
	case len(ids) < 3:
		return fmt.Errorf("the hand merge has no merge commit for branch %s (it was fast-forwarded), so its base cannot be read", rec.Branch)
	case len(ids) > 3:
		return fmt.Errorf("the hand merge %s merged several branches at once: a shadow compares one Automaton's merge", shortSHA(merge))
	}
	h.Base, h.Tip = ids[1], ids[2]
	if ok, err := holds(h.Tip); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("the merge %s did not bring branch %s in as its merged side", shortSHA(merge), rec.Branch)
	}
	others, err := src.gitOK(ctx, src.Dir, "rev-list", "--first-parent", "--merges", merge+".."+commit)
	if err != nil {
		return err
	}
	if n := len(lines(others)); n > 0 {
		return fmt.Errorf("the hand release also carried %d other merges after this one: a shadow compares one Automaton's merge", n)
	}
	return nil
}

// versionAt is the version the first version file carries at commit.
func versionAt(ctx context.Context, g *Merger, dir, commit string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	out, err := g.gitOK(ctx, dir, "show", commit+":"+files[0])
	if err != nil {
		return ""
	}
	if m := versionLine.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// handFacts reads the hand merge's version, tags and conflicts in the copy,
// which holds every object the project has.
func (c *shadowCopy) handFacts(ctx context.Context, src *Merger, rec *mergeshadow.Replay, versionFiles []string) error {
	h := &rec.Hand
	h.Version = versionAt(ctx, c.git, c.dir, h.Commit, versionFiles)
	h.Size = mergeshadow.ReleaseSizeBetween(versionAt(ctx, c.git, c.dir, h.Base, versionFiles), h.Version)

	added, err := src.gitOK(ctx, src.Dir, "rev-list", h.Base+".."+h.Commit)
	if err != nil {
		return err
	}
	isAdded := map[string]bool{}
	for _, id := range lines(added) {
		isAdded[id] = true
	}
	tags, err := src.gitOK(ctx, src.Dir, "for-each-ref", "--format=%(refname:short) %(objectname) %(*objectname)", "refs/tags")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(tags, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && isAdded[f[len(f)-1]] {
			h.Tags = append(h.Tags, f[0])
		}
	}
	r, err := c.git.git(ctx, c.dir, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", h.Base, h.Tip)
	if err != nil {
		return err
	}
	if r.Code > 1 {
		return fmt.Errorf("read the hand merge's conflicts: %s", strings.TrimSpace(tail(r.Out, 500)))
	}
	if r.Code == 1 {
		parts := strings.Split(r.Out, "\x00")
		for _, p := range parts[1:] {
			if p != "" {
				h.Conflicts = append(h.Conflicts, p)
			}
		}
	}
	return nil
}

// ---- the disposable copy ----

type shadowCopy struct {
	git  *Merger // runs git anywhere in the work folder
	dir  string  // the copy of the project
	bare string  // its remote
	// private are the copied files, left out of the copy's status.
	private []string
}

// copiedSettings are the project's own git settings a commit in the copy needs.
var copiedSettings = []string{"user.name", "user.email", "commit.gpgsign", "tag.gpgsign", "core.autocrlf", "core.filemode"}

// makeShadowCopy builds the copy of the project in work: the history of the
// hand merge's base, tip and commit, its main branch at the base, the merged
// branch at the tip and the tags the base already had, with a bare remote of
// its own at the base.
//
// Nothing is pushed, and git is never asked to move more than those three
// commits from one repository to another. A project can be a shallow clone
// with very many tags. When git receives refs from a shallow repository into
// one that is not cut at the same place, it can keep one bit per received ref
// for every commit and walk the history once per ref, so that its memory grows
// with the square of the number of refs times the history. The tags are
// therefore written here as refs, over objects copied by name, and the remote
// is a file copy that is cut at the same place as the copy: a later push
// between the two, such as one a check command makes, has nothing to work out.
func makeShadowCopy(ctx context.Context, src *Merger, work string, rec *mergeshadow.Replay) (*shadowCopy, error) {
	c := &shadowCopy{git: &Merger{Dir: work}, dir: filepath.Join(work, "project"), bare: filepath.Join(work, "remote.git")}
	mainRef := "refs/heads/" + DefaultMainBranch
	branchRef := "refs/heads/" + rec.Branch
	if branchRef == mainRef || strings.HasPrefix(rec.Branch, "-") {
		return nil, fmt.Errorf("%q cannot be a work branch", rec.Branch)
	}
	run := func(dir string, args ...string) (string, error) { return c.git.gitOK(ctx, dir, args...) }
	if _, err := run(work, "check-ref-format", branchRef); err != nil {
		return nil, fmt.Errorf("%q cannot be a work branch: %w", rec.Branch, err)
	}
	format, err := src.gitOK(ctx, src.Dir, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, err
	}
	initArgs := []string{"init", "--quiet", "--initial-branch=" + DefaultMainBranch, "--object-format=" + strings.TrimSpace(format)}
	if _, err := run(work, append(initArgs, c.dir)...); err != nil {
		return nil, err
	}
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"maintenance.auto", "false"}} {
		if _, err := run(c.dir, "config", kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	// By commit id, so that no ref of the project comes along. A shallow
	// project's cut comes with them.
	// git drops its -c settings for the other end of a local fetch, so the
	// thread count of the packing there is named in the command.
	if _, err := run(c.dir, "-c", "protocol.version=2", "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
		"--no-recurse-submodules", "--update-shallow", "--upload-pack=git -c pack.threads=2 upload-pack", "--", src.Dir, rec.Hand.Base, rec.Hand.Tip, rec.Hand.Commit); err != nil {
		return nil, err
	}
	tags, err := c.copyBaseTags(ctx, src, work, rec.Hand.Base)
	if err != nil {
		return nil, fmt.Errorf("copy the tags of the base: %w", err)
	}
	head := "create " + mainRef + " " + rec.Hand.Base + "\n"
	if err := c.updateRefs(ctx, c.dir, head+tags+
		"create "+branchRef+" "+rec.Hand.Tip+"\n"+
		"create refs/remotes/origin/"+DefaultMainBranch+" "+rec.Hand.Base+"\n"); err != nil {
		return nil, err
	}
	for _, key := range copiedSettings {
		r, err := src.git(ctx, src.Dir, "config", "--local", "--get", key)
		if err != nil {
			return nil, err
		}
		if r.Code == 0 {
			if _, err := run(c.dir, "config", key, strings.TrimSpace(r.Out)); err != nil {
				return nil, err
			}
		}
	}
	for _, args := range [][]string{
		{"symbolic-ref", "HEAD", mainRef},
		{"reset", "--hard", "--quiet"},
	} {
		if _, err := run(c.dir, args...); err != nil {
			return nil, err
		}
	}
	initArgs = append(initArgs, "--bare")
	if _, err := run(work, append(initArgs, c.bare)...); err != nil {
		return nil, err
	}
	// The same objects, cut at the same place.
	if err := copyFiles(ctx, filepath.Join(c.dir, ".git", "objects"), filepath.Join(c.bare, "objects")); err != nil {
		return nil, fmt.Errorf("copy the objects to the copy's remote: %w", err)
	}
	shallow, err := os.ReadFile(filepath.Join(c.dir, ".git", "shallow")) // #nosec G304 -- the shadow's own folder
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := os.WriteFile(filepath.Join(c.bare, "shallow"), shallow, 0o600); err != nil {
			return nil, err
		}
	}
	if err := c.updateRefs(ctx, c.bare, head+tags); err != nil {
		return nil, err
	}
	if _, err := run(c.dir, "remote", "add", "origin", c.bare); err != nil {
		return nil, err
	}
	// git's own records can name the project's folder.
	for _, p := range []string{"logs", "FETCH_HEAD"} {
		if err := os.RemoveAll(filepath.Join(c.dir, ".git", p)); err != nil {
			return nil, err
		}
	}
	cfg, err := run(c.dir, "config", "--local", "--list")
	if err != nil {
		return nil, err
	}
	if strings.Contains(cfg, src.Dir+"\n") {
		return nil, errors.New("the copy's settings still name the project")
	}
	return c, nil
}

// copyBaseTags copies the tag objects of the tags base already had, which a
// version check may read, and returns the update-ref lines that make the tags.
// The commits they name are part of the base's history, which the copy has.
// A tag of a tag is left out.
func (c *shadowCopy) copyBaseTags(ctx context.Context, src *Merger, work, base string) (string, error) {
	out, err := src.gitOK(ctx, src.Dir, "for-each-ref", "--merged="+base,
		"--format=%(refname) %(objectname) %(objecttype) %(type)", "refs/tags")
	if err != nil {
		return "", err
	}
	var refs, objects strings.Builder
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || (f[2] == "tag" && (len(f) < 4 || f[3] != "commit")) {
			continue
		}
		fmt.Fprintf(&refs, "create %s %s\n", f[0], f[1])
		if f[2] == "tag" && !seen[f[1]] {
			seen[f[1]] = true
			objects.WriteString(f[1] + "\n")
		}
	}
	if objects.Len() == 0 {
		return refs.String(), nil
	}
	// Exactly the named objects: no history is walked.
	packFile := filepath.Join(work, "tags.pack")
	pack, err := os.OpenFile(packFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- the shadow's own folder
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(packFile) }()
	err = src.gitStream(ctx, src.Dir, strings.NewReader(objects.String()), pack, "pack-objects", "--stdout", "--quiet")
	if cerr := pack.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	in, err := os.Open(packFile) // #nosec G304 -- the shadow's own folder
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	if err := c.git.gitStream(ctx, c.dir, in, io.Discard, "index-pack", "--stdin"); err != nil {
		return "", err
	}
	return refs.String(), nil
}

// updateRefs writes many refs in one git process.
func (c *shadowCopy) updateRefs(ctx context.Context, dir, lines string) error {
	return c.git.gitStream(ctx, dir, strings.NewReader(lines), io.Discard, "update-ref", "--no-deref", "--stdin")
}

// gitStream runs git in dir with in as its input and out as its output.
func (m *Merger) gitStream(ctx context.Context, dir string, in io.Reader, out io.Writer, args ...string) error {
	if !within(m.Dir, dir) {
		return errors.New("refusing to run git outside the project directory")
	}
	cmd, done := git.Command(ctx, args...)
	defer done()
	var stderr bytes.Buffer
	cmd.Dir, cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, scrubbedEnv(), in, out, &stderr
	if err := cmd.Run(); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(tail(stderr.String(), 1000)))
	}
	return nil
}

// copyFiles copies the plain files under from into to. A file is linked where
// the file system allows it: git never rewrites an object file.
func copyFiles(ctx context.Context, from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case !d.Type().IsRegular():
			return fmt.Errorf("%s is not a plain file", rel)
		}
		if os.Link(path, target) == nil {
			return nil
		}
		in, err := os.Open(path) // #nosec G304 G122 -- the shadow's own folder
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- the shadow's own folder
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	})
}

// copyPrivateFiles copies the files the pipeline names as never committed. A
// file the project does not have is left out, so the check that needs it
// refuses.
func (c *shadowCopy) copyPrivateFiles(ctx context.Context, repo string, s *ShadowDefinition) error {
	if s == nil || len(s.PrivateFiles) == 0 {
		return nil
	}
	if probs := s.problems(); len(probs) > 0 {
		return errors.New(strings.Join(probs, "; "))
	}
	// Opened through a root handle: a name or link that leaves the project is
	// refused by the operating system.
	from, err := os.OpenRoot(repo)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()
	to, err := os.OpenRoot(c.dir)
	if err != nil {
		return err
	}
	defer func() { _ = to.Close() }()
	var excludes []string
	for _, name := range s.PrivateFiles {
		rel := filepath.FromSlash(name)
		if _, err := to.Lstat(rel); err == nil {
			continue // committed: the copy has its own
		}
		st, err := from.Stat(rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !st.Mode().IsRegular() || st.Size() > maxCompareBytes {
			return fmt.Errorf("the private file %s cannot be copied: it must be a small regular file inside the project", name)
		}
		f, err := from.Open(rel)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxCompareBytes))
		_ = f.Close()
		if err != nil {
			return err
		}
		if dir := filepath.Dir(rel); dir != "." {
			if err := to.MkdirAll(dir, 0o700); err != nil {
				return err
			}
		}
		if err := to.WriteFile(rel, data, 0o600); err != nil {
			return err
		}
		c.private = append(c.private, name)
		excludes = append(excludes, "/"+name)
	}
	if len(excludes) == 0 {
		return nil
	}
	info := filepath.Join(c.dir, ".git", "info")
	if err := os.MkdirAll(info, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(info, "exclude"), []byte(strings.Join(excludes, "\n")+"\n"), 0o600)
}

// ---- side effects ----

// refMap reads "name id" lines into a map.
func refMap(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			m[f[0]] = f[1]
		}
	}
	return m
}

type refChange struct{ ref, old, new string }

func refChanges(before, after map[string]string) []refChange {
	var out []refChange
	for ref, id := range before {
		if after[ref] != id {
			out = append(out, refChange{ref, id, after[ref]})
		}
	}
	for ref, id := range after {
		if _, ok := before[ref]; !ok {
			out = append(out, refChange{ref: ref, new: id})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ref < out[b].ref })
	return out
}

func (c refChange) String() string {
	switch {
	case c.old == "":
		return c.ref + " appeared at " + shortSHA(c.new)
	case c.new == "":
		return c.ref + " was removed (was " + shortSHA(c.old) + ")"
	}
	return c.ref + " moved from " + shortSHA(c.old) + " to " + shortSHA(c.new)
}

type copyState struct {
	remote, local map[string]string
	worktrees     int
	status        string
}

const refFormat = "--format=%(refname) %(objectname)"

func (c *shadowCopy) snapshot(ctx context.Context, mainBranch string) (copyState, error) {
	var s copyState
	remote, err := c.git.gitOK(ctx, c.bare, "for-each-ref", refFormat)
	if err != nil {
		return s, err
	}
	local, err := c.git.gitOK(ctx, c.dir, "for-each-ref", refFormat)
	if err != nil {
		return s, err
	}
	trees, err := c.git.gitOK(ctx, c.dir, "worktree", "list", "--porcelain")
	if err != nil {
		return s, err
	}
	status, err := c.git.gitOK(ctx, c.dir, "status", "--porcelain", "--untracked-files=all", "--ignored")
	if err != nil {
		return s, err
	}
	s.remote, s.local = refMap(remote), refMap(local)
	s.worktrees = strings.Count("\n"+trees, "\nworktree ")
	s.status = status
	return s, nil
}

// sideEffects reports what the run's commands did to the copy and its remote.
// The engine itself changes neither in a shadow run.
func (c *shadowCopy) sideEffects(ctx context.Context, rec *mergeshadow.Replay, mainBranch string, before copyState) {
	after, err := c.snapshot(ctx, mainBranch)
	if err != nil {
		rec.SideEffects.Found = append(rec.SideEffects.Found, "the copy could not be read after the run: "+redact(err.Error()))
		return
	}
	add := func(format string, a ...any) {
		rec.SideEffects.Found = append(rec.SideEffects.Found, fmt.Sprintf(format, a...))
	}
	for _, ch := range refChanges(before.remote, after.remote) {
		add("a command of the run pushed to the remote (the shadow's own copy of it): %s", ch)
	}
	for _, ch := range refChanges(before.local, after.local) {
		if ch.ref != "refs/remotes/origin/HEAD" {
			add("a command of the run changed a ref of the project (the shadow's copy): %s", ch)
		}
	}
	if after.worktrees != before.worktrees {
		add("the run left %d worktree(s) where there were %d", after.worktrees, before.worktrees)
	}
	if after.status != before.status {
		add("a command of the run changed files of the project's checkout (the shadow's copy): %s", firstLines(changedLines(before.status, after.status), 5))
	}
}

func changedLines(before, after string) []string {
	had := map[string]bool{}
	for _, l := range strings.Split(before, "\n") {
		had[l] = true
	}
	var out []string
	for _, l := range strings.Split(after, "\n") {
		if l != "" && !had[l] {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func firstLines(l []string, n int) string {
	if len(l) > n {
		return strings.Join(l[:n], ", ") + fmt.Sprintf(" and %d more", len(l)-n)
	}
	return strings.Join(l, ", ")
}

type realState struct {
	local, remote map[string]string
	worktrees     map[string]bool
}

// readReal reads the project's branches, tags and worktrees and its remote's
// refs. It writes nothing.
func readReal(ctx context.Context, src *Merger) (realState, error) {
	s := realState{remote: map[string]string{}, worktrees: map[string]bool{}}
	local, err := src.gitOK(ctx, src.Dir, "for-each-ref", refFormat, "refs/heads", "refs/tags", "refs/datawatch-merge-tags")
	if err != nil {
		return s, err
	}
	s.local = refMap(local)
	trees, err := src.gitOK(ctx, src.Dir, "worktree", "list", "--porcelain")
	if err != nil {
		return s, err
	}
	for _, l := range strings.Split(trees, "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok {
			s.worktrees[p] = true
		}
	}
	names, err := src.gitOK(ctx, src.Dir, "remote")
	if err != nil {
		return s, err
	}
	if remote := pickRemote(names); remote != "" {
		out, err := src.gitOK(ctx, src.Dir, "ls-remote", "--", remote)
		if err != nil {
			return s, err
		}
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) == 2 && strings.HasPrefix(f[1], "refs/") {
				s.remote[f[1]] = f[0]
			}
		}
	}
	return s, nil
}

// realSideEffects tells a change the shadow run made to the project or its
// remote from one somebody else made meanwhile. A commit the copy holds that
// no ref of the project held before was made by the run; on the remote, any
// move to a commit the copy holds, or one the project already had, cannot be
// told apart from one a command of the run made, and nor can a change to a
// tag or a removed branch.
func realSideEffects(ctx context.Context, src *Merger, rec *mergeshadow.Replay, c *shadowCopy, work string, before realState, beforeErr error) {
	add := func(format string, a ...any) {
		rec.SideEffects.Found = append(rec.SideEffects.Found, fmt.Sprintf(format, a...))
	}
	after, err := readReal(ctx, src)
	if beforeErr != nil || err != nil {
		if err == nil {
			err = beforeErr
		}
		add("the project or its remote could not be read, so side effects cannot be ruled out: %s", redact(firstLine(err.Error())))
		return
	}
	var known []string
	seen := map[string]bool{}
	for _, m := range []map[string]string{before.local, before.remote} {
		for _, id := range m {
			if !seen[id] {
				seen[id] = true
				known = append(known, id)
			}
		}
	}
	has := func(id string) bool {
		r, err := c.git.git(ctx, c.dir, "cat-file", "-e", id)
		return err == nil && r.Code == 0
	}
	madeByRun := func(id string) bool {
		if !has(id) {
			return false
		}
		for _, k := range append(known, rec.Hand.Commit) {
			if r, err := c.git.git(ctx, c.dir, "merge-base", "--is-ancestor", id, k); err == nil && r.Code == 0 {
				return false
			}
		}
		return true
	}
	// projectHad reports whether the refs read before the run already reached
	// id: such a commit may not be in the copy, yet a command could send it.
	projectHad := func(id string) bool {
		r, err := src.git(ctx, src.Dir, "cat-file", "-e", id+"^{commit}")
		if err != nil || r.Code != 0 {
			return false
		}
		var in strings.Builder
		in.WriteString(id + "\n")
		for _, k := range known {
			in.WriteString("^" + k + "\n")
		}
		var out bytes.Buffer
		// Nothing is listed when what is known already reaches id.
		err = src.gitStream(ctx, src.Dir, strings.NewReader(in.String()), &out, "rev-list", "--max-count=1", "--ignore-missing", "--stdin")
		return err == nil && out.Len() == 0
	}
	classify := func(where string, remote bool, changes []refChange) {
		for _, ch := range changes {
			tag := strings.HasPrefix(ch.ref, "refs/tags/") || strings.HasPrefix(ch.ref, "refs/datawatch-merge-tags/")
			switch {
			case tag:
				add("a tag changed %s while the shadow ran: %s", where, ch)
			case remote && ch.new == "":
				add("a ref was removed %s while the shadow ran: %s", where, ch)
			case ch.new != "" && ((remote && (has(ch.new) || projectHad(ch.new))) || madeByRun(ch.new)):
				add("changed %s to a commit the shadow held or the project already had: %s", where, ch)
			default:
				rec.SideEffects.Elsewhere = append(rec.SideEffects.Elsewhere, where+": "+ch.String())
			}
		}
	}
	classify("on the remote", true, refChanges(before.remote, after.remote))
	classify("in the project", false, refChanges(before.local, after.local))
	for p := range after.worktrees {
		if !before.worktrees[p] && strings.HasPrefix(p, work+string(filepath.Separator)) {
			add("the run added a worktree to the project: %s", p)
		}
	}
	rec.SideEffects.Checked = true
}

// processSettle is how long a stopped command's processes get to leave.
const processSettle = 2 * time.Second

func leftoverProcesses(rec *mergeshadow.Replay, runID string) {
	deadline := time.Now().Add(processSettle)
	for {
		left, checked := autonomous.ProcessesOfMergeRun(runID)
		switch {
		case !checked:
			rec.SideEffects.Found = append(rec.SideEffects.Found, "processes could not be listed here, so a process the run left cannot be ruled out")
			return
		case len(left) == 0:
			return
		case time.Now().After(deadline):
			rec.SideEffects.Found = append(rec.SideEffects.Found, fmt.Sprintf("%d process(es) the run started are still running: %v", len(left), left))
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ---- what the engine did ----

func daemonSide(rec *mergeshadow.Replay, r *Runner, def Definition, out RunResult, runErr error) {
	d := &rec.Daemon
	run := out.Run
	d.RunID, d.Result, d.Reason, d.Version = run.ID, run.Result, run.Reason, run.ReleaseVersion
	if runErr != nil && d.Reason == "" {
		d.Reason = redact(firstLine(runErr.Error()))
	}
	size := r.ReleaseSize
	if r.ReleaseFor != nil {
		if facts, ok := r.ReleaseFor(rec.AutomatonID); ok {
			size = facts.Size
		}
	}
	d.Size = mergeshadow.ReleaseSize(size)
	results := map[string]string{}
	for _, s := range run.Steps {
		results[s.Step] = s.Result
		if s.Result == autonomous.MergeStepFailed && d.StoppedAt == "" {
			d.StoppedAt = s.Step + ": " + firstLine(s.Detail)
		}
	}
	for _, s := range def.Steps {
		if s.Action != "" || (s.Kind != KindLint && s.Kind != KindTest) {
			continue
		}
		result := results[s.Name]
		if result == "" {
			result = "not_run"
		}
		d.Checks = append(d.Checks, mergeshadow.ReplayCheck{Name: s.Name, Kind: string(s.Kind), Result: result})
	}
	for _, c := range run.Conflicts {
		d.Conflicts = append(d.Conflicts, c.Files...)
		if c.Asked && !d.Asked {
			d.Asked, d.AskNote = true, c.Note
		}
	}
	for _, p := range run.PushPlan {
		d.Push = append(d.Push, mergeshadow.ReplayPushRef{Ref: p.Ref, New: p.New, Commits: len(p.Commits)})
		if d.Candidate == "" && strings.HasPrefix(p.Ref, "refs/heads/") {
			d.Candidate = p.New
		}
	}
}

var privatePatternCount = regexp.MustCompile(`(\d+) private pattern`)

func leakSide(rec *mergeshadow.Replay, run autonomous.MergePipelineRun) {
	p := run.PushCheck
	if p == nil {
		return
	}
	l := &rec.Leak
	l.Evaluated = true
	l.ProjectCheckRan, l.ProjectCheckOutput = p.ProjectCheckRan, p.Output
	l.ProjectCheckPassed = p.ProjectCheckRan && !strings.HasPrefix(p.Refusal, "check")
	l.SecretScanRan, l.SecretFindings, l.Refusal = p.SecretScanRan, p.SecretFindings, p.Refusal
	if m := privatePatternCount.FindAllStringSubmatch(p.Output, -1); len(m) > 0 {
		if n, err := strconv.Atoi(m[len(m)-1][1]); err == nil {
			l.PrivatePatterns = n
		}
	}
}

// ---- comparing the two trees ----

var (
	anyDate       = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	readmeVersion = regexp.MustCompile(`Current release: v([0-9]+\.[0-9]+\.[0-9]+)`)
)

// compareTrees compares the candidate with the hand merge's commit. Only the
// date of the README release line, the dates of changelog headings and the
// generated docs index may differ, unless the pipeline says the hand merge's
// release wording does not count.
func compareTrees(ctx context.Context, g *Merger, dir string, def Definition, r *Runner, rec *mergeshadow.Replay) error {
	cand, hand := rec.Daemon.Candidate, rec.Hand.Commit
	out, err := g.gitOK(ctx, dir, "diff-tree", "-r", "-z", "--no-renames", "--no-commit-id", "--name-only", cand, hand)
	if err != nil {
		return err
	}
	ignore := def.Shadow != nil && def.Shadow.ReleaseText == ReleaseTextIgnore
	read := func(commit, file string) (string, bool) {
		text, err := g.gitOK(ctx, dir, "cat-file", "blob", commit+":"+file)
		return text, err == nil && len(text) <= maxCompareBytes
	}
	mode := func(commit, file string) string {
		text, err := g.gitOK(ctx, dir, "--literal-pathspecs", "ls-tree", commit, "--", file)
		if err != nil || len(strings.Fields(text)) == 0 {
			return ""
		}
		return strings.Fields(text)[0]
	}
	allowed := func(file string) bool {
		if ClassifyConflictFile(file, r.versionFiles()) == ClassGenerated {
			return true
		}
		if file != r.readme() && file != r.changelog() {
			return false
		}
		a, ok1 := read(cand, file)
		b, ok2 := read(hand, file)
		if !ok1 || !ok2 || mode(cand, file) != mode(hand, file) {
			return false
		}
		if file == r.readme() {
			return sameReadme(a, b, ignore)
		}
		base, ok := read(rec.Hand.Base, file)
		return ok && sameChangelog(base, a, b, rec.Hand.Version, ignore)
	}
	for _, file := range strings.Split(out, "\x00") {
		switch {
		case file == "":
		case allowed(file):
			rec.Content.Allowed = append(rec.Content.Allowed, file)
		default:
			rec.Content.Differences = append(rec.Content.Differences, file)
		}
	}
	rec.Content.Compared = true
	return nil
}

// sameReadme reports whether two READMEs differ only in the date on the
// release line or, with ignore, only in that line's date and wording.
func sameReadme(a, b string, ignore bool) bool {
	norm := func(s string) string {
		ls := strings.Split(s, "\n")
		for i, l := range ls {
			m := readmeVersion.FindStringSubmatch(l)
			if _, ok := releaseLineRest(l); !ok || m == nil {
				continue
			}
			if ignore {
				ls[i] = "Current release: v" + m[1]
			} else if loc := readmeReleaseShape.FindStringSubmatchIndex(l); loc != nil {
				ls[i] = l[:loc[4]] + "DATE" + l[loc[5]:]
			}
			break
		}
		return strings.Join(ls, "\n")
	}
	return norm(a) == norm(b)
}

// sameChangelog reports whether two changelogs differ only in heading dates
// or, with ignore, only in the text of the one section each adds for version.
func sameChangelog(base, a, b, version string, ignore bool) bool {
	old := map[string]bool{}
	for _, l := range strings.Split(base, "\n") {
		if strings.HasPrefix(l, "## ") {
			old[l] = true
		}
	}
	norm := func(s string) (string, bool) {
		var out []string
		added, dropping := 0, false
		for _, l := range strings.Split(s, "\n") {
			if strings.HasPrefix(l, "## ") {
				dropping = false
				if ignore && !old[l] && version != "" && strings.Contains(l, version) {
					added++
					dropping = true
					out = append(out, "## the new release")
					continue
				}
				l = anyDate.ReplaceAllString(l, "DATE")
			}
			if !dropping {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n"), !ignore || added == 1
	}
	na, oka := norm(a)
	nb, okb := norm(b)
	return oka && okb && na == nb
}

// ---- fingerprints ----

// SettingsFingerprint hashes what decides how a runner merges: the pipeline in
// effect with its conflict rules, the conflict policy, the release settings
// and the merge settings as written. A change restarts the count of matched
// merges.
func SettingsFingerprint(r *Runner, settings []byte) (string, error) {
	res, err := r.resolve()
	if err != nil {
		return "", fmt.Errorf("no merge pipeline could be chosen: %w", err)
	}
	var conflicts ConflictSettings
	if r.ConflictSettings != nil {
		conflicts = r.ConflictSettings()
	} else {
		conflicts = DefaultConflictSettings()
	}
	data, err := json.Marshal(struct {
		Definition    Definition
		Policy, Mode  string
		ConfirmFirstN int
		VersionFiles  []string
		Settings      []byte
	}{res.Definition, string(conflicts.Policy), string(conflicts.Mode), conflicts.ConfirmFirstN, r.VersionFiles, settings})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// CheckFilesFingerprint hashes the check files as commit holds them.
func CheckFilesFingerprint(ctx context.Context, repo, commit string, files []string) (string, error) {
	if strings.HasPrefix(commit, "-") {
		return "", fmt.Errorf("%q is not a commit", commit)
	}
	g := &Merger{Dir: repo}
	args := append([]string{"--literal-pathspecs", "ls-tree", "--full-tree", commit, "--"}, files...)
	out, err := g.gitOK(ctx, repo, args...)
	if err != nil {
		return "", fmt.Errorf("read the check files at %s: %w", shortSHA(commit), err)
	}
	sum := sha256.Sum256([]byte(out))
	return hex.EncodeToString(sum[:]), nil
}
