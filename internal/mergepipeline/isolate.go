// An isolated run builds the merge in a scratch worktree on a temporary
// branch. Every step works there. The project's main branch, and any branch
// the run rebased, move only after the remote is confirmed to hold the pushed
// commit; a run that fails, is held or is killed leaves them as they were, so
// no later run can carry its work. A shadow run is an isolated run that sends
// nothing.
package mergepipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/git"
)

const (
	// scratchBranchPrefix names the temporary branches of isolated runs.
	scratchBranchPrefix = "datawatch-merge/"
	// scratchTagMarks holds one ref per tag an unfinished isolated run made,
	// pointing at the tagged commit, so a tag of a run that was killed before
	// its push can be removed.
	scratchTagMarks = "refs/datawatch-merge-tags/"
	scratchDirName  = "dw-merge-"
	// scratchTmuxNote is the file in a run's scratch folder that names its
	// tmux folder, which lies elsewhere.
	scratchTmuxNote = "tmux-dir"
	scratchTmuxName = scratchDirName + "tmux-"
	// localMainStepName is the step that moves the project's main branch.
	localMainStepName = "update-local-main"
)

// commitPusher is the optional ShipRunner surface an isolated run pushes
// through: it sends one exact commit to the remote's branch, whatever any
// local branch of that name holds.
type commitPusher interface {
	PushCommit(ctx context.Context, dir, commit, branch, tag string) error
}

type branchMove struct{ old, new string }

// isolation is the scratch side of one isolated run.
type isolation struct {
	// project is the Runner as the caller built it, and git runs in its
	// directory.
	project *Runner
	git     *Merger
	runID   string
	root    string // temporary folder: the worktree and the checks' home
	tree    string // the scratch worktree
	tmux    string // private tmux socket folder for checks
	branch  string // the temporary branch that stands in for main
	remote  string
	// refs maps a requested branch to the temporary branch that holds its
	// rebased copy; moves says where the real branch goes after the push.
	refs  map[string]string
	moves map[string]branchMove
	seq   int
	// confirmed is set once the remote is confirmed to hold the pushed
	// commit; promoted once the run works in the project's checkout again.
	confirmed bool
	promoted  bool
	// journal is this run's entry in the push journal, while it has one.
	journal *journalledPush
}

func (s *runState) isolated() bool { return s.iso != nil }

// scratch reports whether the run is still working in its scratch tree.
func (s *runState) scratch() bool { return s.iso != nil && !s.iso.promoted }

func (s *runState) shadow() bool { return s.iso != nil && s.iso.project.Shadow }

// candidateRef is what stands for the requested branch in this run: the
// branch itself, or the temporary branch holding its rebased copy.
func (s *runState) candidateRef(branch string) string {
	if s.iso != nil {
		if ref, ok := s.iso.refs[branch]; ok {
			return ref
		}
	}
	return branch
}

// remoteMainHead asks the remote for its main branch and makes the commit
// available here. A shadow run moves no remote-tracking ref while doing so.
// An empty head means the remote has no such branch yet.
func (i *isolation) remoteMainHead(ctx context.Context) (string, error) {
	main := i.project.mainBranch()
	ls, err := i.git.gitOK(ctx, i.git.Dir, "ls-remote", "--heads", i.remote, "refs/heads/"+main)
	if err != nil {
		return "", fmt.Errorf("read %s on %s: %w", main, i.remote, err)
	}
	head := ""
	for _, line := range strings.Split(ls, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == "refs/heads/"+main && git.IsObjectID(f[0]) {
			head = f[0]
		}
	}
	if head == "" {
		return "", nil
	}
	args := []string{"fetch", "--quiet", i.remote, main}
	if i.project.Shadow {
		args = []string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--refmap=", i.remote, "refs/heads/" + main}
	}
	if _, err := i.git.gitOK(ctx, i.git.Dir, args...); err != nil {
		return "", fmt.Errorf("fetch %s from %s: %w", main, i.remote, err)
	}
	return head, nil
}

