package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// releasePushConfigNoSettings is a GET /api/config reply with none of the
// release push settings set.
const releasePushConfigNoSettings = `{"autonomous":{"enabled":true},"git":{}}`

// releasePushConfigWithSettings is a GET /api/config reply with the settings
// set to non-default values.
const releasePushConfigWithSettings = `{"autonomous":{"push_and_tag":true,"release_path":"pull_request"},"git":{"release_ci_window_minutes":15}}`

func releasePushStatusCall(t *testing.T, s *Server, project string) releasePushView {
	t.Helper()
	res, err := s.handleReleasePushStatus(context.Background(), callReq(map[string]any{"project": project}))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.IsError {
		t.Fatalf("release_push_status returned an error: %+v", res)
	}
	text, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	var view releasePushView
	if err := json.Unmarshal([]byte(text.Text), &view); err != nil {
		t.Fatalf("answer is not JSON: %v\n%s", err, text.Text)
	}
	return view
}

func TestReleasePushStatus_Name(t *testing.T) {
	s := &Server{}
	tool := s.toolReleasePushStatus()
	if tool.Name != "release_push_status" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !strings.Contains(tool.Description, "Automaton") {
		t.Errorf("description must name an Automaton: %q", tool.Description)
	}
	if strings.Contains(tool.Description, "PRD") && !strings.Contains(tool.Description, "never a PRD") {
		t.Errorf("description must not describe the release as a PRD: %q", tool.Description)
	}
}

func TestReleasePushStatus_DefaultsWithNoSettings(t *testing.T) {
	autonomous.SetReleaseRecordStore(nil)
	t.Cleanup(func() { autonomous.SetReleaseRecordStore(nil) })
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(releasePushConfigNoSettings))
	})
	view := releasePushStatusCall(t, s, "demo")
	if view.Project != "demo" {
		t.Errorf("project = %q", view.Project)
	}
	if view.PushAndTag {
		t.Errorf("push_and_tag default must be off")
	}
	if view.ReleasePath != "direct_push" {
		t.Errorf("release_path default = %q, want direct_push", view.ReleasePath)
	}
	if view.CIWindowMinutes != 60 {
		t.Errorf("ci_window_minutes default = %d, want 60", view.CIWindowMinutes)
	}
	if view.LastRelease != nil {
		t.Errorf("last_release must be null with no record: %+v", view.LastRelease)
	}
}

func TestReleasePushStatus_ReadsSettingsAndLastRelease(t *testing.T) {
	// Write one release record as the store's JSONL file holds it, then open
	// the store on that directory.
	dir := t.TempDir()
	rec := autonomous.ReleaseRecord{
		Project:      "demo",
		Tag:          "v1.2.3",
		PushedCommit: "abc123",
		ReleasePath:  "/work/demo",
		PushedAt:     time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC),
		PushOutcome:  "pushed",
		CIOutcome:    autonomous.ReleaseCIPending,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, autonomous.ReleaseRecordFileName), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := autonomous.NewReleaseRecordStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	autonomous.SetReleaseRecordStore(store)
	t.Cleanup(func() { autonomous.SetReleaseRecordStore(nil) })
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(releasePushConfigWithSettings))
	})
	view := releasePushStatusCall(t, s, "demo")
	if !view.PushAndTag || view.ReleasePath != "pull_request" || view.CIWindowMinutes != 15 {
		t.Errorf("settings not read from config: %+v", view)
	}
	if view.LastRelease == nil {
		t.Fatal("last_release missing")
	}
	got := view.LastRelease
	if got.Tag != "v1.2.3" || got.Commit != "abc123" || got.ReleasePath != "/work/demo" || got.PushOutcome != "pushed" {
		t.Errorf("last_release fields wrong: %+v", got)
	}
	if got.CIOutcome == "" {
		t.Errorf("ci_outcome must be carried through from the record")
	}
}

func TestReleasePushStatus_RequiresProject(t *testing.T) {
	s := &Server{}
	res, err := s.handleReleasePushStatus(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a call without a project must be a tool error")
	}
}
