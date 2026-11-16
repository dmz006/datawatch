// Project history read: GET /api/projects/{name}/history.
//
// The history answers "what has this project finished" from records the daemon
// already keeps. It returns the project's finished and archived Automata (PRDs
// whose status is completed or archived, including earlier release work such as
// the 9.0 to 10.0 Automata), merged with the daily progress snapshots for the
// project, newest first and paged. It also returns Gantt rows (one per Automaton
// and one per task) built from the stored timestamps. No storage is added: the
// Automaton store, the Automation records and the progress snapshot store are
// the only inputs.
//
// Membership: an Automaton belongs to the project when an arc of the project
// holds it (PRD.AutomationID, else the Automation whose members include it), or
// when the shared membership resolver (link, Automation project, parent project,
// then directory) places it in this project. Templates are left out.
//
// Timestamps: an Automaton has no start or finish timestamp of its own. Its start
// is ApprovedAt when set, else CreatedAt; its finish is UpdatedAt, the closest
// record, as the project roll-up also reads it. A task's start and end are its
// StartedAt and CompletedAt; either is null when the task never recorded it.
//
// Snapshots: only daily snapshots are listed, and only those that carry grouped
// totals for the project. Each one is reduced to the project's counts at that day.
//
// Paging: limit=<n> (1–200, default 50) and cursor=<opaque> page the merged
// entries list in the order time-descending then key-ascending. The response
// carries next_cursor when there is another page. The Gantt rows cover every
// matching Automaton and are not paged.
//
// Query: limit, cursor. Reads require CapAutonomousRead, as the other project reads do.

package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	historyDefaultLimit = 50
	historyMaxLimit     = 200

	historyKindAutomaton = "automaton"
	historyKindSnapshot  = "snapshot"
	historyKindTask      = "task"
)

// historyEntry is one item of the merged, paged list. Kind says which fields
// are set: automaton entries carry the Automaton fields, snapshot entries carry
// Progress.
type historyEntry struct {
	Kind string    `json:"kind"`
	Time time.Time `json:"time"`

	// Automaton entries.
	ID           string       `json:"id,omitempty"`
	Title        string       `json:"title,omitempty"`
	Status       string       `json:"status,omitempty"`
	AutomationID string       `json:"automation_id,omitempty"`
	StartedAt    *time.Time   `json:"started_at,omitempty"`
	FinishedAt   *time.Time   `json:"finished_at,omitempty"`
	Tasks        *rollupTasks `json:"tasks,omitempty"`

	// Snapshot entries.
	TakenAt  *time.Time                       `json:"taken_at,omitempty"`
	Progress *autonomous.ProgressProjectPoint `json:"progress,omitempty"`

	key string // unexported sort tie-break: kind and id
}

// historyGanttRow is one bar of the Gantt chart: an Automaton (task_id empty)
// or one of its tasks.
type historyGanttRow struct {
	Kind         string     `json:"kind"`
	AutomatonID  string     `json:"automaton_id"`
	AutomationID string     `json:"automation_id,omitempty"`
	TaskID       string     `json:"task_id,omitempty"`
	Title        string     `json:"title,omitempty"`
	State        string     `json:"state"`
	Start        *time.Time `json:"start"`
	End          *time.Time `json:"end"`
}

// projectHistory is the GET /api/projects/{name}/history response.
type projectHistory struct {
	Project     string            `json:"project"`
	GeneratedAt time.Time         `json:"generated_at"`
	Limit       int               `json:"limit"`
	Total       int               `json:"total"`
	HasMore     bool              `json:"has_more"`
	NextCursor  string            `json:"next_cursor,omitempty"`
	Items       []historyEntry    `json:"items"`
	Gantt       []historyGanttRow `json:"gantt"`
}

// historyInput is everything buildProjectHistory reads. It holds plain records
// so the selection, ordering and paging can be tested without a running daemon.
type historyInput struct {
	Project     string
	Dir         string
	Arcs        []config.ProjectArc
	PRDs        []*autonomous.PRD
	Automations []*autonomous.Automation
	// Snapshots are every progress snapshot, oldest first, as the store lists them.
	Snapshots []autonomous.ProgressSnapshot
	Limit     int
	Cursor    *historyCursor
	Now       time.Time
	// Projects maps every configured project name to its directory for the
	// membership resolver; nil means only this project.
	Projects map[string]string
}

