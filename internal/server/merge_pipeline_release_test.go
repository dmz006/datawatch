package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// The release settings are read and written through the same config path as
// conflict_policy: PUT /api/config applies them and GET /api/config shows the
// value in effect.

func TestMergePipelineReleaseDefaultsOnGet(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	m := mergePipelineBlock(t, s)
	if string(m["major_trigger"]) != `"manual"` || string(m["release_cadence"]) != `"per_merge"` ||
		string(m["version_scheme"]) != `"semver"` {
		t.Fatalf("defaults: %v", m)
	}
	if string(m["release_size_map"]) != `{"bug":"patch","feature":"minor"}` {
		t.Errorf("default size map: %s", m["release_size_map"])
	}
}

func TestMergePipelineReleaseSetAndGet(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.release_size_map": "feature=major,chore=none",
		"autonomous.merge_pipeline.major_trigger":    "milestone",
		"autonomous.merge_pipeline.release_cadence":  "per_merge",
		"autonomous.merge_pipeline.version_scheme":   "semver",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	check := func(c config.MergePipelineConfig, where string) {
		t.Helper()
		if !reflect.DeepEqual(c.ReleaseSizeMap, map[string]string{"feature": "major", "chore": "none"}) ||
			c.MajorTrigger != "milestone" || c.ReleaseCadence != "per_merge" || c.VersionScheme != "semver" {
			t.Fatalf("%s: %+v", where, c)
		}
	}
	check(s.cfg.Autonomous.MergePipeline, "live config")
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	check(saved.Autonomous.MergePipeline, "saved file")

	m := mergePipelineBlock(t, s)
	if string(m["major_trigger"]) != `"milestone"` {
		t.Errorf("GET major_trigger: %s", m["major_trigger"])
	}
	// The effective map layers the global entries over the built-ins.
	if string(m["release_size_map"]) != `{"bug":"patch","chore":"none","feature":"major"}` {
		t.Errorf("GET size map: %s", m["release_size_map"])
	}

	// A JSON object body is accepted too, and null returns the key to its default.
	rec = putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.release_size_map": map[string]any{"bug": "minor"},
		"autonomous.merge_pipeline.major_trigger":    nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT object: %d %s", rec.Code, rec.Body.String())
	}
	got := s.cfg.Autonomous.MergePipeline
	if !reflect.DeepEqual(got.ReleaseSizeMap, map[string]string{"bug": "minor"}) || got.MajorTrigger != "" {
		t.Errorf("after object/null: %+v", got)
	}
	if string(mergePipelineBlock(t, s)["major_trigger"]) != `"manual"` {
		t.Errorf("default trigger not back")
	}
}

func TestMergePipelineReleasePerProjectOverride(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.major_trigger":                    "never",
		"autonomous.merge_pipeline.projects.demo.major_trigger":      "milestone",
		"autonomous.merge_pipeline.projects.demo.release_size_map":   `{"feature":"patch"}`,
		"autonomous.merge_pipeline.projects.demo.release_cadence":    "per_merge",
		"autonomous.merge_pipeline.projects.demo.version_scheme":     "semver",
		"autonomous.merge_pipeline.projects.my.app.release_size_map": "bug=minor",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	cfg := s.cfg.Autonomous.MergePipeline
	if got := cfg.EffectiveMajorTrigger("demo"); got != "milestone" {
		t.Errorf("demo trigger %q", got)
	}
	if got := cfg.EffectiveMajorTrigger("other"); got != "never" {
		t.Errorf("other inherits the global value, got %q", got)
	}
	if got := cfg.EffectiveReleaseSizeMap("demo")["feature"]; got != "patch" {
		t.Errorf("demo feature size %q", got)
	}
	if got := cfg.EffectiveReleaseSizeMap("my.app")["bug"]; got != "minor" {
		t.Errorf("a dotted project name must parse, bug size %q", got)
	}
	if got := cfg.EffectiveReleaseSizeMap("other")["feature"]; got != "minor" {
		t.Errorf("other keeps the built-in size, got %q", got)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Autonomous.MergePipeline.Projects["demo"].MajorTrigger != "milestone" {
		t.Errorf("override not saved: %+v", saved.Autonomous.MergePipeline.Projects)
	}

	// The per-project view shows the values in effect.
	v := mpGet(t, s, "demo")
	if v.Settings.MajorTrigger != "milestone" || v.Settings.ReleaseCadence != "per_merge" ||
		v.Settings.VersionScheme != "semver" || v.Settings.ReleaseSizeMap["feature"] != "patch" {
		t.Errorf("view settings: %+v", v.Settings)
	}
	if v.Overrides == nil || v.Overrides.MajorTrigger != "milestone" {
		t.Errorf("view overrides: %+v", v.Overrides)
	}

	// Clearing the only keys of an override removes it.
	rec = putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.projects.my.app.release_size_map": nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT null: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := s.cfg.Autonomous.MergePipeline.Projects["my.app"]; ok {
		t.Errorf("an emptied override should disappear")
	}
}

func TestMergePipelineReleaseRefusals(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	cases := []struct {
		key   string
		value any
		want  string
	}{
		{"autonomous.merge_pipeline.release_cadence", "batch", "release cadence batch is not available yet"},
		{"autonomous.merge_pipeline.release_cadence", "manual", "release cadence manual is not available yet"},
		{"autonomous.merge_pipeline.projects.demo.release_cadence", "batch", "release cadence batch is not available yet"},
		{"autonomous.merge_pipeline.release_cadence", "weekly", "weekly"},
		{"autonomous.merge_pipeline.version_scheme", "calver", "version scheme calver is not available yet"},
		{"autonomous.merge_pipeline.projects.demo.version_scheme", "calver", "version scheme calver is not available yet"},
		{"autonomous.merge_pipeline.major_trigger", "always", "major_trigger"},
		{"autonomous.merge_pipeline.release_size_map", "feature=huge", "huge"},
		{"autonomous.merge_pipeline.release_size_map", "feature", "kind=size"},
		{"autonomous.merge_pipeline.release_size_map", 7, "kind=size"},
	}
	for _, c := range cases {
		rec := putMergePipelineConfig(s, map[string]any{c.key: c.value})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s=%v: status %d, want 400", c.key, c.value, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), c.want) || !strings.Contains(rec.Body.String(), "autonomous.merge_pipeline.") {
			t.Errorf("%s=%v: body %q lacks %q", c.key, c.value, rec.Body.String(), c.want)
		}
	}
	// A refusal changes nothing, and refuses the whole PUT.
	rec := putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.major_trigger":   "never",
		"autonomous.merge_pipeline.release_cadence": "batch",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mixed PUT: %d", rec.Code)
	}
	if got := s.cfg.Autonomous.MergePipeline; got.MajorTrigger != "" || got.ReleaseCadence != "" {
		t.Errorf("a refused PUT changed the block: %+v", got)
	}
}
