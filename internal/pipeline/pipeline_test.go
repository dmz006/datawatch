package pipeline

import (
	"testing"
)

func TestParsePipelineSpec_Linear(t *testing.T) {
	tasks := ParsePipelineSpec("write code -> write tests -> update docs")
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "write code" {
		t.Errorf("task 0 title = %q", tasks[0].Title)
	}
	if len(tasks[0].DependsOn) != 0 {
		t.Error("first task should have no deps")
	}
	if len(tasks[1].DependsOn) != 1 || tasks[1].DependsOn[0] != "t1" {
		t.Errorf("task 1 deps = %v, want [t1]", tasks[1].DependsOn)
	}
	if len(tasks[2].DependsOn) != 1 || tasks[2].DependsOn[0] != "t2" {
		t.Errorf("task 2 deps = %v, want [t2]", tasks[2].DependsOn)
	}
}

func TestParsePipelineSpec_Single(t *testing.T) {
	tasks := ParsePipelineSpec("just one task")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestDetectCycles_NoCycle(t *testing.T) {
	tasks := []*Task{
		{ID: "a", DependsOn: nil},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	cycle := DetectCycles(tasks)
	if cycle != nil {
		t.Errorf("expected no cycle, got %v", cycle)
	}
}

func TestDetectCycles_WithCycle(t *testing.T) {
	tasks := []*Task{
		{ID: "a", DependsOn: []string{"c"}},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	cycle := DetectCycles(tasks)
	if cycle == nil {
		t.Error("expected cycle to be detected")
	}
}

func TestReadyTasks(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1", Title: "first", State: StatePending},
		{ID: "t2", Title: "second", State: StatePending, DependsOn: []string{"t1"}},
		{ID: "t3", Title: "third", State: StatePending, DependsOn: []string{"t1"}},
	}, 3)

	ready := p.ReadyTasks()
	if len(ready) != 1 || ready[0].ID != "t1" {
		t.Errorf("initially only t1 should be ready, got %d tasks", len(ready))
	}

	p.MarkCompleted("t1")
	ready2 := p.ReadyTasks()
	if len(ready2) != 2 {
		t.Errorf("after t1 complete, t2 and t3 should be ready, got %d", len(ready2))
	}
}

func TestPipelineComplete(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1", Title: "a", State: StatePending},
		{ID: "t2", Title: "b", State: StatePending, DependsOn: []string{"t1"}},
	}, 3)

	if p.IsComplete() {
		t.Error("should not be complete initially")
	}

	p.MarkCompleted("t1")
	if p.IsComplete() {
		t.Error("should not be complete with t2 pending")
	}

	p.MarkCompleted("t2")
	if !p.IsComplete() {
		t.Error("should be complete after all tasks done")
	}
}

func TestPipelineCancel(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1", Title: "a", State: StatePending},
		{ID: "t2", Title: "b", State: StatePending},
	}, 3)

	p.Cancel()
	if p.State != StateCancelled {
		t.Errorf("state = %s, want cancelled", p.State)
	}
	for _, task := range p.Tasks {
		if task.State != StateCancelled {
			t.Errorf("task %s state = %s, want cancelled", task.ID, task.State)
		}
	}
}

func TestQualityGate_CompareResults(t *testing.T) {
	baseline := TestResult{PassCount: 10, FailCount: 2}
	current := TestResult{PassCount: 10, FailCount: 4}

	result := CompareResults(baseline, current)
	if !result.Regression {
		t.Error("should detect regression")
	}
	if result.NewFailures != 2 {
		t.Errorf("new failures = %d, want 2", result.NewFailures)
	}
}

func TestQualityGate_Improved(t *testing.T) {
	baseline := TestResult{PassCount: 10, FailCount: 3}
	current := TestResult{PassCount: 12, FailCount: 1}

	result := CompareResults(baseline, current)
	if !result.Improved {
		t.Error("should detect improvement")
	}
	if result.Regression {
		t.Error("should not be regression")
	}
}

func TestQualityGate_Stable(t *testing.T) {
	baseline := TestResult{PassCount: 10, FailCount: 0}
	current := TestResult{PassCount: 10, FailCount: 0}

	result := CompareResults(baseline, current)
	if result.Regression || result.Improved {
		t.Error("should be stable")
	}
}

func TestSummary(t *testing.T) {
	p := NewPipeline("test pipeline", "/dir", []*Task{
		{ID: "t1", Title: "a", State: StateCompleted},
		{ID: "t2", Title: "b", State: StateRunning},
		{ID: "t3", Title: "c", State: StatePending},
	}, 3)
	p.State = StateRunning

	s := p.Summary()
	if s == "" {
		t.Error("summary should not be empty")
	}
}
