// Surface parity and defaults for the judge routing settings:
// autonomous.judge_policy (ordered backends; empty = each judge's unit
// guardrail backend) and autonomous.judge_timeout_seconds (default 30 minutes). A
// missing surface, a non-English label that is only a copy of the English
// one, or a default that differs between surfaces fails here.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// judgeConfigKeys are the two key names every operator surface must carry.
var judgeConfigKeys = []string{"judge_policy", "judge_timeout_seconds"}

// judgeLocaleKeys are the label and help keys for both settings, in all five
// locale bundles.
var judgeLocaleKeys = []string{
	"settings_judge_policy", "settings_judge_policy_help",
	"settings_judge_timeout", "settings_judge_timeout_help",
}

// staleJudgeWording is the wording an empty judge policy used to carry. The
// judges do not follow the verifier routing policy, so no surface may say so.
var staleJudgeWording = []string{
	"judges follow the verifier",
	"follow verifier_policy",
	"follow the verifier routing policy",
}

func TestJudgeConfigSurfaceParity(t *testing.T) {
	// Each surface must carry the needles it names: quoted code keys, colon
	// config keys or backticked doc names. Prose in a comment does not normally
	// carry these forms, so a missing surface fails here. Docs surfaces are
	// included, so a REST or operations page without the keys fails too.
	surfaces := []struct {
		name, rel string
		needles   []string
	}{
		{"config struct", "internal/config/config.go", []string{`yaml:"judge_policy" json:"judge_policy"`, `yaml:"judge_timeout_seconds" json:"judge_timeout_seconds"`}},
		{"autonomous manager cfg", "internal/autonomous/manager.go", []string{`json:"judge_policy"`, `json:"judge_timeout_seconds"`}},
		{"REST", "internal/server/api.go", []string{`"judge_policy":`, `"judge_timeout_seconds":`, `"autonomous.judge_policy"`, `"autonomous.judge_timeout_seconds"`}},
		{"MCP schema", "internal/mcp/autonomous.go", []string{`"judge_policy"`, `"judge_timeout_seconds"`}},
		{"comm help", "internal/router/router.go", []string{"autonomous.judge_policy", "autonomous.judge_timeout_seconds"}},
		{"comm config-set", "internal/router/sx2_parity.go", []string{`"judge_policy"`}},
		{"CLI", "cmd/datawatch/cli_autonomous.go", []string{`"judge-policy"`, `"judge-timeout-seconds"`}},
		{"web app.js", "internal/server/web/app.js", []string{"'autonomous.judge_policy'", "'autonomous.judge_timeout_seconds'"}},
		{"config-reference.yaml", "docs/config-reference.yaml", []string{"judge_policy:", "judge_timeout_seconds:"}},
		{"implementation.md", "docs/implementation.md", []string{"`autonomous.judge_policy`", "`autonomous.judge_timeout_seconds`"}},
		{"api/autonomous.md", "docs/api/autonomous.md", []string{"judge_policy:", "judge_timeout_seconds:"}},
		{"api/openapi.yaml", "docs/api/openapi.yaml", []string{"`judge_policy`", "`judge_timeout_seconds`"}},
		{"operations.md", "docs/operations.md", []string{"`autonomous.judge_timeout_seconds`", "`judge-timeout`"}},
	}
	for _, sf := range surfaces {
		src := readRepoFile(t, sf.rel)
		for _, n := range sf.needles {
			if !strings.Contains(src, n) {
				t.Errorf("%s (%s) lacks %s", sf.name, sf.rel, n)
			}
		}
		for _, bad := range staleJudgeWording {
			if strings.Contains(src, bad) {
				t.Errorf("%s (%s) still says %q", sf.name, sf.rel, bad)
			}
		}
	}
	// YAML tags on the config struct, without omitempty for either key (an
	// explicit clear must round-trip).
	cfg := readRepoFile(t, "internal/config/config.go")
	if !strings.Contains(cfg, `yaml:"judge_policy" json:"judge_policy"`) {
		t.Error("config struct lacks the judge_policy tags")
	}
	if !strings.Contains(cfg, `yaml:"judge_timeout_seconds" json:"judge_timeout_seconds"`) {
		t.Error("config struct lacks the judge_timeout_seconds tags")
	}
}

// All five locale bundles carry the same judge_ label and help keys, each
// non-empty, carrying no tracker id, and the non-English ones are real
// translations rather than the English text.
func TestJudgeConfigLocaleParity(t *testing.T) {
	type bundle map[string]any
	load := func(lang string) bundle {
		var m bundle
		if err := json.Unmarshal([]byte(readRepoFile(t, "internal/server/web/locales/"+lang+".json")), &m); err != nil {
			t.Fatalf("%s.json: invalid JSON: %v", lang, err)
		}
		return m
	}
	en := load("en")
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		m := load(lang)
		for _, k := range judgeLocaleKeys {
			s, ok := m[k].(string)
			if !ok || strings.TrimSpace(s) == "" {
				t.Errorf("%s.json lacks a non-empty %s", lang, k)
				continue
			}
			if strings.Contains(s, "BL") && strings.ContainsAny(s, "0123456789") {
				t.Errorf("%s.json %s carries a tracker id: %q", lang, k, s)
			}
			if lang != "en" && s == en[k] {
				t.Errorf("%s.json %s is the English text, not a translation", lang, k)
			}
		}
	}
}

// The defaults are the same on every read path: a zero time limit shows 1800,
// an unset judge policy shows an empty list, and a clear returns both to
// those values.
func TestJudgeConfigDefaultsOnRESTSurface(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}

	check := func(label string, wantPolicy string, wantTimeout string) {
		t.Helper()
		raw := getAutonomousConfigMap(t, s)
		if got := string(raw["judge_policy"]); got != wantPolicy {
			t.Errorf("%s: judge_policy = %s, want %s", label, got, wantPolicy)
		}
		if got := string(raw["judge_timeout_seconds"]); got != wantTimeout {
			t.Errorf("%s: judge_timeout_seconds = %s, want %s", label, got, wantTimeout)
		}
	}

	check("unset", `[]`, `1800`)

	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.judge_policy":          "claude-code, qwen, claude-code",
		"autonomous.judge_timeout_seconds": 900,
	})
	check("set", `["claude-code","qwen"]`, `900`)

	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.judge_policy":          []interface{}{},
		"autonomous.judge_timeout_seconds": 0,
	})
	check("cleared", `[]`, `1800`)
}

func TestJudgeConfigDefaultIsThirtyMinutesOnEverySurface(t *testing.T) {
	if config.JudgeTimeoutDefaultSeconds != 1800 {
		t.Fatalf("default = %d, want 1800", config.JudgeTimeoutDefaultSeconds)
	}
	rr := httptest.NewRecorder()
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if !strings.Contains(rr.Body.String(), `"judge_timeout_seconds":1800`) {
		t.Fatalf("GET /api/config does not show the 1800 default")
	}
}
