package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type linkBackfillEnv struct {
	m        *Manager
	repo     string
	worktree string
	other    string
}

func newLinkBackfillEnv(t *testing.T) *linkBackfillEnv {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	root := t.TempDir()
	repo := filepath.Join(root, "datawatch")
	if err := os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "wt1"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, "wt1")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	gitdir := "gitdir: " + filepath.Join(repo, ".git", "worktrees", "wt1") + "\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(gitdir), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	m.SetProjectLinkProjectsFn(func() map[string]ProjectLinkProject {
		return map[string]ProjectLinkProject{
			"datawatch": {Dir: repo},
			"other":     {Dir: other},
		}
	})
	return &linkBackfillEnv{m: m, repo: repo, worktree: wt, other: other}
}

func (e *linkBackfillEnv) prd(t *testing.T, title, dir string) *PRD {
	t.Helper()
	p, err := e.m.CreatePRD("spec "+title, dir, "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	p.Title = title
	if err := e.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *linkBackfillEnv) project(t *testing.T, id string) string {
	t.Helper()
	p, ok := e.m.Store().GetPRD(id)
	if !ok {
		t.Fatalf("prd %s missing", id)
	}
	return p.Project
}

func linkDecisions(p *PRD) int {
	n := 0
	for _, d := range p.Decisions {
		if d.Kind == projectLinkBackfillDecisionKind {
			n++
		}
	}
	return n
}

func entryFor(r ProjectLinkBackfillReport, id string) ProjectLinkBackfillEntry {
	for _, e := range r.Entries {
		if e.ID == id {
			return e
		}
	}
	return ProjectLinkBackfillEntry{}
}

func TestProjectLinkBackfill_DryRunChangesNothing(t *testing.T) {
	e := newLinkBackfillEnv(t)
	a := e.prd(t, "by dir", e.repo)
	r := e.m.ProjectLinkBackfill(false)
	if r.Apply || r.Counts.Matched != 1 || r.Counts.Applied != 0 {
		t.Fatalf("report = %+v", r)
	}
	if got := entryFor(r, a.ID); got.Source != ProjectSourceDirectory || got.Project != "datawatch" || got.Applied {
		t.Fatalf("entry = %+v", got)
	}
	if e.project(t, a.ID) != "" {
		t.Fatal("dry-run wrote a link")
	}
	if p, _ := e.m.Store().GetPRD(a.ID); linkDecisions(p) != 0 {
		t.Fatal("dry-run wrote a decision")
	}
	latest, ok := e.m.LatestProjectLinkBackfill()
	if !ok || latest.Counts.Matched != 1 {
		t.Fatalf("latest = %+v ok=%v", latest, ok)
	}
}

func TestProjectLinkBackfill_ApplyByDirectoryAndWorktree(t *testing.T) {
	e := newLinkBackfillEnv(t)
	a := e.prd(t, "repo dir", e.repo)
	b := e.prd(t, "worktree dir", e.worktree)
	r := e.m.ProjectLinkBackfill(true)
	if r.Counts.Applied != 2 || r.Counts.Failed != 0 {
		t.Fatalf("counts = %+v", r.Counts)
	}
	for _, p := range []*PRD{a, b} {
		if e.project(t, p.ID) != "datawatch" {
			t.Fatalf("%s not linked", p.Title)
		}
		if got := entryFor(r, p.ID); got.Source != ProjectSourceDirectory || !got.Applied {
			t.Fatalf("entry = %+v", got)
		}
	}
}

func TestProjectLinkBackfill_ApplyByAutomationAndParent(t *testing.T) {
	e := newLinkBackfillEnv(t)
	member := e.prd(t, "member", e.other)
	au, err := e.m.AutomationStore().Create(&Automation{
		Name: "held", Project: "datawatch",
		Members: []AutomationMember{{PRDID: member.ID, Kind: "prd"}},
	})
	if err != nil {
		t.Skipf("cannot build an Automation in this harness: %v", err)
	}
	// The parent carries an explicit link; the child has none and sits elsewhere.
	parent := e.prd(t, "parent", e.other)
	parent.Project = "datawatch"
	if err := e.m.Store().SavePRD(parent); err != nil {
		t.Fatal(err)
	}
	child := e.prd(t, "child", e.other)
	child.ParentPRDID = parent.ID
	if err := e.m.Store().SavePRD(child); err != nil {
		t.Fatal(err)
	}
	r := e.m.ProjectLinkBackfill(true)
	if got := entryFor(r, member.ID); got.Source != ProjectSourceAutomation || !got.Applied {
		t.Fatalf("member entry = %+v (automation %s)", got, au.ID)
	}
	if got := entryFor(r, child.ID); got.Source != ProjectSourceParent || !got.Applied {
		t.Fatalf("child entry = %+v", got)
	}
	if e.project(t, member.ID) != "datawatch" || e.project(t, child.ID) != "datawatch" {
		t.Fatal("links not written")
	}
}

func TestProjectLinkBackfill_ExistingLinksUntouchedAndNoMatchSkipped(t *testing.T) {
	e := newLinkBackfillEnv(t)
	linked := e.prd(t, "already linked", e.repo)
	linked.Project = "other"
	if err := e.m.Store().SavePRD(linked); err != nil {
		t.Fatal(err)
	}
	stray := e.prd(t, "stray", e.other)
	tpl := e.prd(t, "template", e.repo)
	tpl.IsTemplate = true
	if err := e.m.Store().SavePRD(tpl); err != nil {
		t.Fatal(err)
	}
	r := e.m.ProjectLinkBackfill(true)
	if e.project(t, linked.ID) != "other" {
		t.Fatal("existing link changed")
	}
	if entryFor(r, linked.ID).ID != "" || entryFor(r, tpl.ID).ID != "" {
		t.Fatal("linked Automaton or template was reported")
	}
	got := entryFor(r, stray.ID)
	if !got.Skipped || got.Reason == "" || got.Applied || e.project(t, stray.ID) != "" {
		t.Fatalf("stray entry = %+v", got)
	}
	if r.Counts.Skipped != 1 || r.Counts.Applied != 0 {
		t.Fatalf("counts = %+v", r.Counts)
	}
}

func TestProjectLinkBackfill_DecisionPerLinkAndIdempotent(t *testing.T) {
	e := newLinkBackfillEnv(t)
	a := e.prd(t, "a", e.repo)
	b := e.prd(t, "b", e.worktree)
	e.m.ProjectLinkBackfill(true)
	for _, p := range []*PRD{a, b} {
		got, _ := e.m.Store().GetPRD(p.ID)
		if linkDecisions(got) != 1 {
			t.Fatalf("%s decisions = %d", p.Title, linkDecisions(got))
		}
		var note string
		for _, d := range got.Decisions {
			if d.Kind == projectLinkBackfillDecisionKind {
				note = d.Note
			}
		}
		if !strings.Contains(note, "datawatch") || !strings.Contains(note, "directory") || !strings.Contains(note, "backfill") {
			t.Fatalf("note = %q", note)
		}
	}
	r := e.m.ProjectLinkBackfill(true)
	if r.Counts.Scanned != 0 || r.Counts.Applied != 0 || len(r.Entries) != 0 {
		t.Fatalf("second run = %+v", r)
	}
	for _, p := range []*PRD{a, b} {
		got, _ := e.m.Store().GetPRD(p.ID)
		if linkDecisions(got) != 1 {
			t.Fatalf("second run recorded again: %d", linkDecisions(got))
		}
	}
}

func TestProjectLinkBackfill_FailureDoesNotStopOthers(t *testing.T) {
	e := newLinkBackfillEnv(t)
	bad := e.prd(t, "bad", e.repo)
	good1 := e.prd(t, "good1", e.repo)
	good2 := e.prd(t, "good2", e.worktree)
	e.m.projectLink.save = func(p *PRD) error {
		if p.ID == bad.ID {
			return errors.New("disk full")
		}
		return e.m.Store().SavePRD(p)
	}
	r := e.m.ProjectLinkBackfill(true)
	if r.Counts.Failed != 1 || r.Counts.Applied != 2 {
		t.Fatalf("counts = %+v", r.Counts)
	}
	if got := entryFor(r, bad.ID); got.Applied || !strings.Contains(got.Error, "disk full") {
		t.Fatalf("bad entry = %+v", got)
	}
	if e.project(t, bad.ID) != "" {
		t.Fatal("failed record kept a link")
	}
	if p, _ := e.m.Store().GetPRD(bad.ID); linkDecisions(p) != 0 {
		t.Fatal("failed record kept a decision")
	}
	if e.project(t, good1.ID) != "datawatch" || e.project(t, good2.ID) != "datawatch" {
		t.Fatal("other records not linked")
	}
}

func TestStartProjectLinkBackfill_DoneMarker(t *testing.T) {
	e := newLinkBackfillEnv(t)
	a := e.prd(t, "a", e.repo)
	e.m.StartProjectLinkBackfill(false)
	if e.project(t, a.ID) != "" || e.m.ProjectLinkBackfillDone() {
		t.Fatal("apply=false changed something")
	}
	e.m.StartProjectLinkBackfill(true)
	if e.project(t, a.ID) != "datawatch" || !e.m.ProjectLinkBackfillDone() {
		t.Fatal("apply=true did not link and mark done")
	}
	late := e.prd(t, "late", e.repo)
	e.m.StartProjectLinkBackfill(true)
	if e.project(t, late.ID) != "" {
		t.Fatal("pass re-ran after the done marker")
	}
}