func (i *isolation) isAncestor(ctx context.Context, a, b string) (bool, error) {
	res, err := i.git.git(ctx, i.git.Dir, "merge-base", "--is-ancestor", a, b)
	if err != nil {
		return false, err
	}
	switch res.Code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("compare %s and %s: exit %d: %s", a, b, res.Code, strings.TrimSpace(tail(res.Out, 500)))
}

// baseOf picks the commit the merge is built on: the remote's main, or local
// main when there is no remote branch. Local main must hold nothing the
// remote does not: a push would send it along with this run's work.
func (i *isolation) baseOf(ctx context.Context) (base, remoteHead string, err error) {
	main := i.project.mainBranch()
	local, err := i.git.revParse(ctx, i.git.Dir, "refs/heads/"+main)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", main, err)
	}
	if i.remote == "" {
		return local, "", nil
	}
	remoteHead, err = i.remoteMainHead(ctx)
	if err != nil || remoteHead == "" {
		return local, "", err
	}
	out, err := i.git.gitOK(ctx, i.git.Dir, "rev-list", remoteHead+".."+local)
	if err != nil {
		return "", "", err
	}
	if ahead := strings.Fields(out); len(ahead) > 0 {
		named := make([]string, 0, 5)
		for _, c := range ahead {
			if len(named) == 5 {
				named = append(named, "...")
				break
			}
			named = append(named, shortSHA(c))
		}
		return "", "", fmt.Errorf("local %s holds %d commit(s) that %s/%s does not (%s): a merge run would send them with its own work, so it did nothing. Push them or move them off %s first",
			main, len(ahead), i.remote, main, strings.Join(named, ", "), main)
	}
	return remoteHead, remoteHead, nil
}

// openIsolation makes the scratch tree and points the run at it.
func (s *runState) openIsolation(ctx context.Context) error {
	project := s.r
	i := &isolation{
		project: project, git: &Merger{Dir: project.Dir}, runID: s.run.ID,
		branch: scratchBranchPrefix + s.run.ID, refs: map[string]string{}, moves: map[string]branchMove{},
	}
	names, err := i.git.gitOK(ctx, project.Dir, "remote")
	if err != nil {
		return err
	}
	i.remote = pickRemote(names)
	base, remoteHead, err := i.baseOf(ctx)
	if err != nil {
		return err
	}
	i.sweep(ctx, remoteHead)

	if project.ScratchRoot != "" {
		if err := os.MkdirAll(project.ScratchRoot, 0o700); err != nil {
			return fmt.Errorf("make the scratch folder: %w", err)
		}
	}
	// The folder is named for the run: whatever a killed run left running
	// can be recognised before its merge lock is taken over.
	if i.root, err = os.MkdirTemp(project.ScratchRoot, scratchDirName+s.run.ID+"-"); err != nil {
		return fmt.Errorf("make the scratch folder: %w", err)
	}
	s.iso = i // from here close() cleans up
	i.tree = filepath.Join(i.root, "tree")
	if err := os.Mkdir(filepath.Join(i.root, "home"), 0o700); err != nil {
		return err
	}
	// Directly under the system temporary folder: a tmux socket path has a
	// short length limit.
	if i.tmux, err = os.MkdirTemp("", scratchTmuxName); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(i.root, scratchTmuxNote), []byte(i.tmux), 0o600); err != nil {
		return err
	}
	if _, err := i.git.gitOK(ctx, project.Dir, "worktree", "add", "--quiet", "-b", i.branch, i.tree, base); err != nil {
		return fmt.Errorf("make the scratch worktree: %w", err)
	}

	inner := *project
	inner.Dir, inner.MainBranch = i.tree, i.branch
	if inner.Project == "" {
		inner.Project = filepath.Base(project.Dir)
	}
	if project.Shadow {
		// Nothing that asks, sends, records an outcome or calls a model.
		inner.Model, inner.Questions, inner.ReleaseAnswers, inner.Ship, inner.Notifier = nil, nil, nil, nil, nil
		inner.Landing, inner.Completer, inner.Counter, inner.Pending, inner.PlanRecords = nil, nil, nil, nil, nil
		inner.PostShip = PostShipSettings{}
	}
	s.r = &inner
	s.m.Dir, s.m.Project = i.tree, inner.Project
	s.m.Model, s.m.Questions, s.m.Counter, s.m.Pending = inner.Model, inner.Questions, inner.Counter, inner.Pending
	s.m.CheckEnv, s.m.checkLogDir = s.checkEnvironment, i.root
	return nil
}

