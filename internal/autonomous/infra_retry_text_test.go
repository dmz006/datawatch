package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func defaultInfraRetrySettings() InfraRetrySettings {
	return InfraRetrySettings{
		Enabled:         true,
		MaxTries:        3,
		BackoffSeconds:  []int{60, 300, 540},
		Categories:      []string{"spawn_error"},
		ExhaustedAction: "hold",
		RetryDependents: true,
		RaiseQuestion:   true,
	}
}

func TestInfraRetryText_SettingsLine(t *testing.T) {
	out := RenderInfraRetryText(InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings()}, 0)
	first := strings.SplitN(out, "\n", 2)[0]
	want := "Infrastructure retry: on, 3 tries, back-off 60s/300s/540s, hold when used up"
	if first != want {
		t.Errorf("first line = %q, want %q", first, want)
	}
}

func TestInfraRetryText_SettingsVariants(t *testing.T) {
	s := defaultInfraRetrySettings()
	s.Enabled = false
	s.MaxTries = 1
	s.BackoffSeconds = []int{}
	s.ExhaustedAction = "count"
	out := RenderInfraRetryText(InfraRetryStatus{InfraRetrySettings: s}, 0)
	want := "Infrastructure retry: off, 1 try, no back-off, count as a failure when used up\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want prefix %q", out, want)
	}
}

func TestInfraRetryText_EmptyList(t *testing.T) {
	out := RenderInfraRetryText(InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings(), Tasks: []InfraRetryTask{}}, 0)
	if !strings.HasSuffix(out, "No infrastructure retries yet.\n") {
		t.Errorf("empty status lacks the empty line:\n%s", out)
	}
}

func sampleInfraRetryTasks() []InfraRetryTask {
	at := time.Date(2026, 10, 30, 9, 15, 0, 0, time.UTC)
	return []InfraRetryTask{
		{
			AutomatonID: "p1", AutomatonTitle: "Add paging", TaskID: "t1", TaskTitle: "Write handler",
			Status: TaskPending, RetryCount: 2, MaxTries: 3,
			Retries: []InfraRetryEntry{
				{Number: 1, Class: "spawn_error", Reason: "tmux refused the session", At: at, DelaySeconds: 60, NextAt: at.Add(time.Minute)},
				{Number: 2, Class: "daemon_restart", At: at.Add(10 * time.Minute), DelaySeconds: 300},
			},
		},
		{
			AutomatonID: "p2", AutomatonTitle: "Fix login", TaskID: "t7", TaskTitle: "Add test",
			Status: TaskReviewNeeded, RetryCount: 3, MaxTries: 3,
			Retries: []InfraRetryEntry{{Number: 3, Class: "storage_write", Reason: "disk full", At: at}},
			Held:    true, HeldCause: "storage_write: disk full", QuestionID: "q9",
		},
	}
}

func TestInfraRetryText_TaskBlocks(t *testing.T) {
	st := InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings(), Tasks: sampleInfraRetryTasks(), Count: 2}
	out := RenderInfraRetryText(st, 0)
	for _, w := range []string{
		`Automaton p1 "Add paging", task t1 "Write handler": pending, retry 2 of 3` + "\n",
		"  retry 1: spawn_error: tmux refused the session (2026-10-30 09:15:00)\n",
		"  retry 2: daemon_restart (2026-10-30 09:25:00)\n",
		`Automaton p2 "Fix login", task t7 "Add test": review_needed, retry 3 of 3` + "\n",
		"  held after infrastructure retries: storage_write: disk full\n",
		"  waiting on question q9\n",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "No infrastructure retries yet.") {
		t.Errorf("a status with tasks must not say it is empty:\n%s", out)
	}
	// Only the held task carries the held line.
	if strings.Count(out, "held after infrastructure retries:") != 1 {
		t.Errorf("held line must appear once:\n%s", out)
	}
}

func TestInfraRetryText_LimitCapsTasks(t *testing.T) {
	st := InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings(), Tasks: sampleInfraRetryTasks(), Count: 2}
	out := RenderInfraRetryText(st, 1)
	if !strings.Contains(out, "task t1") || strings.Contains(out, "task t7") {
		t.Errorf("limit 1 must show only the first task:\n%s", out)
	}
}

func TestInfraRetryText_NeverSaysPRD(t *testing.T) {
	st := InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings(), Tasks: sampleInfraRetryTasks(), Count: 2}
	for _, out := range []string{
		RenderInfraRetryText(st, 0),
		RenderInfraRetryText(InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings()}, 0),
	} {
		if strings.Contains(out, "PRD") {
			t.Errorf("user-visible text must say Automaton, not PRD:\n%s", out)
		}
	}
}

func TestInfraRetryText_ParseRoundTrip(t *testing.T) {
	want := InfraRetryStatus{InfraRetrySettings: defaultInfraRetrySettings(), Tasks: sampleInfraRetryTasks(), Count: 2}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseInfraRetryStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	if RenderInfraRetryText(got, 0) != RenderInfraRetryText(want, 0) {
		t.Errorf("parsed status renders differently from the original")
	}
}

func TestInfraRetryText_ParseRejectsBadBody(t *testing.T) {
	if _, err := ParseInfraRetryStatus([]byte("not json")); err == nil || !strings.Contains(err.Error(), "infrastructure retry status") {
		t.Errorf("expected a clear parse error, got %v", err)
	}
}
