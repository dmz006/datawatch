package autonomous

// The infrastructure retry read view: the effective settings, the tasks that
// were retried or are held, their order and limit, and the open question.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestInfraRetryView_DefaultSettings(t *testing.T) {
	got := InfraRetrySettingsFrom(config.InfraRetryConfig{})
	if !got.Enabled || got.MaxTries != 3 || !got.RetryDependents || !got.RaiseQuestion || got.ExhaustedAction != "hold" {
		t.Fatalf("defaults = %+v", got)
	}
	if len(got.BackoffSeconds) != 3 || got.BackoffSeconds[0] != 60 || got.BackoffSeconds[2] != 540 {
		t.Errorf("backoff = %v, want the default list", got.BackoffSeconds)
	}
	if len(got.Categories) != 4 {
		t.Errorf("categories = %v, want all four classes", got.Categories)
	}
}

func TestInfraRetryView_ExplicitValuesSurvive(t *testing.T) {
	got := InfraRetrySettingsFrom(config.InfraRetryConfig{
		Enabled:         boolPtr(false),
		MaxTries:        5,
		BackoffSeconds:  []int{},
		Categories:      []string{},
		ExhaustedAction: config.ExhaustedActionCount,
		RetryDependents: boolPtr(false),
		RaiseQuestion:   boolPtr(false),
	})
	if got.Enabled || got.RetryDependents || got.RaiseQuestion || got.MaxTries != 5 || got.ExhaustedAction != "count" {
		t.Fatalf("explicit values = %+v", got)
	}
	if got.BackoffSeconds == nil || len(got.BackoffSeconds) != 0 || got.Categories == nil || len(got.Categories) != 0 {
		t.Fatalf("explicit empty lists = %v / %v, want empty and not nil", got.BackoffSeconds, got.Categories)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"backoff_seconds":[]`) || !strings.Contains(string(b), `"categories":[]`) {
		t.Errorf("JSON = %s, want empty lists encoded as []", b)
	}
}

func TestInfraRetryView_NoTasksEncodesEmptyList(t *testing.T) {
	_, api, _, _ := apiFixture(t)
	st := api.InfraRetryStatus("", 50)
	b, _ := json.Marshal(st)
	if st.Count != 0 || !strings.Contains(string(b), `"tasks":[]`) || !strings.Contains(string(b), `"count":0`) {
		t.Fatalf("empty status = %s", b)
	}
}

func TestInfraRetryView_HeldTaskWithOpenQuestion(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	_, store := wireInfraHoldRig(t, r, true)
	r.run(t, alwaysFailSpawn, passingVerify)
	prd, task := heldTask(t, r)
	open := store.List(QuestionFilter{State: QuestionOpen})
	if len(open) != 1 {
		t.Fatalf("open questions = %d, want 1", len(open))
	}

	st := r.api.InfraRetryStatus("", 50)
	if st.Count != 1 || len(st.Tasks) != 1 {
		t.Fatalf("status lists %d tasks (count %d), want 1: %+v", len(st.Tasks), st.Count, st.Tasks)
	}
	row := st.Tasks[0]
	if row.AutomatonID != prd.ID || row.AutomatonTitle != prd.Title || row.TaskID != task.ID || row.TaskTitle != task.Title {
		t.Errorf("identity = %+v", row)
	}
	if row.Status != TaskReviewNeeded || !row.Held || row.RetryCount != 3 || row.MaxTries != 3 {
		t.Errorf("state = status %q held %v retry_count %d max_tries %d", row.Status, row.Held, row.RetryCount, row.MaxTries)
	}
	if !strings.Contains(row.HeldCause, holdTestClass) || !strings.Contains(row.HeldCause, holdTestCause) {
		t.Errorf("held_cause = %q, want the class and the cause", row.HeldCause)
	}
	if row.QuestionID != open[0].ID {
		t.Errorf("question_id = %q, want %q", row.QuestionID, open[0].ID)
	}
	if len(row.Retries) != 3 {
		t.Fatalf("retries = %d, want 3", len(row.Retries))
	}
	for i, rec := range row.Retries {
		if rec.Number != i+1 || rec.Class != holdTestClass || !strings.Contains(rec.Reason, holdTestCause) || rec.At.IsZero() {
			t.Errorf("retry %d = %+v", i, rec)
		}
	}
	if row.Retries[0].DelaySeconds != 60 || row.Retries[2].DelaySeconds != 540 {
		t.Errorf("delays = %d, %d, want the default 60 and 540", row.Retries[0].DelaySeconds, row.Retries[2].DelaySeconds)
	}
}

func TestInfraRetryView_HeldTaskWithoutQuestion(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	wireInfraHoldRig(t, r, false)
	r.run(t, alwaysFailSpawn, passingVerify)
	st := r.api.InfraRetryStatus("", 50)
	if len(st.Tasks) != 1 || !st.Tasks[0].Held || st.Tasks[0].QuestionID != "" {
		t.Fatalf("status = %+v, want one held task with no question", st.Tasks)
	}
}

func TestInfraRetryView_RecoveredTaskStaysListed(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	_, task := r.run(t, func(_ context.Context, _ SpawnRequest, n int) (SpawnResult, error) {
		if n == 1 {
			return SpawnResult{}, errors.New(holdTestCause)
		}
		return SpawnResult{}, nil
	}, passingVerify)
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %q, want completed", task.Status)
	}
	st := r.api.InfraRetryStatus("", 50)
	if len(st.Tasks) != 1 {
		t.Fatalf("status lists %d tasks, want the recovered one", len(st.Tasks))
	}
	row := st.Tasks[0]
	if row.Held || row.QuestionID != "" || row.HeldCause != "" {
		t.Errorf("a recovered task is not held: %+v", row)
	}
	if row.RetryCount != 0 || len(row.Retries) != 1 {
		t.Errorf("retry_count %d, retries %d, want the count cleared and the history kept", row.RetryCount, len(row.Retries))
	}
}

// seedInfraRetryTasks stores an Automaton with three tasks: one retried
// earlier, one retried later and one never retried.
func seedInfraRetryTasks(t *testing.T, api *API) (prdID string, older, newer, plain string) {
	t.Helper()
	prd, err := api.M.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := api.M.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: []Task{
		{Title: "Older"}, {Title: "Newer"}, {Title: "Plain"},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	fresh, _ := api.M.Store().GetPRD(prd.ID)
	base := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	tasks := fresh.Story[0].Tasks
	tasks[0].InfraRetries = []InfraRetryRecord{{Number: 1, Class: config.InfraRetryClassStorageWrite, At: base}}
	tasks[0].InfraRetryCount = 1
	tasks[1].InfraRetries = []InfraRetryRecord{{Number: 1, Class: config.InfraRetryClassDaemonRestart, At: base.Add(time.Hour)}}
	tasks[1].InfraRetryCount = 1
	if err := api.M.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd.ID, tasks[0].ID, tasks[1].ID, tasks[2].ID
}

func TestInfraRetryView_OrderLimitAndFilter(t *testing.T) {
	_, api, _, _ := apiFixture(t)
	prdID, older, newer, plain := seedInfraRetryTasks(t, api)

	st := api.InfraRetryStatus("", 50)
	if len(st.Tasks) != 2 || st.Tasks[0].TaskID != newer || st.Tasks[1].TaskID != older {
		t.Fatalf("tasks = %+v, want the newest retry first and the never-retried task left out", st.Tasks)
	}
	for _, row := range st.Tasks {
		if row.TaskID == plain {
			t.Errorf("a task without retries was listed: %+v", row)
		}
	}

	one := api.InfraRetryStatus("", 1)
	if one.Count != 1 || len(one.Tasks) != 1 || one.Tasks[0].TaskID != newer {
		t.Errorf("limit 1 = %+v, want only the newest", one.Tasks)
	}
	if all := api.InfraRetryStatus("", 0); len(all.Tasks) != 2 {
		t.Errorf("limit 0 lists %d tasks, want all 2", len(all.Tasks))
	}

	named := api.InfraRetryStatus(prdID, 50)
	if len(named.Tasks) != 2 {
		t.Errorf("filter by the Automaton = %d tasks, want 2", len(named.Tasks))
	}
	other := api.InfraRetryStatus("no-such-automaton", 50)
	if other.Count != 0 || other.Tasks == nil || len(other.Tasks) != 0 {
		t.Errorf("filter by an unknown Automaton = %+v, want an empty list", other)
	}
	if other.MaxTries != 3 || !other.Enabled {
		t.Errorf("the settings are served whatever the filter: %+v", other.InfraRetrySettings)
	}
}

func TestInfraRetryView_FollowsLiveConfig(t *testing.T) {
	_, api, _, _ := apiFixture(t)
	api.M.mu.Lock()
	api.M.cfg.InfraRetry = config.InfraRetryConfig{MaxTries: 7, Enabled: boolPtr(false)}
	api.M.mu.Unlock()
	st := api.InfraRetryStatus("", 50)
	if st.Enabled || st.MaxTries != 7 {
		t.Fatalf("settings = %+v, want the live config", st.InfraRetrySettings)
	}
}

func TestInfraRetryView_TaskEntryCarriesRetries(t *testing.T) {
	_, api, _, _ := apiFixture(t)
	prdID, older, _, plain := seedInfraRetryTasks(t, api)
	prd, _ := api.M.Store().GetPRD(prdID)
	view := BuildTasksView(prd)
	by := map[string]TaskEntry{}
	for _, e := range view.Tasks {
		by[e.ID] = e
	}
	if e := by[older]; e.InfraRetryCount != 1 || len(e.InfraRetries) != 1 || e.InfraRetries[0].Class != config.InfraRetryClassStorageWrite {
		t.Errorf("retried task entry = %+v", e)
	}
	b, _ := json.Marshal(by[plain])
	if strings.Contains(string(b), "infra_retr") {
		t.Errorf("a task without retries carries retry fields: %s", b)
	}
	b, _ = json.Marshal(by[older])
	if !strings.Contains(string(b), `"infra_retries":[`) || !strings.Contains(string(b), `"infra_retry_count":1`) {
		t.Errorf("retried task JSON = %s", b)
	}
}
