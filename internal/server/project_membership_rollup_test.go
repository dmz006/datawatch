// Membership tests for the project roll-up, history, Gantt rows, progress
// grouping and the events filter: every surface reads the same resolver.
// Pure-function tests only; nothing here starts, signals or kills a process.

package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// memberPRD is a minimal Automaton for membership tests.
func memberPRD(id, dir string, status autonomous.PRDStatus) *autonomous.PRD {
	return &autonomous.PRD{
		ID: id, Title: "title " + id, ProjectDir: dir, Status: status,
		CreatedAt: rollupNow.Add(-48 * time.Hour), UpdatedAt: rollupNow.Add(-time.Hour),
	}
}

func memberIn(list []rollupAutomaton, id string) bool {
	for _, a := range list {
		if a.ID == id {
			return true
		}
	}
	return false
}

func memberRollup(project, dir string, projects map[string]string, prds []*autonomous.PRD, autos []*autonomous.Automation) projectRollup {
	return buildProjectRollup(rollupInput{
		Project: project, Dir: dir, Projects: projects, PRDs: prds, Automations: autos,
		WindowDays: 7, Now: rollupNow,
	})
}

// makeWorktreeDir creates a temp dir whose .git file names a worktree of repo.
func makeWorktreeDir(t *testing.T, repo string) string {
	t.Helper()
	wt := t.TempDir()
	line := "gitdir: " + filepath.Join(repo, ".git", "worktrees", "x") + "\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return wt
}

func TestRollupMembership_WorktreeWithExplicitLink(t *testing.T) {
	repo := t.TempDir()
	wt := makeWorktreeDir(t, repo)
	p := memberPRD("wt-linked", wt, autonomous.PRDRunning)
	p.Project = "acme"
	// The link places it even though the project's Dir is not the repository.
	got := memberRollup("acme", "/srv/placeholder-acme", map[string]string{"acme": "/srv/placeholder-acme"},
		[]*autonomous.PRD{p}, nil)
	if !memberIn(got.Unassigned.Automata, "wt-linked") || !memberIn(got.Total.Automata, "wt-linked") {
		t.Fatalf("linked worktree Automaton missing from roll-up: %+v", got)
	}
	if len(got.WithoutProject.Automata) != 0 {
		t.Fatalf("linked Automaton must not be without a project: %+v", got.WithoutProject.Automata)
	}
}

func TestRollupMembership_WorktreeWithoutLinkMatchesRepositoryDir(t *testing.T) {
	repo := t.TempDir()
	wt := makeWorktreeDir(t, repo)
	p := memberPRD("wt-plain", wt, autonomous.PRDRunning)
	got := memberRollup("acme", repo, map[string]string{"acme": repo}, []*autonomous.PRD{p}, nil)
	if !memberIn(got.Unassigned.Automata, "wt-plain") {
		t.Fatalf("worktree must match the project whose Dir is the repository: %+v", got.Unassigned.Automata)
	}
	if len(got.WithoutProject.Automata) != 0 {
		t.Fatalf("unexpected without_project: %+v", got.WithoutProject.Automata)
	}
}

func TestRollupMembership_AutomationProjectIncludesUnlinkedMember(t *testing.T) {
	p := memberPRD("in-auto", "/srv/placeholder-elsewhere", autonomous.PRDRunning)
	auto := &autonomous.Automation{ID: "auto-1", Project: "acme",
		Members: []autonomous.AutomationMember{{PRDID: "in-auto"}}}
	got := memberRollup("acme", "/srv/placeholder-acme", map[string]string{"acme": "/srv/placeholder-acme"},
		[]*autonomous.PRD{p}, []*autonomous.Automation{auto})
	if !memberIn(got.Unassigned.Automata, "in-auto") {
		t.Fatalf("Automaton in an Automation of this project must be included: %+v", got.Unassigned.Automata)
	}
}

func TestRollupMembership_SpawnedChildFollowsParentProject(t *testing.T) {
	parent := memberPRD("parent", "/srv/placeholder-elsewhere", autonomous.PRDRunning)
	parent.Project = "acme"
	child := memberPRD("child", "/srv/placeholder-elsewhere", autonomous.PRDRunning)
	child.ParentPRDID = "parent"
	got := memberRollup("acme", "/srv/placeholder-acme", map[string]string{"acme": "/srv/placeholder-acme"},
		[]*autonomous.PRD{parent, child}, nil)
	if !memberIn(got.Unassigned.Automata, "child") || !memberIn(got.Unassigned.Automata, "parent") {
		t.Fatalf("child of a project Automaton must join the parent's project: %+v", got.Unassigned.Automata)
	}
	if len(got.WithoutProject.Automata) != 0 {
		t.Fatalf("child must not be without a project: %+v", got.WithoutProject.Automata)
	}
}

