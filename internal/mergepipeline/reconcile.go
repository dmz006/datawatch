// Confirmation and repair of an isolated run's push. What a push will send is
// journalled before it leaves, inside the repository's git directory. Only the
// remote says whether it arrived: after a push error, a lost reply, a killed
// run or a restart, the journal is compared with the remote and the project is
// brought to what the remote holds. Nothing here pushes or releases.
package mergepipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// pushJournalDirName is the journal's folder in the git directory.
	pushJournalDirName = "datawatch-merge-pushes"
	// pushJournalKeep is how long a push the remote does not show is still
	// watched for when the remote has not moved: it may yet arrive.
	pushJournalKeep = 24 * time.Hour
	// reconcileStepName is the run step that reports a repaired push.
	reconcileStepName = "reconcile-push"
)

// confirmsPush reports whether a ship runner pushes an exact commit, so the
// remote can be asked for it. The daemon's does. Nothing is released through
// such a runner before the remote confirms the commit.
func confirmsPush(s ShipRunner) bool {
	_, ok := s.(commitPusher)
	return ok
}

// The daemon's ship runner must stay one whose pushes are confirmed.
var _ commitPusher = ExecShipRunner{}

type journalledBranch struct {
	Name string `json:"name"`
	Old  string `json:"old"` // the selected commit
	New  string `json:"new"` // what the branch is once its work is pushed
}

type journalledPush struct {
	RunID       string             `json:"run_id"`
	AutomatonID string             `json:"automaton_id,omitempty"`
	Release     string             `json:"release,omitempty"`
	Remote      string             `json:"remote"`
	Main        string             `json:"main"`
	Base        string             `json:"base"`
	Commit      string             `json:"commit"`
	Tag         string             `json:"tag,omitempty"`
	Branches    []journalledBranch `json:"branches"`
	At          time.Time          `json:"at"`
}

// readsAsOption reports whether a value that is later given to git as an
// argument starts with "-", which no record this package writes does.
func (p journalledPush) readsAsOption() bool {
	values := []string{p.Remote, p.Main, p.Base, p.Commit, p.Tag}
	for _, b := range p.Branches {
		values = append(values, b.Old, b.New)
	}
	for _, v := range values {
		if strings.HasPrefix(v, "-") {
			return true
		}
	}
	return false
}

// pushedTipLanding is the optional Landing surface a repair uses when the
// branch changed after its push: it records the landing for the exact tip that
// was pushed, which the pushed commit must hold.
type pushedTipLanding interface {
	RecordMergeLandingOfPushedTip(ctx context.Context, id, release, commit, tip string, at time.Time) (*autonomous.PRD, error)
}

// ReconciledPush is what reconciling one journalled push found and did.
type ReconciledPush struct {
	RunID, AutomatonID, Commit string
	Branches                   []string
	// Pushed says the remote holds the commit. Done says nothing is left to
	// repair, so the journal entry is gone.
	Pushed, Done bool
	Note         string
}

func journalDir(ctx context.Context, g *Merger) (string, error) {
	out, err := g.gitOK(ctx, g.Dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("find the git directory: %w", err)
	}
	dir := strings.TrimSpace(out)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(g.Dir, dir)
	}
	return filepath.Join(dir, pushJournalDirName), nil
}

// writeJournal stores p durably: it must survive the run that wrote it.
func writeJournal(dir string, p journalledPush) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "entry-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, p.RunID+".json"))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if d, derr := os.Open(dir); derr == nil { // #nosec G304 -- the journal folder
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// readJournal lists the entries, oldest first. An entry that cannot be read is
// an error: it may be a push nobody has accounted for.
func readJournal(dir string) ([]journalledPush, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []journalledPush
	for _, name := range names {
		data, err := os.ReadFile(name) // #nosec G304 -- a file of the journal folder
		if err != nil {
			return nil, fmt.Errorf("read the push journal %s: %w", name, err)
		}
		var p journalledPush
		if err := json.Unmarshal(data, &p); err != nil || p.RunID+".json" != filepath.Base(name) || p.Commit == "" || p.readsAsOption() {
			return nil, fmt.Errorf("the push journal %s cannot be understood; check what it records, then remove it", name)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].At.Before(out[b].At) })
	return out, nil
}

