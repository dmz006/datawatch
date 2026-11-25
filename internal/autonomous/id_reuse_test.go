// An id must never be issued twice. newID's high half is bits 16..31 of the
// nanosecond clock, so the same id comes round every 2^32 ns; these tests force
// that repeat with a fake clock on every create path.

package autonomous

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// idWrap is the clock distance after which newID's high half repeats.
const idWrap = time.Duration(1 << 32)

type fakeIDClock struct{ at time.Time }

// freezeIDClock puts a fake clock under newID with a fresh generator.
func freezeIDClock(t *testing.T) *fakeIDClock {
	t.Helper()
	idMu.Lock()
	bucket, counter, clock, issued := idLastBucket, idCounter, idClock, idIssued
	c := &fakeIDClock{at: time.Date(2026, 11, 20, 8, 26, 15, 0, time.UTC)}
	idClock = func() time.Time { return c.at }
	idLastBucket, idCounter, idIssued = 0, 0, map[string]bool{}
	idMu.Unlock()
	t.Cleanup(func() {
		idMu.Lock()
		idLastBucket, idCounter, idClock, idIssued = bucket, counter, clock, issued
		idMu.Unlock()
	})
	return c
}

// wrap moves the clock one full cycle on: the same process draws the same id.
func (c *fakeIDClock) wrap() { c.at = c.at.Add(idWrap) }

// restart forgets everything the generator remembers, as a daemon restart does.
func (c *fakeIDClock) restart() {
	idMu.Lock()
	defer idMu.Unlock()
	idLastBucket, idCounter, idIssued = 0, 0, map[string]bool{}
}

// next is the id a freshly started generator draws at the clock's time.
func (c *fakeIDClock) next() string {
	return fmt.Sprintf("%04x0001", uint32(c.at.UnixNano()>>16)&0xffff)
}

func idTestManager(t *testing.T, dir string, cfg Config) *Manager {
	t.Helper()
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func idTestPRD(t *testing.T, s *Store, spec string) *PRD {
	t.Helper()
	p, err := s.CreatePRD(spec, "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD(%q): %v", spec, err)
	}
	return p
}

// requireIntactPRD reads the record back from disk with a fresh store.
func requireIntactPRD(t *testing.T, dir, id, spec string) {
	t.Helper()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := s.GetPRD(id)
	if !ok {
		t.Fatalf("record %s is gone", id)
	}
	if got.Spec != spec {
		t.Fatalf("record %s was overwritten: spec = %q, want %q", id, got.Spec, spec)
	}
}

func requireDistinct(t *testing.T, what, first, second string) {
	t.Helper()
	if first == second {
		t.Fatalf("%s: the second create was given the first one's id %s", what, first)
	}
}

func TestIDReuse_CreatePRDAfterClockCycleKeepsFirstRecord(t *testing.T) {
	c := freezeIDClock(t)
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := idTestPRD(t, s, "the first record")
	c.wrap()
	second := idTestPRD(t, s, "the second record")
	requireDistinct(t, "automaton", first.ID, second.ID)
	requireIntactPRD(t, dir, first.ID, "the first record")
	requireIntactPRD(t, dir, second.ID, "the second record")
}

func TestIDReuse_CreatePRDAfterRestartKeepsFirstRecord(t *testing.T) {
	c := freezeIDClock(t)
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := idTestPRD(t, s, "the first record")
	c.restart()
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	second := idTestPRD(t, reopened, "the second record")
	requireDistinct(t, "automaton after a restart", first.ID, second.ID)
	requireIntactPRD(t, dir, first.ID, "the first record")
}

// A record file the store could not read is still a record.
func TestIDReuse_CreatePRDRefusesAnIDWithOnlyAFileOnDisk(t *testing.T) {
	c := freezeIDClock(t)
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		filepath.Join(prdRecordsDirName, "%s.json"),
		filepath.Join(archiveDirName, "%s.json"),
	} {
		c.wrap()
		c.restart()
		id := c.next()
		path := filepath.Join(s.dir, fmt.Sprintf(rel, id))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		p := idTestPRD(t, s, "new")
		requireDistinct(t, rel, id, p.ID)
		if got, _ := os.ReadFile(path); !bytes.Equal(got, []byte("not json")) {
			t.Fatalf("%s was overwritten", path)
		}
	}
}

