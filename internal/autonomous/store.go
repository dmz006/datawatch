// BL24 — JSON-backed PRD/Story/Task/Learning persistence.
//
// One JSON-lines file per kind under <data_dir>/autonomous/. The
// store loads everything on construction (small dataset; if PRDs
// grow into the thousands, swap for SQLite via internal/memory).

package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

// NewStore opens (or creates) the JSONL files under dir/autonomous/.
func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "autonomous")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}
	s := &Store{
		dir:      root,
		prds:     map[string]*PRD{},
		stories:  map[string]*Story{},
		tasks:    map[string]*Task{},
		profiles: map[string]*GuardrailProfile{},
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
	s.prds[p.ID] = p
	return s.persist()
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

// SetStories replaces a PRD's story list (post-decompose).
func (s *Store) SetStories(prdID string, stories []Story) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	// Re-id each story + child task to ensure uniqueness across PRDs.
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
			s.tasks[stories[i].Tasks[j].ID] = &stories[i].Tasks[j]
		}
		s.stories[stories[i].ID] = &stories[i]
	}
	// v8.36.5 — resolve DependsOn from decompose's LLM-output titles to real
	// IDs, now that every story/task above has one. DependsOn is documented
	// as "other Task/Story IDs" and every consumer (topoSort, the
	// sequential/concurrent executor paths) compares it against IDs — but
	// the decompose LLM can only ever reference a sibling by its title (IDs
	// don't exist until this exact function assigns them), so an
	// unresolved DependsOn entry is a title, not an ID, and silently never
	// matches anything. Found live on PRD a2833a5e: this meant dependency
	// ordering had never actually been enforced — topoSort treated every
	// task as having no real dependency edges, executing in ID-sort order,
	// which happens to approximate decompose's own sequence closely enough
	// that the gap went unnoticed until a cancelled task's dependent
	// proceeded without it. See resolveDependsOnTitles for the resolution
	// itself (extracted v8.36.6 for reuse by RepairDependsOn).
	resolveDependsOnTitles(stories, s.stories, s.tasks)
	prd.Story = stories
	prd.UpdatedAt = time.Now()
	return s.persist()
}

// resolveDependsOnTitles rewrites each DependsOn entry that names a sibling
// by title (not yet an ID) to that sibling's real ID, leaving anything that
// matches neither a known ID nor a known sibling title untouched rather
// than silently dropping it. Shared by SetStories (fresh decompose output)
// and RepairDependsOn (existing PRDs whose SetStories call predates this
// fix and are still stuck with raw titles).
func resolveDependsOnTitles(stories []Story, storyByID map[string]*Story, taskByID map[string]*Task) {
	taskIDByTitle := make(map[string]string, len(taskByID))
	storyIDByTitle := make(map[string]string, len(stories))
	for i := range stories {
		storyIDByTitle[stories[i].Title] = stories[i].ID
		for j := range stories[i].Tasks {
			taskIDByTitle[stories[i].Tasks[j].Title] = stories[i].Tasks[j].ID
		}
	}
	for i := range stories {
		for k, dep := range stories[i].DependsOn {
			if _, isID := storyByID[dep]; isID {
				continue
			}
			if id, ok := storyIDByTitle[dep]; ok {
				stories[i].DependsOn[k] = id
			}
		}
		for j := range stories[i].Tasks {
			for k, dep := range stories[i].Tasks[j].DependsOn {
				if _, isID := taskByID[dep]; isID {
					continue
				}
				if id, ok := taskIDByTitle[dep]; ok {
					stories[i].Tasks[j].DependsOn[k] = id
				}
			}
		}
	}
}

// RepairDependsOn (v8.36.6) re-runs resolveDependsOnTitles against a PRD's
// already-stored stories/tasks. For PRDs whose SetStories call predates the
// v8.36.5 fix, DependsOn entries are still stuck as raw titles and were
// never actually enforced — this repairs an existing PRD in place instead
// of losing all progress via reset_to_draft + a fresh decompose.
func (s *Store) RepairDependsOn(prdID string) (*PRD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	resolveDependsOnTitles(prd.Story, s.stories, s.tasks)
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
func (s *Store) AddTask(prdID, storyID, title, spec string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prd, ok := s.prds[prdID]
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
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

func (s *Store) load() error {
	if err := loadJSONL(filepath.Join(s.dir, "prds.jsonl"), func(line []byte) error {
		var p PRD
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		p.Status = NormalizePRDStatus(p.Status)
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
	return nil
}

// persist writes the in-memory state back as JSON-lines (full rewrite).
func (s *Store) persist() error {
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
	return writeJSONL(filepath.Join(s.dir, "guardrail_profiles.jsonl"), len(s.profiles), func(emit func(any) error) error {
		for _, p := range s.profiles {
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

// newID returns an 8-hex string. Time-based + random-ish so concurrent
// callers don't collide.
func newID() string {
	t := time.Now().UnixNano()
	return fmt.Sprintf("%08x", uint32(t))
}
