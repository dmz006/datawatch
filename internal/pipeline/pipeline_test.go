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

func TestMarkRunning(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1", State: StatePending},
	}, 3)
	p.MarkRunning("t1", "sess-abc")
	task := p.TaskByID("t1")
	if task.State != StateRunning {
		t.Errorf("expected running, got %s", task.State)
	}
	if task.SessionID != "sess-abc" {
		t.Errorf("expected session 'sess-abc', got %q", task.SessionID)
	}
	if task.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestMarkFailed(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1", State: StateRunning},
	}, 3)
	p.MarkFailed("t1", "something broke")
	task := p.TaskByID("t1")
	if task.State != StateFailed {
		t.Errorf("expected failed, got %s", task.State)
	}
	if task.Error != "something broke" {
		t.Errorf("expected error text, got %q", task.Error)
	}
	if p.State != StateFailed {
		t.Errorf("pipeline should be failed, got %s", p.State)
	}
}

func TestRunningCount(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1", State: StateRunning},
		{ID: "t2", State: StateRunning},
		{ID: "t3", State: StatePending},
	}, 3)
	if p.RunningCount() != 2 {
		t.Errorf("expected 2 running, got %d", p.RunningCount())
	}
}

func TestTaskByID_NotFound(t *testing.T) {
	p := NewPipeline("test", "/dir", []*Task{
		{ID: "t1"},
	}, 3)
	if p.TaskByID("nonexistent") != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestNewPipeline_DefaultMaxParallel(t *testing.T) {
	p := NewPipeline("test", "/dir", nil, 0)
	if p.MaxParallel != 3 {
		t.Errorf("expected default 3, got %d", p.MaxParallel)
	}
}

func TestParsePipelineSpec_UnicodeArrow(t *testing.T) {
	tasks := ParsePipelineSpec("a \xe2\x86\x92 b \xe2\x86\x92 c") // " → "
	// May or may not split on unicode arrow
	t.Logf("Unicode arrow: got %d tasks", len(tasks))
}

func TestParsePipelineSpec_PipeSep(t *testing.T) {
	tasks := ParsePipelineSpec("a | b | c") // " | "
	// May or may not split on pipe
	t.Logf("Pipe separator: got %d tasks", len(tasks))
}

func TestRouterAdapter_ListJSON(t *testing.T) {
	exec := NewExecutor(nil, "test")
	adapter := NewRouterAdapter(exec)
	list := adapter.ListJSON()
	if list == nil {
		t.Fatal("expected non-nil list")
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}
