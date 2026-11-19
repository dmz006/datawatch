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

// aumGit is a fake MergeGitRunner: per-branch answers, every call recorded.
type aumGit struct {
	mu     sync.Mutex
	calls  [][]string
	ahead  map[string]string // branch -> rev-list --count output
	gone   map[string]bool   // branch -> for-each-ref empty
	failOn map[string]bool   // branch -> rev-list fails
}

func (f *aumGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()
	last := args[len(args)-1]
	switch args[0] {
	case "for-each-ref":
		b := strings.TrimPrefix(last, "refs/heads/")
		if f.gone[b] {
			return "", nil
		}
		return "refs/heads/" + b + "\n", nil
	case "rev-list":
		b := last[strings.Index(last, "..")+2:]
		if f.failOn[b] {
			return "", errors.New("boom")
		}
		return f.ahead[b], nil
	case "symbolic-ref":
		return "", errors.New("none")
	}
	return "", nil
}

func (f *aumGit) assertReadOnly(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		switch c[0] {
		case "for-each-ref", "rev-list", "remote", "symbolic-ref", "cherry", "log", "rev-parse", "merge-base":
		default:
			t.Errorf("non read-only git call: %v", c)
		}
		for _, a := range c {
			if a == "-d" || a == "-D" || a == "--delete" || a == "merge" || a == "branch" || a == "push" {
				t.Errorf("destructive git arg %q in %v", a, c)
			}
		}
	}
}

// aumRig builds a manager with an injected clock and git runner (the same
// fake sits on the supervisor loop steps).
func aumRig(t *testing.T, git *aumGit, n int) (*coordHarness, *SupervisorLoopSteps, time.Time) {
	t.Helper()
	h := newCoordHarness(t, n)
	eng := &supervisorloop.Engine{Settings: supervisorloop.DefaultSettings(), Clock: slClock{}, Store: &slMemStore{}}
	steps := NewSupervisorLoopSteps(h.c, eng, SupervisorLoopHooks{})
	at := slBase
	steps.now = func() time.Time { return at }
	steps.mergeGit = git
	cfg := h.m.Config()
	cfg.WorktreeBaseRepo = "/repo"
	h.m.SetConfig(cfg)
	h.m.mu.Lock()
	h.m.archivedUnmergedGit = steps.mergeGit
	h.m.now = steps.now
	h.m.mu.Unlock()
	return h, steps, at
}

func aumArchive(t *testing.T, h *coordHarness, i int, branch, title string, at time.Time) string {
	t.Helper()
	p, ok := h.m.Store().GetPRD(h.prds[i].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Status = PRDArchived
	p.Title = title
	p.Spec = "spec for " + title
	p.Git.Branch = branch
	p.Git.BaseBranch = "main"
	p.Archive = &ArchiveRef{Archived: true, ArchivedAt: at}
	p.Story = []Story{{Tasks: []Task{{Status: TaskCompleted}}}}
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestArchivedUnmergedListsUnmergedOnly(t *testing.T) {
	git := &aumGit{
		ahead:  map[string]string{"work/a": "3\n", "work/b": "0\n", "work/d": "1\n"},
		gone:   map[string]bool{"work/c": true},
		failOn: map[string]bool{"work/e": true},
	}
	h, _, at := aumRig(t, git, 5)
	a := aumArchive(t, h, 0, "work/a", "Alpha", at.Add(-time.Hour))
	aumArchive(t, h, 1, "work/b", "Merged", at)
	aumArchive(t, h, 2, "work/c", "Dropped", at)
	aumArchive(t, h, 3, "", "NoBranch", at)
	e := aumArchive(t, h, 4, "work/e", "Unknown", at)

	got := h.m.ArchivedUnmerged(context.Background())
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %+v", got)
	}
	byID := map[string]ArchivedUnmergedEntry{got[0].ID: got[0], got[1].ID: got[1]}
	ea := byID[a]
	if ea.Title != "Alpha" || ea.Branch != "work/a" || ea.BaseBranch != "main" || ea.Ahead != 3 ||
		ea.State != ArchivedUnmergedAhead || ea.FormerStatus != string(PRDCompleted) || !ea.ArchivedAt.Equal(at.Add(-time.Hour)) {
		t.Errorf("entry a = %+v", ea)
	}
	if eu := byID[e]; eu.State != ArchivedUnmergedUnknown || eu.Detail == "" {
		t.Errorf("unknown entry = %+v", eu)
	}
	git.assertReadOnly(t)
}

func aumCarryRig(t *testing.T, git *aumGit) (*coordHarness, string) {
	t.Helper()
	h, _, at := aumRig(t, git, 1)
	return h, aumArchive(t, h, 0, "work/a", "Alpha", at)
}

func TestArchivedUnmergedCarryForwardMakesDraft(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "2\n"}}
	h, id := aumCarryRig(t, git)
	d, err := h.m.CarryForwardArchived(id, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == id || d.Status != PRDDraft || d.ApprovedBy != "" || d.ApprovedAt != nil || len(d.Story) != 0 {
		t.Errorf("draft = %+v", d)
	}
	if d.Title != "Alpha" || d.Spec != "spec for Alpha" || d.Git.Branch != "work/a" || d.Git.BaseBranch != "main" || d.CarriedFromID != id {
		t.Errorf("carried fields = %+v", d)
	}
	src, _ := h.m.Store().GetPRD(id)
	if src.Status != PRDArchived || src.Git.Branch != "work/a" {
		t.Errorf("source changed: %+v", src)
	}
	git.assertReadOnly(t)
}

