// Project roll-up read: GET /api/projects/{name}/rollup.
//
// The roll-up answers "how far along is this project" from the records the
// daemon already keeps. It reads the project's arcs (ordered Automation slots,
// config.ProjectArc), the Automata (PRDs) those Automations hold, each Automaton's
// task progress, its cost in usage points and the throughput over a window. No
// storage is added: the Automaton store, the Automation records and the cost
// report engine are the only inputs.
//
// Grouping: an Automaton belongs to the arc whose Automation holds it, found by
// PRD.AutomationID or, failing that, the Automation whose members include it.
// An Automaton of this project (its ProjectDir matches the project's dir) that
// no arc holds goes in the "unassigned" group. Automata of other projects that
// no arc holds are left out.
//
// Cost: points only, never API dollars. Each Automaton's points are the
// cost report's estimated_usage_points for its group_by=automaton row, which
// the report derives from the billing profile's point-cost calibration
// (PointCostEquivalent). An Automaton whose backends are not on a bundled
// plan has no point figure and reads as 0; points_priced says whether any
// Automaton was priced.
//
// Membership: an Automaton belongs to the project by its explicit project link
// first (rollupInput.Linked, which also maps a worktree to its repository),
// then by the arc that holds it, and by directory only as a last fallback.
//
// Throughput: an Automaton counts as finished when it is completed, or when it
// is cancelled or archived and its work branch is already on the project's
// default branch (the read-only merge proof the merged-member decision uses),
// or when it carries a shipped record (a merge commit). The supervisor and the
// daemon cancel and archive an Automaton right after its work is merged, so a
// status of completed alone would read shipped work as zero. The finish time is
// the merge or ship time when known, else the last update, and it must fall in
// the window. A cancelled Automaton whose branch is not merged is not counted.
// A task counts from its own CompletedAt when it is completed and that time
// falls in the window, for every Automaton of the project whatever its final
// status, and never twice. Both are divided by the window length in days.
//
// Query: window=<days> (1–365, default 7) sets the throughput window and the
// cost window. Reads require CapAutonomousRead, as the other project reads do.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	rollupDefaultWindowDays = 7
	rollupMaxWindowDays     = 365
)

// rollupTasks is task progress: how many are done out of how many.
type rollupTasks struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// rollupThroughput is what finished in the window and the per-day rate.
type rollupThroughput struct {
	AutomataFinished int     `json:"automata_finished"`
	TasksFinished    int     `json:"tasks_finished"`
	AutomataPerDay   float64 `json:"automata_per_day"`
	TasksPerDay      float64 `json:"tasks_per_day"`
}

// rollupAutomaton is one Automaton line of a group.
type rollupAutomaton struct {
	ID           string      `json:"id"`
	Title        string      `json:"title,omitempty"`
	Status       string      `json:"status"`
	AutomationID string      `json:"automation_id,omitempty"`
	Finished     bool        `json:"finished"`
	Tasks        rollupTasks `json:"tasks"`
	Points       float64     `json:"points"`
	SessionIDs   []string    `json:"session_ids"`
}

// rollupGroup is one arc, the unassigned bucket, or the whole-project total.
// The arc fields are set only on arc groups.
type rollupGroup struct {
	AutomationID string            `json:"automation_id,omitempty"`
	Priority     int               `json:"priority,omitempty"`
	State        string            `json:"state,omitempty"`
	Automata     []rollupAutomaton `json:"automata"`
	Tasks        rollupTasks       `json:"tasks"`
	Points       float64           `json:"points"`
	Throughput   rollupThroughput  `json:"throughput"`
}

// projectRollup is the GET /api/projects/{name}/rollup response.
type projectRollup struct {
	Project      string        `json:"project"`
	WindowDays   int           `json:"window_days"`
	GeneratedAt  time.Time     `json:"generated_at"`
	PointsPriced bool          `json:"points_priced"`
	Arcs         []rollupGroup `json:"arcs"`
	Unassigned   rollupGroup   `json:"unassigned"`
	Total        rollupGroup   `json:"total"`
}

// rollupInput is everything buildProjectRollup reads. It holds plain records
// so the grouping and arithmetic can be tested without a running daemon.
type rollupInput struct {
	Project     string
	Dir         string
	Arcs        []config.ProjectArc
	PRDs        []*autonomous.PRD
	Automations []*autonomous.Automation
	// SessionIDs returns the session ids of one Automaton (worker, verifier, judge).
	SessionIDs func(prdID string) []string
	// Points maps an Automaton id to its points. An Automaton with no entry is unpriced.
	Points     map[string]float64
	WindowDays int
	Now        time.Time
	// Linked reports whether the Automaton is explicitly linked to this
	// project, including a worktree-based Automaton mapped to its repository.
	// It is checked before the arc and the directory. Nil means no link.
	Linked func(p *autonomous.PRD) bool
	// Merged is the read-only merge proof: whether the Automaton's work branch
	// is already on the project's default branch and, when known, when it
	// landed (zero time = unknown). It is asked only for a cancelled or
	// archived Automaton with no shipped record. Nil means not merged.
	Merged func(p *autonomous.PRD) (merged bool, at time.Time)

	// seenTasks keeps a task from being counted twice in the throughput.
	seenTasks map[string]bool
}

