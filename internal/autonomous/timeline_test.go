package autonomous

import (
	"testing"
	"time"
)

var tlNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func tlAt(h int) *time.Time {
	t := time.Date(2026, 11, 11, h, 0, 0, 0, time.UTC)
	return &t
}

func tlTask(id string, st TaskStatus, s, e *time.Time) Task {
	return Task{ID: id, Title: id, Status: st, StartedAt: s, CompletedAt: e}
}

func tlPRD(id, dir, kind string, tags []string, stories ...Story) *PRD {
	return &PRD{ID: id, Title: id, ProjectDir: dir, Kind: kind, TagIDs: tags, Story: stories}
}

func TestBuildTimelineEmpty(t *testing.T) {
	r := BuildTimeline(tlNow, nil, TimelineFilter{})
	if !r.Today.Equal(tlNow) || len(r.Projects) != 0 || r.Counts != (TimelineCounts{}) {
		t.Fatalf("unexpected %+v", r)
	}
}

func TestBuildTimelineOpenBarAndProjection(t *testing.T) {
	cases := []struct {
		name    string
		tasks   []Task
		wantNil bool
		reason  string
		finish  time.Time
	}{
		{"no history", []Task{tlTask("a", TaskInProgress, tlAt(8), nil), tlTask("b", TaskPending, nil, nil)}, true, "fewer than two members have finished", time.Time{}},
		{"one finished", []Task{tlTask("a", TaskCompleted, tlAt(8), tlAt(9)), tlTask("b", TaskPending, nil, nil)}, true, "fewer than two members have finished", time.Time{}},
		// 2 done in 4 active hours = 0.5/h; 2 remaining = 4h.
		{"enough history", []Task{tlTask("a", TaskCompleted, tlAt(8), tlAt(9)), tlTask("b", TaskCompleted, tlAt(9), tlAt(10)),
			tlTask("c", TaskInProgress, tlAt(10), nil), tlTask("d", TaskPending, nil, nil)}, false, "", tlNow.Add(4 * time.Hour)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := BuildTimeline(tlNow, []*PRD{tlPRD("p", "/w", "", nil, Story{ID: "s", Tasks: c.tasks})}, TimelineFilter{})
			a := r.Projects[0].Automata[0]
			if (a.ProjectedFinish == nil) != c.wantNil {
				t.Fatalf("finish=%v", a.ProjectedFinish)
			}
			if c.wantNil && a.ProjectionReason != c.reason {
				t.Fatalf("reason=%q", a.ProjectionReason)
			}
			if !c.wantNil && !a.ProjectedFinish.Equal(c.finish) {
				t.Fatalf("finish=%v want %v", a.ProjectedFinish, c.finish)
			}
			open := a.Stages[0].Members[0]
			if c.name == "no history" && (open.End != nil || open.Start == nil) {
				t.Fatalf("open bar wrong: %+v", open)
			}
			if a.End != nil {
				t.Fatalf("automaton should be open")
			}
		})
	}
}

func TestBuildTimelineBlockedAndWaiting(t *testing.T) {
	blocked := tlTask("b", TaskBlocked, tlAt(8), nil)
	blocked.UpdatedAt = *tlAt(10)
	wait := tlTask("w", TaskWaitingCapacity, nil, nil)
	wait.UpdatedAt = *tlAt(11)
	wait.WaitReason = "lane full"
	p := tlPRD("p", "/w", "", nil, Story{ID: "s", Tasks: []Task{blocked, wait}})
	p.Decisions = []Decision{
		{At: *tlAt(9), Kind: "review_needed"},
		{At: *tlAt(11), Kind: "operator_decision"},
		{At: *tlAt(11), Kind: "decision_gate_hold"},
	}
	r := BuildTimeline(tlNow, []*PRD{p}, TimelineFilter{})
	a := r.Projects[0].Automata[0]
	ms := a.Stages[0].Members
	if ms[0].ID != "b" || len(ms[0].Intervals) != 1 || ms[0].Intervals[0].Kind != TimelineBlocked || ms[0].Intervals[0].End != nil {
		t.Fatalf("blocked member: %+v", ms)
	}
	if ms[1].Intervals[0].Kind != TimelineWaiting || ms[1].Intervals[0].Reason != "lane full" {
		t.Fatalf("waiting member: %+v", ms[1])
	}
	if len(a.Intervals) != 2 || a.Intervals[0].End == nil || !a.Intervals[0].End.Equal(*tlAt(11)) || a.Intervals[1].Kind != TimelineWaiting || a.Intervals[1].End != nil {
		t.Fatalf("automaton intervals: %+v", a.Intervals)
	}
	if r.Counts.Blocked != 1 {
		t.Fatalf("blocked count %d", r.Counts.Blocked)
	}
}

func TestBuildTimelineFiltersAndGrouping(t *testing.T) {
	s1 := Story{ID: "s1", Title: "One", Tasks: []Task{tlTask("a", TaskCompleted, tlAt(8), tlAt(9))}}
	s2 := Story{ID: "s2", Title: "Two", Tasks: []Task{tlTask("b", TaskPending, nil, nil)}}
	prds := []*PRD{
		tlPRD("p2", "/b", "bug", []string{"x"}, s2),
		tlPRD("p1", "/a", "feature", []string{"x", "y"}, s1, s2),
		tlPRD("p3", "/a", "other", nil, s1),
	}
	cases := []struct {
		name     string
		f        TimelineFilter
		projects int
		automata int
		stages   int
	}{
		{"none", TimelineFilter{}, 2, 3, 4},
		{"project", TimelineFilter{Project: "/a"}, 1, 2, 3},
		{"automaton", TimelineFilter{Automaton: "p2"}, 1, 1, 1},
		{"stage", TimelineFilter{Stage: "s2"}, 2, 2, 2},
		{"kind", TimelineFilter{Kind: "bug"}, 1, 1, 1},
		{"tag", TimelineFilter{Tag: "y"}, 1, 1, 2},
		{"no match", TimelineFilter{Tag: "zzz"}, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := BuildTimeline(tlNow, prds, c.f)
			if r.Counts.Projects != c.projects || r.Counts.Automata != c.automata || r.Counts.Stages != c.stages {
				t.Fatalf("counts %+v", r.Counts)
			}
		})
	}
	r := BuildTimeline(tlNow, prds, TimelineFilter{})
	if r.Projects[0].Project != "/a" || r.Projects[0].Automata[0].ID != "p1" || r.Projects[0].Automata[1].ID != "p3" {
		t.Fatalf("sort order wrong: %+v", r.Projects)
	}
	if r.Counts.Members != 4 {
		t.Fatalf("members %d", r.Counts.Members)
	}
}