// sweep removes what a killed run left behind. The caller holds the
// repository lock, so no other run owns any of it.
func (i *isolation) sweep(ctx context.Context, remoteHead string) {
	repo := i.git.Dir
	if marks, err := i.git.gitOK(ctx, repo, "for-each-ref", "--format=%(refname) %(objectname)", strings.TrimSuffix(scratchTagMarks, "/")); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(marks), "\n") {
			ref, commit, ok := strings.Cut(line, " ")
			if !ok || !git.IsObjectID(commit) {
				continue
			}
			pushed := false
			if remoteHead != "" {
				pushed, _ = i.isAncestor(ctx, commit, remoteHead)
			}
			if i.project.Shadow {
				continue
			}
			if pushed || i.dropUnpushedTag(ctx, strings.TrimPrefix(ref, scratchTagMarks), commit) {
				_, _ = i.git.git(ctx, repo, "update-ref", "-d", ref)
			}
		}
	}
	if list, err := i.git.gitOK(ctx, repo, "worktree", "list", "--porcelain"); err == nil {
		path := ""
		for _, line := range strings.Split(list, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				path = p
			}
			if !strings.HasPrefix(line, "branch refs/heads/"+scratchBranchPrefix) {
				continue
			}
			_, _ = i.git.git(ctx, repo, "worktree", "remove", "--force", path)
			if root := filepath.Dir(path); strings.HasPrefix(filepath.Base(root), scratchDirName) {
				removeNotedTmuxDir(root)
				_ = os.RemoveAll(root)
			}
		}
	}
	_, _ = i.git.git(ctx, repo, "worktree", "prune")
	i.deleteBranches(ctx, scratchBranchPrefix)
}

// removeNotedTmuxDir removes the tmux folder a killed run noted in its
// scratch folder. Only a folder with the name this package gives them goes.
func removeNotedTmuxDir(root string) {
	data, err := os.ReadFile(filepath.Join(root, scratchTmuxNote)) // #nosec G304 -- a fixed name in the run's own folder
	if err != nil {
		return
	}
	dir := strings.TrimSpace(string(data))
	if filepath.IsAbs(dir) && strings.HasPrefix(filepath.Base(dir), scratchTmuxName) {
		_ = os.RemoveAll(dir)
	}
}

// deleteTag removes the local tag only while it still names commit.
func (i *isolation) deleteTag(ctx context.Context, tag, commit string) {
	if strings.HasPrefix(tag, "-") {
		return // git would read the name as an option
	}
	at, err := i.git.revParse(ctx, i.git.Dir, "refs/tags/"+tag+"^{commit}")
	if err != nil || at != commit {
		return
	}
	if _, err := i.git.gitOK(ctx, i.git.Dir, "tag", "-d", tag); err != nil {
		log.Printf("[merge-pipeline] remove the unpushed tag %s: %v", tag, err)
	}
}

func (i *isolation) deleteBranches(ctx context.Context, prefix string) {
	out, err := i.git.gitOK(ctx, i.git.Dir, "for-each-ref", "--format=%(refname)", "refs/heads/"+strings.TrimSuffix(scratchBranchPrefix, "/"))
	if err != nil {
		return
	}
	for _, ref := range strings.Fields(out) {
		if strings.HasPrefix(ref, "refs/heads/"+prefix) {
			_, _ = i.git.git(ctx, i.git.Dir, "update-ref", "-d", ref)
		}
	}
}

