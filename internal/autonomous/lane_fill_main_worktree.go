package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file holds the last two lane fill eligibility rules.
//
// main_carries_work: a queued Automaton whose work the base branch already
// carries is not started again. The question is asked of an injected probe
// (the real one reads git), only for queued Automata, a few per poll, and the
// answer is remembered for as long as the base branch head stays the same.
//
// worktree_invalid: an Automaton that has a recorded worktree is only taken
// when that worktree is a real, registered one. An Automaton with no worktree
// yet is fine: it gets one when it runs.
//
// Like the proposal rule, both gather their facts in the snapshot, outside any
// lock, and store the outcome on the candidate; EvaluateLaneFillEligibility
// only maps the outcome to a refusal. Anything that cannot be read is unknown
// and never refuses.

// The states of the main-carries check on a candidate.
const (
	// LaneFillMainCarries: the base branch already carries the work.
	LaneFillMainCarries = "carries"
	// LaneFillMainClear: the probe answered and the base branch does not
	// carry the work.
	LaneFillMainClear = "clear"
	// LaneFillMainUnknown: no answer (no probe, an error, or past the
	// per-poll bound). It never refuses.
	LaneFillMainUnknown = "unknown"
)

// The states of the worktree check on a candidate.
const (
	// LaneFillWorktreeInvalid: a recorded worktree that is not usable.
	LaneFillWorktreeInvalid = "invalid"
	// LaneFillWorktreeValid: a recorded worktree that is usable.
	LaneFillWorktreeValid = "valid"
	// LaneFillWorktreeNone: no worktree is recorded yet; the Automaton gets
	// one when it runs.
	LaneFillWorktreeNone = "none"
	// LaneFillWorktreeUnknown: the check could not be made. It never refuses.
	LaneFillWorktreeUnknown = "unknown"
)

const (
	// laneFillMainProbeMaxPerPoll bounds how many Automata are probed in one
	// poll, newest first, so a long queue cannot stall the poll. Answers that
	// are cached for the current base branch head do not count.
	laneFillMainProbeMaxPerPoll = 8
	// laneFillMainProbeTimeout bounds one probe, all its git calls together.
	laneFillMainProbeTimeout = 20 * time.Second
)

// LaneFillMainProbe answers whether the base branch already carries the work
// of an Automaton. branch is the Automaton's recorded branch, empty when it
// has none. evidence names the commit or the comparison result behind the
// answer. An error means unknown and never refuses.
type LaneFillMainProbe interface {
	Carries(ctx context.Context, id, branch string) (carries bool, evidence string, err error)
}

// LaneFillMainHeadReader is the optional second half of a probe: it names the
// current head commit of the base branch. When the probe has it, an answer is
// remembered for as long as that head stays the same; without it nothing is
// remembered and only the per-poll bound limits the work.
type LaneFillMainHeadReader interface {
	MainHead(ctx context.Context) (string, error)
}

// laneFillMainCached is one remembered probe answer.
type laneFillMainCached struct {
	head     string
	carries  bool
	evidence string
}

// SetLaneFillMainProbe sets how the main-carries rule asks git. Until one is
// set the rule gives unknown and never refuses.
func (c *Coordinator) SetLaneFillMainProbe(p LaneFillMainProbe) {
	if c == nil {
		return
	}
	c.lf.mu.Lock()
	c.lf.mainProbe = p
	c.lf.mainCache = map[string]laneFillMainCached{}
	c.lf.mu.Unlock()
}

// SetLaneFillWorktreeFS sets the view the worktree rule lists the registered
// worktrees through (the same view the supervisor watch uses). Until one is
// set the rule gives unknown and never refuses.
func (c *Coordinator) SetLaneFillWorktreeFS(fsys SupervisorWatchWorktreeFS) {
	if c == nil {
		return
	}
	c.lf.mu.Lock()
	c.lf.worktreeFS = fsys
	c.lf.mu.Unlock()
}

// laneFillMainRefusal maps a candidate's main-carries state to the refusal.
func laneFillMainRefusal(c LaneFillCandidate) (code, text string) {
	if c.MainState != LaneFillMainCarries {
		return "", ""
	}
	return LaneFillReasonMainCarriesWork,
		fmt.Sprintf("the base branch already carries this work: %s", c.MainEvidence)
}

// laneFillWorktreeRefusal maps a candidate's worktree state to the refusal.
func laneFillWorktreeRefusal(c LaneFillCandidate) (code, text string) {
	if c.WorktreeState != LaneFillWorktreeInvalid {
		return "", ""
	}
	return LaneFillReasonWorktreeInvalid,
		fmt.Sprintf("its recorded worktree is not usable: %s", c.WorktreeProblem)
}

