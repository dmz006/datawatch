package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// lanGit is a fake MergeGitRunner for the landing ancestor check: per-branch
// answers, every call recorded.
type lanGit struct {
	mu       sync.Mutex
	calls    [][]string
	ancestor map[string]bool // branch -> merge-base --is-ancestor exit success
	ahead    map[string]bool // branch -> holds a commit the base does not have
	ff       map[string]bool // branch -> merge base equals the tip (fast-forward landing)
	tips     map[string]string
	merges   map[string]string // branch -> rev-list --ancestry-path --merges --reverse output
}

func (f *lanGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()
	b := args[len(args)-1]
	switch {
	case args[0] == "symbolic-ref":
		return "refs/remotes/origin/main\n", nil
	case args[0] == "rev-parse" && strings.HasPrefix(b, "refs/heads/"):
		tip, ok := f.tips[strings.TrimPrefix(b, "refs/heads/")]
		if !ok {
			return "", errors.New("unknown ref")
		}
		return tip + "\n", nil
	case args[0] == "merge-base" && len(args) > 1 && args[1] == "--is-ancestor":
		if f.ancestor[args[2]] {
			return "", nil
		}
		return "", errors.New("not an ancestor")
	case args[0] == "merge-base":
		if f.ff[args[1]] {
			tip, _ := f.tips[args[1]]
			return tip + "\n", nil // fast-forward: the merge base is the branch tip
		}
		return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", nil
	case args[0] == "for-each-ref":
		return "refs/heads/" + strings.TrimPrefix(b, "refs/heads/") + "\n", nil
	case args[0] == "rev-list" && len(args) == 3 && args[1] == "--count":
		br := b[strings.Index(b, "..")+2:]
		if f.ahead[br] {
			return "1\n", nil
		}
		return "0\n", nil
	case args[0] == "cherry":
		if f.ahead[b] {
			return "+ 1111111111111111111111111111111111111111\n", nil
		}
		return "", nil
	case args[0] == "log" && b == "main":
		return "main subject\n", nil
	case args[0] == "log":
		if f.ahead[b[strings.Index(b, "..")+2:]] {
			return "branch subject\n", nil
		}
		return "", nil
	case args[0] == "rev-list":
		return f.merges[b], nil
	}
	return "", errors.New("unexpected git call: " + strings.Join(args, " "))
}

func (f *lanGit) hadCall(t *testing.T, want ...string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if len(c) == len(want) {
			ok := true
			for i := range want {
				if c[i] != want[i] {
					ok = false
					break
				}
			}
			if ok {
				return
			}
		}
	}
	t.Fatalf("expected git call %v not found in %v", want, f.calls)
}

// lanRig builds a manager and the supervisor loop steps wired to git, with
// base repository set so the read-only checks have somewhere to look.
func lanRig(t *testing.T, git *lanGit, n int) (*coordHarness, *SupervisorLoopSteps) {
	t.Helper()
	h := newCoordHarness(t, n)
	eng := &supervisorloop.Engine{Settings: supervisorloop.DefaultSettings(), Clock: slClock{}, Store: &slMemStore{}}
	steps := NewSupervisorLoopSteps(h.c, eng, SupervisorLoopHooks{})
	steps.mergeGit = git
	cfg := h.m.Config()
	cfg.WorktreeBaseRepo = t.TempDir()
	h.m.SetConfig(cfg)
	h.m.mu.Lock()
	h.m.archivedUnmergedGit = git
	h.m.mu.Unlock()
	return h, steps
}