// linkOverrideFixture: "moved" sits in project A's directory but links to B.
func linkOverrideFixture() (map[string]string, []*autonomous.PRD) {
	projects := map[string]string{"alpha": "/srv/placeholder-alpha", "beta": "/srv/placeholder-beta"}
	moved := memberPRD("moved", "/srv/placeholder-alpha", autonomous.PRDCompleted)
	moved.Project = "beta"
	moved.Story = []autonomous.Story{{ID: "s", Tasks: []autonomous.Task{
		{ID: "t", Status: autonomous.TaskCompleted, CompletedAt: &moved.UpdatedAt}}}}
	return projects, []*autonomous.PRD{moved}
}

func TestMembershipLinkOverridesDirectory_Rollup(t *testing.T) {
	projects, prds := linkOverrideFixture()
	a := memberRollup("alpha", projects["alpha"], projects, prds, nil)
	b := memberRollup("beta", projects["beta"], projects, prds, nil)
	if memberIn(a.Total.Automata, "moved") || memberIn(a.Unassigned.Automata, "moved") || memberIn(a.WithoutProject.Automata, "moved") {
		t.Errorf("A's roll-up must not hold the Automaton linked to B: %+v", a)
	}
	if !memberIn(b.Unassigned.Automata, "moved") || !memberIn(b.Total.Automata, "moved") {
		t.Errorf("B's roll-up must hold the linked Automaton: %+v", b)
	}
}

func TestMembershipLinkOverridesDirectory_History(t *testing.T) {
	projects, prds := linkOverrideFixture()
	ids := func(project string) []string {
		out := buildProjectHistory(historyInput{
			Project: project, Dir: projects[project], Projects: projects, PRDs: prds,
			Limit: 50, Now: historyNow,
		})
		var r []string
		for _, it := range out.Items {
			if it.Kind == historyKindAutomaton {
				r = append(r, it.ID)
			}
		}
		return r
	}
	if got := ids("alpha"); len(got) != 0 {
		t.Errorf("A history = %v, want none", got)
	}
	if got := ids("beta"); len(got) != 1 || got[0] != "moved" {
		t.Errorf("B history = %v, want [moved]", got)
	}
}

func TestMembershipLinkOverridesDirectory_GanttRows(t *testing.T) {
	projects, prds := linkOverrideFixture()
	count := func(project string) int {
		res := autonomous.BuildTimeline(rollupNow, prds, autonomous.TimelineFilter{
			Project: project, Projects: projects,
		})
		n := 0
		for _, g := range res.Projects {
			for _, a := range g.Automata {
				if a.ID == "moved" {
					n++
				}
			}
		}
		return n
	}
	if n := count("alpha"); n != 0 {
		t.Errorf("A Gantt rows hold the linked Automaton %d times, want 0", n)
	}
	if n := count("beta"); n != 1 {
		t.Errorf("B Gantt rows hold the linked Automaton %d times, want 1", n)
	}
}

func TestMembershipLinkOverridesDirectory_EventsFilter(t *testing.T) {
	projects, prds := linkOverrideFixture()
	scope := func(project string) projectEventScope {
		return buildProjectEventScope(projectEventInput{
			Project: project, Projects: projects, Dir: projects[project], PRDs: prds,
		})
	}
	if scope("alpha").PRDs["moved"] {
		t.Error("A events scope must not hold the Automaton linked to B")
	}
	if !scope("beta").PRDs["moved"] {
		t.Error("B events scope must hold the linked Automaton")
	}
	ev := []autonomous.LifecycleEvent{{ID: "e1", PRDID: "moved"}}
	if got := filterProjectEvents(scope("alpha"), ev); len(got) != 0 {
		t.Errorf("A events = %+v, want none", got)
	}
	if got := filterProjectEvents(scope("beta"), ev); len(got) != 1 {
		t.Errorf("B events = %+v, want 1", got)
	}
}

