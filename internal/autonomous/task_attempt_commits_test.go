package autonomous

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestTaskRecordAttemptCommitKeepsOrder records distinct shas in the order
// given.
func TestTaskRecordAttemptCommitKeepsOrder(t *testing.T) {
	task := &Task{}
	task.RecordAttemptCommit("aaa")
	task.RecordAttemptCommit("bbb")
	task.RecordAttemptCommit("ccc")
	want := []string{"aaa", "bbb", "ccc"}
	if !reflect.DeepEqual(task.AttemptCommitSHAs, want) {
		t.Fatalf("AttemptCommitSHAs = %v, want %v", task.AttemptCommitSHAs, want)
	}
}

// TestTaskRecordAttemptCommitIgnoresEmpty ensures an empty sha is a no-op:
// the list is not extended and, on a fresh task, stays empty (nil-safe via
// omitempty on the wire).
func TestTaskRecordAttemptCommitIgnoresEmpty(t *testing.T) {
	task := &Task{}
	task.RecordAttemptCommit("")
	if len(task.AttemptCommitSHAs) != 0 {
		t.Fatalf("empty sha recorded: %v", task.AttemptCommitSHAs)
	}
	task.RecordAttemptCommit("aaa")
	task.RecordAttemptCommit("")
	task.RecordAttemptCommit("")
	want := []string{"aaa"}
	if !reflect.DeepEqual(task.AttemptCommitSHAs, want) {
		t.Fatalf("AttemptCommitSHAs = %v, want %v", task.AttemptCommitSHAs, want)
	}
	// A nil receiver must not panic.
	var nilTask *Task
	nilTask.RecordAttemptCommit("")
}

// TestTaskRecordAttemptCommitIgnoresDuplicates records the same sha only
// once, regardless of how many times it is recorded.
func TestTaskRecordAttemptCommitIgnoresDuplicates(t *testing.T) {
	task := &Task{}
	task.RecordAttemptCommit("aaa")
	task.RecordAttemptCommit("bbb")
	task.RecordAttemptCommit("aaa")
	task.RecordAttemptCommit("bbb")
	want := []string{"aaa", "bbb"}
	if !reflect.DeepEqual(task.AttemptCommitSHAs, want) {
		t.Fatalf("AttemptCommitSHAs = %v, want %v", task.AttemptCommitSHAs, want)
	}
}

// TestTaskJSONRoundTrip ensures a Task carrying PreTaskSHA, BaseSHA and
// AttemptCommitSHAs survives a JSON marshal/unmarshal trip unchanged —
// the same path the PRD store persistence and REST views take.
func TestTaskJSONRoundTrip(t *testing.T) {
	original := &Task{
		ID:                "task-1",
		StoryID:           "story-1",
		Title:             "title",
		Status:            TaskCompleted,
		PreTaskSHA:        "per-attempt-sha",
		BaseSHA:           "per-task-sha",
		AttemptCommitSHAs: []string{"c1", "c2"},
		RetryCount:        2,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PreTaskSHA != original.PreTaskSHA {
		t.Errorf("PreTaskSHA = %q, want %q", decoded.PreTaskSHA, original.PreTaskSHA)
	}
	if decoded.BaseSHA != original.BaseSHA {
		t.Errorf("BaseSHA = %q, want %q", decoded.BaseSHA, original.BaseSHA)
	}
	if !reflect.DeepEqual(decoded.AttemptCommitSHAs, original.AttemptCommitSHAs) {
		t.Errorf("AttemptCommitSHAs = %v, want %v", decoded.AttemptCommitSHAs, original.AttemptCommitSHAs)
	}
	// A second trip must stay identical.
	data2, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var decoded2 Task
	if err := json.Unmarshal(data2, &decoded2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, decoded2) {
		t.Errorf("round trip is not stable:\n%+v\nvs\n%+v", decoded, decoded2)
	}
}

// TestTaskJSONRoundTripOmitsEmpty verifies omitempty: a task without
// BaseSHA/AttemptCommitSHAs does not emit those keys at all.
func TestTaskJSONRoundTripOmitsEmpty(t *testing.T) {
	task := &Task{ID: "task-2", PreTaskSHA: "sha"}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["base_sha"]; ok {
		t.Errorf("empty BaseSHA should be omitted from JSON, got: %s", data)
	}
	if _, ok := raw["attempt_commit_shas"]; ok {
		t.Errorf("empty AttemptCommitSHAs should be omitted from JSON, got: %s", data)
	}
}