// close removes the scratch tree, its temporary branches and, unless the push
// was confirmed, the tag the run made.
func (s *runState) closeIsolation(parent context.Context) {
	i := s.iso
	if i == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Minute)
	defer cancel()
	if s.tagMade != "" {
		mark := scratchTagMarks + s.tagMade
		if commit, err := i.git.revParse(ctx, i.git.Dir, mark); err == nil {
			if i.confirmed || i.dropUnpushedTag(ctx, s.tagMade, commit) {
				_, _ = i.git.git(ctx, i.git.Dir, "update-ref", "-d", mark)
			}
		}
	}
	if i.tree != "" {
		_, _ = i.git.git(ctx, i.git.Dir, "worktree", "remove", "--force", i.tree)
	}
	for _, dir := range []string{i.root, i.tmux} {
		if dir != "" {
			if err := os.RemoveAll(dir); err != nil {
				log.Printf("[merge-pipeline] remove the scratch folder: %v", err)
			}
		}
	}
	_, _ = i.git.git(ctx, i.git.Dir, "worktree", "prune")
	i.deleteBranches(ctx, i.branch)
}

// resetCandidate starts the candidate over on the remote's new main.
func (s *runState) resetCandidate(ctx context.Context) (string, error) {
	i := s.iso
	if i.remote == "" {
		return "", errors.New("the project has no remote to bring the candidate up to")
	}
	base, _, err := i.baseOf(ctx)
	if err != nil {
		return "", err
	}
	if _, err := s.m.gitOK(ctx, i.tree, "reset", "--hard", "--quiet", base); err != nil {
		return "", fmt.Errorf("move the candidate to %s: %w", shortSHA(base), err)
	}
	i.deleteBranches(ctx, i.branch+"-")
	i.refs, i.moves = map[string]string{}, map[string]branchMove{}
	return base, nil
}

// rebaseCandidates rebases a copy of each requested branch onto the
// candidate's main. The real branches do not move. A branch whose rebase
// conflicts is merged as it is when the pipeline has a merge step, so the
// conflict rules settle it or ask.
func (s *runState) rebaseCandidates(ctx context.Context) (string, error) {
	i := s.iso
	main := s.r.mainBranch()
	notes := []string{"no remote; the main branch was not fetched"}
	if i.remote != "" {
		notes = []string{"fetched " + i.project.mainBranch() + " from " + i.remote}
	}
	fail := func(line string, err error) (string, error) {
		return strings.Join(append([]string{line}, notes...), "\n"), err
	}
	for _, b := range s.req.Branches {
		if strings.HasPrefix(b, "-") {
			return fail(fmt.Sprintf("invalid branch name %q; nothing was rebased", b), fmt.Errorf("invalid branch name %q", b))
		}
		old, err := i.git.revParse(ctx, i.git.Dir, "refs/heads/"+b)
		if err != nil {
			return fail(fmt.Sprintf("branch %q does not exist", b), fmt.Errorf("branch %q does not exist: %w", b, err))
		}
		if err := s.checkPin(b, old); err != nil {
			return fail(err.Error()+"; nothing was merged", err)
		}
		inMain, err := i.isAncestor(ctx, old, "refs/heads/"+main)
		if err != nil {
			return fail(err.Error(), err)
		}
		based, err := i.isAncestor(ctx, "refs/heads/"+main, old)
		if err != nil {
			return fail(err.Error(), err)
		}
		if inMain || based {
			notes = append(notes, "already on "+i.project.mainBranch()+": "+b)
			continue
		}
		head, err := s.rebaseCopy(ctx, old, main)
		if err != nil {
			if !s.hasAction(ActionMergeBranches) {
				return fail(fmt.Sprintf("rebase of %q onto %s stopped; that branch was left untouched: %s", b, i.project.mainBranch(), firstLine(err.Error())),
					fmt.Errorf("rebase of %q failed: %w", b, err))
			}
			notes = append(notes, fmt.Sprintf("the rebase of %s did not apply; it is merged as it is, so the conflict rules decide", b))
			continue
		}
		i.seq++
		ref := fmt.Sprintf("%s-b%d", i.branch, i.seq)
		if _, err := i.git.gitOK(ctx, i.git.Dir, "update-ref", "refs/heads/"+ref, head); err != nil {
			return fail(err.Error(), err)
		}
		i.refs[b], i.moves[b] = ref, branchMove{old: old, new: head}
		notes = append(notes, "rebased a copy of "+b+" onto "+i.project.mainBranch())
	}
	return strings.Join(notes, "\n"), nil
}

