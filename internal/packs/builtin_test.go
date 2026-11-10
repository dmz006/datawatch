package packs

import (
	"testing"
)

// builtinTaskTitles are the six standard tasks the built-in pack ships, in
// manifest order.
var builtinTaskTitles = []string{
	"Changelog entry",
	"Parity issue draft",
	"Docs set",
	"Bounded load-step rule",
	"Release version note",
	"Final gate",
}

func TestLoadBuiltinStandardTasks(t *testing.T) {
	p, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}

	if got, want := len(p.Tasks), len(builtinTaskTitles); got != want {
		t.Fatalf("loaded pack has %d tasks, want %d: %v", got, want, taskTitles(p.Tasks))
	}
	for i, want := range builtinTaskTitles {
		if p.Tasks[i].Title != want {
			t.Errorf("task %d title = %q, want %q", i, p.Tasks[i].Title, want)
		}
	}

	if p.Digest == "" {
		t.Error("loaded pack has no content digest")
	}
	if p.Dir == "" {
		t.Fatal("loaded pack has no folder; rule checks cannot run")
	}

	readOnly := 0
	for _, task := range p.Tasks {
		if task.ReadOnly {
			readOnly++
			continue
		}
		if err := CheckTaskRules(p, task); err != nil {
			t.Errorf("CheckTaskRules for non-read-only task %q: %v", task.Title, err)
		}
	}
	if readOnly != 1 {
		t.Errorf("loaded pack has %d read-only tasks, want 1", readOnly)
	}
}

func taskTitles(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Title
	}
	return out
}