// lanPending sets the record to the given terminal status with a branch and a
// pending landing record taken from the live clock.
func lanPending(t *testing.T, h *coordHarness, i int, status PRDStatus, branch string) string {
	t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Status = status
	p.Git.Branch = branch
	p.Git.BaseBranch = "main"
	p.Landing = &Landing{State: LandingPending, Actor: "test", At: time.Now(), Reason: "branch created"}
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func lanLanded(t *testing.T, h *coordHarness, id string, wantCommit string) *Landing {
	t.Helper()
	p, ok := h.m.store.GetPRD(id)
	if !ok {
		t.Fatal("prd missing")
	}
	if p.Landing == nil || p.Landing.State != LandingLanded {
		t.Fatalf("landing = %+v, want landed with commit %s", p.Landing, wantCommit)
	}
	if p.Landing.Commit != wantCommit {
		t.Fatalf("landing commit = %q, want %q", p.Landing.Commit, wantCommit)
	}
	if p.Landing.Actor != landingAncestorActor {
		t.Fatalf("landing actor = %q, want %q", p.Landing.Actor, landingAncestorActor)
	}
	return p.Landing
}

// TestLandingAncestorMergeCommit: a branch merged by a merge commit is
// reported landed, and the record carries the first merge commit on the
// default branch that contains the tip.
func TestLandingAncestorMergeCommit(t *testing.T) {
	const (
		tip      = "1111111111111111111111111111111111111111"
		first    = "2222222222222222222222222222222222222222"
		second   = "3333333333333333333333333333333333333333"
		otherRef = "4444444444444444444444444444444444444444"
	)
	git := &lanGit{
		ancestor: map[string]bool{"work/merged": true},
		ff:       map[string]bool{},
		tips:     map[string]string{"work/merged": tip},
		merges:   map[string]string{tip + "..main": first + "\n" + second + "\n" + otherRef + "\n"},
	}
	h, _ := lanRig(t, git, 1)
	id := lanPending(t, h, 0, PRDCompleted, "work/merged")

	if !h.m.CheckLandingAncestor(context.Background(), id) {
		t.Fatal("CheckLandingAncestor reported not landed")
	}
	git.hadCall(t, "merge-base", "--is-ancestor", "work/merged", "main")
	git.hadCall(t, "rev-list", "--ancestry-path", "--merges", "--reverse", tip+"..main")
	lanLanded(t, h, id, first)
}

// TestLandingAncestorFastForward: a fast-forwarded branch is reported landed
// with the branch tip as the merge commit, without the merge-commit search.
func TestLandingAncestorFastForward(t *testing.T) {
	const tip = "5555555555555555555555555555555555555555"
	git := &lanGit{
		ancestor: map[string]bool{"work/fast": true},
		ff:       map[string]bool{"work/fast": true},
		tips:     map[string]string{"work/fast": tip},
	}
	h, _ := lanRig(t, git, 1)
	id := lanPending(t, h, 0, PRDRejected, "work/fast")

	if !h.m.CheckLandingAncestor(context.Background(), id) {
		t.Fatal("CheckLandingAncestor reported not landed")
	}
	lanLanded(t, h, id, tip)
	for _, c := range git.calls {
		if c[0] == "rev-list" {
			t.Fatalf("fast-forward landing must not run the merge-commit search, saw %v", c)
		}
	}
}

// TestLandingAncestorArchiveRefusesAheadBranch: a branch with commits ahead
// is not landed - the record stays pending and Archive refuses with the
// unchanged error text.
func TestLandingAncestorArchiveRefusesAheadBranch(t *testing.T) {
	git := &lanGit{
		ancestor: map[string]bool{"work/ahead": false},
		tips:     map[string]string{"work/ahead": "6666666666666666666666666666666666666666"},
	}
	h, _ := lanRig(t, git, 1)
	id := lanPending(t, h, 0, PRDCompleted, "work/ahead")

	if h.m.CheckLandingAncestor(context.Background(), id) {
		t.Fatal("CheckLandingAncestor reported landed for a branch with commits ahead")
	}
	p, _ := h.m.store.GetPRD(id)
	if p.Landing.State != LandingPending {
		t.Fatalf("landing state = %q, want pending", p.Landing.State)
	}
	if p.Status != PRDCompleted {
		t.Fatalf("status = %q, want unchanged completed", p.Status)
	}

	_, err := h.m.Archive(id, "operator")
	if err == nil {
		t.Fatal("Archive succeeded, want refusal")
	}
	want := `automaton "` + id + `" cannot be archived: its branch work/ahead has not landed on the default branch; merge it, or record keep or drop for its landing`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	p, _ = h.m.store.GetPRD(id)
	if p.Status != PRDCompleted || p.Landing.State != LandingPending {
		t.Fatalf("record changed by the refused archive: status %q, landing %q", p.Status, p.Landing.State)
	}
}

// TestLandingAncestorCloseFinishedRecords: the loop's close_finished_records
// step archives the landed records (merge commit and fast-forward) and leaves
// the branch with commits ahead, and the archived records read landed.
func TestLandingAncestorCloseFinishedRecords(t *testing.T) {
	const (
		tipFast  = "7777777777777777777777777777777777777777"
		tipMerge = "8888888888888888888888888888888888888888"
		mergeCm  = "9999999999999999999999999999999999999999"
	)
	git := &lanGit{
		ancestor: map[string]bool{"work/fast": true, "work/merged": true, "work/ahead": false},
		ahead:    map[string]bool{"work/ahead": true},
		ff:       map[string]bool{"work/fast": true},
		tips: map[string]string{
			"work/fast":   tipFast,
			"work/merged": tipMerge,
			"work/ahead":  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		merges: map[string]string{tipMerge + "..main": mergeCm + "\n"},
	}
	h, steps := lanRig(t, git, 3)
	fast := lanPending(t, h, 0, PRDCompleted, "work/fast")
	merged := lanPending(t, h, 1, PRDRejected, "work/merged")
	ahead := lanPending(t, h, 2, PRDCancelled, "work/ahead")

	_, found, err := steps.RunStep(context.Background(), stepCloseFinishedRecords)
	if !found {
		t.Fatal("step close_finished_records not found")
	}
	if err != nil {
		t.Fatalf("close_finished_records: %v", err)
	}
	for id, want := range map[string]PRDStatus{fast: PRDArchived, merged: PRDArchived, ahead: PRDCancelled} {
		p, ok := h.m.store.GetPRD(id)
		if !ok {
			t.Fatalf("prd %s missing", id)
		}
		if p.Status != want {
			t.Fatalf("prd %s status = %q, want %q", id, p.Status, want)
		}
	}
	lanLanded(t, h, fast, tipFast)
	lanLanded(t, h, merged, mergeCm)
	p, _ := h.m.store.GetPRD(ahead)
	if p.Landing.State != LandingPending {
		t.Fatalf("ahead branch landing = %q, want pending", p.Landing.State)
	}
}