// laneFillProbeMain fills MainState and MainEvidence on the queued candidates
// that every earlier rule let through. branches maps an Automaton id to its
// recorded branch. At most laneFillMainProbeMaxPerPoll uncached Automata are
// probed per call, newest first; the rest stay unknown until a later poll.
// No lock is held while the probe runs.
func (c *Coordinator) laneFillProbeMain(ctx context.Context, cfg LaneFillConfig, cands []LaneFillCandidate, branches map[string]string, now time.Time) {
	if !cfg.EffectiveCheckMainCarries() {
		return
	}
	c.lf.mu.Lock()
	probe := c.lf.mainProbe
	c.lf.mu.Unlock()
	if probe == nil {
		return
	}

	var head string
	if hr, ok := probe.(LaneFillMainHeadReader); ok {
		hctx, cancel := context.WithTimeout(ctx, laneFillMainProbeTimeout)
		h, err := hr.MainHead(hctx)
		cancel()
		if err == nil {
			head = strings.TrimSpace(h)
		}
	}

	// The candidates to ask about: queued and not already refused by an
	// earlier rule, newest first.
	var idx []int
	for i := range cands {
		if !laneFillQueuedStatus(cands[i].Status) {
			continue
		}
		if code, _ := EvaluateLaneFillEligibility(cfg, cands[i], now); code != "" {
			continue
		}
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := cands[idx[a]], cands[idx[b]]
		if !x.CreatedAt.Equal(y.CreatedAt) {
			return x.CreatedAt.After(y.CreatedAt)
		}
		return x.ID > y.ID
	})

	probed := 0
	seen := make(map[string]bool, len(idx))
	for _, i := range idx {
		id := cands[i].ID
		seen[id] = true
		if head != "" {
			c.lf.mu.Lock()
			e, ok := c.lf.mainCache[id]
			c.lf.mu.Unlock()
			if ok && e.head == head {
				cands[i].MainState, cands[i].MainEvidence = laneFillMainStateOf(e.carries), e.evidence
				continue
			}
		}
		if probed >= laneFillMainProbeMaxPerPoll {
			cands[i].MainState = LaneFillMainUnknown
			continue
		}
		probed++
		pctx, cancel := context.WithTimeout(ctx, laneFillMainProbeTimeout)
		carries, evidence, err := probe.Carries(pctx, id, branches[id])
		cancel()
		if err != nil {
			log.Printf("[autonomous] lane fill: main-carries check for Automaton %s could not be made, not refusing: %v", id, err)
			cands[i].MainState = LaneFillMainUnknown
			continue
		}
		cands[i].MainState, cands[i].MainEvidence = laneFillMainStateOf(carries), evidence
		if head != "" {
			c.lf.mu.Lock()
			if c.lf.mainCache == nil {
				c.lf.mainCache = map[string]laneFillMainCached{}
			}
			c.lf.mainCache[id] = laneFillMainCached{head: head, carries: carries, evidence: evidence}
			c.lf.mu.Unlock()
		}
	}

	// Forget answers for Automata that are no longer queued work.
	c.lf.mu.Lock()
	for id := range c.lf.mainCache {
		if !seen[id] {
			delete(c.lf.mainCache, id)
		}
	}
	c.lf.mu.Unlock()
}

func laneFillMainStateOf(carries bool) string {
	if carries {
		return LaneFillMainCarries
	}
	return LaneFillMainClear
}

// CheckLaneFillWorktree decides whether a recorded worktree is usable: the
// directory exists, holds a .git entry and is in the registered worktree list
// of its repository. list reads that list and is called only when the first
// two checks pass. It returns the state and, for invalid, the problem. A
// directory that cannot be read, or a list that cannot be read, gives unknown.
func CheckLaneFillWorktree(dir string, list func() ([]string, error)) (state, problem string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return LaneFillWorktreeNone, ""
	}
	st, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return LaneFillWorktreeInvalid, fmt.Sprintf("the directory %s does not exist", dir)
		}
		return LaneFillWorktreeUnknown, ""
	}
	if !st.IsDir() {
		return LaneFillWorktreeInvalid, fmt.Sprintf("%s is not a directory", dir)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		if os.IsNotExist(err) {
			return LaneFillWorktreeInvalid, fmt.Sprintf("the directory %s has no .git entry", dir)
		}
		return LaneFillWorktreeUnknown, ""
	}
	if list == nil {
		return LaneFillWorktreeUnknown, ""
	}
	registered, err := list()
	if err != nil {
		return LaneFillWorktreeUnknown, ""
	}
	want := evalOrClean(dir)
	for _, r := range registered {
		if strings.TrimSpace(r) != "" && evalOrClean(r) == want {
			return LaneFillWorktreeValid, ""
		}
	}
	return LaneFillWorktreeInvalid, fmt.Sprintf("the directory %s is not in the registered worktree list of the repository", dir)
}

// laneFillCheckWorktrees fills WorktreeState and WorktreeProblem on the
// queued candidates. dirs maps an Automaton id to its recorded worktree
// directory; an Automaton missing from the map has none yet and is not
// refused. The registered list is read at most once per call, through the
// supervisor watch view, and only when some directory needs it.
func (c *Coordinator) laneFillCheckWorktrees(cfg LaneFillConfig, cands []LaneFillCandidate, dirs map[string]string, baseRepo string) {
	if !cfg.EffectiveRequireWorktree() {
		return
	}
	c.lf.mu.Lock()
	fsys := c.lf.worktreeFS
	c.lf.mu.Unlock()

	var registered []string
	var listErr error
	listRead := false
	list := func() ([]string, error) {
		if !listRead {
			listRead = true
			switch {
			case fsys == nil:
				listErr = fmt.Errorf("no worktree view is configured")
			case strings.TrimSpace(baseRepo) == "":
				listErr = fmt.Errorf("no base repository is configured")
			default:
				registered, listErr = fsys.RegisteredWorktrees(baseRepo)
			}
		}
		return registered, listErr
	}

	for i := range cands {
		if !laneFillQueuedStatus(cands[i].Status) {
			continue
		}
		cands[i].WorktreeState, cands[i].WorktreeProblem = CheckLaneFillWorktree(dirs[cands[i].ID], list)
	}
}

// LaneFillBaseBranch resolves the base branch of repo the way the merge proof
// does: the remote HEAD, else the branch checked out in the repository. It is
// exported for the real main-carries probe.
func LaneFillBaseBranch(ctx context.Context, git MergeGitRunner, repo string) (string, error) {
	return defaultBranchForRepo(ctx, git, repo)
}
