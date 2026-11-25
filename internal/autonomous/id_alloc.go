// Id allocation: an id is not issued while anything still holds it.
//
// Why a draw has to be checked. newID's high half is bits 16..31 of the
// nanosecond clock: 65,536 windows of 65.536 µs, so it repeats every 2^32 ns
// (about 4.29 s). The low half counts ids within one window and is 0001 for
// most ids. Most ids are therefore one of about 65,536 values, and one new id
// matches any one earlier ...0001 id with a chance of about one in 65,536.
// That adds up against every record ever issued: with 830 such records, one
// more unchecked create had about a 1.3% chance of overwriting one of them, and
// over the 830 creates about five overwrites were to be expected (the chance of
// at least one was above 99%). Stories and tasks share the same values and are
// far more numerous.
//
// "Held" means a record in the store that issues the id, a record or archive
// file on disk, or work an earlier record left behind: a branch under
// automaton/ or a folder under the worktree folder. newID also never repeats
// within one process, so an id assigned to a plan that is not stored yet is not
// issued twice.

package autonomous

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// maxIDDraws bounds the retries; it is two full runs of the counter.
	maxIDDraws = 1 << 17

	leftoverGitTimeout = 20 * time.Second

	taskBranchMarker  = "--tasks-"
	taskFoldersSuffix = "-tasks"
)

var errNoUnusedID = errors.New("could not draw an unused id")

// drawUnusedID calls gen until it returns an id that taken rejects no more.
func drawUnusedID(gen func() string, taken func(string) bool) (string, error) {
	for i := 0; i < maxIDDraws; i++ {
		if id := gen(); id != "" && !taken(id) {
			return id, nil
		}
	}
	return "", errNoUnusedID
}

// idLeftover is one branch or worktree folder that carries an id in its name.
type idLeftover struct {
	// name is the branch name after "automaton/", or the folder name.
	name string
	// full is the ref name or the folder path.
	full string
	// repo is the repository of a branch; empty for a folder.
	repo string
	// task marks a task branch or task folder, whose name ends in the task id.
	task bool
}

// ownedBy reports whether the leftover is named for the record id:
// automaton/<id>*, or a folder <id>*.
func (l idLeftover) ownedBy(id string) bool { return strings.HasPrefix(l.name, id) }

type idLeftovers []idLeftover

// holds reports whether any leftover would collide with a record given id.
func (ls idLeftovers) holds(id string) bool {
	for _, l := range ls {
		if l.ownedBy(id) || (l.task && strings.HasSuffix(l.name, "-"+id)) {
			return true
		}
	}
	return false
}

// idRoots are the places leftover work can be.
type idRoots struct {
	worktreeDir string
	repos       []string
}

// scanIDLeftovers lists the worktree folders and the automaton branches. It
// only reads. A repository it cannot list is logged and skipped, so a broken
// repository does not stop every create.
func scanIDLeftovers(r idRoots) idLeftovers {
	var out idLeftovers
	folders := func(dir string, task bool) []os.DirEntry {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("[autonomous] id check: read %s: %v (folders there were not checked)", dir, err)
			}
			return nil
		}
		var kept []os.DirEntry
		for _, e := range entries {
			if e.Type().IsRegular() {
				continue
			}
			kept = append(kept, e)
			out = append(out, idLeftover{name: e.Name(), full: filepath.Join(dir, e.Name()), task: task})
		}
		return kept
	}
	if r.worktreeDir != "" {
		for _, e := range folders(r.worktreeDir, false) {
			if strings.HasSuffix(e.Name(), taskFoldersSuffix) {
				folders(filepath.Join(r.worktreeDir, e.Name()), true)
			}
		}
		folders(savedVerifyScriptsBase(r.worktreeDir), false)
	}
	seen := map[string]bool{}
	for _, repo := range r.repos {
		repo = strings.TrimSpace(repo)
		if repo == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		if st, err := os.Stat(repo); err != nil || !st.IsDir() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), leftoverGitTimeout)
		refs, err := runGitInCtx(ctx, repo, "for-each-ref", "--format=%(refname)")
		cancel()
		if err != nil {
			if !strings.Contains(refs, "not a git repository") {
				log.Printf("[autonomous] id check: list branches in %s: %v (branches there were not checked)", repo, err)
			}
			continue
		}
		for _, ref := range strings.Split(refs, "\n") {
			_, name, found := strings.Cut(strings.TrimSpace(ref), "/automaton/")
			if !found || !strings.HasPrefix(ref, "refs/") {
				continue
			}
			out = append(out, idLeftover{name: name, full: strings.TrimSpace(ref), repo: repo,
				task: strings.Contains(name, taskBranchMarker)})
		}
	}
	return out
}

// insideDir reports whether path is dir or lies under it.
func insideDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// knownIDRoots is the worktree folder and every repository that could hold a
// leftover branch: the configured base repository, the extra ones, and the
// project folder of every record. A project folder inside the worktree folder
// is a worktree of the base repository and is not listed again. Call it without
// s.mu held.
func (s *Store) knownIDRoots(extraRepos ...string) idRoots {
	if s.idRootsFn == nil {
		return idRoots{}
	}
	r := s.idRootsFn()
	repos := append(r.repos, extraRepos...)
	s.mu.Lock()
	for _, p := range s.prds {
		repos = append(repos, p.ProjectDir)
	}
	s.mu.Unlock()
	r.repos = nil
	for _, repo := range repos {
		if repo == "" || (r.worktreeDir != "" && insideDir(repo, r.worktreeDir)) {
			continue
		}
		r.repos = append(r.repos, repo)
	}
	return r
}

