// get_alerts paging params (BL432 / GH#242).

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
)

func TestGetAlerts_ToolDeclaresPagingParams(t *testing.T) {
	tool := (&Server{}).toolGetAlerts()
	for _, p := range []string{"limit", "unread_only", "before"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("get_alerts missing %q param", p)
		}
	}
}

func TestGetAlerts_LimitAndBeforeCursor(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	var made []*alerts.Alert
	for _, title := range []string{"first", "second", "third"} {
		made = append(made, s.alertStore.Add(alerts.LevelInfo, title, "", ""))
	}
	res, err := s.handleGetAlerts(context.Background(), call(map[string]any{"limit": 2}))
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(text, "third") || !strings.Contains(text, "second") || strings.Contains(text, "first") {
		t.Fatalf("page 1 = %q, want newest two", text)
	}
	if !strings.Contains(text, "before="+made[1].ID) {
		t.Fatalf("page 1 = %q, want next-page hint before=%s", text, made[1].ID)
	}
	res, _ = s.handleGetAlerts(context.Background(), call(map[string]any{"limit": 2, "before": made[1].ID}))
	text = resultText(t, res, nil)
	if !strings.Contains(text, "first") || strings.Contains(text, "second") || strings.Contains(text, "before=") {
		t.Fatalf("page 2 = %q, want only the oldest alert and no hint", text)
	}
}

func TestGetAlerts_UnreadOnly(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	read := s.alertStore.Add(alerts.LevelInfo, "already-seen", "", "")
	s.alertStore.Add(alerts.LevelInfo, "fresh", "", "")
	if err := s.alertStore.MarkRead(read.ID); err != nil {
		t.Fatal(err)
	}
	res, _ := s.handleGetAlerts(context.Background(), call(map[string]any{"unread_only": true}))
	text := resultText(t, res, nil)
	if !strings.Contains(text, "fresh") || strings.Contains(text, "already-seen") {
		t.Fatalf("unread_only result = %q", text)
	}
}

func TestGetAlerts_SessionFilterStillApplies(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	s.alertStore.Add(alerts.LevelInfo, "mine", "", "s1")
	s.alertStore.Add(alerts.LevelInfo, "other", "", "s2")
	res, _ := s.handleGetAlerts(context.Background(), call(map[string]any{"session_id": "s1"}))
	text := resultText(t, res, nil)
	if !strings.Contains(text, "mine") || strings.Contains(text, "other") {
		t.Fatalf("session filter result = %q", text)
	}
}

func TestGetAlerts_UnknownBeforeIsError(t *testing.T) {
	s := gh201Phase5MCPServer(t)
	s.alertStore.Add(alerts.LevelInfo, "x", "", "")
	res, _ := s.handleGetAlerts(context.Background(), call(map[string]any{"before": "nope"}))
	if !res.IsError {
		t.Fatal("expected error result for unknown before id")
	}
}