// historyCursor is the position after the last entry of a page.
type historyCursor struct {
	T int64  `json:"t"` // Time, unix nanos
	K string `json:"k"` // entry key
}

// historyEntryKey is the stable tie-break for entries that share a time.
func historyEntryKey(kind, id string) string { return kind + ":" + id }

// encodeHistoryCursor makes the opaque next_cursor of a page ending at e.
func encodeHistoryCursor(e historyEntry) string {
	b, _ := json.Marshal(historyCursor{T: e.Time.UnixNano(), K: e.key})
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeHistoryCursor reads a cursor from the query. Empty means no cursor.
func decodeHistoryCursor(raw string) (*historyCursor, error) {
	if raw == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var c historyCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &c, nil
}

// parseHistoryLimit reads the limit query value. Empty means the default.
func parseHistoryLimit(raw string) (int, error) {
	if raw == "" {
		return historyDefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("limit must be a positive whole number")
	}
	if n > historyMaxLimit {
		n = historyMaxLimit
	}
	return n, nil
}

// historyAfterCursor reports whether entry e comes strictly after the cursor in the
// list order (time descending, key ascending).
func historyAfterCursor(e historyEntry, c historyCursor) bool {
	et := e.Time.UnixNano()
	if et != c.T {
		return et < c.T
	}
	return e.key > c.K
}

// buildProjectHistory selects the project's finished Automata, the daily
// snapshots, merges and pages them, and builds the Gantt rows.
func buildProjectHistory(in historyInput) projectHistory {
	limit := in.Limit
	if limit < 1 {
		limit = historyDefaultLimit
	}
	out := projectHistory{Project: in.Project, GeneratedAt: in.Now, Limit: limit,
		Items: []historyEntry{}, Gantt: []historyGanttRow{}}

	// Which automation holds each Automaton, as the project roll-up finds it.
	memberOf := map[string]string{}
	for _, a := range in.Automations {
		if a == nil {
			continue
		}
		for _, m := range a.Members {
			if m.PRDID == "" {
				continue
			}
			if _, ok := memberOf[m.PRDID]; !ok {
				memberOf[m.PRDID] = a.ID
			}
		}
	}
	arcHeld := map[string]bool{}
	for _, arc := range in.Arcs {
		arcHeld[arc.AutomationID] = true
	}
	projects := in.Projects
	if projects == nil {
		projects = map[string]string{in.Project: in.Dir}
	}
	idx := autonomous.NewAutomatonProjectIndex(in.PRDs, in.Automations, projects)

	var entries []historyEntry
	for _, p := range in.PRDs {
		if p == nil || p.IsTemplate {
			continue
		}
		if p.Status != autonomous.PRDCompleted && p.Status != autonomous.PRDArchived {
			continue
		}
		auto := p.AutomationID
		if auto == "" {
			auto = memberOf[p.ID]
		}
		inProject := (auto != "" && arcHeld[auto]) || idx.Of(p.ID).Project == in.Project
		if !inProject {
			continue
		}
		entries = append(entries, historyAutomatonEntry(p, auto))
		out.Gantt = append(out.Gantt, historyGanttFor(p, auto)...)
	}

	for _, s := range in.Snapshots {
		if s.Reason != autonomous.ProgressSnapshotDaily || s.Grouped.ByProject == nil {
			continue
		}
		g := s.Grouped.ByProject[in.Project]
		taken := s.TakenAt
		entries = append(entries, historyEntry{
			Kind: historyKindSnapshot, Time: s.TakenAt, TakenAt: &taken,
			Progress: &autonomous.ProgressProjectPoint{
				Date: s.Date, Reason: s.Reason,
				PRDs: g.PRDs, Done: g.Done, Running: g.Running, Waiting: g.Waiting, Blocked: g.Blocked,
				Tasks: g.Tasks, TasksDone: g.TasksDone, TasksLeft: g.TasksLeft, WorkHours: g.WorkHours,
			},
			key: historyEntryKey(historyKindSnapshot, s.Date),
		})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].Time.Equal(entries[j].Time) {
			return entries[i].Time.After(entries[j].Time)
		}
		return entries[i].key < entries[j].key
	})

	out.Total = len(entries)
	start := 0
	if in.Cursor != nil {
		c := *in.Cursor
		start = sort.Search(len(entries), func(i int) bool { return historyAfterCursor(entries[i], c) })
	}
	end := start + limit
	if end >= len(entries) {
		end = len(entries)
	} else {
		out.HasMore = true
	}
	out.Items = append(out.Items, entries[start:end]...)
	if out.HasMore && len(out.Items) > 0 {
		out.NextCursor = encodeHistoryCursor(out.Items[len(out.Items)-1])
	}
	return out
}