func TestArchivedUnmergedCarryForwardRefusals(t *testing.T) {
	git := &aumGit{
		ahead: map[string]string{"work/a": "2\n", "work/z": "0\n"},
		gone:  map[string]bool{"work/g": true},
	}
	h, _, _ := aumRig(t, git, 4)
	setBranch := func(i int, status PRDStatus, branch string) string {
		p, _ := h.m.Store().GetPRD(h.prds[i].ID)
		p.Status, p.Git.Branch = status, branch
		_ = h.m.Store().SavePRD(p)
		return p.ID
	}
	live := setBranch(0, PRDCompleted, "work/a")
	none := setBranch(1, PRDArchived, "")
	gone := setBranch(2, PRDArchived, "work/g")
	zero := setBranch(3, PRDArchived, "work/z")

	for name, c := range map[string]string{"not archived": live, "no branch": none, "gone": gone, "zero ahead": zero, "missing": "nope"} {
		if _, err := h.m.CarryForwardArchived(c, "operator"); err == nil {
			t.Errorf("%s: expected refusal", name)
		}
	}
	if _, err := h.m.CarryForwardArchived(zero, ""); err == nil {
		t.Error("empty actor accepted")
	}
	git.assertReadOnly(t)
}

func TestArchivedUnmergedCarryForwardSecondCarryNeedsForce(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "2\n"}}
	h, id := aumCarryRig(t, git)
	first, err := h.m.CarryForwardArchived(id, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.CarryForwardArchived(id, "operator"); err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("second carry err = %v, want refusal naming %s", err, first.ID)
	}
	second, err := h.m.CarryForwardArchivedForced(id, "operator", true)
	if err != nil || second.ID == first.ID {
		t.Fatalf("forced carry = %v, %v", second, err)
	}
	if l := h.m.ArchivedUnmerged(context.Background()); len(l) != 1 || l[0].CarriedTo == "" {
		t.Errorf("listing = %+v", l)
	}
	git.assertReadOnly(t)
}

func TestArchivedUnmergedCloseGuardUnchanged(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "4\n"}}
	h, steps, _ := aumRig(t, git, 1)
	p, _ := h.m.Store().GetPRD(h.prds[0].ID)
	p.Git.Branch = "work/a"
	if keep, n := steps.archiveKeepsBranch(p); !keep || n != 4 {
		t.Errorf("keep=%v ahead=%d, want true 4", keep, n)
	}
}
