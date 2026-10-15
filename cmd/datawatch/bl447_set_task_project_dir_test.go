// BL447 — prd-set-task-project-dir <prd> <task> [dir].

package main

import (
	"io"
	"reflect"
	"testing"
)

func TestCLISetTaskProjectDir_Set(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetTaskProjectDirCmd(), []string{"p1", "t1", "/srv/x"})
	want := map[string]string{"task_id": "t1", "project_dir": "/srv/x", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetTaskProjectDir_ClearWhenOmitted(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetTaskProjectDirCmd(), []string{"p1", "t1"})
	want := map[string]string{"task_id": "t1", "project_dir": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetTaskProjectDir_ArgCount(t *testing.T) {
	for _, argv := range [][]string{{"p1"}, {"p1", "t1", "/a", "extra"}} {
		c := newAutonomousPRDSetTaskProjectDirCmd()
		c.SetArgs(argv)
		c.SetOut(io.Discard)
		c.SetErr(io.Discard)
		if err := c.Execute(); err == nil {
			t.Errorf("%v: expected arg error", argv)
		}
	}
}