// historyAutomatonEntry is the list entry for one finished Automaton.
func historyAutomatonEntry(p *autonomous.PRD, auto string) historyEntry {
	start, finish := historyAutomatonTimes(p)
	tasks := rollupTasks{}
	for _, st := range p.Story {
		for _, t := range st.Tasks {
			tasks.Total++
			if t.Status == autonomous.TaskCompleted {
				tasks.Done++
			}
		}
	}
	at := finish
	if at.IsZero() {
		at = start
	}
	return historyEntry{
		Kind: historyKindAutomaton, Time: at,
		ID: p.ID, Title: p.Title, Status: string(p.Status), AutomationID: auto,
		StartedAt: timePtr(start), FinishedAt: timePtr(finish), Tasks: &tasks,
		key: historyEntryKey(historyKindAutomaton, p.ID),
	}
}

// historyGanttFor returns the Automaton's bar followed by one bar per task.
func historyGanttFor(p *autonomous.PRD, auto string) []historyGanttRow {
	start, finish := historyAutomatonTimes(p)
	rows := []historyGanttRow{{
		Kind: historyKindAutomaton, AutomatonID: p.ID, AutomationID: auto,
		Title: p.Title, State: string(p.Status),
		Start: timePtr(start), End: timePtr(finish),
	}}
	for _, st := range p.Story {
		for _, t := range st.Tasks {
			rows = append(rows, historyGanttRow{
				Kind: historyKindTask, AutomatonID: p.ID, AutomationID: auto, TaskID: t.ID,
				Title: t.Title, State: string(t.Status),
				Start: t.StartedAt, End: t.CompletedAt,
			})
		}
	}
	return rows
}

// historyAutomatonTimes returns an Automaton's start (ApprovedAt else CreatedAt)
// and finish (UpdatedAt). Zero values mean the record did not carry the time.
func historyAutomatonTimes(p *autonomous.PRD) (time.Time, time.Time) {
	start := p.CreatedAt
	if p.ApprovedAt != nil && !p.ApprovedAt.IsZero() {
		start = *p.ApprovedAt
	}
	return start, p.UpdatedAt
}

// timePtr returns a pointer to t, or nil when t is zero, so an unknown time
// reads as null in the response.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// handleProjectHistory serves GET /api/projects/{name}/history.
func (s *Server) handleProjectHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	entry, ok := s.cfg.Projects[name]
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	limit, err := parseHistoryLimit(q.Get("limit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cursor, err := decodeHistoryCursor(q.Get("cursor"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		http.Error(w, "automations not available", http.StatusServiceUnavailable)
		return
	}

	var snaps []autonomous.ProgressSnapshot
	if p, ok := s.autonomousMgr.(progressProvider); ok {
		if store := p.ProgressSnapshots(); store != nil {
			snaps = store.List(0, time.Now())
		}
	}

	resp := buildProjectHistory(historyInput{
		Project:     name,
		Dir:         entry.Dir,
		Arcs:        entry.Arcs,
		PRDs:        mgr.Store().ListPRDs(),
		Automations: mgr.ListAutomations(),
		Snapshots:   snaps,
		Limit:       limit,
		Cursor:      cursor,
		Now:         time.Now(),
		Projects:    rollupProjectDirs(s.cfg.Projects),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
