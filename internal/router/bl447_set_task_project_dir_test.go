// BL447 — comm `autonomous set-task-project-dir`.

package router

import (
	"reflect"
	"strings"
	"testing"
)

func TestCommSetTaskProjectDir_Set(t *testing.T) {
	path, got := commSetLLMBody(t, "autonomous set-task-project-dir p1 t1 /srv/proj/sub")
	want := map[string]string{"task_id": "t1", "project_dir": "/srv/proj/sub", "actor": "operator"}
	if path != "/api/autonomous/prds/p1/set_task_project_dir" || !reflect.DeepEqual(got, want) {
		t.Errorf("path=%s body=%v want %v", path, got, want)
	}
}

func TestCommSetTaskProjectDir_ClearUnderscoreAlias(t *testing.T) {
	_, got := commSetLLMBody(t, "autonomous set_task_project_dir p1 t1")
	want := map[string]string{"task_id": "t1", "project_dir": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCommSetTaskProjectDir_Usage(t *testing.T) {
	r := &Router{}
	got := strings.Join(r.HandleTestMessage("autonomous set-task-project-dir p1"), " ")
	if !strings.Contains(got, "usage") {
		t.Errorf("expected usage, got %q", got)
	}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(help, "set-task-project-dir") {
		t.Errorf("help missing verb: %q", help)
	}
}
