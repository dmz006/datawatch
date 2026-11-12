package mcp

import (
	"context"
	"reflect"
	"testing"
)

func TestMCPResume_Body(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDResume(context.Background(), callReq(map[string]any{"id": "p1", "note": "go"}))
		return err
	})
	want := map[string]string{"actor": "operator", "note": "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPResume_ToolSchema(t *testing.T) {
	tool := (&Server{}).toolAutonomousPRDResume()
	if tool.Name != "autonomous_prd_resume" {
		t.Fatalf("name = %q", tool.Name)
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "id" {
		t.Errorf("required = %v, want [id]", tool.InputSchema.Required)
	}
}