func TestRollupMembership_NoLinkNoMatchIsWithoutProjectOnce(t *testing.T) {
	projects := map[string]string{"alpha": "/srv/placeholder-alpha", "beta": "/srv/placeholder-beta"}
	orphan := memberPRD("orphan", "/srv/placeholder-nowhere", autonomous.PRDRunning)
	inAlpha := memberPRD("in-alpha", "/srv/placeholder-alpha", autonomous.PRDRunning)
	prds := []*autonomous.PRD{orphan, inAlpha}
	for _, name := range []string{"alpha", "beta"} {
		got := memberRollup(name, projects[name], projects, prds, nil)
		for _, g := range append(append([]rollupGroup{}, got.Arcs...), got.Unassigned, got.Total) {
			if memberIn(g.Automata, "orphan") {
				t.Errorf("%s: orphan must be absent from every project group and Total", name)
			}
		}
		n := 0
		for _, a := range got.WithoutProject.Automata {
			if a.ID == "orphan" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: orphan appears %d times in without_project, want 1", name, n)
		}
		if memberIn(got.WithoutProject.Automata, "in-alpha") {
			t.Errorf("%s: matched Automaton leaked into without_project", name)
		}
	}
}

func TestRollupMembership_UnassignedHoldsOnlyAutomataNoArcHolds(t *testing.T) {
	projects := map[string]string{"acme": "/srv/placeholder-acme"}
	held := memberPRD("held", "/srv/placeholder-acme", autonomous.PRDRunning)
	held.AutomationID = "auto-arc"
	heldByMember := memberPRD("held-member", "/srv/placeholder-acme", autonomous.PRDRunning)
	free := memberPRD("free", "/srv/placeholder-acme", autonomous.PRDRunning)
	got := buildProjectRollup(rollupInput{
		Project: "acme", Dir: projects["acme"], Projects: projects,
		Arcs:        []config.ProjectArc{{AutomationID: "auto-arc"}},
		PRDs:        []*autonomous.PRD{held, heldByMember, free},
		Automations: []*autonomous.Automation{{ID: "auto-arc", Members: []autonomous.AutomationMember{{PRDID: "held-member"}}}},
		WindowDays:  7, Now: rollupNow,
	})
	if ids := rollupIDs(got.Unassigned.Automata); len(ids) != 1 || ids[0] != "free" {
		t.Fatalf("unassigned = %v, want [free]", ids)
	}
	if ids := rollupIDs(got.Arcs[0].Automata); len(ids) != 2 {
		t.Fatalf("arc = %v, want both held Automata", ids)
	}
}

func TestMembershipByProjectGrouping_CostAndProgressFollowResolver(t *testing.T) {
	projects := map[string]string{"alpha": "/srv/placeholder-alpha", "beta": "/srv/placeholder-beta"}
	moved := memberPRD("moved", "/srv/placeholder-alpha", autonomous.PRDCompleted)
	moved.Project = "beta"
	plain := memberPRD("plain", "/srv/placeholder-alpha", autonomous.PRDCompleted)
	orphan := memberPRD("orphan", "/srv/placeholder-nowhere", autonomous.PRDCompleted)
	prds := []*autonomous.PRD{moved, plain, orphan}
	points := map[string]float64{"moved": 3, "plain": 2, "orphan": 5}

	// Cost: points by project follow the same membership as the roll-up.
	in := rollupInput{Projects: projects, PRDs: prds, Points: points, WindowDays: 7, Now: rollupNow}
	in.Project, in.Dir = "alpha", projects["alpha"]
	a := buildProjectRollup(in)
	in.Project, in.Dir = "beta", projects["beta"]
	b := buildProjectRollup(in)
	if a.Total.Points != 2 || b.Total.Points != 3 {
		t.Errorf("points alpha=%v beta=%v, want 2 and 3", a.Total.Points, b.Total.Points)
	}
	if a.WithoutProject.Points != 5 || b.WithoutProject.Points != 5 {
		t.Errorf("without_project points = %v/%v, want 5", a.WithoutProject.Points, b.WithoutProject.Points)
	}
	if a.Total.Points+a.WithoutProject.Points != 7 {
		t.Errorf("orphan points must not be in Total: %v", a.Total.Points)
	}

	// Progress: the by-project grouping reads the resolver.
	m := autonomous.ComputeProgress(autonomous.ProgressInput{
		PRDs:    prds,
		Options: autonomous.ProgressOptions{Projects: projects},
	})
	bp := m.Grouped.ByProject
	if bp["alpha"].PRDs != 1 || bp["beta"].PRDs != 1 {
		t.Errorf("by_project = %+v, want one Automaton each in alpha and beta", bp)
	}
	// A record the resolver cannot place keeps its own ProjectKey (its
	// directory) and so lands in neither configured project.
	if bp["/srv/placeholder-nowhere"].PRDs != 1 {
		t.Errorf("unplaced Automaton must keep its ProjectKey group: %+v", bp)
	}
	if m.Grouped.Totals.PRDs != 3 {
		t.Errorf("totals = %d, want 3", m.Grouped.Totals.PRDs)
	}
}