// rebaseCopy rebases commit onto main in a throwaway worktree and returns the
// new head. The same commit on the same main gives the same result, so a
// question about a conflict is not raised again by every run.
func (s *runState) rebaseCopy(ctx context.Context, commit, main string) (string, error) {
	tmp := filepath.Join(s.iso.root, "rebase")
	defer func() {
		_, _ = s.m.git(context.WithoutCancel(ctx), s.r.Dir, "worktree", "remove", "--force", tmp)
		_ = os.RemoveAll(tmp)
	}()
	if _, err := s.iso.git.gitOK(ctx, s.iso.git.Dir, "worktree", "add", "--quiet", "--detach", tmp, commit); err != nil {
		return "", err
	}
	g := &Merger{Dir: tmp}
	res, err := g.git(ctx, tmp, "rebase", "--committer-date-is-author-date", "refs/heads/"+main)
	if err != nil || res.Code != 0 {
		_, _ = g.git(context.WithoutCancel(ctx), tmp, "rebase", "--abort")
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("the rebase hit a conflict or failed (exit %d)", res.Code)
	}
	return g.revParse(ctx, tmp, "HEAD")
}

func (s *runState) hasAction(action string) bool {
	if s.m == nil {
		return false
	}
	for _, st := range s.m.Pipeline.Steps {
		if st.Action == action {
			return true
		}
	}
	return false
}

// beforePush is the last look before an isolated run sends anything: what the
// run holds must still be what may be merged.
func (s *runState) beforePush(ctx context.Context) error {
	i := s.iso
	if s.failedSteps > 0 {
		return errors.New("a step of this run failed, so nothing is pushed")
	}
	for b, want := range s.pins {
		at, err := i.git.revParse(ctx, i.git.Dir, "refs/heads/"+b)
		if err != nil {
			return fmt.Errorf("read selected branch %q: %w", b, err)
		}
		if at != want {
			return fmt.Errorf("branch %q moved since it was selected: it was %s and is now %s", b, want, at)
		}
	}
	if i.project.Selection != nil {
		e, err := i.project.Selection.SelectMergeRun(ctx, s.req.Branches, s.req.AutomatonID)
		if err != nil {
			return fmt.Errorf("the work is no longer eligible to merge: %w", err)
		}
		if want, ok := s.pins[s.req.Branches[0]]; ok && (e.AutomatonID != s.req.AutomatonID || e.Tip != want) {
			return errors.New("the selection changed while the run was checking the work")
		}
	}
	if !i.project.Shadow && i.project.StillAllowed != nil {
		if err := i.project.StillAllowed(ctx); err != nil {
			return fmt.Errorf("merging is no longer allowed: %w", err)
		}
	}
	tree, err := s.m.treeState(ctx, i.tree)
	if err != nil {
		return err
	}
	if tree.listing != "" {
		return fmt.Errorf("the scratch tree is not clean, so what was checked is not what would be pushed: %s", tree.files())
	}
	main := i.project.mainBranch()
	ok, err := i.isAncestor(ctx, "refs/heads/"+main, s.baseHead)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("local %s changed during the run and holds work the candidate was not built on", main)
	}
	return nil
}

