// The worker wait loop is written inline in the daemon wiring, so this reads
// the source: the loop must treat a decision that is made but not typed as a
// parked worker before any step that can nudge, stall or type into it.
package main

import (
	"os"
	"strings"
	"testing"
)

func TestWaitLoopHoldsWhileADecisionWaitsToBeTyped(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	open := strings.Index(src, "autonomousMgrRef.OpenBlockingQuestionForTask(task.ID)")
	nudge := strings.Index(src, "nudge.Step(time.Now())")
	if open < 0 || nudge < open {
		t.Fatal("the wait loop's question check or nudge step moved: update this test")
	}
	between := src[open:nudge]
	hold := strings.Index(between, "if autonomousMgrRef.DecisionResumeWaiting(task.ID) {")
	if hold < 0 {
		t.Fatal("the wait loop does not hold while a decision waits to be typed")
	}
	block := between[hold:]
	if end := strings.Index(block, "continue"); end < 0 || !strings.Contains(block[:end], "idleSince = time.Time{}") {
		t.Fatal("the hold must reset the idle clock and skip the rest of the tick")
	}
}
