// Surface parity and defaults for the judge routing settings:
// autonomous.judge_policy (ordered backends; empty = follow the verifier
// policy) and autonomous.judge_timeout_seconds (default 30 minutes). A
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

func TestJudgeConfigSurfaceParity(t *testing.T) {
	// Each surface names the keys it must carry. The comm config-set verb
	// parses a plain number itself, so only the list key needs naming there.
	surfaces := map[string]struct {
		rel  string
		keys []string
	}{
		"config struct":          {"internal/config/config.go", judgeConfigKeys},
		"autonomous manager cfg": {"internal/autonomous/manager.go", judgeConfigKeys},
		"REST":                   {"internal/server/api.go", judgeConfigKeys},
		"MCP":                    {"internal/mcp/autonomous.go", judgeConfigKeys},
		"comm help":              {"internal/router/router.go", judgeConfigKeys},
		"comm config-set":        {"internal/router/sx2_parity.go", judgeConfigKeys[:1]},
		"CLI":                    {"cmd/datawatch/cli_autonomous.go", judgeConfigKeys},
		"web app.js":             {"internal/server/web/app.js", judgeConfigKeys},
		"config-reference.yaml":  {"docs/config-reference.yaml", judgeConfigKeys},
		"implementation.md":      {"docs/implementation.md", judgeConfigKeys},
	}
	for name, sf := range surfaces {
		src := readRepoFile(t, sf.rel)
		for _, k := range sf.keys {
			if !strings.Contains(src, k) {
				t.Errorf("%s (%s) lacks %s", name, sf.rel, k)
			}
		}
	}
	// The PWA, REST and comm surfaces also name the full dotted key.
	for _, rel := range []string{"internal/server/web/app.js", "internal/server/api.go"} {
		src := readRepoFile(t, rel)
		for _, k := range judgeConfigKeys {
			if !strings.Contains(src, "autonomous."+k) {
				t.Errorf("%s lacks the dotted key autonomous.%s", rel, k)
			}
		}
	}
	// CLI flags use dashes.
	cli := readRepoFile(t, "cmd/datawatch/cli_autonomous.go")
	for _, k := range judgeConfigKeys {
		if !strings.Contains(cli, `"`+strings.ReplaceAll(k, "_", "-")+`"`) {
			t.Errorf("CLI lacks flag --%s", strings.ReplaceAll(k, "_", "-"))
		}
	}
	// YAML tags on the config struct, without omitempty for the list (an
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