// remoteTag asks the remote what it has under the tag name: the object id, or
// "" when it has no such tag.
func (i *isolation) remoteTag(ctx context.Context, tag string) (string, error) {
	ref := "refs/tags/" + tag
	out, err := i.git.gitOK(ctx, i.git.Dir, "ls-remote", "--tags", i.remote, ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == ref {
			return f[0], nil
		}
	}
	return "", nil
}

// dropUnpushedTag removes the local tag while it names commit, but never one
// the remote has, and never when the remote cannot be asked. It reports
// whether the question is settled.
func (i *isolation) dropUnpushedTag(ctx context.Context, tag, commit string) bool {
	if i.remote != "" {
		have, err := i.remoteTag(ctx, tag)
		if err != nil {
			log.Printf("[merge-pipeline] keeping the tag %s until the remote can be asked about it: %v", tag, err)
			return false
		}
		if have != "" {
			return true
		}
	}
	i.deleteTag(ctx, tag, commit)
	return true
}

// matchPushedTag makes the local tag agree with the remote's for a push that
// arrived.
func (i *isolation) matchPushedTag(ctx context.Context, tag, commit string) error {
	have, err := i.remoteTag(ctx, tag)
	if err != nil {
		return fmt.Errorf("ask the remote about the tag %s: %w", tag, err)
	}
	if have == "" {
		i.deleteTag(ctx, tag, commit)
		return nil
	}
	ref := "refs/tags/" + tag
	local, err := i.git.revParse(ctx, i.git.Dir, ref)
	if err != nil {
		if _, err := i.git.gitOK(ctx, i.git.Dir, "fetch", "--quiet", "--no-tags", i.remote, ref+":"+ref); err != nil {
			return fmt.Errorf("fetch the tag %s: %w", tag, err)
		}
		return nil
	}
	if local != have {
		return fmt.Errorf("the local tag %s differs from the remote's; neither was changed", tag)
	}
	return nil
}

func (r *Runner) newReconciler(ctx context.Context, remote string) *isolation {
	return &isolation{project: r, git: &Merger{Dir: r.Dir}, remote: remote}
}