// idLeftovers scans for leftover work. Call it without s.mu held. A store no
// Manager wired has no roots and finds none.
func (s *Store) idLeftovers(extraRepos ...string) idLeftovers {
	return scanIDLeftovers(s.knownIDRoots(extraRepos...))
}

// idInUseLocked reports whether the store holds id for any kind of record, in
// memory or as a file. Callers hold s.mu.
func (s *Store) idInUseLocked(id string) bool {
	if s.prds[id] != nil || s.stories[id] != nil || s.tasks[id] != nil ||
		s.tags[id] != nil || s.profiles[id] != nil || s.retryProfiles[id] != nil {
		return true
	}
	for i := range s.learnings {
		if s.learnings[i].ID == id {
			return true
		}
	}
	name := recordName(id)
	for _, path := range []string{
		filepath.Join(s.recordsDir(), name+".json"),
		filepath.Join(s.recordsDir(), name+".files"),
		s.archiveFilePath(id),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// unusedIDLocked draws an id from gen that neither the store nor a leftover
// holds and that avoid does not list. Callers hold s.mu.
func (s *Store) unusedIDLocked(gen func() string, left idLeftovers, avoid map[string]bool) (string, error) {
	return drawUnusedID(gen, func(id string) bool {
		return avoid[id] || s.idInUseLocked(id) || left.holds(id)
	})
}

// unusedID is unusedIDLocked for callers that hold no lock.
func (s *Store) unusedID(gen func() string) (string, error) {
	left := s.idLeftovers()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unusedIDLocked(gen, left, nil)
}

// planIDs returns the ids a plan already carries, and whether any story or task
// still needs one.
func planIDs(stories []Story) (carried map[string]bool, missing bool) {
	carried = map[string]bool{}
	note := func(id string) {
		if id == "" {
			missing = true
			return
		}
		carried[id] = true
	}
	for i := range stories {
		note(stories[i].ID)
		for j := range stories[i].Tasks {
			note(stories[i].Tasks[j].ID)
		}
	}
	return carried, missing
}

// assignPlanIDs gives the id-less stories and tasks of a parsed plan an unused
// id, in place.
func (m *Manager) assignPlanIDs(stories []Story) {
	if _, missing := planIDs(stories); !missing {
		return
	}
	if m.store == nil {
		ensurePlanIDs(stories)
		return
	}
	left := m.store.idLeftovers()
	ensurePlanIDs(stories, func(id string) bool {
		m.store.mu.Lock()
		defer m.store.mu.Unlock()
		return m.store.idInUseLocked(id) || left.holds(id)
	})
}

// idRootsSnapshot is the part of the live config the id check reads. It is kept
// apart from m.cfg so the stores can read it without taking m.mu.
type idRootsSnapshot struct{ worktreeDir, baseRepo string }

func (m *Manager) setIDRootsSnapshot(cfg Config) {
	dir := strings.TrimSpace(cfg.WorktreeDir)
	if dir == "" {
		dir = defaultWorktreeDir(m.dataDir)
	}
	m.idRootsCfg.Store(&idRootsSnapshot{worktreeDir: dir, baseRepo: strings.TrimSpace(cfg.WorktreeBaseRepo)})
}

// managerIDRoots names the worktree folder, the base repository and the
// configured project folders.
func (m *Manager) managerIDRoots() idRoots {
	var r idRoots
	if snap := m.idRootsCfg.Load(); snap != nil {
		r.worktreeDir = snap.worktreeDir
		r.repos = append(r.repos, snap.baseRepo)
	}
	m.projectLink.mu.Lock()
	projects := m.projectLink.projects
	m.projectLink.mu.Unlock()
	if projects != nil {
		for _, p := range projects() {
			r.repos = append(r.repos, p.Dir)
		}
	}
	return r
}

// wireIDGuard makes the stores the Manager owns check leftover work before they
// issue an id.
func (m *Manager) wireIDGuard() {
	m.setIDRootsSnapshot(m.cfg)
	m.store.idRootsFn = m.managerIDRoots
	scan := func() idLeftovers { return m.store.idLeftovers() }
	m.automations.idLeftoversFn = scan
	m.pendingItems.idLeftoversFn = scan
	m.templates.idLeftoversFn = scan
}

// GuardRecordIDs makes a decision record store check leftover work before it
// issues an id. SetDecisionRecords calls it.
func (m *Manager) GuardRecordIDs(s *DecisionRecordStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idLeftoversFn = func() idLeftovers {
		if m.store == nil {
			return nil
		}
		return m.store.idLeftovers()
	}
}

// scanWith runs a store's wired scan; an unwired store finds nothing.
func scanWith(fn func() idLeftovers) idLeftovers {
	if fn == nil {
		return nil
	}
	return fn()
}

// newMemberID draws an Automation member id not listed in used and adds it.
func newMemberID(used map[string]bool) (string, error) {
	id, err := drawUnusedID(newAutomationID, func(id string) bool { return used[id] })
	if err == nil {
		used[id] = true
	}
	return id, err
}

// automationMemberIDs lists the member ids an Automation holds, including the
// inserts that wait for approval.
func automationMemberIDs(a *Automation) map[string]bool {
	used := map[string]bool{}
	for _, mem := range a.Members {
		used[mem.ID] = true
	}
	for _, p := range a.PendingInserts {
		used[p.ID] = true
	}
	return used
}
