package server

// The post-ship hook settings of the autonomous.merge_pipeline block, seen
// through GET and PUT /api/config and the merge-pipeline view: the keys are
// global, post_ship.hooks takes a JSON array of hooks or their JSON text,
// null clears a key, and a bad value refuses the whole PUT by name.

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// postShipBlock pulls the post_ship object out of the merge pipeline block
// of GET /api/config.
func postShipBlock(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	m := mergePipelineBlock(t, s)
	raw, ok := m["post_ship"]
	if !ok {
		t.Fatalf("GET /api/config: no post_ship under the merge pipeline block: %v", m)
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("post_ship decode: %v: %s", err, raw)
	}
	return out
}

func TestConfigPostShipDefaultsVisible(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	m := postShipBlock(t, s)
	if string(m["enabled"]) != "false" || string(m["registry_file"]) != `""` ||
		string(m["registry_row_pattern"]) != `""` || string(m["companion_repository"]) != `""` ||
		string(m["issue_title_template"]) != `""` || string(m["notify_template"]) != `""` ||
		string(m["hooks"]) != "null" {
		t.Fatalf("defaults: %v", m)
	}
	v := mpGet(t, s, "demo")
	if v.PostShip.Enabled || v.PostShip.HookCount != 0 || len(v.PostShip.EnabledHooks) != 0 {
		t.Errorf("merge-pipeline view post_ship: %+v", v.PostShip)
	}
}

func TestConfigPostShipPutRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putMergePipelineConfig(s, map[string]any{
		// The enabled hooks and their targets go in the same PUT.
		"autonomous.merge_pipeline.post_ship.enabled":              true,
		"autonomous.merge_pipeline.post_ship.registry_file":        "docs/registry.md",
		"autonomous.merge_pipeline.post_ship.registry_row_pattern": "| {project} | shipped |",
		"autonomous.merge_pipeline.post_ship.companion_repository": "dmz006/companion",
		"autonomous.merge_pipeline.post_ship.issue_title_template": "Parity: {project} {version}",
		"autonomous.merge_pipeline.post_ship.notify_template":      "Shipped {version} of {project}.",
		"autonomous.merge_pipeline.post_ship.hooks": []any{
			map[string]any{"name": "registry row", "builtin": "registry_row", "enabled": true},
			map[string]any{"name": "parity", "builtin": "parity_issue", "enabled": true},
			map[string]any{"name": "tagging", "builtin": "tag"},
			map[string]any{"name": "ping", "command": "curl -s http://localhost/ping"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	want := config.PostShipConfig{
		Enabled: true,
		Hooks: []config.PostShipHook{
			{Name: "registry row", Builtin: "registry_row", Enabled: true},
			{Name: "parity", Builtin: "parity_issue", Enabled: true},
			{Name: "tagging", Builtin: "tag"},
			{Name: "ping", Command: "curl -s http://localhost/ping"},
		},
		RegistryFile:        "docs/registry.md",
		RegistryRowPattern:  "| {project} | shipped |",
		CompanionRepository: "dmz006/companion",
		IssueTitleTemplate:  "Parity: {project} {version}",
		NotifyTemplate:      "Shipped {version} of {project}.",
	}
	check := func(c config.MergePipelineConfig, where string) {
		t.Helper()
		if !reflect.DeepEqual(c.PostShip, want) {
			t.Fatalf("%s: got %+v\nwant %+v", where, c.PostShip, want)
		}
	}
	check(s.cfg.Autonomous.MergePipeline, "live config")
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load saved: %v", err)
	}
	check(saved.Autonomous.MergePipeline, "saved file")

	m := postShipBlock(t, s)
	if string(m["enabled"]) != "true" || string(m["registry_file"]) != `"docs/registry.md"` {
		t.Errorf("GET after PUT: %v", m)
	}
	var got []config.PostShipHook
	if err := json.Unmarshal(m["hooks"], &got); err != nil {
		t.Fatalf("hooks decode: %v: %s", err, m["hooks"])
	}
	if !reflect.DeepEqual(got, want.Hooks) {
		t.Errorf("GET hooks: got %+v\nwant %+v", got, want.Hooks)
	}

	v := mpGet(t, s, "demo")
	if !v.PostShip.Enabled || v.PostShip.HookCount != 4 ||
		!reflect.DeepEqual(v.PostShip.EnabledHooks, []string{"registry row", "parity"}) {
		t.Errorf("merge-pipeline view post_ship: %+v", v.PostShip)
	}

	// The chat channel sends the hooks as their JSON text; the same array must
	// come out.
	text := `[{"name":"registry row","builtin":"registry_row","enabled":true}]`
	rec = putMergePipelineConfig(s, map[string]any{"autonomous.merge_pipeline.post_ship.hooks": text})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT hooks text: %d %s", rec.Code, rec.Body.String())
	}
	if got := s.cfg.Autonomous.MergePipeline.PostShip.Hooks; len(got) != 1 || got[0].Builtin != "registry_row" || !got[0].Enabled {
		t.Errorf("hooks set from text: %+v", got)
	}

	// null clears a key back to unset, one at a time.
	rec = putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.post_ship.enabled":              false,
		"autonomous.merge_pipeline.post_ship.hooks":                nil,
		"autonomous.merge_pipeline.post_ship.registry_file":        nil,
		"autonomous.merge_pipeline.post_ship.registry_row_pattern": nil,
		"autonomous.merge_pipeline.post_ship.companion_repository": nil,
		"autonomous.merge_pipeline.post_ship.issue_title_template": nil,
		"autonomous.merge_pipeline.post_ship.notify_template":      nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT null: %d %s", rec.Code, rec.Body.String())
	}
	if got := s.cfg.Autonomous.MergePipeline.PostShip; got.Enabled || got.Hooks != nil || got.RegistryFile != "" ||
		got.RegistryRowPattern != "" || got.CompanionRepository != "" || got.IssueTitleTemplate != "" || got.NotifyTemplate != "" {
		t.Errorf("null did not clear everything: %+v", got)
	}
}

