package autonomous

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Timeline interval kinds.
const (
	TimelineBlocked = "blocked"
	TimelineWaiting = "waiting"
)

// TimelineFilter narrows a timeline. Every field is optional and matches
// exactly; an empty field matches everything. Project matches the Automaton's
// project directory, Automaton its id or title, Stage a stage (story) id or
// title, Kind the Automaton kind (feature, bug or other) and Tag a tag id.
type TimelineFilter struct {
	Project   string `json:"project,omitempty"`
	Automaton string `json:"automaton,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Tag       string `json:"tag,omitempty"`

	// Projects maps configured project names to directories. When set, the
	// Project filter matches an Automaton the membership resolver places in
	// the named project (or in the project whose directory is named); when
	// unset it compares the Automaton's directory.
	Projects map[string]string `json:"-"`
	// Automations are the Automation records the resolver reads.
	Automations []*Automation `json:"-"`

	index *AutomatonProjectIndex
}

// TimelineInterval is a span in which a member or Automaton was blocked or
// waiting. End is nil while the span is still open.
type TimelineInterval struct {
	Kind   string     `json:"kind"`
	Start  time.Time  `json:"start"`
	End    *time.Time `json:"end"`
	Reason string     `json:"reason,omitempty"`
}

// TimelineMember is one task drawn as a bar.
type TimelineMember struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Status    string             `json:"status"`
	Start     *time.Time         `json:"start"`
	End       *time.Time         `json:"end"`
	Intervals []TimelineInterval `json:"intervals,omitempty"`
}

// TimelineStage is one story drawn as a band over its members.
type TimelineStage struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	Status  string           `json:"status"`
	Start   *time.Time       `json:"start"`
	End     *time.Time       `json:"end"`
	Members []TimelineMember `json:"members"`
}

// TimelineAutomaton is one Automaton with its stages and projection.
type TimelineAutomaton struct {
	ID               string             `json:"id"`
	Title            string             `json:"title"`
	Project          string             `json:"project"`
	Kind             string             `json:"kind"`
	Tags             []string           `json:"tags,omitempty"`
	Status           string             `json:"status"`
	Start            *time.Time         `json:"start"`
	End              *time.Time         `json:"end"`
	Stages           []TimelineStage    `json:"stages"`
	Intervals        []TimelineInterval `json:"intervals,omitempty"`
	ProjectedFinish  *time.Time         `json:"projected_finish"`
	ProjectionReason string             `json:"projection_reason,omitempty"`
}

// TimelineProject groups the Automata of one project.
type TimelineProject struct {
	Project  string              `json:"project"`
	Automata []TimelineAutomaton `json:"automata"`
}

// TimelineCounts totals what the timeline shows.
type TimelineCounts struct {
	Projects int `json:"projects"`
	Automata int `json:"automata"`
	Stages   int `json:"stages"`
	Members  int `json:"members"`
	Blocked  int `json:"blocked"`
}

// TimelineResult is the timeline over the recorded Automata.
type TimelineResult struct {
	Today    time.Time         `json:"today"`
	Projects []TimelineProject `json:"projects"`
	Counts   TimelineCounts    `json:"counts"`
}

func timelineMinFinished() int { return 2 }

// BuildTimeline builds the timeline from the recorded start and finish times.
// It is pure: now is the today marker and nothing reads a clock.
func BuildTimeline(now time.Time, prds []*PRD, filter TimelineFilter) TimelineResult {
	res := TimelineResult{Today: now, Projects: []TimelineProject{}}
	byProject := map[string][]TimelineAutomaton{}
	if len(filter.Projects) > 0 && filter.Project != "" {
		filter.index = NewAutomatonProjectIndex(prds, filter.Automations, filter.Projects)
	}
	for _, p := range prds {
		if p == nil || !timelineMatchPRD(p, filter) {
			continue
		}
		a := buildTimelineAutomaton(now, p, filter.Stage)
		if filter.Stage != "" && len(a.Stages) == 0 {
			continue
		}
		byProject[a.Project] = append(byProject[a.Project], a)
	}
	names := make([]string, 0, len(byProject))
	for n := range byProject {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		as := byProject[n]
		sort.SliceStable(as, func(i, j int) bool {
			if c := timelineCmpStart(as[i].Start, as[j].Start); c != 0 {
				return c < 0
			}
			if as[i].Title != as[j].Title {
				return as[i].Title < as[j].Title
			}
			return as[i].ID < as[j].ID
		})
		res.Projects = append(res.Projects, TimelineProject{Project: n, Automata: as})
		res.Counts.Projects++
		for _, a := range as {
			res.Counts.Automata++
			res.Counts.Stages += len(a.Stages)
			for _, s := range a.Stages {
				res.Counts.Members += len(s.Members)
				for _, m := range s.Members {
					if timelineHasOpenKind(m.Intervals, TimelineBlocked) || m.Status == string(TaskBlocked) {
						res.Counts.Blocked++
					}
				}
			}
		}
	}
	return res
}

// timelineInProject applies the Project filter: through the membership
// resolver when it is available, else by the Automaton's directory.
func timelineInProject(p *PRD, f TimelineFilter) bool {
	if f.index == nil {
		return p.ProjectDir == f.Project
	}
	name := f.index.Of(p.ID).Project
	if name == "" {
		return false
	}
	if name == f.Project {
		return true
	}
	dir := strings.TrimSpace(f.Projects[name])
	return dir != "" && filepath.Clean(dir) == filepath.Clean(f.Project)
}

func timelineMatchPRD(p *PRD, f TimelineFilter) bool {
	if f.Project != "" && !timelineInProject(p, f) {
		return false
	}
	if f.Automaton != "" && p.ID != f.Automaton && p.Title != f.Automaton {
		return false
	}
	if f.Kind != "" && EffectiveKind(p, nil) != strings.ToLower(strings.TrimSpace(f.Kind)) {
		return false
	}
	if f.Tag != "" {
		found := false
		for _, t := range p.TagIDs {
			if t == f.Tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func buildTimelineAutomaton(now time.Time, p *PRD, stageFilter string) TimelineAutomaton {
	a := TimelineAutomaton{
		ID: p.ID, Title: p.Title, Project: p.ProjectDir, Kind: EffectiveKind(p, nil),
		Tags: append([]string(nil), p.TagIDs...), Status: string(p.Status),
		Stages: []TimelineStage{},
	}
	var finished []time.Time
	var firstStart *time.Time
	allDone := true
	var lastEnd *time.Time
	totalMembers, doneMembers := 0, 0
	for _, s := range p.Story {
		inFilter := stageFilter == "" || s.ID == stageFilter || s.Title == stageFilter
		st := TimelineStage{ID: s.ID, Title: s.Title, Status: string(s.Status), Members: []TimelineMember{}}
		stageDone := len(s.Tasks) > 0
		for _, t := range s.Tasks {
			totalMembers++
			m := timelineMember(now, t)
			if t.Status == TaskCompleted && t.CompletedAt != nil {
				doneMembers++
				finished = append(finished, *t.CompletedAt)
			}
			if t.Status != TaskCompleted && t.Status != TaskCancelled {
				stageDone = false
			}
			firstStart = timelineMin(firstStart, t.StartedAt)
			lastEnd = timelineMax(lastEnd, t.CompletedAt)
			st.Start = timelineMin(st.Start, t.StartedAt)
			if m.End == nil && m.Start != nil {
				stageDone = false
			}
			st.End = timelineMax(st.End, t.CompletedAt)
			st.Members = append(st.Members, m)
		}
		if !stageDone {
			st.End = nil
			allDone = false
		}
		if len(s.Tasks) == 0 {
			allDone = false
		}
		sort.SliceStable(st.Members, func(i, j int) bool {
			if c := timelineCmpStart(st.Members[i].Start, st.Members[j].Start); c != 0 {
				return c < 0
			}
			return st.Members[i].ID < st.Members[j].ID
		})
		if inFilter {
			a.Stages = append(a.Stages, st)
		}
	}
	sort.SliceStable(a.Stages, func(i, j int) bool {
		if c := timelineCmpStart(a.Stages[i].Start, a.Stages[j].Start); c != 0 {
			return c < 0
		}
		return a.Stages[i].ID < a.Stages[j].ID
	})
	a.Start = firstStart
	if allDone && totalMembers > 0 {
		a.End = lastEnd
	}
	a.Intervals = timelineDecisionIntervals(now, p.Decisions)
	a.ProjectedFinish, a.ProjectionReason = timelineProjection(now, firstStart, finished, totalMembers-doneMembers, a.End != nil)
	return a
}

func timelineMember(now time.Time, t Task) TimelineMember {
	m := TimelineMember{ID: t.ID, Title: t.Title, Status: string(t.Status), Start: t.StartedAt, End: t.CompletedAt}
	var kind, reason string
	switch t.Status {
	case TaskBlocked, TaskReviewNeeded, TaskFailed:
		kind, reason = TimelineBlocked, string(t.Status)
	case TaskWaitingCapacity, TaskWaitingDecision, TaskAwaitingApproval:
		kind, reason = TimelineWaiting, t.WaitReason
		if reason == "" {
			reason = string(t.Status)
		}
	}
	if kind != "" && !t.UpdatedAt.IsZero() {
		m.Intervals = append(m.Intervals, TimelineInterval{Kind: kind, Start: t.UpdatedAt, Reason: reason})
	}
	// Delivery failures are recorded blocked moments that already ended.
	for _, d := range t.DeliveryFailureEvents {
		m.Intervals = append(m.Intervals, TimelineInterval{Kind: TimelineBlocked, Start: d.At, End: timelinePtr(d.At), Reason: d.Reason})
	}
	sort.SliceStable(m.Intervals, func(i, j int) bool { return m.Intervals[i].Start.Before(m.Intervals[j].Start) })
	return m
}

// timelineDecisionIntervals pairs hold decisions with the next resolving one.
func timelineDecisionIntervals(now time.Time, ds []Decision) []TimelineInterval {
	sorted := append([]Decision(nil), ds...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	var out []TimelineInterval
	var open *TimelineInterval
	closeOpen := func(at time.Time) {
		if open != nil {
			open.End = timelinePtr(at)
			out = append(out, *open)
			open = nil
		}
	}
	for _, d := range sorted {
		switch d.Kind {
		case "review_needed":
			closeOpen(d.At)
			open = &TimelineInterval{Kind: TimelineBlocked, Start: d.At, Reason: d.Kind}
		case "decision_gate_hold":
			closeOpen(d.At)
			open = &TimelineInterval{Kind: TimelineWaiting, Start: d.At, Reason: d.Kind}
		case "operator_decision", "operator_answer", "resume", "approve":
			closeOpen(d.At)
		}
	}
	if open != nil {
		out = append(out, *open)
	}
	_ = now
	return out
}

// timelineProjection projects a finish from completed members per active hour.
func timelineProjection(now time.Time, start *time.Time, finished []time.Time, remaining int, done bool) (*time.Time, string) {
	if done {
		return nil, "finished"
	}
	if len(finished) < timelineMinFinished() || start == nil {
		return nil, "fewer than two members have finished"
	}
	hours := ActiveHours(*start, now, nil)
	if hours <= 0 {
		return nil, "no active time recorded"
	}
	rate := float64(len(finished)) / hours
	if rate <= 0 {
		return nil, "no throughput measured"
	}
	if remaining <= 0 {
		return timelinePtr(now), ""
	}
	d := time.Duration(float64(remaining) / rate * float64(time.Hour))
	return timelinePtr(now.Add(d)), ""
}

func timelinePtr(t time.Time) *time.Time { return &t }

func timelineMin(a, b *time.Time) *time.Time {
	if b == nil || (a != nil && !b.Before(*a)) {
		return a
	}
	return b
}

func timelineMax(a, b *time.Time) *time.Time {
	if b == nil || (a != nil && !b.After(*a)) {
		return a
	}
	return b
}

// timelineCmpStart orders nil starts last.
func timelineCmpStart(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case a.Before(*b):
		return -1
	case a.After(*b):
		return 1
	}
	return 0
}

func timelineHasOpenKind(is []TimelineInterval, kind string) bool {
	for _, i := range is {
		if i.Kind == kind && i.End == nil {
			return true
		}
	}
	return false
}