// releaseCommit is the commit of branch this run releases: the selected one,
// else the branch's head.
func (s *runState) releaseCommit(branch string) string {
	if pin, ok := s.pins[branch]; ok {
		return pin
	}
	parent := s.base
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	at, err := s.iso.git.revParse(ctx, s.iso.git.Dir, "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return at
}

// pushCandidate sends the candidate's exact commit, or in a shadow run records
// what it would send.
func (s *runState) pushCandidate(ctx context.Context, tag string) (string, error) {
	if err := s.beforePush(ctx); err != nil {
		return "push refused: " + err.Error(), err
	}
	main := s.iso.project.mainBranch()
	head, err := s.mainHead(ctx)
	if err != nil {
		return "", err
	}
	if s.shadow() {
		return s.shadowPush(ctx, head, tag)
	}
	if s.r.Ship == nil {
		return "", errors.New("no ship runner is wired, so the step was not run")
	}
	note := "pushed " + main + " at " + shortSHA(head)
	if tag != "" {
		note += " and " + tag
	}
	if err := s.journalPush(ctx, head, tag); err != nil {
		return "push refused: it could not be journalled first: " + err.Error(), err
	}
	if p, ok := s.r.Ship.(commitPusher); ok {
		return doneOr(note, p.PushCommit(ctx, s.r.Dir, head, main, tag))
	}
	return doneOr(note, s.r.Ship.Push(ctx, s.r.Dir, main, tag))
}

// shadowPush runs every check the push would run, sends nothing and records
// the refs it would have sent.
func (s *runState) shadowPush(ctx context.Context, head, tag string) (string, error) {
	i := s.iso
	main := i.project.mainBranch()
	out, err := s.m.gitOK(ctx, i.tree, "rev-list", "--reverse", s.baseHead+".."+head)
	if err != nil {
		return "", err
	}
	plan := []autonomous.MergePushRef{{Ref: "refs/heads/" + main, Old: s.baseHead, New: head, Commits: strings.Fields(out)}}
	if tag != "" {
		plan = append(plan, autonomous.MergePushRef{Ref: "refs/tags/" + tag, New: head})
	}
	s.run.PushPlan = plan
	if i.remote != "" {
		// The scan enforces whatever the daemon's mode is: a shadow run
		// reports what the push would deliver.
		res := git.CheckOutgoing(ctx, i.tree, i.remote, []git.OutgoingRef{{Src: head, Dst: "refs/heads/" + main}}, git.EnforcedScanPolicy())
		s.run.PushCheck = &autonomous.MergePushCheck{
			ProjectCheckRan: res.ProjectCheckRan, Output: redact(res.ProjectCheckOutput),
			SecretScanRan: res.SecretScanned, SecretFindings: len(res.SecretFindings),
		}
		if res.Kind != "" {
			s.run.PushCheck.Refusal = redact(string(res.Kind) + ": " + res.Reason)
			err := fmt.Errorf("the push would be refused: %s: %s", res.Kind, redact(res.Reason))
			return err.Error(), err
		}
	}
	s.skipped = true
	return fmt.Sprintf("shadow run: nothing was sent; the push would send %s at %s (%d commit(s)) with %d tag(s), and its checks passed",
		main, shortSHA(head), len(plan[0].Commits), len(plan)-1), nil
}

// promote follows a push step that ran: once the remote is confirmed to hold
// the pushed commit, the rebased branches and the project's main move to what
// was pushed and the rest of the run works in the project's checkout. Without
// that confirmation nothing moves. It reports whether the push was confirmed
// and whether moving main failed.
func (s *runState) promote(ctx context.Context) (confirmed, failed bool) {
	i := s.iso
	step := Step{Name: localMainStepName, Kind: KindShip, OnFailure: OnFailureContinue}
	result, _, _ := s.recordStep(ctx, "isolate", step, DefaultStepTimeout, func(c context.Context) (string, string, error) {
		main := i.project.mainBranch()
		head, err := s.mainHead(c)
		if err != nil {
			return autonomous.MergeStepFailed, err.Error(), err
		}
		s.pushedCommit = head
		held := ""
		if i.remote != "" {
			if held, err = i.remoteMainHead(c); err != nil {
				return autonomous.MergeStepFailed, "the push could not be confirmed, so local " + main + " was left as it was: " + err.Error(), err
			}
		}
		ok := held != ""
		if ok {
			ok, err = i.isAncestor(c, head, held)
		}
		if err != nil || !ok {
			note := fmt.Sprintf("the remote does not show commit %s, so local %s and the branches were left as they were", shortSHA(head), main)
			if confirmsPush(s.r.Ship) {
				note += " and nothing is released"
				return autonomous.MergeStepFailed, note, errors.New("the push is not confirmed: " + note)
			}
			return autonomous.MergeStepSkipped, note, nil
		}
		i.confirmed, confirmed = true, true
		var problems []string
		branches := make([]string, 0, len(i.moves))
		for b := range i.moves {
			branches = append(branches, b)
		}
		sort.Strings(branches)
		for _, b := range branches {
			mv := i.moves[b]
			if _, err := i.git.gitOK(c, i.git.Dir, "update-ref", "refs/heads/"+b, mv.new, mv.old); err != nil {
				problems = append(problems, "branch "+b+" was not moved to its rebased copy: "+err.Error())
			}
		}
		if err := i.advanceMain(c, head); err != nil {
			problems = append(problems, "local "+main+" was not moved and is only behind the remote: "+err.Error())
		} else {
			i.promoted = true
			s.r, s.m.Dir, s.m.CheckEnv, s.m.checkLogDir = i.project, i.project.Dir, nil, ""
			s.observedMain = head
		}
		if len(problems) > 0 {
			err := errors.New(strings.Join(problems, "; "))
			return autonomous.MergeStepFailed, "pushed " + shortSHA(head) + ", but " + err.Error(), err
		}
		return autonomous.MergeStepOK, "the remote holds " + shortSHA(head) + "; local " + main + " is at it", nil
	})
	return confirmed, confirmed && result == autonomous.MergeStepFailed
}

// advanceMain moves the project's main to head by fast-forward only.
func (i *isolation) advanceMain(ctx context.Context, head string) error {
	repo, main := i.git.Dir, i.project.mainBranch()
	cur, err := i.git.gitOK(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(cur) == main {
		_, err := i.git.gitOK(ctx, repo, "merge", "--ff-only", "--quiet", head)
		return err
	}
	old, err := i.git.revParse(ctx, repo, "refs/heads/"+main)
	if err != nil {
		return err
	}
	if ok, err := i.isAncestor(ctx, old, head); err != nil || !ok {
		return fmt.Errorf("%s does not fast-forward to %s", main, shortSHA(head))
	}
	_, err = i.git.gitOK(ctx, repo, "update-ref", "refs/heads/"+main, head, old)
	return err
}

// ---- the environment of a check ----

// checkEnvDrop are the variable name prefixes and parts a check never sees:
// anything that reaches the operator's terminal server, this daemon, a git
// host, a model account or a stored credential.
var (
	checkEnvDropPrefix = []string{"DATAWATCH_", "DW_", "TMUX", "GH_", "GITHUB_", "GITLAB_", "GITEA_", "ANTHROPIC_", "OPENAI_", "CLAUDE", "GEMINI_", "XDG_", "MERGE_"}
	checkEnvDropPart   = []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "API_KEY", "APIKEY", "PRIVATE_KEY"}
	checkEnvDropName   = map[string]bool{"HOME": true, "SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true,
		"MAKEFLAGS": true, "MFLAGS": true, "MAKEFILES": true, "MAKELEVEL": true,
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true}
)

// sharedCacheDirs keeps the build tools' caches where they are although a
// check gets its own home: each variable that is unset is set to the place
// the tool would have used.
func sharedCacheDirs(env map[string]string) {
	home := env["HOME"]
	if home == "" {
		return
	}
	cache := env["XDG_CACHE_HOME"]
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	config := env["XDG_CONFIG_HOME"]
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	def := func(name, value string) {
		if env[name] == "" {
			env[name] = value
		}
	}
	def("GOPATH", filepath.Join(home, "go"))
	def("GOMODCACHE", filepath.Join(strings.Split(env["GOPATH"], string(os.PathListSeparator))[0], "pkg", "mod"))
	def("GOCACHE", filepath.Join(cache, "go-build"))
	def("GOLANGCI_LINT_CACHE", filepath.Join(cache, "golangci-lint"))
	def("GOENV", filepath.Join(config, "go", "env"))
	def("CARGO_HOME", filepath.Join(home, ".cargo"))
	def("RUSTUP_HOME", filepath.Join(home, ".rustup"))
	def("npm_config_cache", filepath.Join(home, ".npm"))
	def("PLAYWRIGHT_BROWSERS_PATH", filepath.Join(cache, "ms-playwright"))
}

// isolatedEnv builds a check's environment from base: an empty home, a
// private tmux folder, no credentials, the tool caches kept, then extra. It
// is an environment, not a sandbox: a check can still open any file the
// daemon's user can.
func isolatedEnv(base []string, home, tmux string, extra []string) []string {
	all := map[string]string{}
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		all[k] = v
	}
	sharedCacheDirs(all)
	names := make([]string, 0, len(all))
	for k := range all {
		upper := strings.ToUpper(k)
		drop := checkEnvDropName[upper]
		for _, p := range checkEnvDropPrefix {
			drop = drop || strings.HasPrefix(upper, p)
		}
		for _, p := range checkEnvDropPart {
			drop = drop || strings.Contains(upper, p)
		}
		if !drop {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	env := make([]string, 0, len(names)+len(extra)+8)
	for _, k := range names {
		env = append(env, k+"="+all[k])
	}
	env = append(env, "HOME="+home, "TMUX_TMPDIR="+tmux,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return append(env, extra...)
}

// checkEnvironment is the environment of every project command of an isolated
// run, with the facts of the merge a check may need.
func (s *runState) checkEnvironment() []string {
	i := s.iso
	version := s.run.ReleaseVersion
	if version == "" {
		version, _ = s.r.currentVersion()
	}
	shadow := ""
	var closed []string
	if s.shadow() {
		shadow = "1"
		closed = shadowCheckEnv
	}
	return isolatedEnv(os.Environ(), filepath.Join(i.root, "home"), i.tmux, append(append([]string(nil), closed...),
		"MERGE_PROJECT_DIR="+i.project.Dir,
		"MERGE_BASE_COMMIT="+s.baseHead,
		"MERGE_MAIN_BRANCH="+i.project.mainBranch(),
		"MERGE_RELEASE_SIZE="+s.r.ReleaseSize,
		"MERGE_RELEASE_VERSION="+version,
		"MERGE_SHADOW="+shadow,
	))
}

// shadowCheckEnv closes two more ways out for the commands of a shadow run:
// git over ssh and the system's git settings (a stored credential helper). It
// is not a sandbox.
var shadowCheckEnv = []string{"GIT_SSH_COMMAND=false", "GIT_SSH=false", "GIT_CONFIG_NOSYSTEM=1"}

// shadowRuns reports whether a shadow run runs this command step: only checks
// and the steps that write the release into files.
func shadowRuns(k Kind) bool {
	return k == KindTest || k == KindLint || k == KindVersionBump || k == KindChangelog
}

// StepsAShadowDoesNotRun names the command steps of the pipeline that a shadow
// run leaves out because of their kind.
func (d Definition) StepsAShadowDoesNotRun() []string {
	var out []string
	for _, s := range d.Steps {
		if s.Action == "" && !shadowRuns(s.Kind) {
			out = append(out, s.Name)
		}
	}
	return out
}

// guardCandidate refuses a command step after which what was checked is not
// what would be pushed: it moved the candidate, or left the scratch tree other
// than it found it (a changed, staged or untracked file; ignored files aside).
// A project's own version and changelog commands may write and commit; the
// push still needs a clean tree.
func (s *runState) guardCandidate(ctx context.Context, step Step, run func() (string, error)) (string, error) {
	if !s.scratch() || step.Kind == KindVersionBump || step.Kind == KindChangelog {
		return run()
	}
	where := func() (string, treeState, error) {
		ref, err := s.m.gitOK(ctx, s.iso.tree, "symbolic-ref", "-q", "HEAD")
		if err != nil {
			return "", treeState{}, fmt.Errorf("the scratch tree left its branch: %w", err)
		}
		head, err := s.mainHead(ctx)
		if err != nil {
			return "", treeState{}, err
		}
		tree, err := s.m.treeState(ctx, s.iso.tree)
		return strings.TrimSpace(ref) + " " + head, tree, err
	}
	before, treeBefore, err := where()
	if err != nil {
		return "", err
	}
	detail, err := run()
	if err != nil {
		return detail, err
	}
	after, treeAfter, aerr := where()
	switch {
	case aerr != nil:
	case after != before:
		aerr = errors.New("the command moved the candidate, so what it checked is not what would be pushed from this tree")
	case treeAfter != treeBefore:
		aerr = fmt.Errorf("the command changed the scratch tree, so what is tested would not be what is pushed: %s", treeAfter.files())
	}
	if aerr != nil {
		return aerr.Error() + "\n" + detail, aerr
	}
	return detail, nil
}
