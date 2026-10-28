// BL24 — JSON-backed PRD/Story/Task/Learning persistence.
//
// One JSON-lines file per kind under <data_dir>/autonomous/. The
// store loads everything on construction (small dataset; if PRDs
// grow into the thousands, swap for SQLite via internal/memory).

package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store is the in-memory + JSONL-backed PRD/Story/Task/Learning store.
type Store struct {
	mu        sync.Mutex
	dir       string
	prds      map[string]*PRD
	stories   map[string]*Story // keyed by Story.ID
	tasks     map[string]*Task  // keyed by Task.ID
	learnings []Learning
	// BL303 S2 — guardrail profiles
	profiles map[string]*GuardrailProfile
	// BL410 Phase 1 — retry profiles
	retryProfiles map[string]*RetryProfile
}

// NewStore opens (or creates) the JSONL files under dir/autonomous/.
func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "autonomous")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}
	s := &Store{
		dir:           root,
		prds:          map[string]*PRD{},
		stories:       map[string]*Story{},
		tasks:         map[string]*Task{},
		profiles:      map[string]*GuardrailProfile{},
		retryProfiles: map[string]*RetryProfile{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// CreatePRD generates an ID, persists, and returns the new PRD.
func (s *Store) CreatePRD(spec, projectDir, backend, model string, effort Effort) (*PRD, error) {
	return s.CreatePRDWithParent(spec, projectDir, backend, model, effort, "", "", 0)
}

// CreatePRDWithParent (BL191 Q4, v5.9.0) is the recursion-aware sibling
// of CreatePRD. parentPRDID + parentTaskID are empty strings + depth=0
// for root PRDs; child PRDs spawned by Task.SpawnPRD set them so the
// genealogy tree is queryable and the recursion-depth check has data.
func (s *Store) CreatePRDWithParent(spec, projectDir, backend, model string, effort Effort, parentPRDID, parentTaskID string, depth int) (*PRD, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("spec required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prd := &PRD{
		ID:           newID(),
		Spec:         spec,
		ProjectDir:   projectDir,
		Backend:      backend,
		Model:        model,
		Effort:       effort,
		Status:       PRDDraft,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		ParentPRDID:  parentPRDID,
		ParentTaskID: parentTaskID,
		Depth:        depth,
	}
	s.prds[prd.ID] = prd
	if err := s.persist(); err != nil {
		return nil, err
	}
	return prd, nil
}

// ListChildPRDs returns every PRD whose ParentPRDID == prdID, oldest-
// first. BL191 Q4 (v5.9.0). Used by GET /api/autonomous/prds/{id}/children
// and the chat verb to surface the genealogy tree.
func (s *Store) ListChildPRDs(prdID string) []*PRD {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*PRD
	for _, p := range s.prds {
		if p.ParentPRDID == prdID {
			out = append(out, p)
		}
	}
	// Stable order: oldest first.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.Before(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// DeletePRD (v5.19.0) hard-deletes a PRD from the in-memory map +
// JSONL store. Used by the PWA + CLI to permanently remove a
// cancelled / completed / archived PRD that the operator no longer
// wants cluttering the list. Distinct from Cancel which only flips
// Status to PRDCancelled. Removes children too (recursion-aware).
func (s *Store) DeletePRD(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.prds[id]; !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	// Walk descendants and remove them too — orphan child PRDs would
	// be confusing with a dangling parent_prd_id pointer.
	toRemove := []string{id}
	for changed := true; changed; {
		changed = false
		for cid, c := range s.prds {
			for _, p := range toRemove {
				if c.ParentPRDID == p {
					already := false
					for _, q := range toRemove {
						if q == cid {
							already = true
							break
						}
					}
					if !already {
						toRemove = append(toRemove, cid)
						changed = true
					}
				}
			}
		}
	}
	for _, p := range toRemove {
		delete(s.prds, p)
		// Drop child Story / Task index entries to keep the in-memory
		// indexes consistent with the JSONL.
		for sid, st := range s.stories {
			if st.PRDID == p {
				delete(s.stories, sid)
			}
		}
		for tid, t := range s.tasks {
			if t.PRDID == p {
				delete(s.tasks, tid)
			}
		}
	}
	return s.persist()
}

// UpdatePRDFields (v5.19.0) edits PRD-level fields (Title, Spec) on
// a non-running PRD. Status, ParentPRDID, ChildPRDID, Depth, IsTemplate
// are NOT editable — those are managed by the lifecycle. Returns the
// updated PRD. Refuses to edit a PRD that's currently `running` to
// avoid racing the executor walk.
func (s *Store) UpdatePRDFields(id, title, spec string) (*PRD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prds[id]
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	if p.Status == PRDRunning {
		return nil, fmt.Errorf("prd %q is running; cancel first", id)
	}
	if title != "" {
		p.Title = title
	}
	if spec != "" {
		p.Spec = spec
	}
	p.UpdatedAt = time.Now()
	if err := s.persist(); err != nil {
		return nil, err
	}
	return p, nil
}

// SavePRD upserts.
//
// BL291 (v5.5.0) — trim PRD.Decisions to the most recent maxDecisionsPerPRD
// rows before persisting. Without the cap a PRD that's been re-decomposed
// + re-run hundreds of times grows multi-MB Decisions slices that bloat
// every JSONL row + the in-memory store snapshot the loop reads.
func (s *Store) SavePRD(p *PRD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.UpdatedAt = time.Now()
	p.Decisions = trimDecisions(p.Decisions)
	p.Rev++
	s.prds[p.ID] = p
	return s.persist()
}

// ErrPRDStale (BL451) is returned by CommitPRDIfRev when the PRD's Rev
// moved since the caller read it, or when mutate declined the change.
var ErrPRDStale = errors.New("prd modified concurrently")

// CommitPRDIfRev (BL451) applies mutate to the stored PRD and persists
// it only if p.Rev still equals expectedRev, holding s.mu throughout.
// If mutate returns false nothing is persisted and ErrPRDStale is
// returned, letting callers re-validate invariants under the lock.
// GetPRD returns the shared stored pointer, so mutate must be the only
// place the guarded change is applied.
func (s *Store) CommitPRDIfRev(id string, expectedRev int, mutate func(p *PRD) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prds[id]
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	if p.Rev != expectedRev {
		return ErrPRDStale
	}
	if !mutate(p) {
		return ErrPRDStale
	}
	p.UpdatedAt = time.Now()
	p.Decisions = trimDecisions(p.Decisions)
	p.Rev++
	return s.persist()
}

// PRDRev (BL451) returns the PRD's current revision under the lock.
func (s *Store) PRDRev(id string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prds[id]
	if !ok {
		return 0, false
	}
	return p.Rev, true
}

// GetPRD by ID; returns false on miss.
func (s *Store) GetPRD(id string) (*PRD, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prds[id]
	return p, ok
}

// ListPRDs returns all PRDs newest-first.
func (s *Store) ListPRDs() []*PRD {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*PRD, 0, len(s.prds))
	for _, p := range s.prds {
		out = append(out, p)
	}
	// Newest first.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// storiesSnapshot returns copies of the stories the store holds for prdID,
// as persisted (used to verify a decompose actually stored its stories).
func (s *Store) storiesSnapshot(prdID string) []Story {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Story
	for _, st := range s.stories {
		if st.PRDID == prdID {
			out = append(out, *st)
		}
	}
	return out
}

// SetStories replaces a PRD's story list (post-decompose).
func (s *Store) SetStories(prdID string, stories []Story) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	// Re-id each story + child task to ensure uniqueness across PRDs.
	// (local mutation only — the store maps are untouched until the full
	// validation pass below succeeds).
	for i := range stories {
		if stories[i].ID == "" {
			stories[i].ID = newID()
		}
		stories[i].PRDID = prdID
		stories[i].CreatedAt = time.Now()
		stories[i].UpdatedAt = time.Now()
		// B95: LLM output may include a "status" field (e.g. "running") that is
		// not a valid initial StoryStatus. Force all freshly-set stories to
		// StoryPending so the executor's transition guard always applies cleanly.
		stories[i].Status = StoryPending
		for j := range stories[i].Tasks {
			if stories[i].Tasks[j].ID == "" {
				stories[i].Tasks[j].ID = newID()
			}
			stories[i].Tasks[j].StoryID = stories[i].ID
			stories[i].Tasks[j].PRDID = prdID
			stories[i].Tasks[j].CreatedAt = time.Now()
			stories[i].Tasks[j].UpdatedAt = time.Now()
		}
	}
	// v8.36.5 — resolve DependsOn from decompose's LLM-output titles to real
	// IDs, now that every story/task above has one. DependsOn is documented
	// as "other Task/Story IDs" and every consumer (topoSort, the
	// sequential/concurrent executor paths) compares it against IDs — but
	// the decompose LLM can only ever reference a sibling by its title (or a
	// positional code; IDs don't exist until this exact function assigns
	// them), so an unresolved DependsOn entry silently never matched
	// anything. Found live on PRD a2833a5e: this meant dependency ordering
	// had never actually been enforced — topoSort treated every task as
	// having no real dependency edges, executing in ID-sort order, which
	// happens to approximate decompose's own sequence closely enough that
	// the gap went unnoticed until a cancelled task's dependent proceeded
	// without it. Resolution itself lives in resolveDependsOn (extracted
	// v8.36.6 for reuse by RepairDependsOn).
	//
	// Full validation pass BEFORE mutating the store maps: if any entry is
	// unresolvable or the resolved plan contains a dependency cycle, reject
	// the whole plan and persist nothing.
	if err := resolveDependsOn(stories, s.stories, s.tasks); err != nil {
		return err
	}
	for i := range stories {
		s.stories[stories[i].ID] = &stories[i]
		for j := range stories[i].Tasks {
			s.tasks[stories[i].Tasks[j].ID] = &stories[i].Tasks[j]
		}
	}
	prd.Story = stories
	prd.UpdatedAt = time.Now()
	return s.persist()
}

// dependsOnReStoryTask matches a positional "sN-tM" reference (the Mth task
// of the Nth story, 1-based)
var dependsOnReStoryTask = regexp.MustCompile(`^s([1-9][0-9]*)-t([1-9][0-9]*)$`)

// dependsOnReStory matches a positional "sN" reference (the Nth story, 1-based)
var dependsOnReStory = regexp.MustCompile(`^s([1-9][0-9]*)$`)

// dependsOnRePlainNumber matches a bare numeric reference (the Nth story, 1-based)
var dependsOnRePlainNumber = regexp.MustCompile(`^[1-9][0-9]*$`)

// resolveDependsOn validates and rewrites each DependsOn entry of the given
// stories (which must already have real IDs assigned) into a real ID, and
// rejects the plan if any entry cannot be resolved or if the resolved plan
// contains a dependency cycle entirely within its own IDs.
//
// Each entry is trimmed of surrounding whitespace, then resolved in this
// precedence:
//  1. already a known story ID (story-level entry) or task ID (task-level
//     entry) in the store's maps — kept as-is;
//  2. a sibling story title (story-level) or sibling task title (task-level)
//     from the same plan — that sibling's real ID. Task-level entries match
//     task titles only, as before. (Interpretation documented here for the
//     live-observed case on PRD a2833a5e, where decompose's LLM output came
//     back with sibling titles rather than IDs.)
//  3. a positional code, 1-based against the order of the stories slice
//     being stored: "sN-tM" is the Mth task of the Nth story, "sN" is the
//     Nth story, and bare number N is the Nth story at BOTH the story and
//     the task level (a task may depend on an earlier story's completion);
//  4. anything else — including positional codes out of range for this plan
//     (N or M below 1, or past the last story/task) — is UNSRESOLVED and
//     the whole plan is rejected with an error naming the exact raw entry
//     and its owner story/task.
//
// After resolution, edges whose both endpoints are within this plan's own
// story+task ID set are checked for cycles; edges pointing outside the
// plan are ignored. A cycle rejects the plan with the cycle members named.
//
// Shared by SetStories (fresh decompose output — a rejection means nothing
// was ever stored or persisted) and RepairDependsOn (existing PRDs whose
// SetStories call predates the v8.36.5 fix and are still stuck with raw
// titles).
func resolveDependsOn(stories []Story, storyByID map[string]*Story, taskByID map[string]*Task) error {
	taskIDByTitle := make(map[string]string, len(taskByID))
	storyIDByTitle := make(map[string]string, len(stories))
	for i := range stories {
		storyIDByTitle[stories[i].Title] = stories[i].ID
		for j := range stories[i].Tasks {
			taskIDByTitle[stories[i].Tasks[j].Title] = stories[i].Tasks[j].ID
		}
	}

	errUnresolvable := func(raw, owner string) error {
		return fmt.Errorf("unresolvable depends_on entry %q on %s", raw, owner)
	}

	// resolve maps one raw entry to its real ID, applying the precedence
	// described above; storyLevel selects the known-ID map and the
	// sibling-title map, positional codes work at both levels.
	resolve := func(raw string, storyLevel bool) (string, bool) {
		e := strings.TrimSpace(raw)
		if storyLevel {
			if _, ok := storyByID[e]; ok {
				return e, true
			}
			if id, ok := storyIDByTitle[e]; ok {
				return id, true
			}
		} else {
			if _, ok := taskByID[e]; ok {
				return e, true
			}
			if id, ok := taskIDByTitle[e]; ok {
				return id, true
			}
		}
		return positionalDependsOn(e, stories)
	}

	for i := range stories {
		for k, dep := range stories[i].DependsOn {
			id, ok := resolve(dep, true)
			if !ok {
				return errUnresolvable(dep, "story "+quote(stories[i].Title))
			}
			stories[i].DependsOn[k] = id
		}
		for j := range stories[i].Tasks {
			for k, dep := range stories[i].Tasks[j].DependsOn {
				id, ok := resolve(dep, false)
				if !ok {
					return errUnresolvable(dep, "task "+quote(stories[i].Tasks[j].Title))
				}
				stories[i].Tasks[j].DependsOn[k] = id
			}
		}
	}
	if cyc := dependsOnCycle(stories); cyc != "" {
		return fmt.Errorf("depends_on cycle: %s", cyc)
	}
	return nil
}

// quote wraps a name for use inside an error message.
func quote(s string) string { return fmt.Sprintf("%q", s) }

// positionalDependsOn resolves a positional code ("sN-tM" -> the Mth task
// of the Nth story, "sN" or bare number N -> the Nth story) against the
// stored story order; (ok) is false when the code is out of range.
func positionalDependsOn(ref string, stories []Story) (string, bool) {
	if m := dependsOnReStoryTask.FindStringSubmatch(ref); m != nil {
		n, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if n >= 1 && n <= len(stories) && mm <= len(stories[n-1].Tasks) {
			return stories[n-1].Tasks[mm-1].ID, true
		}
		return "", false
	}
	if m := dependsOnReStory.FindStringSubmatch(ref); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n >= 1 && n <= len(stories) {
			return stories[n-1].ID, true
		}
		return "", false
	}
	if dependsOnRePlainNumber.MatchString(ref) {
		n, err := strconv.Atoi(ref)
		if err == nil && n >= 1 && n <= len(stories) {
			return stories[n-1].ID, true
		}
	}
	return "", false
}

// dependsOnCycle returns "A -> B -> A" describing any dependency cycle
// entirely within this plan's own story+task IDs, or "" if there is none.
// Edges whose target is outside the plan's ID set are ignored: after
// resolution every in-plan entry is a real ID, so a missing match means the
// edge points at something outside this plan and can't form a cycle among
// in-plan members.
func dependsOnCycle(stories []Story) string {
	inPlan := make(map[string]bool)
	adj := make(map[string][]string)
	for i := range stories {
		inPlan[stories[i].ID] = true
		for j := range stories[i].Tasks {
			inPlan[stories[i].Tasks[j].ID] = true
		}
	}
	addEdge := func(from, to string) {
		if inPlan[to] {
			adj[from] = append(adj[from], to)
		}
	}
	for i := range stories {
		for _, d := range stories[i].DependsOn {
			addEdge(stories[i].ID, d)
		}
		for j := range stories[i].Tasks {
			for _, d := range stories[i].Tasks[j].DependsOn {
				addEdge(stories[i].Tasks[j].ID, d)
			}
		}
	}

	const (
		white, gray, black = 0, 1, 2
	)
	color := make(map[string]int)
	var stack []string
	var cycle []string
	var dfs func(n string) bool
	dfs = func(n string) bool {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range adj[n] {
			switch color[m] {
			case white:
				if dfs(m) {
					return true
				}
			case gray:
				start := 0
				for k, v := range stack {
					if v == m {
						start = k
						break
					}
				}
				cycle = append(append([]string{}, stack[start:]...), m)
				return true
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	for i := range stories {
		if color[stories[i].ID] == white && dfs(stories[i].ID) {
			return strings.Join(cycle, " -> ")
		}
		for j := range stories[i].Tasks {
			id := stories[i].Tasks[j].ID
			if color[id] == white && dfs(id) {
				return strings.Join(cycle, " -> ")
			}
		}
	}
	return ""
}

// RepairDependsOn (v8.36.6) re-runs resolveDependsOn against a PRD's
// already-stored stories/tasks. For PRDs whose SetStories call predates the
// v8.36.5 fix, DependsOn entries are still stuck as raw titles and were
// never actually enforced — this repairs an existing PRD in place instead
// of losing all progress via reset_to_draft + a fresh decompose. If an
// entry can no longer be resolved, or the stored plan is cyclic, the error
// is returned and nothing is persisted.
func (s *Store) RepairDependsOn(prdID string) (*PRD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := resolveDependsOn(prd.Story, s.stories, s.tasks); err != nil {
		return nil, err
	}
	prd.UpdatedAt = time.Now()
	if err := s.persist(); err != nil {
		return nil, err
	}
	return prd, nil
}

// SaveTask updates one task's state.
func (s *Store) SaveTask(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.UpdatedAt = time.Now()
	s.tasks[t.ID] = t
	return s.persist()
}

// GetTask by ID.
func (s *Store) GetTask(id string) (*Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	return t, ok
}

// TaskSnapshot returns a value copy of one task taken under the store
// lock. Safe for concurrent readers: unlike GetTask's shared pointer,
// whose fields the executor mutates (status/wait transitions) while a
// run is in flight, the returned copy is decoupled from it. Callers
// that inspect task fields while a Manager is running should use this.
func (s *Store) TaskSnapshot(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	return *t, true
}

// MutateTask applies f to one canonical task under the store lock and
// persists the result. Status/wait mutations made this way are
// synchronized with TaskSnapshot reads from other goroutines, unlike
// direct field writes on GetTask's shared pointer (a pattern that is
// race-unsafe the moment a concurrent reader — the API server polls
// task state during runs — is involved).
func (s *Store) MutateTask(id string, f func(*Task)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	f(t)
	t.UpdatedAt = time.Now()
	_ = s.persist()
	return true
}

// reindexPRDLocked rebuilds the s.stories/s.tasks lookup maps for every
// story/task currently in prd.Story. Callers must hold s.mu. Required after
// any append/remove on prd.Story or a nested Tasks slice: Go's append can
// reallocate the backing array, which would silently orphan any pointer
// previously stored in s.stories/s.tasks (SetStories only ever populates
// these once from a freshly-decoded slice, so this reallocation hazard was
// never hit before AddStory/AddTask/RemoveStory/RemoveTask started
// mutating an existing PRD's slices incrementally).
func (s *Store) reindexPRDLocked(prd *PRD) {
	for i := range prd.Story {
		s.stories[prd.Story[i].ID] = &prd.Story[i]
		for j := range prd.Story[i].Tasks {
			s.tasks[prd.Story[i].Tasks[j].ID] = &prd.Story[i].Tasks[j]
		}
	}
}

// AddStory appends a new, empty (no tasks yet) story to a PRD. Operator-
// requested: structural edits (add/remove a story or task) without having
// to re-run decompose and hand the whole structure back to the LLM.
func (s *Store) AddStory(prdID, title, description string) (*Story, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	story := Story{
		ID:          newID(),
		PRDID:       prdID,
		Title:       title,
		Description: description,
		Status:      StoryPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	prd.Story = append(prd.Story, story)
	prd.UpdatedAt = time.Now()
	s.reindexPRDLocked(prd)
	if err := s.persist(); err != nil {
		return nil, err
	}
	return &prd.Story[len(prd.Story)-1], nil
}

// RemoveStory deletes a story and all of its tasks from a PRD.
func (s *Store) RemoveStory(prdID, storyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	idx := -1
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	for _, t := range prd.Story[idx].Tasks {
		delete(s.tasks, t.ID)
	}
	delete(s.stories, storyID)
	prd.Story = append(prd.Story[:idx], prd.Story[idx+1:]...)
	prd.UpdatedAt = time.Now()
	s.reindexPRDLocked(prd)
	return s.persist()
}

// AddTask appends a new task to an existing story.
func (s *Store) AddTask(prdID, storyID, title, spec string, dependsOn []string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if len(dependsOn) > 0 {
		existing := map[string]bool{}
		for _, st := range prd.Story {
			for _, t := range st.Tasks {
				existing[t.ID] = true
			}
		}
		for _, dep := range dependsOn {
			if !existing[dep] {
				return nil, fmt.Errorf("depends_on task %q not found in prd %q", dep, prdID)
			}
		}
	}
	for i := range prd.Story {
		if prd.Story[i].ID != storyID {
			continue
		}
		task := Task{
			ID:        newID(),
			StoryID:   storyID,
			PRDID:     prdID,
			Title:     title,
			Spec:      spec,
			DependsOn: dependsOn,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		prd.Story[i].Tasks = append(prd.Story[i].Tasks, task)
		prd.Story[i].UpdatedAt = time.Now()
		prd.UpdatedAt = time.Now()
		s.reindexPRDLocked(prd)
		if err := s.persist(); err != nil {
			return nil, err
		}
		return &prd.Story[i].Tasks[len(prd.Story[i].Tasks)-1], nil
	}
	return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
}

// RemoveTask deletes one task from a story.
func (s *Store) RemoveTask(prdID, storyID, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	for i := range prd.Story {
		if prd.Story[i].ID != storyID {
			continue
		}
		idx := -1
		for j := range prd.Story[i].Tasks {
			if prd.Story[i].Tasks[j].ID == taskID {
				idx = j
				break
			}
		}
		if idx == -1 {
			return fmt.Errorf("task %q not found in story %q", taskID, storyID)
		}
		delete(s.tasks, taskID)
		prd.Story[i].Tasks = append(prd.Story[i].Tasks[:idx], prd.Story[i].Tasks[idx+1:]...)
		prd.Story[i].UpdatedAt = time.Now()
		prd.UpdatedAt = time.Now()
		s.reindexPRDLocked(prd)
		return s.persist()
	}
	return fmt.Errorf("story %q not found in prd %q", storyID, prdID)
}

// maxLearnings (BL292, v5.6.0) — cap on the in-memory + JSONL learnings
// store. BL57 KG learnings get appended on every PRD task completion;
// over a long-lived daemon the slice + the rewrite-everything persist
// pattern blows up. Trim keeps the most-recent N — older learnings are
// already mirrored into episodic memory + the KG so the autonomous
// store doesn't need to be the source of truth.
const maxLearnings = 1000

// AddLearning appends one learning.
func (s *Store) AddLearning(l Learning) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.ID == "" {
		l.ID = newID()
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	s.learnings = append(s.learnings, l)
	if len(s.learnings) > maxLearnings {
		s.learnings = s.learnings[len(s.learnings)-maxLearnings:]
	}
	return s.persist()
}

// ListLearnings returns all stored learnings.
func (s *Store) ListLearnings() []Learning {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Learning, len(s.learnings))
	copy(out, s.learnings)
	return out
}

// load reads JSONL files into memory. Missing files are not errors.
// SaveGuardrailProfile upserts a profile (BL303 S2).
func (s *Store) SaveGuardrailProfile(p *GuardrailProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[p.ID] = p
	return s.persist()
}

// GetGuardrailProfile returns a profile by ID (BL303 S2).
func (s *Store) GetGuardrailProfile(id string) (*GuardrailProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[id]
	return p, ok
}

// ListGuardrailProfiles returns all profiles newest-first (BL303 S2).
func (s *Store) ListGuardrailProfiles() []*GuardrailProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*GuardrailProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	// Sort newest-first.
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// DeleteGuardrailProfile removes a profile by ID (BL303 S2).
func (s *Store) DeleteGuardrailProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.profiles[id]; !ok {
		return fmt.Errorf("guardrail profile %q not found", id)
	}
	delete(s.profiles, id)
	return s.persist()
}

// SaveRetryProfile upserts a profile (BL410 Phase 1).
func (s *Store) SaveRetryProfile(p *RetryProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryProfiles[p.ID] = p
	return s.persist()
}

// GetRetryProfile returns a profile by ID (BL410 Phase 1).
func (s *Store) GetRetryProfile(id string) (*RetryProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.retryProfiles[id]
	return p, ok
}

// ListRetryProfiles returns all profiles newest-first (BL410 Phase 1).
func (s *Store) ListRetryProfiles() []*RetryProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*RetryProfile, 0, len(s.retryProfiles))
	for _, p := range s.retryProfiles {
		out = append(out, p)
	}
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// DeleteRetryProfile removes a profile by ID (BL410 Phase 1).
func (s *Store) DeleteRetryProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.retryProfiles[id]; !ok {
		return fmt.Errorf("retry profile %q not found", id)
	}
	delete(s.retryProfiles, id)
	return s.persist()
}

func (s *Store) load() error {
	if err := loadJSONL(filepath.Join(s.dir, "prds.jsonl"), func(line []byte) error {
		var p PRD
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		p.Status = NormalizePRDStatus(p.Status)
		migrateGuidedMode(&p) // BL406 Phase 5 — B111's real fix
		s.prds[p.ID] = &p
		// rebuild story/task indexes
		for i := range p.Story {
			st := &p.Story[i]
			s.stories[st.ID] = st
			for j := range st.Tasks {
				s.tasks[st.Tasks[j].ID] = &st.Tasks[j]
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s.clearStaleProgress() // boot-time safety re-check of the progress invariant
	if err := loadJSONL(filepath.Join(s.dir, "learnings.jsonl"), func(line []byte) error {
		var l Learning
		if err := json.Unmarshal(line, &l); err != nil {
			return err
		}
		s.learnings = append(s.learnings, l)
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONL(filepath.Join(s.dir, "guardrail_profiles.jsonl"), func(line []byte) error {
		var p GuardrailProfile
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		s.profiles[p.ID] = &p
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONL(filepath.Join(s.dir, "retry_profiles.jsonl"), func(line []byte) error {
		var p RetryProfile
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		s.retryProfiles[p.ID] = &p
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// clearStaleProgress enforces the task-progress invariant at persist time:
// Progress and LoopingWarning are only ever meaningful while the task is in
// progress (in_progress or verifying — the two states the worker can be
// actively running in). Any terminal or waiting task must carry neither,
// and centralizing the check here means it holds no matter which save path
// ran — SavePRD, SaveTask, MutateTask, the executor's terminal transitions,
// the coordinator's auto-fail, resets, cancels — instead of every caller
// having to remember to clear the field.
//
// Callers must hold s.mu. The mutation is deliberate and happens before the
// serialize step, so both the on-disk JSONL and the shared in-memory task
// pointer (which GetPRD/BuildTasksView read straight off) end up clean.
func (s *Store) clearStaleProgress() {
	for _, t := range s.tasks {
		if t == nil || progressActive(t.Status) {
			continue
		}
		if t.Progress != nil || t.LoopingWarning != "" {
			t.ClearProgress()
		}
	}
}

// persist writes the in-memory state back as JSON-lines (full rewrite).
func (s *Store) persist() error {
	s.clearStaleProgress()
	if err := writeJSONL(filepath.Join(s.dir, "prds.jsonl"), len(s.prds), func(emit func(any) error) error {
		for _, p := range s.prds {
			if err := emit(p); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(s.dir, "learnings.jsonl"), len(s.learnings), func(emit func(any) error) error {
		for _, l := range s.learnings {
			if err := emit(l); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(s.dir, "guardrail_profiles.jsonl"), len(s.profiles), func(emit func(any) error) error {
		for _, p := range s.profiles {
			if err := emit(p); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return writeJSONL(filepath.Join(s.dir, "retry_profiles.jsonl"), len(s.retryProfiles), func(emit func(any) error) error {
		for _, p := range s.retryProfiles {
			if err := emit(p); err != nil {
				return err
			}
		}
		return nil
	})
}

func loadJSONL(path string, on func([]byte) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := on([]byte(line)); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONL(path string, _ int, fn func(emit func(any) error) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	enc := json.NewEncoder(f)
	return fn(func(v any) error { return enc.Encode(v) })
}

// newID returns an 8-hex string (store.go IDs throughout the package are
// documented as "8-hex").
//
// The IDs are not just opaque labels: the executor falls back to ID
// lexicographic order when scheduling dependency-free tasks (executor.go
// topoSort: "executing in ID-sort order"), so creation order must equal ID
// order — IDs have to be monotonic.
//
// The old implementation was fmt.Sprintf("%08x", uint32(UnixNano)) — the
// low 32 bits of the nanosecond clock, which wrapped every 2^32 ns ≈
// 4.295s. Two objects created ~4.3s (or 8.6s, 12.9s, ...) apart therefore
// got the SAME id, and worse, an object created earlier could get a
// LARGER id than one created later (across the wrap), reordering the
// ID-sort execution order. The current encoding is a 16-bit
// microsecond-window (increments every 65536 µs ≈ 65.5 ms, wrapping in
// ~280 years) + a 16-bit per-window counter that only ever moves
// forward. That is strictly monotonic within a process (up to a 1M ids/s
// sustained rate, after which the counter carries into the next window
// rather than colliding) and unique within a process; cross-process odds
// stay the usual 1/2^32, as with any 8-hex id scheme.
var (
	idMu         sync.Mutex
	idLastBucket uint32
	idCounter    uint32
)

func newID() string {
	idMu.Lock()
	defer idMu.Unlock()
	bucket := uint32(time.Now().UnixNano() >> 16) // µs since epoch, windowed to 65536 µs
	if bucket > idLastBucket {
		idLastBucket = bucket
		idCounter = 1
	} else {
		// First call, or clock did not advance past the last window, or the
		// clock stepped backwards: keep the monotonic sequence going instead
		// of issuing a smaller id.
		idCounter++
		if idCounter >= 1<<16 { // sustained >1M ids/s: carry into next window, don't collide
			idCounter = 1
			idLastBucket++
			if bucket > idLastBucket {
				idLastBucket = bucket
				idCounter = 1
			}
		}
	}
	return fmt.Sprintf("%08x", (idLastBucket<<16)|idCounter)
}
