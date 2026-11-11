// Merge pipeline keys (autonomous.merge_pipeline.*) over REST for the
// Settings rows under Automata: every key is written with PUT /api/config and
// read back with GET /api/config.

package server

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAutomataSettingsMoveMergePipelineSurfaces(t *testing.T) {
	enc := func(v any) any { return v }
	cases := []struct {
		key  string
		put  any
		path []string // path of the value inside the merge_pipeline map
		want any
	}{
		{"autonomous.merge_pipeline.file_name", "ci/pipeline.yaml", []string{"file_name"}, "ci/pipeline.yaml"},
		{"autonomous.merge_pipeline.source_order", []any{"record", "file"}, []string{"source_order"}, []any{"record", "file"}},
		{"autonomous.merge_pipeline.fallback_on_bad_file", "none", []string{"fallback_on_bad_file"}, "none"},
		{"autonomous.merge_pipeline.conflict_policy", "stop_and_ask", []string{"conflict_policy"}, "stop_and_ask"},
		{"autonomous.merge_pipeline.conflict_model", "some-model", []string{"conflict_model"}, "some-model"},
		{"autonomous.merge_pipeline.conflict_mode", "on", []string{"conflict_mode"}, "on"},
		{"autonomous.merge_pipeline.conflict_confirm_first_n", 0, []string{"conflict_confirm_first_n"}, float64(0)},
		{"autonomous.merge_pipeline.version_files", []any{"cmd/x/main.go"}, []string{"version_files"}, []any{"cmd/x/main.go"}},
		{"autonomous.merge_pipeline.release_size_map", map[string]any{"feature": "patch"}, []string{"release_size_map", "feature"}, "patch"},
		{"autonomous.merge_pipeline.major_trigger", "milestone", []string{"major_trigger"}, "milestone"},
		{"autonomous.merge_pipeline.release_cadence", "per_merge", []string{"release_cadence"}, "per_merge"},
		{"autonomous.merge_pipeline.version_scheme", "semver", []string{"version_scheme"}, "semver"},
		{"autonomous.merge_pipeline.release_approval.patch", "manual", []string{"release_approval", "patch"}, "manual"},
		{"autonomous.merge_pipeline.release_approval.minor", "ask", []string{"release_approval", "minor"}, "ask"},
		{"autonomous.merge_pipeline.release_approval.major", "auto", []string{"release_approval", "major"}, "auto"},
		{"autonomous.merge_pipeline.post_ship.enabled", true, []string{"post_ship", "enabled"}, true},
		{"autonomous.merge_pipeline.post_ship.registry_file", "docs/reg.md", []string{"post_ship", "registry_file"}, "docs/reg.md"},
		{"autonomous.merge_pipeline.post_ship.registry_row_pattern", "row {project}", []string{"post_ship", "registry_row_pattern"}, "row {project}"},
		{"autonomous.merge_pipeline.post_ship.companion_repository", "owner/repo", []string{"post_ship", "companion_repository"}, "owner/repo"},
		{"autonomous.merge_pipeline.post_ship.issue_title_template", "Parity {version}", []string{"post_ship", "issue_title_template"}, "Parity {version}"},
		{"autonomous.merge_pipeline.post_ship.notify_template", "Shipped {version}", []string{"post_ship", "notify_template"}, "Shipped {version}"},
		{"autonomous.merge_pipeline.post_ship.hooks",
			[]any{map[string]any{"name": "say", "command": "echo hi", "enabled": true}},
			[]string{"post_ship", "hooks", "0", "name"}, "say"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			wireReleaseModeRecords(t, s)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{c.key: enc(c.put)})
			raw, ok := getAutonomousConfigMap(t, s)["merge_pipeline"]
			if !ok {
				t.Fatal("GET /api/config autonomous map lacks merge_pipeline")
			}
			var cur any
			if err := json.Unmarshal(raw, &cur); err != nil {
				t.Fatalf("decode merge_pipeline: %v", err)
			}
			for _, p := range c.path {
				switch n := cur.(type) {
				case map[string]any:
					cur = n[p]
				case []any:
					if p != "0" || len(n) == 0 {
						t.Fatalf("path %v: no element %s", c.path, p)
					}
					cur = n[0]
				default:
					t.Fatalf("path %v: cannot descend into %T at %s", c.path, cur, p)
				}
			}
			if !reflect.DeepEqual(cur, c.want) {
				t.Fatalf("GET after PUT %s = %#v, want %#v", c.key, cur, c.want)
			}
		})
	}
}

// TestAutomataSettingsMoveDecisionSurfaces writes each decision record key
// with PUT /api/config and reads it back with GET /api/config.
func TestAutomataSettingsMoveDecisionSurfaces(t *testing.T) {
	cases := []struct {
		key  string
		put  any
		path string
		want any
	}{
		{"autonomous.decisions.enabled", false, "enabled", false},
		{"autonomous.decisions.folder", "docs/decisions", "folder", "docs/decisions"},
		{"autonomous.decisions.folder_glob", "*.md", "folder_glob", "*.md"},
		{"autonomous.decisions.clarity_mode", "refuse", "clarity_mode", "refuse"},
		{"autonomous.decisions.clarity_min_text_chars", 80, "clarity_min_text_chars", float64(80)},
		{"autonomous.decisions.doc_status_mode", "on", "doc_status_mode", "on"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{c.key: c.put})
			raw, ok := getAutonomousConfigMap(t, s)["decisions"]
			if !ok {
				t.Fatal("GET /api/config autonomous map lacks decisions")
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("decode decisions: %v", err)
			}
			if !reflect.DeepEqual(m[c.path], c.want) {
				t.Fatalf("GET after PUT %s = %#v, want %#v", c.key, m[c.path], c.want)
			}
		})
	}
}