func TestConfigPostShipPutRefusesBadValues(t *testing.T) {
	bad := map[string]struct {
		patch map[string]any
		want  string
	}{
		"enabled hook, target unset": {
			map[string]any{
				"autonomous.merge_pipeline.post_ship.enabled": true,
				"autonomous.merge_pipeline.post_ship.hooks":   []any{map[string]any{"name": "row", "builtin": "registry_row", "enabled": true}},
			},
			"registry_file: required because hook",
		},
		"enabled parity hook, repo unset": {
			map[string]any{
				"autonomous.merge_pipeline.post_ship.enabled":              true,
				"autonomous.merge_pipeline.post_ship.registry_file":        "docs/registry.md",
				"autonomous.merge_pipeline.post_ship.registry_row_pattern": "| x |",
				"autonomous.merge_pipeline.post_ship.issue_title_template": "Parity",
				"autonomous.merge_pipeline.post_ship.hooks":                []any{map[string]any{"name": "parity", "builtin": "parity_issue", "enabled": true}},
			},
			"companion_repository: required because hook",
		},
		"hook both builtin and command": {
			map[string]any{"autonomous.merge_pipeline.post_ship.hooks": []any{map[string]any{"name": "x", "builtin": "tag", "command": "true"}}},
			"set either builtin or command, not both",
		},
		"hook neither builtin nor command": {
			map[string]any{"autonomous.merge_pipeline.post_ship.hooks": []any{map[string]any{"name": "x"}}},
			"set either builtin or command",
		},
		"unknown builtin": {
			map[string]any{"autonomous.merge_pipeline.post_ship.hooks": []any{map[string]any{"name": "x", "builtin": "deploy"}}},
			`"deploy" is unknown`,
		},
		"hook without name": {
			map[string]any{"autonomous.merge_pipeline.post_ship.hooks": []any{map[string]any{"builtin": "tag"}}},
			"a hook name is required",
		},
		"hook name twice": {
			map[string]any{"autonomous.merge_pipeline.post_ship.hooks": []any{
				map[string]any{"name": "x", "builtin": "tag"},
				map[string]any{"name": "x", "command": "true"},
			}},
			"listed twice",
		},
		"hook list too long": {
			func() map[string]any {
				many := make([]any, 0, config.PostShipMaxHooks+1)
				for i := 0; i <= config.PostShipMaxHooks; i++ {
					many = append(many, map[string]any{"name": "h" + string(rune('a'+i/26)) + string(rune('a'+i%26)), "builtin": "tag"})
				}
				return map[string]any{"autonomous.merge_pipeline.post_ship.hooks": many}
			}(),
			"at most 20 hooks are allowed",
		},
		"hooks not an array":     {map[string]any{"autonomous.merge_pipeline.post_ship.hooks": "not json"}, "use a JSON array"},
		"hooks wrong element":    {map[string]any{"autonomous.merge_pipeline.post_ship.hooks": []any{7}}, "unmarshal"},
		"enabled not boolean":    {map[string]any{"autonomous.merge_pipeline.post_ship.enabled": "yes"}, "use true or false"},
		"registry file absolute": {map[string]any{"autonomous.merge_pipeline.post_ship.registry_file": "/etc/x"}, "registry_file"},
		"registry file escapes":  {map[string]any{"autonomous.merge_pipeline.post_ship.registry_file": "../x"}, "registry_file"},
		"target not text":        {map[string]any{"autonomous.merge_pipeline.post_ship.notify_template": 5}, "notify_template"},
		"per-project form": {
			map[string]any{"autonomous.merge_pipeline.projects.demo.post_ship.enabled": true},
			"no per-project form",
		},
	}
	for name, c := range bad {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		before := s.cfg.Autonomous.MergePipeline.Clone()
		// A good key in the same PUT must not be applied when another is bad.
		patch := map[string]any{"autonomous.merge_pipeline.conflict_policy": "mechanical"}
		for k, v := range c.patch {
			patch[k] = v
		}
		rec := putMergePipelineConfig(s, patch)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: want 400 naming %q, got %d %q", name, c.want, rec.Code, rec.Body.String())
		}
		if !reflect.DeepEqual(s.cfg.Autonomous.MergePipeline.Clone(), before) {
			t.Errorf("%s: a refused PUT changed the config", name)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Errorf("%s: a refused PUT wrote the config file", name)
		}
	}
}