func TestIDReuse_StoriesAndTasks(t *testing.T) {
	c := freezeIDClock(t)
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	s := m.Store()
	a := idTestPRD(t, s, "a")
	c.wrap()
	b := idTestPRD(t, s, "b")

	c.wrap()
	sa, err := s.AddStory(a.ID, "story a", "")
	if err != nil {
		t.Fatal(err)
	}
	sa1 := sa.ID
	c.wrap()
	sb, err := s.AddStory(b.ID, "story b", "")
	if err != nil {
		t.Fatal(err)
	}
	sb1 := sb.ID
	requireDistinct(t, "story", sa1, sb1)

	c.wrap()
	ta, err := s.AddTask(a.ID, sa1, "task a", "spec a", nil)
	if err != nil {
		t.Fatal(err)
	}
	ta1 := ta.ID
	c.wrap()
	tb, err := s.AddTask(b.ID, sb1, "task b", "spec b", nil)
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "task", ta1, tb.ID)
	got, ok := s.GetTask(ta1)
	if !ok || got.PRDID != a.ID || got.Title != "task a" {
		t.Fatalf("task %s now resolves to %+v", ta1, got)
	}
}

func TestIDReuse_SetStoriesAndPlanIDs(t *testing.T) {
	c := freezeIDClock(t)
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	s := m.Store()
	a := idTestPRD(t, s, "a")
	c.wrap()
	b := idTestPRD(t, s, "b")

	plan := func() []Story {
		return []Story{{Title: "one", Tasks: []Task{{Title: "first", Spec: "x"}}}}
	}
	ids := func(prdID string) []string {
		p, _ := s.GetPRD(prdID)
		return []string{p.Story[0].ID, p.Story[0].Tasks[0].ID}
	}
	c.wrap()
	c.restart()
	if err := s.SetStories(a.ID, plan()); err != nil {
		t.Fatal(err)
	}
	c.restart() // the same ids are next in line again
	if err := s.SetStories(b.ID, plan()); err != nil {
		t.Fatal(err)
	}
	got := append(ids(a.ID), ids(b.ID)...)
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("story or task id %s issued twice: %v", id, got)
		}
		seen[id] = true
	}

	// ids the planner path assigns before SetStories stores them
	c.restart()
	early := plan()
	m.assignPlanIDs(early)
	for _, id := range []string{early[0].ID, early[0].Tasks[0].ID} {
		if seen[id] {
			t.Fatalf("plan id %s is already in the store", id)
		}
	}
}

// An id assigned to a plan but not yet stored must not be issued again.
func TestIDReuse_PlanIDsAreReservedBeforeTheyAreStored(t *testing.T) {
	c := freezeIDClock(t)
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	early := []Story{{Title: "one"}}
	m.assignPlanIDs(early)
	c.wrap()
	p := idTestPRD(t, m.Store(), "created while the plan was being checked")
	requireDistinct(t, "plan reservation", early[0].ID, p.ID)
}

// A plan's own id must not be given to one of its other stories or tasks.
func TestIDReuse_PlanIDsAvoidIDsThePlanAlreadyCarries(t *testing.T) {
	c := freezeIDClock(t)
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	carried := c.next()
	stories := []Story{{Title: "new"}, {ID: carried, Title: "carried"}}
	m.assignPlanIDs(stories)
	requireDistinct(t, "carried plan id", carried, stories[0].ID)
}