// parseRollupWindow reads the window query value. Empty means the default.
func parseRollupWindow(raw string) (int, error) {
	if raw == "" {
		return rollupDefaultWindowDays, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > rollupMaxWindowDays {
		return 0, fmt.Errorf("window must be a whole number of days from 1 to %d", rollupMaxWindowDays)
	}
	return n, nil
}

// buildProjectRollup groups the project's Automata by arc and sums them.
func buildProjectRollup(in rollupInput) projectRollup {
	in.seenTasks = map[string]bool{}
	since := in.Now.Add(-time.Duration(in.WindowDays) * 24 * time.Hour)
	inWindow := func(t *time.Time) bool {
		return t != nil && !t.Before(since) && !t.After(in.Now)
	}

	// Which automation holds each Automaton: PRD.AutomationID first, else
	// the first Automation (in list order) with it as a member.
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

	arcIndex := map[string]int{}
	out := projectRollup{Project: in.Project, WindowDays: in.WindowDays, GeneratedAt: in.Now}
	out.Arcs = make([]rollupGroup, 0, len(in.Arcs))
	for _, arc := range in.Arcs {
		if _, dup := arcIndex[arc.AutomationID]; dup {
			continue
		}
		arcIndex[arc.AutomationID] = len(out.Arcs)
		state := arc.State
		if state == "" {
			state = config.ArcStatePlanned
		}
		out.Arcs = append(out.Arcs, rollupGroup{
			AutomationID: arc.AutomationID, Priority: arc.Priority, State: state,
			Automata: []rollupAutomaton{},
		})
	}
	out.Unassigned = rollupGroup{Automata: []rollupAutomaton{}}

	cleanDir := filepath.Clean(in.Dir)
	seenPRD := map[string]bool{}
	for _, p := range in.PRDs {
		if p == nil || p.IsTemplate || seenPRD[p.ID] {
			continue
		}
		auto := p.AutomationID
		if auto == "" {
			auto = memberOf[p.ID]
		}
		linked := in.Linked != nil && in.Linked(p)
		var g *rollupGroup
		if i, ok := arcIndex[auto]; ok && auto != "" {
			g = &out.Arcs[i]
		} else if linked {
			g = &out.Unassigned
		} else if p.ProjectDir != "" && filepath.Clean(p.ProjectDir) == cleanDir {
			g = &out.Unassigned
		} else {
			continue
		}
		seenPRD[p.ID] = true
		g.Automata = append(g.Automata, rollupAutomatonFor(p, auto, in, inWindow, g))
	}

	for i := range out.Arcs {
		finishGroup(&out.Arcs[i], in.WindowDays)
	}
	finishGroup(&out.Unassigned, in.WindowDays)

	out.Total = rollupGroup{Automata: []rollupAutomaton{}}
	for _, g := range append(append([]rollupGroup{}, out.Arcs...), out.Unassigned) {
		out.Total.Automata = append(out.Total.Automata, g.Automata...)
		out.Total.Tasks.Done += g.Tasks.Done
		out.Total.Tasks.Total += g.Tasks.Total
		out.Total.Points += g.Points
		out.Total.Throughput.AutomataFinished += g.Throughput.AutomataFinished
		out.Total.Throughput.TasksFinished += g.Throughput.TasksFinished
	}
	setPerDay(&out.Total.Throughput, in.WindowDays)

	out.PointsPriced = len(in.Points) > 0
	return out
}

// rollupAutomatonFor builds one Automaton line and adds its finished tasks to
// the group's throughput. The group's task and finished counts are set here so
// finishGroup only has to sum points and divide.
func rollupAutomatonFor(p *autonomous.PRD, auto string, in rollupInput, inWindow func(*time.Time) bool, g *rollupGroup) rollupAutomaton {
	ra := rollupAutomaton{
		ID: p.ID, Title: p.Title, Status: string(p.Status),
		AutomationID: auto, SessionIDs: []string{},
	}
	if in.SessionIDs != nil {
		if ids := in.SessionIDs(p.ID); ids != nil {
			ra.SessionIDs = ids
		}
	}
	finished, finishedAt := rollupFinished(p, in)
	ra.Finished = finished
	if finished && !finishedAt.IsZero() && inWindow(&finishedAt) {
		g.Throughput.AutomataFinished++
	}
	for _, st := range p.Story {
		for _, t := range st.Tasks {
			ra.Tasks.Total++
			g.Tasks.Total++
			if t.Status == autonomous.TaskCompleted {
				ra.Tasks.Done++
				g.Tasks.Done++
				key := p.ID + "/" + t.ID
				if inWindow(t.CompletedAt) && !in.seenTasks[key] {
					in.seenTasks[key] = true
					g.Throughput.TasksFinished++
				}
			}
		}
	}
	if pts, ok := in.Points[p.ID]; ok {
		ra.Points = pts
	}
	return ra
}

// rollupFinished decides whether an Automaton counts as finished and when it
// finished: completed; or cancelled or archived with its branch already on the
// default branch; or carrying a shipped record. The time is the merge or ship
// time when known, else the last update.
func rollupFinished(p *autonomous.PRD, in rollupInput) (bool, time.Time) {
	at := func(known time.Time) time.Time {
		if !known.IsZero() {
			return known
		}
		return p.UpdatedAt
	}
	var shipAt time.Time
	if p.MergedAt != nil {
		shipAt = *p.MergedAt
	}
	switch {
	case p.IsShipped():
		return true, at(shipAt)
	case p.Status == autonomous.PRDCompleted:
		return true, at(shipAt)
	case (p.Status == autonomous.PRDCancelled || p.Status == autonomous.PRDArchived) && in.Merged != nil:
		if merged, mergedAt := in.Merged(p); merged {
			return true, at(mergedAt)
		}
	}
	return false, time.Time{}
}

// finishGroup sums the points and turns the window counts into per-day rates.
func finishGroup(g *rollupGroup, windowDays int) {
	for _, a := range g.Automata {
		g.Points += a.Points
	}
	setPerDay(&g.Throughput, windowDays)
}

func setPerDay(t *rollupThroughput, windowDays int) {
	days := float64(windowDays)
	t.AutomataPerDay = float64(t.AutomataFinished) / days
	t.TasksPerDay = float64(t.TasksFinished) / days
}

// costRollupPoints prices every Automaton in the window through the cost report
// engine and keeps only the points figure. Automata with no point figure are absent.
func (s *Server) costRollupPoints(mgr *autonomous.Manager, now time.Time, windowDays int) map[string]float64 {
	points := map[string]float64{}
	src := s.newManagerCostSource(mgr)
	billing := s.resolvedBilling()
	req := costreport.Request{
		GroupBy: costreport.GroupByAutomaton,
		Since:   now.Add(-time.Duration(windowDays) * 24 * time.Hour),
		Until:   now,
	}
	rep, err := costreport.Build(s.cfg.CostReport, req, src, src, s.exposedRate, billing, now)
	if err != nil || rep == nil {
		return points
	}
	for _, row := range rep.Rows {
		if row.EstimatedUsagePoints != nil {
			points[row.Key] = *row.EstimatedUsagePoints
		}
	}
	return points
}

// rollupLinkedFn reports an Automaton as linked to the project when it works
// in a git worktree of the project's repository: its directory sits under the
// daemon's worktree base and the project's directory is the configured base
// repository. This maps a worktree to its repository; the directory of a
// worktree never equals the project's directory.
func (s *Server) rollupLinkedFn(mgr *autonomous.Manager, projectDir string) func(*autonomous.PRD) bool {
	cfg := mgr.Config()
	repo := filepath.Clean(cfg.WorktreeBaseRepo)
	if cfg.WorktreeBaseRepo == "" || filepath.Clean(projectDir) != repo {
		return nil
	}
	return func(p *autonomous.PRD) bool {
		return p.Git.Branch != "" && p.ProjectDir != "" && filepath.Clean(p.ProjectDir) != repo
	}
}

// rollupMergedFn is the read-only merge proof the merged-member decision uses:
// the Automaton's branch against the default branch of the base repository.
// When it is merged, the time is the commit date of the proving commit.
func (s *Server) rollupMergedFn(mgr *autonomous.Manager) func(*autonomous.PRD) (bool, time.Time) {
	cfg := mgr.Config()
	git := autonomous.NewMergeGitRunner()
	return func(p *autonomous.PRD) (bool, time.Time) {
		if p.Git.Branch == "" || cfg.WorktreeBaseRepo == "" {
			return false, time.Time{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		proof := autonomous.MergedMemberProof(ctx, git, cfg.WorktreeBaseRepo, p.Git.Branch)
		if !proof.Merged {
			return false, time.Time{}
		}
		out, err := git.Run(ctx, cfg.WorktreeBaseRepo, "log", "-1", "--format=%cI", proof.Commit)
		if err != nil {
			return true, time.Time{}
		}
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(out))
		if err != nil {
			return true, time.Time{}
		}
		return true, at
	}
}

// handleProjectRollup serves GET /api/projects/{name}/rollup.
func (s *Server) handleProjectRollup(w http.ResponseWriter, r *http.Request) {
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
	windowDays, err := parseRollupWindow(r.URL.Query().Get("window"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		http.Error(w, "automations not available", http.StatusServiceUnavailable)
		return
	}

	now := time.Now()
	resp := buildProjectRollup(rollupInput{
		Project:     name,
		Dir:         entry.Dir,
		Arcs:        entry.Arcs,
		PRDs:        mgr.Store().ListPRDs(),
		Automations: mgr.ListAutomations(),
		SessionIDs:  mgr.PRDSessionIDs,
		Points:      s.costRollupPoints(mgr, now, windowDays),
		WindowDays:  windowDays,
		Now:         now,
		Linked:      s.rollupLinkedFn(mgr, entry.Dir),
		Merged:      s.rollupMergedFn(mgr),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
