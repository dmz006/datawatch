package autonomous

import (
	"strings"
	"testing"
)

func TestCeilingRetryParagraphAfterCeilingFailure(t *testing.T) {
	task := &Task{Attempts: []TaskAttempt{{Number: 1, MemoryKilled: true, CeilingMB: 2048, PeakMemoryMB: 2300, KilledProcess: "go"}}}
	got := ceilingRetryParagraph(task)
	for _, want := range []string{"limit of 2048 MB", "process go", "peak 2300 MB", "unbounded recursion",
		"stream instead of holding data", "cap collection sizes", "test with a small input first"} {
		if !strings.Contains(got, want) {
			t.Errorf("paragraph missing %q: %s", want, got)
		}
	}
}

func TestCeilingRetryParagraphUnknownProcess(t *testing.T) {
	task := &Task{Attempts: []TaskAttempt{{Number: 1, MemoryKilled: true, CeilingMB: 512, PeakMemoryMB: 600}}}
	if got := ceilingRetryParagraph(task); !strings.Contains(got, "process unknown") {
		t.Errorf("want unknown process, got %s", got)
	}
}

func TestCeilingRetryParagraphAbsentOtherwise(t *testing.T) {
	if ceilingRetryParagraph(nil) != "" || ceilingRetryParagraph(&Task{}) != "" {
		t.Error("expected empty for no attempts")
	}
	other := &Task{Attempts: []TaskAttempt{{Number: 1, Outcome: "failed", PeakMemoryMB: 10}}}
	if got := ceilingRetryParagraph(other); got != "" {
		t.Errorf("paragraph shown for non-ceiling failure: %s", got)
	}
	// A ceiling kill followed by an ordinary failure: only the last attempt counts.
	mixed := &Task{Attempts: []TaskAttempt{{Number: 1, MemoryKilled: true}, {Number: 2, Outcome: "failed"}}}
	if got := ceilingRetryParagraph(mixed); got != "" {
		t.Errorf("paragraph shown after later non-ceiling failure: %s", got)
	}
}
