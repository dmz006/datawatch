package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// mergePipelineConflictBody is the merge pipeline view with the
// conflict_confirmation block the daemon reports for a project whose conflict
// mode is on, with two of its five first proposals counted.
const mergePipelineConflictBody = `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"}]},"source":"default","reason":"no file, no record","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record","default"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":null,"conflict_confirmation":{"mode":"on","confirm_first_n":5,"counted":2,"remaining":3,"enabled":true}}`

// TestMergePipelineConflictArgsDeclared checks that autonomous_config_set
// declares both conflict arguments, with the config key and valid values named
// in each description.
func TestMergePipelineConflictArgsDeclared(t *testing.T) {
	props := (&Server{}).toolAutonomousConfigSet().InputSchema.Properties
	for _, k := range []string{"merge_pipeline_conflict_mode", "merge_pipeline_conflict_confirm_first_n"} {
		if _, ok := props[k]; !ok {
			t.Fatalf("autonomous_config_set lacks %s", k)
		}
	}
	for k, want := range map[string][]string{
		"merge_pipeline_conflict_mode":            {"autonomous.merge_pipeline.conflict_mode", "shadow", "on"},
		"merge_pipeline_conflict_confirm_first_n": {"autonomous.merge_pipeline.conflict_confirm_first_n", "0 or more"},
	} {
		raw, ok := props[k].(map[string]any)
		if !ok {
			t.Fatalf("%s has no schema map: %T", k, props[k])
		}
		desc, _ := raw["description"].(string)
		for _, w := range want {
			if !strings.Contains(desc, w) {
				t.Errorf("%s description lacks %q: %s", k, w, desc)
			}
		}
	}
}

// TestMergePipelineConflictArgsMapToConfigKeys checks that both arguments map
// to the daemon's dotted config keys, under the global block and a project's.
func TestMergePipelineConflictArgsMapToConfigKeys(t *testing.T) {
	got, err := mergePipelineConfigArgs(callReq(map[string]any{
		"merge_pipeline_conflict_mode":            "on",
		"merge_pipeline_conflict_confirm_first_n": 3,
	}))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	checkSentKeys(t, got, map[string]any{
		"autonomous.merge_pipeline.conflict_mode":            "on",
		"autonomous.merge_pipeline.conflict_confirm_first_n": 3,
	})

	got, err = mergePipelineConfigArgs(callReq(map[string]any{
		"merge_pipeline_project":                  "demo",
		"merge_pipeline_conflict_confirm_first_n": 0,
	}))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	checkSentKeys(t, got, map[string]any{
		"autonomous.merge_pipeline.projects.demo.conflict_confirm_first_n": 0,
	})
}

// TestMergePipelineConflictStatusIncludesBlock checks that the status read
// carries the conflict_confirmation block in both its text and JSON forms.
func TestMergePipelineConflictStatusIncludesBlock(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mergePipelineConflictBody))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	text := automationText(t, res)
	want := "Conflict confirmation: mode on, confirm first 5, counted 2, remaining 3, enabled yes"
	if !strings.Contains(text, want) {
		t.Errorf("text lacks %q: %s", want, text)
	}

	res, _ = s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo", "format": "json"}))
	if res == nil || res.IsError {
		t.Fatalf("json res=%+v", res)
	}
	if body := automationText(t, res); !strings.Contains(body, `"conflict_confirmation"`) {
		t.Errorf("json lacks conflict_confirmation: %s", body)
	}
}

// TestMergePipelineConflictStatusUnreadableCounter checks that an unreadable
// counter shows as unknown with its reason, not as a zero count.
func TestMergePipelineConflictStatusUnreadableCounter(t *testing.T) {
	body := `{"project":"demo","definition":{"steps":[]},"source":"default","reason":"x","settings":{"file_name":"f","source_order":[],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":null,"conflict_confirmation":{"mode":"shadow","confirm_first_n":5,"counted":null,"remaining":null,"enabled":false,"error":"conflict counter unreadable: disk"}}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	text := automationText(t, res)
	for _, w := range []string{"counted unknown, remaining unknown, enabled no", "conflict counter unreadable: disk"} {
		if !strings.Contains(text, w) {
			t.Errorf("text lacks %q: %s", w, text)
		}
	}
}
