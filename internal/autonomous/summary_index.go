package autonomous

import (
	"sort"
	"strings"
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
	// DerivedKind is the kind the title and spec heuristic gives (feature or
	// bug), kept so a read can resolve the effective kind without the spec.
	DerivedKind string `json:"derived_kind,omitempty"`

	// IsTemplate marks a template record, which no project roll-up lists.
	IsTemplate bool `json:"is_template,omitempty"`

	// Project link.
	Project        string `json:"project,omitempty"`
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
	// MergeCommit is the shipped record's merge commit; non-empty means shipped.
	MergeCommit string `json:"merge_commit,omitempty"`
	// GitBranch is the work branch the merge proof is asked about.
	GitBranch string `json:"git_branch,omitempty"`
	// ApprovedAt is when the Automaton was approved, when known.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// Tasks is the compact timing of every task, in story order. The slice is
	// built once per record change and never edited in place, so clones share it.
	Tasks []SummaryTask `json:"tasks,omitempty"`

	// SessionIDs lists every session that cost money, in the order
	// Manager.PRDSessionIDs returns them (without the live decompose lookup).
	SessionIDs []string `json:"session_ids,omitempty"`

	// Session ids the cost roll-up needs. Decompose holds the planner
	// session recorded on the Automaton.
	WorkerSessionIDs    []string `json:"worker_session_ids,omitempty"`
	VerifierSessionIDs  []string `json:"verifier_session_ids,omitempty"`
	JudgeSessionIDs     []string `json:"judge_session_ids,omitempty"`
	DecomposeSessionIDs []string `json:"decompose_session_ids,omitempty"`
}

// SummaryTask is the timing of one task: enough for throughput and span
// reads without the task text.
type SummaryTask struct {
	ID          string     `json:"id"`
	Status      TaskStatus `json:"status"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// IsShipped reports whether the entry carries a shipped record.
func (e SummaryEntry) IsShipped() bool { return strings.TrimSpace(e.MergeCommit) != "" }

// SummaryEntryOf derives the summary entry of a record without a store, for
// callers and tests that hold records.
func SummaryEntryOf(p *PRD) SummaryEntry { return buildSummaryEntry(p) }

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// membershipRecord is the minimal record the project resolver reads.
func (e SummaryEntry) membershipRecord() *PRD {
	return &PRD{ID: e.ID, Project: e.Project, ProjectDir: e.ProjectDir,
		AutomationID: e.AutomationID, ParentPRDID: e.ParentPRDID}
}

// ProgressRecord returns a minimal record the progress engine can count: the
// scalars, one story holding the compact task timings, and a spec that makes
// the kind heuristic give the same answer as the full record. It is built
// from the entry alone and never read back as the record itself.
func (e SummaryEntry) ProgressRecord() *PRD {
	p := &PRD{
		ID: e.ID, Title: e.Title, Status: e.Status, Kind: e.Kind,
		ProjectDir: e.ProjectDir, Project: e.Project, ProjectProfile: e.ProjectProfile,
		AutomationID: e.AutomationID, ParentPRDID: e.ParentPRDID,
		IsTemplate: e.IsTemplate, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		ApprovedAt: e.ApprovedAt, TagIDs: e.TagIDs,
	}
	if e.DerivedKind == PRDKindBug && DeriveKind(e.Title, "") != PRDKindBug {
		p.Spec = "fix" // the bug keyword came from the spec
	}
	if len(e.Tasks) > 0 {
		tasks := make([]Task, 0, len(e.Tasks))
		for _, t := range e.Tasks {
			tasks = append(tasks, Task{ID: t.ID, Status: t.Status, StartedAt: t.StartedAt, CompletedAt: t.CompletedAt})
		}
		p.Story = []Story{{ID: "summary", Tasks: tasks}}
	}
	return p
}

// ProgressEntries is the summary index as the progress engine reads it.
func (s *Store) ProgressEntries() []SummaryEntry { return s.SummaryEntries() }

// NewAutomatonProjectIndexFromSummary resolves every entry once, with the
// same resolver as NewAutomatonProjectIndex.
func NewAutomatonProjectIndexFromSummary(entries []SummaryEntry, automations []*Automation, projects map[string]string) *AutomatonProjectIndex {
	recs := make([]*PRD, 0, len(entries))
	for _, e := range entries {
		recs = append(recs, e.membershipRecord())
	}
	return NewAutomatonProjectIndex(recs, automations, projects)
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
		DerivedKind:      DeriveKind(p.Title, p.Spec),
		IsTemplate:       p.IsTemplate,
		Project:          p.Project,
		ProjectDir:       p.ProjectDir,
		MergeCommit:      p.MergeCommit,
		GitBranch:        p.Git.Branch,
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
	e.MergedAt = copyTimePtr(p.MergedAt)
	e.ApprovedAt = copyTimePtr(p.ApprovedAt)
	var workers, verifiers, judges, decompose, all idSet
	for _, v := range p.Verdicts {
		judges.add(v.JudgeSessionID)
		all.add(v.JudgeSessionID)
	}
	for i := range p.Decisions {
		judges.add(p.Decisions[i].JudgeSessionID)
		all.add(p.Decisions[i].JudgeSessionID)
	}
	decompose.add(p.PlannerSessionID)
	var latest time.Time
	for i := range p.Story {
		st := &p.Story[i]
		for _, v := range st.Verdicts {
			judges.add(v.JudgeSessionID)
			all.add(v.JudgeSessionID)
		}
		for j := range st.Tasks {
			t := &st.Tasks[j]
			e.TaskCount++
			e.TaskStatusCounts[string(t.Status)]++
			e.Tasks = append(e.Tasks, SummaryTask{ID: t.ID, Status: t.Status,
				StartedAt: copyTimePtr(t.StartedAt), CompletedAt: copyTimePtr(t.CompletedAt)})
			workers.add(t.SessionID)
			all.add(t.SessionID)
			verifiers.add(t.VerifierSessionID)
			if t.Verification != nil {
				verifiers.add(t.Verification.VerifierID)
				all.add(t.Verification.VerifierID)
			}
			for _, v := range t.Verdicts {
				judges.add(v.JudgeSessionID)
				all.add(v.JudgeSessionID)
			}
			if t.CompletedAt != nil && t.CompletedAt.After(latest) {
				latest = *t.CompletedAt
			}
		}
	}
	if isTerminalPRDStatus(p.Status) {
		e.FinishedAt = latest
	}
	e.SessionIDs = all.ids
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
	c.SessionIDs = cloneSummaryStrings(e.SessionIDs)
	c.WorkerSessionIDs = cloneSummaryStrings(e.WorkerSessionIDs)
	c.VerifierSessionIDs = cloneSummaryStrings(e.VerifierSessionIDs)
	c.JudgeSessionIDs = cloneSummaryStrings(e.JudgeSessionIDs)
	c.DecomposeSessionIDs = cloneSummaryStrings(e.DecomposeSessionIDs)
	c.MergedAt = copyTimePtr(e.MergedAt)
	c.ApprovedAt = copyTimePtr(e.ApprovedAt)
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
