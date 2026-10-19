package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAttemptSnapshotRecordedOnTask(t *testing.T) {
	task := &Task{ID: "t1"}

	recordAttemptSnapshot(task, "")
	if len(task.AttemptRefs) != 0 {
		t.Fatalf("empty ref must be ignored, got %v", task.AttemptRefs)
	}

	recordAttemptSnapshot(task, "refs/datawatch/attempts/t1/1")
	recordAttemptSnapshot(task, "refs/datawatch/attempts/t1/2")
	if len(task.AttemptRefs) != 2 ||
		task.AttemptRefs[0] != "refs/datawatch/attempts/t1/1" ||
		task.AttemptRefs[1] != "refs/datawatch/attempts/t1/2" {
		t.Fatalf("refs not appended in order: %v", task.AttemptRefs)
	}

	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"attempt_refs":["refs/datawatch/attempts/t1/1","refs/datawatch/attempts/t1/2"]`) {
		t.Fatalf("attempt_refs missing from JSON: %s", raw)
	}
	var back Task
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.AttemptRefs) != 2 || back.AttemptRefs[1] != "refs/datawatch/attempts/t1/2" {
		t.Fatalf("attempt_refs lost in round trip: %v", back.AttemptRefs)
	}

	empty, err := json.Marshal(&Task{ID: "t2"})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(empty), "attempt_refs") {
		t.Fatalf("empty attempt_refs must be omitted: %s", empty)
	}
}