func TestIDReuse_TagsTemplatesAndLearnings(t *testing.T) {
	c := freezeIDClock(t)
	dir := t.TempDir()
	m := idTestManager(t, dir, DefaultConfig())

	c.wrap()
	t1, err := m.CreateTag("one", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	c.restart()
	t2, err := m.CreateTag("two", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "tag", t1.ID, t2.ID)
	c.restart()
	sys, err := m.EnsureSystemTag("three")
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "system tag", t1.ID, sys.ID)
	c.restart()
	auto, err := m.createAutoTag("four", "marker")
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "automatic tag", t1.ID, auto.ID)

	c.wrap()
	c.restart()
	one, err := m.Templates().Create("one", "", "spec one", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.restart()
	two, err := m.Templates().Create("two", "", "spec two", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "template", one.ID, two.ID)

	c.wrap()
	c.restart()
	if err := m.Store().AddLearning(Learning{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	c.restart()
	if err := m.Store().AddLearning(Learning{Text: "two"}); err != nil {
		t.Fatal(err)
	}
	ls := m.Store().ListLearnings()
	requireDistinct(t, "learning", ls[len(ls)-2].ID, ls[len(ls)-1].ID)
}

// fakeProfileIDs makes newProfileID draw the given 4-byte values in turn.
func fakeProfileIDs(t *testing.T, draws ...byte) {
	t.Helper()
	var b []byte
	for _, d := range draws {
		b = append(b, d, d, d, d)
	}
	old := profileIDReader
	profileIDReader = bytes.NewReader(b)
	t.Cleanup(func() { profileIDReader = old })
}

func TestIDReuse_AutomationPendingItemAndProfiles(t *testing.T) {
	m := idTestManager(t, t.TempDir(), DefaultConfig())

	fakeProfileIDs(t, 0xaa, 0xaa, 0xbb)
	g1, err := m.CreateGuardrailProfile("one", "", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := m.CreateGuardrailProfile("two", "", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "guardrail profile", g1.ID, g2.ID)
	if got, ok := m.Store().GetGuardrailProfile(g1.ID); !ok || got.Name != "one" {
		t.Fatalf("guardrail profile %s now holds %+v", g1.ID, got)
	}

	fakeProfileIDs(t, 0xcc, 0xcc, 0xdd)
	r1, err := m.CreateRetryProfile("one", "", nil, 0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m.CreateRetryProfile("two", "", nil, 0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "retry profile", r1.ID, r2.ID)

	fakeProfileIDs(t, 0x11, 0x11, 0x22)
	a1, err := m.CreateAutomation(AutomationInput{Name: "one"}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := m.CreateAutomation(AutomationInput{Name: "two"}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "automation", a1.ID, a2.ID)
	if got, ok := m.GetAutomation(a1.ID); !ok || got.Name != "one" {
		t.Fatalf("automation %s now holds %+v", a1.ID, got)
	}

	// A caller-supplied id that is taken is refused, not overwritten.
	if _, err := m.automations.Create(&Automation{ID: a1.ID, Name: "three"}); err == nil {
		t.Fatalf("Create with the used id %s was accepted", a1.ID)
	}
	if got, _ := m.GetAutomation(a1.ID); got.Name != "one" {
		t.Fatalf("automation %s was overwritten: %+v", a1.ID, got)
	}

	fakeProfileIDs(t, 0x33, 0x33, 0x44)
	i1, err := m.CreatePendingItem("one", "ref-1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	i2, err := m.CreatePendingItem("two", "ref-2", "operator")
	if err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "pending item", i1.ID, i2.ID)
}

func TestIDReuse_AutomationMemberIDs(t *testing.T) {
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	p1 := idTestPRD(t, m.Store(), "member one")
	p2 := idTestPRD(t, m.Store(), "member two")
	// automation id, then the same member id twice, then a fresh one
	fakeProfileIDs(t, 0x01, 0x55, 0x55, 0x66)
	a, err := m.CreateAutomation(AutomationInput{Name: "two members", Members: []AutomationMember{
		{PRDID: p1.ID}, {PRDID: p2.ID},
	}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	requireDistinct(t, "member", a.Members[0].ID, a.Members[1].ID)
}

func TestIDReuse_DecisionRecordRefusesLeftoverArtifact(t *testing.T) {
	dir := t.TempDir()
	m := idTestManager(t, dir, DefaultConfig())
	_, ds, _ := newDecisionStoreTest(t)
	m.GuardRecordIDs(ds)
	held := fmt.Sprintf("dr-%020x", 1)
	if err := os.MkdirAll(filepath.Join(defaultWorktreeDir(dir), held), 0o755); err != nil {
		t.Fatal(err)
	}
	got := mustCreateDecision(t, ds, nil)
	requireDistinct(t, "decision record", held, got.ID)
}

func idTestGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func idTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	idTestGit(t, repo, nil, "init", "-q", "-b", "main")
	idTestGit(t, repo, nil, "config", "user.email", "t@example.invalid")
	idTestGit(t, repo, nil, "config", "user.name", "t")
	idTestGit(t, repo, nil, "config", "core.logAllRefUpdates", "true")
	idTestGit(t, repo, nil, "commit", "-q", "--allow-empty", "-m", "base")
	return repo
}

func TestIDReuse_CreatePRDRefusesLeftoverBranchOrWorktreeFolder(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T, repo, worktrees, id string)
	}{
		{"branch in the base repo", func(t *testing.T, repo, _, id string) {
			idTestGit(t, repo, nil, "branch", "automaton/"+id+"-old-work")
		}},
		{"bare branch", func(t *testing.T, repo, _, id string) {
			idTestGit(t, repo, nil, "branch", "automaton/"+id)
		}},
		{"remote-tracking branch", func(t *testing.T, repo, _, id string) {
			idTestGit(t, repo, nil, "update-ref", "refs/remotes/origin/automaton/"+id+"-x", "HEAD")
		}},
		{"worktree folder", func(t *testing.T, _, worktrees, id string) {
			if err := os.MkdirAll(filepath.Join(worktrees, id), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"task worktree folder", func(t *testing.T, _, worktrees, id string) {
			if err := os.MkdirAll(filepath.Join(worktrees, id+"-tasks"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"saved verification scripts", func(t *testing.T, _, worktrees, id string) {
			if err := os.MkdirAll(filepath.Join(worktrees, ".verify-scripts", id), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"task id on a task folder", func(t *testing.T, _, worktrees, id string) {
			if err := os.MkdirAll(filepath.Join(worktrees, "0000aaaa-tasks", "some-title-"+id), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"task id on a task branch", func(t *testing.T, repo, _, id string) {
			idTestGit(t, repo, nil, "branch", "automaton/0000aaaa-x--tasks-some-title-"+id)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := freezeIDClock(t)
			repo, worktrees := idTestRepo(t), t.TempDir()
			cfg := DefaultConfig()
			cfg.WorktreeBaseRepo, cfg.WorktreeDir = repo, worktrees
			m := idTestManager(t, t.TempDir(), cfg)
			held := c.next()
			tc.make(t, repo, worktrees, held)
			p := idTestPRD(t, m.Store(), "new")
			requireDistinct(t, tc.name, held, p.ID)
		})
	}
}

// The branch can live in the new record's own repository, in another record's,
// or in a configured project's.
func TestIDReuse_CreatePRDRefusesBranchInAnyKnownRepo(t *testing.T) {
	for _, where := range []string{"new record", "another record", "configured project"} {
		t.Run(where, func(t *testing.T) {
			c := freezeIDClock(t)
			repo := idTestRepo(t)
			m := idTestManager(t, t.TempDir(), DefaultConfig())
			dir := ""
			switch where {
			case "new record":
				dir = repo
			case "another record":
				if _, err := m.Store().CreatePRD("other", repo, "", "", ""); err != nil {
					t.Fatal(err)
				}
				c.wrap()
				c.restart()
			case "configured project":
				m.SetProjectLinkProjectsFn(func() map[string]ProjectLinkProject {
					return map[string]ProjectLinkProject{"p": {Dir: repo}}
				})
			}
			held := c.next()
			idTestGit(t, repo, nil, "branch", "automaton/"+held+"-old")
			p, err := m.Store().CreatePRD("new", dir, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			requireDistinct(t, where, held, p.ID)
		})
	}
}

// A configuration change is picked up without a restart.
func TestIDReuse_SetConfigMovesTheWorktreeFolder(t *testing.T) {
	c := freezeIDClock(t)
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	worktrees := t.TempDir()
	cfg := m.Config()
	cfg.WorktreeDir = worktrees
	m.SetConfig(cfg)
	held := c.next()
	if err := os.MkdirAll(filepath.Join(worktrees, held), 0o755); err != nil {
		t.Fatal(err)
	}
	requireDistinct(t, "after SetConfig", held, idTestPRD(t, m.Store(), "new").ID)
}
