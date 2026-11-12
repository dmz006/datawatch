package autonomous

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

var timelineTextNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func timelineTextPRDs() []*PRD {
	return []*PRD{
		{
			ID: "aaaa0001", Title: "Fix the crash", Status: PRDRunning,
			ProjectDir: "/home/<user>/projA", Kind: PRDKindBug,
			Story: []Story{{
				ID: "aaaa0001-s1", Title: "Find and fix it", Status: StoryInProgress,
				Tasks: []Task{
					{ID: "aaaa0001-t1", Title: "Reproduce the crash", Status: TaskCompleted,
						StartedAt:   timelinePtr(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)),
						CompletedAt: timelinePtr(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))},
					{ID: "aaaa0001-t2", Title: "Land the fix", Status: TaskCompleted,
						StartedAt:   timelinePtr(time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)),
						CompletedAt: timelinePtr(time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC))},
				},
			}},
		},
		{
			ID: "bbbb0002", Title: "Add the widget", Status: PRDRunning,
			ProjectDir: "/home/<user>/projB", Kind: PRDKindFeature,
			Story: []Story{{
				ID: "bbbb0002-s1", Title: "Wire it up", Status: StoryInProgress,
				Tasks: []Task{
					{ID: "bbbb0002-t1", Title: "Build the shell", Status: TaskCompleted,
						StartedAt:   timelinePtr(time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)),
						CompletedAt: timelinePtr(time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC))},
					{ID: "bbbb0002-t2", Title: "Paint it", Status: TaskCompleted,
						StartedAt:   timelinePtr(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)),
						CompletedAt: timelinePtr(time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC))},
					{ID: "bbbb0002-t3", Title: "Fit the widget", Status: TaskBlocked,
						StartedAt:  timelinePtr(time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)),
						UpdatedAt:  time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC),
						WaitReason: "waiting on the operator"},
					{ID: "bbbb0002-t4", Title: "Ship it", Status: TaskPending},
				},
			}},
		},
	}
}

func TestRenderTimelineBarsGolden(t *testing.T) {
	got := RenderTimelineBars(BuildTimeline(timelineTextNow, timelineTextPRDs(), TimelineFilter{}))
	want := `Timeline as of 2026-10-23: 2 projects, 2 Automata, 2 stages, 6 members, 1 blocked
span 2026-10-20 -> 2026-10-25   # work  X blocked  ~ waiting  T today  P projected finish

/home/<user>/projA
  Fix the crash (bug, running) — finished 2026-10-21
    Find and fix it (in_progress)
      Reproduce the crash              #                      T                
      Land the fix                            ##              T                
/home/<user>/projB
  Add the widget (feature, running) — projected 2026-10-25
    Wire it up (in_progress)
      Build the shell                         ##              T               P
      Paint it                                        #       T               P
      Fit the widget                                         XT               P
      Ship it                                                 T               P
`
	if got != want {
		t.Fatalf("bars differ from the golden\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTimelineSummaryGolden(t *testing.T) {
	got := RenderTimelineSummary(BuildTimeline(timelineTextNow, timelineTextPRDs(), TimelineFilter{}))
	want := `Timeline as of 2026-10-23: 2 projects, 2 Automata, 2 stages, 6 members, 1 blocked
Fix the crash (running): 2 members, 2 done, 0 blocked — finished 2026-10-21
Add the widget (running): 4 members, 2 done, 1 blocked — projected 2026-10-25
`
	if got != want {
		t.Fatalf("summary differs from the golden\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTimelineEmpty(t *testing.T) {
	res := BuildTimeline(timelineTextNow, nil, TimelineFilter{})
	if got := RenderTimelineBars(res); got != "No Automata to draw.\n" {
		t.Fatalf("bars for the empty chart: %q", got)
	}
	if got := RenderTimelineSummary(res); got != "No Automata to draw." {
		t.Fatalf("summary for the empty chart: %q", got)
	}
}

func TestParseTimelineRoundTrip(t *testing.T) {
	res := BuildTimeline(timelineTextNow, timelineTextPRDs(), TimelineFilter{})
	body, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseTimeline(body)
	if err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, again) {
		t.Fatalf("round trip differs\n got: %s\nwant: %s", again, body)
	}
}

func TestParseTimelineRejectsGarbage(t *testing.T) {
	if _, err := ParseTimeline([]byte("not json")); err == nil {
		t.Fatal("garbage must not decode")
	}
}

func TestTimelineChartSpanWidensASingleInstant(t *testing.T) {
	res := TimelineResult{
		Today: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		Projects: []TimelineProject{{Project: "p", Automata: []TimelineAutomaton{
			{Title: "a", Stages: []TimelineStage{{Title: "s", Members: []TimelineMember{{ID: "m", Title: "not started"}}}}},
		}}},
	}
	lo, hi := timelineChartSpan(res)
	if !hi.After(lo) {
		t.Fatalf("a single-instant chart must widen to at least one day: %s -> %s", lo, hi)
	}
	// A not-yet-started member still draws its today marker and nothing else.
	bar := timelineMemberBar(res.Projects[0].Automata[0].Stages[0].Members[0], lo, hi, res.Today, nil)
	if bar[0] != 'T' {
		t.Fatalf("today marker must sit on the first column when today is the span start: %q", bar)
	}
}

func TestTimelineTitleTruncatesLongNames(t *testing.T) {
	long := "a very long member title that goes on and on past the width"
	if got := timelineTitle(long); len([]rune(got)) != timelineTitleWidth {
		t.Fatalf("truncated title must be exactly %d runes, got %q", timelineTitleWidth, got)
	}
	if got := timelineTitle("short"); got != "short" {
		t.Fatalf("short titles stay untouched, got %q", got)
	}
}