// reconcileOne compares one journalled push with the remote and repairs the
// project. It never pushes.
func (r *Runner) reconcileOne(ctx context.Context, dir string, p journalledPush) ReconciledPush {
	out := ReconciledPush{RunID: p.RunID, AutomatonID: p.AutomatonID, Commit: p.Commit}
	for _, b := range p.Branches {
		out.Branches = append(out.Branches, b.Name)
	}
	i := r.newReconciler(ctx, p.Remote)
	remove := func() {
		if err := os.Remove(filepath.Join(dir, p.RunID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("[merge-pipeline] remove the journal entry of %s: %v", p.RunID, err)
			return
		}
		out.Done = true
	}
	held, err := i.remoteMainHead(ctx)
	if err != nil {
		out.Note = "the remote could not be asked about commit " + shortSHA(p.Commit) + ", so nothing was changed: " + redact(err.Error())
		return out
	}
	if held != "" {
		if has, herr := i.git.git(ctx, i.git.Dir, "cat-file", "-e", p.Commit+"^{commit}"); herr == nil && has.Code == 0 {
			if out.Pushed, err = i.isAncestor(ctx, p.Commit, held); err != nil {
				out.Note = redact(err.Error())
				return out
			}
		}
	}
	if !out.Pushed {
		if p.Tag != "" && !i.dropUnpushedTag(ctx, p.Tag, p.Commit) {
			out.Note = "the remote does not show commit " + shortSHA(p.Commit)
			return out
		}
		out.Note = "the remote does not show commit " + shortSHA(p.Commit)
		// A push still under way can only arrive while the remote has not
		// moved from where it started.
		if held != p.Base || r.now().Sub(p.At) > pushJournalKeep {
			remove()
			out.Note += "; it can no longer arrive"
		}
		return out
	}

	var problems, newer []string
	// pushedTip is set when a branch no longer is what was pushed.
	pushedTip := ""
	byTip, canRecordTip := r.Landing.(pushedTipLanding)
	for _, b := range p.Branches {
		cur, err := i.git.revParse(ctx, i.git.Dir, "refs/heads/"+b.Name)
		switch {
		case err == nil && cur == b.New:
		case err == nil && cur == b.Old:
			if _, err := i.git.gitOK(ctx, i.git.Dir, "update-ref", "refs/heads/"+b.Name, b.New, b.Old); err != nil {
				problems = append(problems, "branch "+b.Name+" was not moved to what was pushed: "+err.Error())
			}
		default:
			// The branch took other work, or went away, after the push. It is
			// left as it is, as it would be had the run finished first: what is
			// recorded is the tip that was pushed, which the pushed commit must
			// hold.
			holds, herr := i.isAncestor(ctx, b.New, p.Commit)
			if herr != nil || !holds || (r.Landing != nil && p.AutomatonID != "" && !canRecordTip) {
				problems = append(problems, "branch "+b.Name+" has changed since the push")
				continue
			}
			pushedTip = b.New
			newer = append(newer, "branch "+b.Name+" no longer is what was pushed: it was left as it is, and any newer work on it was not part of this push")
		}
	}
	main := r.mainBranch()
	if has, err := i.isAncestor(ctx, p.Commit, "refs/heads/"+main); err != nil {
		problems = append(problems, err.Error())
	} else if !has {
		if err := i.advanceMain(ctx, p.Commit); err != nil {
			problems = append(problems, "local "+main+" was not moved: "+err.Error())
		}
	}
	if p.Tag != "" {
		if err := i.matchPushedTag(ctx, p.Tag, p.Commit); err != nil {
			problems = append(problems, err.Error())
		}
		_, _ = i.git.git(ctx, i.git.Dir, "update-ref", "-d", scratchTagMarks+p.Tag)
	}
	if r.Landing != nil && p.AutomatonID != "" {
		var err error
		if pushedTip != "" && canRecordTip {
			_, err = byTip.RecordMergeLandingOfPushedTip(ctx, p.AutomatonID, p.Release, p.Commit, pushedTip, p.At)
		} else {
			_, err = r.Landing.RecordMergeLanding(ctx, p.AutomatonID, p.Release, p.Commit, p.At)
		}
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		out.Note = "the remote holds commit " + shortSHA(p.Commit) + ", but the repair is not complete (it is tried again): " + redact(strings.Join(problems, "; "))
		return out
	}
	remove()
	out.Note = "the remote holds commit " + shortSHA(p.Commit) + ": branches, local " + main + " and the landing are recorded. No deploy or post-ship hook was run for it"
	if len(newer) > 0 {
		out.Note += ". " + strings.Join(newer, "; ")
	}
	return out
}

// reconcile repairs every journalled push. The caller holds the repository
// lock.
func (r *Runner) reconcile(ctx context.Context) ([]ReconciledPush, error) {
	g := &Merger{Dir: r.Dir}
	dir, err := journalDir(ctx, g)
	if err != nil {
		return nil, err
	}
	entries, err := readJournal(dir)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	out := make([]ReconciledPush, 0, len(entries))
	for _, p := range entries {
		res := r.reconcileOne(ctx, dir, p)
		log.Printf("[merge-pipeline] journalled push of run %s: %s", res.RunID, res.Note)
		out = append(out, res)
	}
	r.sweepLeftovers(ctx, g)
	return out, nil
}

// sweepLeftovers clears what killed runs left behind. The caller holds the
// repository lock.
func (r *Runner) sweepLeftovers(ctx context.Context, g *Merger) {
	names, err := g.gitOK(ctx, r.Dir, "remote")
	if err != nil {
		return
	}
	i := r.newReconciler(ctx, pickRemote(names))
	head := ""
	if i.remote != "" {
		head, _ = i.remoteMainHead(ctx)
	}
	i.sweep(ctx, head)
}

// ReconcilePushes repairs the pushes earlier runs journalled, under the
// repository lock, and clears what killed runs left behind. The daemon calls
// it at start and before it lists what is waiting to merge. With an empty
// journal it does nothing and takes no lock.
func (r *Runner) ReconcilePushes(ctx context.Context) ([]ReconciledPush, error) {
	g := &Merger{Dir: r.Dir}
	dir, err := journalDir(ctx, g)
	if err != nil {
		return nil, err
	}
	if entries, err := readJournal(dir); err != nil || len(entries) == 0 {
		return nil, err
	}
	id := newRunID()
	if r.Locks != nil {
		status, err := r.Locks.Acquire(r.Dir, autonomous.MergeLockHolder{RunID: id})
		if err != nil {
			return nil, errors.New(lockRefusal(status, err))
		}
		defer func() {
			if err := r.Locks.Release(r.Dir, id); err != nil {
				log.Printf("[merge-pipeline] release the merge lock on %s: %v", r.Dir, err)
			}
		}()
	}
	return r.reconcile(ctx)
}

// pickRemote is origin when the repository has it, else its last remote.
//
// A name that starts with "-" is never picked: git would read it as an option.
func pickRemote(listed string) string {
	remote := ""
	for _, n := range strings.Fields(listed) {
		if !strings.HasPrefix(n, "-") && (remote == "" || n == "origin") {
			remote = n
		}
	}
	return remote
}

// reconcileBeforeRun repairs earlier pushes before a run selects anything, and
// refuses the run when its own work is among what the remote already holds.
func (s *runState) reconcileBeforeRun(ctx context.Context) error {
	results, err := s.r.reconcile(ctx)
	if err != nil {
		return err
	}
	var refuse error
	for _, res := range results {
		res := res
		step := Step{Name: reconcileStepName, Kind: KindCustom, OnFailure: OnFailureContinue}
		_, _, _ = s.recordStep(ctx, "reconcile", step, DefaultStepTimeout, func(context.Context) (string, string, error) {
			note := "earlier run " + res.RunID + ": " + res.Note
			if res.Pushed && !res.Done {
				return autonomous.MergeStepFailed, note, errors.New(note)
			}
			return autonomous.MergeStepOK, note, nil
		})
		if !res.Pushed || refuse != nil {
			continue
		}
		same := res.AutomatonID != "" && res.AutomatonID == s.req.AutomatonID
		for _, b := range res.Branches {
			for _, asked := range s.req.Branches {
				same = same || b == asked
			}
		}
		if same {
			refuse = fmt.Errorf("this work was already pushed by run %s as commit %s, so it is not released again: %s",
				res.RunID, shortSHA(res.Commit), res.Note)
		}
	}
	return refuse
}

// journalPush records, before anything leaves, what this push will send and
// what must follow once the remote holds it.
func (s *runState) journalPush(ctx context.Context, head, tag string) error {
	i := s.iso
	p := journalledPush{
		RunID: s.run.ID, AutomatonID: s.req.AutomatonID, Remote: i.remote, Main: i.project.mainBranch(),
		Base: s.baseHead, Commit: head, Tag: tag, At: s.r.now(),
	}
	if v, err := s.releaseVersionOf(); err == nil && !s.noRelease {
		p.Release = v
	}
	for _, b := range s.req.Branches {
		old, err := i.git.revParse(ctx, i.git.Dir, "refs/heads/"+b)
		if err != nil {
			return err
		}
		next := old
		if mv, ok := i.moves[b]; ok {
			next = mv.new
		}
		p.Branches = append(p.Branches, journalledBranch{Name: b, Old: old, New: next})
	}
	dir, err := journalDir(ctx, i.git)
	if err != nil {
		return err
	}
	if err := writeJournal(dir, p); err != nil {
		return err
	}
	i.journal = &p
	return nil
}

// settleJournal removes this run's entry once nothing is left to repair.
func (s *runState) settleJournal(ctx context.Context) {
	i := s.iso
	if i == nil || i.journal == nil {
		return
	}
	dir, err := journalDir(ctx, i.git)
	if err == nil {
		err = os.Remove(filepath.Join(dir, i.journal.RunID+".json"))
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[merge-pipeline] remove the journal entry of %s: %v", i.journal.RunID, err)
		return
	}
	i.journal = nil
}

// reconcileFailedPush asks the remote about a push that reported an error: it
// may have arrived all the same. It reports whether the remote holds it.
func (s *runState) reconcileFailedPush(ctx context.Context) bool {
	i := s.iso
	if i == nil || i.journal == nil {
		return false
	}
	entry := *i.journal
	pushed := false
	step := Step{Name: reconcileStepName, Kind: KindShip, OnFailure: OnFailureContinue}
	_, _, _ = s.recordStep(ctx, "reconcile", step, DefaultStepTimeout, func(c context.Context) (string, string, error) {
		dir, err := journalDir(c, i.git)
		if err != nil {
			return autonomous.MergeStepFailed, err.Error(), err
		}
		res := i.project.reconcileOne(c, dir, entry)
		pushed = res.Pushed
		if res.Done {
			i.journal = nil
		}
		if res.Pushed && !res.Done {
			return autonomous.MergeStepFailed, res.Note, errors.New(res.Note)
		}
		return autonomous.MergeStepOK, res.Note, nil
	})
	if pushed {
		i.confirmed = true
		s.pushedCommit = entry.Commit
	}
	return pushed
}
