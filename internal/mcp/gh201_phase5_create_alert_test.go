// GH#201 Phase 5 — create_alert MCP tool.

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
)

func gh201Phase5MCPServer(t *testing.T) *Server {
	t.Helper()
	store, err := alerts.NewStore(t.TempDir() + "/alerts.json")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{alertStore: store}
}

func TestGH201Phase5_ToolCreateAlert_Name(t *testing.T) {
	s := &Server{}
	if got := s.toolCreateAlert().Name; got != "create_alert" {
		t.Errorf("tool name = %q, want create_alert", got)
	}
}

func TestGH201Phase5_HandleCreateAlert_DefaultsToInfo(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	res, err := s.handleCreateAlert(context.Background(), call(map[string]any{"title": "disk space low"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(text, "created") {
		t.Errorf("result = %q, want it to confirm creation", text)
	}
	all := s.alertStore.List()
	if len(all) != 1 {
		t.Fatalf("expected 1 alert in the store, got %d", len(all))
	}
	if all[0].Level != alerts.LevelInfo {
		t.Errorf("level = %q, want info (default)", all[0].Level)
	}
}

func TestGH201Phase5_HandleCreateAlert_RequiresTitle(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	res, err := s.handleCreateAlert(context.Background(), call(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(strings.ToLower(text), "title required") {
		t.Errorf("result = %q, want a title-required error", text)
	}
}

func TestGH201Phase5_HandleCreateAlert_RejectsUnknownLevel(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	res, err := s.handleCreateAlert(context.Background(), call(map[string]any{"title": "x", "level": "critical"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(strings.ToLower(text), "error") {
		t.Errorf("result = %q, want an error for an unrecognized level", text)
	}
	if len(s.alertStore.List()) != 0 {
		t.Error("an invalid level must not create an alert")
	}
}

func TestGH201Phase5_HandleCreateAlert_NoStoreConfigured(t *testing.T) {
	s := &Server{}
	res, err := s.handleCreateAlert(context.Background(), call(map[string]any{"title": "x"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(strings.ToLower(text), "not available") {
		t.Errorf("result = %q, want a not-available message", text)
	}
}
