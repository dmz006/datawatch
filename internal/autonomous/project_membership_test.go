package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func pmFixture(projects map[string]string, prds ...*PRD) (func(string) *PRD, map[string]string) {
	m := map[string]*PRD{}
	for _, p := range prds {
		m[p.ID] = p
	}
	return func(id string) *PRD { return m[id] }, projects
}

func TestResolveProjectLinkWins(t *testing.T) {
	p := &PRD{ID: "a", Project: "alpha", ProjectDir: "/x/alpha"}
	by, pr := pmFixture(map[string]string{"alpha": "/x/alpha"}, p)
	n, s := ResolveAutomatonProject(p, by, nil, pr)
	if n != "alpha" || s != ProjectSourceLink {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveLinkBeatsDirectory(t *testing.T) {
	p := &PRD{ID: "a", Project: "beta", ProjectDir: "/x/alpha"}
	by, pr := pmFixture(map[string]string{"alpha": "/x/alpha", "beta": "/x/beta"}, p)
	n, s := ResolveAutomatonProject(p, by, nil, pr)
	if n != "beta" || s != ProjectSourceLink {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveAutomationProject(t *testing.T) {
	p := &PRD{ID: "a", AutomationID: "z"}
	by, pr := pmFixture(map[string]string{"beta": "/x/beta"}, p)
	au := &Automation{ID: "z", Project: "beta"}
	n, s := ResolveAutomatonProject(p, by, func(*PRD) *Automation { return au }, pr)
	if n != "beta" || s != ProjectSourceAutomation {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveParentProject(t *testing.T) {
	parent := &PRD{ID: "p", Project: "beta"}
	child := &PRD{ID: "c", ParentPRDID: "p", ProjectDir: "/x/other"}
	by, pr := pmFixture(map[string]string{"beta": "/x/beta"}, parent, child)
	n, s := ResolveAutomatonProject(child, by, nil, pr)
	if n != "beta" || s != ProjectSourceParent {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveWorktreeMapsToRepo(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt", "task")
	if err := os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "task"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitdir := filepath.Join(repo, ".git", "worktrees", "task")
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RepositoryDirOf(wt); got != repo {
		t.Fatalf("worktree: %s", got)
	}
	if got := RepositoryDirOf(filepath.Join(wt, "sub", "deep")); got != repo {
		t.Fatalf("subdir: %s", got)
	}
	if got := RepositoryDirOf(gitdir); got != repo {
		t.Fatalf("gitdir path: %s", got)
	}
	p := &PRD{ID: "a", ProjectDir: wt}
	by, pr := pmFixture(map[string]string{"proj": repo}, p)
	n, s := ResolveAutomatonProject(p, by, nil, pr)
	if n != "proj" || s != ProjectSourceDirectory {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolvePlainDirectoryFallback(t *testing.T) {
	p := &PRD{ID: "a", ProjectDir: "/x/alpha/"}
	by, pr := pmFixture(map[string]string{"alpha": "/x/alpha"}, p)
	n, s := ResolveAutomatonProject(p, by, nil, pr)
	if n != "alpha" || s != ProjectSourceDirectory {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveUnknownLinkFallsBack(t *testing.T) {
	p := &PRD{ID: "a", Project: "ghost", ProjectDir: "/x/alpha"}
	by, pr := pmFixture(map[string]string{"alpha": "/x/alpha"}, p)
	n, s := ResolveAutomatonProject(p, by, nil, pr)
	if n != "alpha" || s != ProjectSourceDirectory {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveParentCycleTerminates(t *testing.T) {
	a := &PRD{ID: "a", ParentPRDID: "b"}
	b := &PRD{ID: "b", ParentPRDID: "a"}
	by, pr := pmFixture(map[string]string{"alpha": "/x/alpha"}, a, b)
	n, s := ResolveAutomatonProject(a, by, nil, pr)
	if n != "" || s != ProjectSourceNone {
		t.Fatalf("got %s %s", n, s)
	}
}

func TestResolveNone(t *testing.T) {
	p := &PRD{ID: "a", ProjectDir: "/x/zzz"}
	by, pr := pmFixture(map[string]string{"alpha": "/x/alpha"}, p)
	if n, s := ResolveAutomatonProject(p, by, nil, pr); n != "" || s != ProjectSourceNone {
		t.Fatalf("got %s %s", n, s)
	}
	if n, s := ResolveAutomatonProject(nil, by, nil, pr); n != "" || s != ProjectSourceNone {
		t.Fatalf("nil: %s %s", n, s)
	}
}

func TestAutomatonProjectIndex(t *testing.T) {
	p1 := &PRD{ID: "p1"}
	p2 := &PRD{ID: "p2", ParentPRDID: "p1"}
	au := &Automation{ID: "z", Project: "beta", Members: []AutomationMember{{ID: "m", PRDID: "p1"}}}
	idx := NewAutomatonProjectIndex([]*PRD{p1, p2}, []*Automation{au}, map[string]string{"beta": "/x/beta"})
	if m := idx.Of("p1"); m.Project != "beta" || m.Source != ProjectSourceAutomation {
		t.Fatalf("p1 %+v", m)
	}
	if m := idx.Of("p2"); m.Project != "beta" || m.Source != ProjectSourceParent {
		t.Fatalf("p2 %+v", m)
	}
	if m := idx.Of("nope"); m.Source != ProjectSourceNone {
		t.Fatalf("nope %+v", m)
	}
}
