// BL447 — autonomous_prd_set_task_project_dir forwarding.

package mcp

import (
	"context"
	"reflect"
	"testing"
)

func TestMCPSetTaskProjectDir_Body(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetTaskProjectDir(context.Background(), callReq(map[string]any{"id": "p1", "task_id": "t1", "project_dir": "/srv/x"}))
		return err
	})
	want := map[string]string{"task_id": "t1", "project_dir": "/srv/x", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPSetTaskProjectDir_ClearWhenOmitted(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetTaskProjectDir(context.Background(), callReq(map[string]any{"id": "p1", "task_id": "t1"}))
		return err
	})
	if got["project_dir"] != "" || got["task_id"] != "t1" {
		t.Errorf("body = %v", got)
	}
}
