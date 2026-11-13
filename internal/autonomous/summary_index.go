package autonomous

import (
	"sort"
	"time"
)

// SummaryEntry is the in-memory summary of one Automaton. It holds only
// scalars, short lists and small count maps, so an entry never grows with
// the amount of spec, story or task text the record carries, and a list
// read needs no copy of the record itself.
type SummaryEntry struct {
	ID     string    `json:"id"`
	Title  string    `json:"title,omitempty"`
	Status PRDStatus `json:"status"`
	Kind   string    `json:"kind,omitempty"`

	// Project link.
	ProjectDir     string `json:"project_dir,omitempty"`
	ProjectProfile string `json:"project_profile,omitempty"`

	AutomationID string `json:"automation_id,omitempty"`
	ParentPRDID  string `json:"parent_prd_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// FinishedAt is the latest task completion time once the Automaton is in
	// a terminal status; zero otherwise or when no task recorded one.
	FinishedAt time.Time `json:"finished_at,omitempty"`

	StoryCount int `json:"story_count"`
	TaskCount  int `json:"task_count"`
	// TaskStatusCounts maps a task status to the number of tasks in it.
	TaskStatusCounts map[string]int `json:"task_status_counts"`
	// TotalTasks is the parallel-execution task total the record carries
	// when known (zero otherwise).
	TotalTasks int `json:"total_tasks,omitempty"`

	// TagIDs are the managed tags; SystemTags the placement-derived ones.
	TagIDs     []string `json:"tag_ids,omitempty"`
	SystemTags []string `json:"system_tags,omitempty"`

	MergedInRelease string     `json:"merged_in_release,omitempty"`
	MergedAt        *time.Time `json:"merged_at,omitempty"`

	// Session ids the cost roll-up needs. Decompose holds the planner
	// session recorded on the Automaton.
	WorkerSessionIDs    []string `json:"worker_session_ids,omitempty"`
	VerifierSessionIDs  []string `json:"verifier_session_ids,omitempty"`
	JudgeSessionIDs     []string `json:"judge_session_ids,omitempty"`
	DecomposeSessionIDs []string `json:"decompose_session_ids,omitempty"`
}

func isTerminalPRDStatus(s PRDStatus) bool {
	switch s {
	case PRDCompleted, PRDCancelled, PRDArchived, PRDFailed:
		return true
	}
	return false
}

func cloneSummaryStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

// idSet collects de-duplicated, non-empty ids in first-seen order.
type idSet struct {
	seen map[string]struct{}
	ids  []string
}

func (s *idSet) add(id string) {
	if id == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if _, dup := s.seen[id]; dup {
		return
	}
	s.seen[id] = struct{}{}
	s.ids = append(s.ids, id)
}

// buildSummaryEntry derives the entry for p. It reads p and copies out
// scalars only; the caller holds Store.mu.
func buildSummaryEntry(p *PRD) SummaryEntry {
	e := SummaryEntry{
		ID:               p.ID,
		Title:            p.Title,
		Status:           p.Status,
		Kind:             p.Kind,
		ProjectDir:       p.ProjectDir,
		ProjectProfile:   p.ProjectProfile,
		AutomationID:     p.AutomationID,
		ParentPRDID:      p.ParentPRDID,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
		StoryCount:       len(p.Story),
		TaskStatusCounts: map[string]int{},
		TotalTasks:       p.TotalTasks,
		TagIDs:           cloneSummaryStrings(p.TagIDs),
		SystemTags:       cloneSummaryStrings(p.SystemTags),
		MergedInRelease:  p.MergedInRelease,
	}
	if p.MergedAt != nil {
		t := *p.MergedAt
		e.MergedAt = &t
	}
	var workers, verifiers, judges, decompose idSet
	for _, v := range p.Verdicts {
		judges.add(v.JudgeSessionID)
	}
	for i := range p.Decisions {
		judges.add(p.Decisions[i].JudgeSessionID)
	}
	decompose.add(p.PlannerSessionID)
	var latest time.Time
	for i := range p.Story {
		st := &p.Story[i]
		for _, v := range st.Verdicts {
			judges.add(v.JudgeSessionID)
		}
		for j := range st.Tasks {
			t := &st.Tasks[j]
			e.TaskCount++
			e.TaskStatusCounts[string(t.Status)]++
			workers.add(t.SessionID)
			verifiers.add(t.VerifierSessionID)
			if t.Verification != nil {
				verifiers.add(t.Verification.VerifierID)
			}
			for _, v := range t.Verdicts {
				judges.add(v.JudgeSessionID)
			}
			if t.CompletedAt != nil && t.CompletedAt.After(latest) {
				latest = *t.CompletedAt
			}
		}
	}
	if isTerminalPRDStatus(p.Status) {
		e.FinishedAt = latest
	}
	e.WorkerSessionIDs = workers.ids
	e.VerifierSessionIDs = verifiers.ids
	e.JudgeSessionIDs = judges.ids
	e.DecomposeSessionIDs = decompose.ids
	return e
}

func (e SummaryEntry) clone() SummaryEntry {
	c := e
	c.TaskStatusCounts = make(map[string]int, len(e.TaskStatusCounts))
	for k, v := range e.TaskStatusCounts {
		c.TaskStatusCounts[k] = v
	}
	c.TagIDs = cloneSummaryStrings(e.TagIDs)
	c.SystemTags = cloneSummaryStrings(e.SystemTags)
	c.WorkerSessionIDs = cloneSummaryStrings(e.WorkerSessionIDs)
	c.VerifierSessionIDs = cloneSummaryStrings(e.VerifierSessionIDs)
	c.JudgeSessionIDs = cloneSummaryStrings(e.JudgeSessionIDs)
	c.DecomposeSessionIDs = cloneSummaryStrings(e.DecomposeSessionIDs)
	if e.MergedAt != nil {
		t := *e.MergedAt
		c.MergedAt = &t
	}
	return c
}

// refreshSummaryLocked rebuilds the entry of one Automaton (or drops it when
// the store no longer holds it) and bumps the revision. Caller holds s.mu.
func (s *Store) refreshSummaryLocked(id string) {
	if s.sumIdx == nil {
		s.sumIdx = map[string]SummaryEntry{}
	}
	if p, ok := s.prds[id]; ok && p != nil {
		s.sumIdx[id] = buildSummaryEntry(p)
	} else {
		delete(s.sumIdx, id)
	}
	s.rev++
}

// rebuildSummaryLocked rebuilds every entry. Used when a change cannot name
// the Automaton it touched. Caller holds s.mu.
func (s *Store) rebuildSummaryLocked() {
	s.sumIdx = make(map[string]SummaryEntry, len(s.prds))
	for id, p := range s.prds {
		if p != nil {
			s.sumIdx[id] = buildSummaryEntry(p)
		}
	}
	s.rev++
}

// Revision is a counter that increases on every change to any Automaton
// (create, update, task change, delete, load). A read never changes it.
func (s *Store) Revision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev
}

// SummaryEntries returns a copy of every entry, newest first (created time,
// then id). It does not touch the Automaton records.
func (s *Store) SummaryEntries() []SummaryEntry {
	s.mu.Lock()
	out := make([]SummaryEntry, 0, len(s.sumIdx))
	for _, e := range s.sumIdx {
		out = append(out, e.clone())
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// SummaryEntry returns a copy of one Automaton's entry.
func (s *Store) SummaryEntry(id string) (SummaryEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sumIdx[id]
	if !ok {
		return SummaryEntry{}, false
	}
	return e.clone(), true
}

// SummaryEntriesForProject returns the entries linked to a project
// directory, newest first.
func (s *Store) SummaryEntriesForProject(projectDir string) []SummaryEntry {
	all := s.SummaryEntries()
	out := all[:0]
	for _, e := range all {
		if e.ProjectDir == projectDir {
			out = append(out, e)
		}
	}
	return out
}
