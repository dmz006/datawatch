package autonomous

import (
	"os"
	"path/filepath"
	"strings"
)

// Sources reported by ResolveAutomatonProject.
const (
	ProjectSourceLink       = "link"
	ProjectSourceAutomation = "automation"
	ProjectSourceParent     = "parent"
	ProjectSourceDirectory  = "directory"
	ProjectSourceNone       = "none"
)

const projectParentDepthCap = 16

// ResolveAutomatonProject decides which configured project an Automaton
// belongs to. It is pure: it reads only its arguments and the filesystem
// markers RepositoryDirOf inspects, and never touches a session.
//
// Order: the Automaton's own link, the link of the Automation holding it, the
// first ancestor (via ParentPRDID, capped and cycle-safe) that resolves by
// either, then a directory match. A link wins over a directory match.
// projects maps project name to its configured directory.
func ResolveAutomatonProject(p *PRD, byID func(id string) *PRD, automationOf func(*PRD) *Automation, projects map[string]string) (project, source string) {
	if p == nil {
		return "", ProjectSourceNone
	}
	if name, src := resolveByLink(p, automationOf, projects); name != "" {
		return name, src
	}
	seen := map[string]bool{p.ID: true}
	cur := p
	for depth := 0; depth < projectParentDepthCap && byID != nil && cur.ParentPRDID != ""; depth++ {
		if seen[cur.ParentPRDID] {
			break
		}
		seen[cur.ParentPRDID] = true
		parent := byID(cur.ParentPRDID)
		if parent == nil {
			break
		}
		if name, _ := resolveByLink(parent, automationOf, projects); name != "" {
			return name, ProjectSourceParent
		}
		cur = parent
	}
	if name := projectByDirectory(p.ProjectDir, projects); name != "" {
		return name, ProjectSourceDirectory
	}
	return "", ProjectSourceNone
}

func resolveByLink(p *PRD, automationOf func(*PRD) *Automation, projects map[string]string) (string, string) {
	if p.Project != "" {
		if _, ok := projects[p.Project]; ok {
			return p.Project, ProjectSourceLink
		}
	}
	if automationOf != nil {
		if a := automationOf(p); a != nil && a.Project != "" {
			if _, ok := projects[a.Project]; ok {
				return a.Project, ProjectSourceAutomation
			}
		}
	}
	return "", ""
}

func projectByDirectory(dir string, projects map[string]string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	raw := filepath.Clean(dir)
	repo := RepositoryDirOf(dir)
	best := ""
	for name, pd := range projects {
		if strings.TrimSpace(pd) == "" {
			continue
		}
		c := filepath.Clean(pd)
		if c == raw || c == repo {
			if best == "" || name < best {
				best = name
			}
		}
	}
	return best
}

// RepositoryDirOf maps a directory to its repository root without running any
// process. A git worktree (its .git is a file naming <repo>/.git/worktrees/x)
// maps to <repo>; a subdirectory resolves through the nearest .git above it.
// With nothing found it returns the cleaned input.
func RepositoryDirOf(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	clean := filepath.Clean(dir)
	if r, ok := cutWorktreeGitDir(clean); ok {
		return r
	}
	cur := clean
	for {
		gp := filepath.Join(cur, ".git")
		if fi, err := os.Lstat(gp); err == nil {
			if fi.IsDir() {
				return cur
			}
			if b, err := os.ReadFile(gp); err == nil {
				line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
				if rest, ok := strings.CutPrefix(line, "gitdir:"); ok {
					target := strings.TrimSpace(rest)
					if !filepath.IsAbs(target) {
						target = filepath.Join(cur, target)
					}
					if r, ok := cutWorktreeGitDir(filepath.Clean(target)); ok {
						return r
					}
				}
			}
			return cur
		}
		next := filepath.Dir(cur)
		if next == cur {
			return clean
		}
		cur = next
	}
}

// cutWorktreeGitDir returns the repository root for a path containing
// /.git/worktrees/ by cutting at the .git segment.
func cutWorktreeGitDir(p string) (string, bool) {
	sep := string(filepath.Separator)
	marker := sep + ".git" + sep + "worktrees" + sep
	if i := strings.Index(p+sep, marker); i > 0 {
		return p[:i], true
	}
	return "", false
}

// AutomatonProjectIndex precomputes project membership once per roll-up.
type AutomatonProjectIndex struct {
	byPRD map[string]AutomatonProjectMembership
}

// AutomatonProjectMembership is one resolved membership.
type AutomatonProjectMembership struct {
	Project string
	Source  string
}

// NewAutomatonProjectIndex resolves every Automaton once. Automations are
// scanned once to build the member lookup.
func NewAutomatonProjectIndex(prds []*PRD, automations []*Automation, projects map[string]string) *AutomatonProjectIndex {
	ids := make(map[string]*PRD, len(prds))
	for _, p := range prds {
		if p != nil {
			ids[p.ID] = p
		}
	}
	holder := map[string]*Automation{}
	byAutomationID := map[string]*Automation{}
	for _, a := range automations {
		if a == nil {
			continue
		}
		byAutomationID[a.ID] = a
		for _, m := range a.Members {
			if m.PRDID != "" {
				if _, ok := holder[m.PRDID]; !ok {
					holder[m.PRDID] = a
				}
			}
		}
	}
	automationOf := func(p *PRD) *Automation {
		if p.AutomationID != "" {
			if a := byAutomationID[p.AutomationID]; a != nil {
				return a
			}
		}
		return holder[p.ID]
	}
	idx := &AutomatonProjectIndex{byPRD: make(map[string]AutomatonProjectMembership, len(prds))}
	for _, p := range prds {
		if p == nil {
			continue
		}
		name, src := ResolveAutomatonProject(p, func(id string) *PRD { return ids[id] }, automationOf, projects)
		idx.byPRD[p.ID] = AutomatonProjectMembership{Project: name, Source: src}
	}
	return idx
}

// Of returns the membership of an Automaton ID (source "none" if unknown).
func (x *AutomatonProjectIndex) Of(prdID string) AutomatonProjectMembership {
	if x == nil {
		return AutomatonProjectMembership{Source: ProjectSourceNone}
	}
	m, ok := x.byPRD[prdID]
	if !ok {
		return AutomatonProjectMembership{Source: ProjectSourceNone}
	}
	return m
}
