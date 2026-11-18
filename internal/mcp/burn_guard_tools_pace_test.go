// burn_guard_config_set/get tools with the pace_line policy: all six settings
// live in the set schema with a description, a set call forwards them in the
// PUT body, and the get description names them.

package mcp

import (
	"context"
	"strings"
	"testing"
)

type paceMCPArg struct {
	name string
	typ  string
}

func TestBurnGuardPaceMCPSetSchema(t *testing.T) {
	def := (&Server{}).toolBurnGuardConfigSet()
	want := []paceMCPArg{
		{"mode", "string"},
		{"week_tolerance", "number"},
		{"points_to_week_factor", "number"},
		{"window_percent_per_point", "number"},
		{"window_stop_percent", "number"},
		{"reading_freshness_minutes", "number"},
	}
	for _, a := range want {
		propAny, ok := def.InputSchema.Properties[a.name]
		if !ok {
			t.Fatalf("burn_guard_config_set schema lacks %s", a.name)
		}
		prop, ok := propAny.(map[string]any)
		if !ok {
			t.Fatalf("%s property = %T, want map", a.name, propAny)
		}
		if prop["type"] != a.typ {
			t.Errorf("%s type = %v, want %s", a.name, prop["type"], a.typ)
		}
		if desc, _ := prop["description"].(string); desc == "" {
			t.Errorf("%s has no description", a.name)
		}
	}
	get := (&Server{}).toolBurnGuardConfigGet()
	for _, key := range []string{"mode", "week_tolerance", "points_to_week_factor", "window_percent_per_point", "window_stop_percent", "reading_freshness_minutes"} {
		if !strings.Contains(get.Description, key) {
			t.Errorf("burn_guard_config_get description lacks %s: %q", key, get.Description)
		}
	}
}

func TestBurnGuardPaceMCPSetForwardsBody(t *testing.T) {
	s, rec := bgServer(t, 200, `{"ok":true}`)
	payload := map[string]any{
		"mode":                      "on",
		"week_tolerance":            float64(5),
		"points_to_week_factor":     float64(2.5),
		"window_percent_per_point":  float64(10),
		"window_stop_percent":       float64(90),
		"reading_freshness_minutes": float64(30),
	}
	res, err := s.handleBurnGuardConfigSet(context.Background(), callReq(payload))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != "PUT" || rec.path != "/api/usage/burn-guard/config" {
		t.Fatalf("proxied %s %s, want PUT /api/usage/burn-guard/config", rec.method, rec.path)
	}
	for k, v := range payload {
		if rec.body[k] != v {
			t.Errorf("PUT body %s = %v, want %v", k, rec.body[k], v)
		}
	}
	if rec.body["week_tolerance"] != float64(5) || rec.body["reading_freshness_minutes"] != float64(30) {
		t.Fatalf("PUT body = %v", rec.body)
	}
}
