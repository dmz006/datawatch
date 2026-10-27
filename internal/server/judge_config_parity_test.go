// Surface parity and defaults for the judge routing settings:
// autonomous.judge_policy (ordered backends; empty = the verifier routing
// policy), autonomous.judge_timeout_seconds (default 30 minutes) and
// autonomous.judge_size_threshold_lines (default 400, 0 = size rule off). A missing
// surface, a non-English label that is only a copy of the English one, a
// default that differs between surfaces, or wording that describes the old
// behaviour fails here.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// judgeLocaleKeys are the label and help keys for the judge settings, in all
// five locale bundles.
var judgeLocaleKeys = []string{
	"settings_judge_policy", "settings_judge_policy_help",
	"settings_judge_timeout", "settings_judge_timeout_help",
	"settings_judge_size_threshold", "settings_judge_size_threshold_help",
}

// staleJudgeWording is the wording an empty judge policy and a judge hold
// used to carry: each judge on its unit's guardrail backend, and a PRD blocked
// by a judge-timeout block. An empty judge policy follows the verifier routing
// policy and a judge hold needs review, so no surface may say the old thing.
var staleJudgeWording = []string{
	"each judge runs on its unit",
	"each judge's unit",
	"unit guardrail backend",
	"judge-timeout block",
	"judge timed out on every backend and blocked",
}

// judgeRoutingWording is the default every surface that names the judge policy
// must state: an empty list follows the verifier routing policy.
const judgeRoutingWording = "verifier routing policy"

func TestJudgeConfigSurfaceParity(t *testing.T) {
	// Each surface must carry the needles it names: quoted code keys, colon
	// config keys or backticked doc names. Prose in a comment does not normally
	// carry these forms, so a missing surface fails here. Docs surfaces are
	// included, so a REST or operations page without the keys fails too.
	surfaces := []struct {
		name, rel string
		needles   []string
	}{
		{"config struct", "internal/config/config.go", []string{`yaml:"judge_policy" json:"judge_policy"`, `yaml:"judge_timeout_seconds" json:"judge_timeout_seconds"`, `yaml:"judge_size_threshold_lines" json:"judge_size_threshold_lines"`, judgeRoutingWording}},
		{"autonomous manager cfg", "internal/autonomous/manager.go", []string{`json:"judge_policy"`, `json:"judge_timeout_seconds"`, `json:"judge_size_threshold_lines"`}},
		{"REST", "internal/server/api.go", []string{`"judge_policy":`, `"judge_timeout_seconds":`, `"autonomous.judge_policy"`, `"autonomous.judge_timeout_seconds"`, `"judge_size_threshold_lines":`, `"autonomous.judge_size_threshold_lines"`, judgeRoutingWording}},
		{"MCP schema", "internal/mcp/autonomous.go", []string{`"judge_policy"`, `"judge_timeout_seconds"`, `"judge_size_threshold_lines"`, judgeRoutingWording}},
		{"comm help", "internal/router/router.go", []string{"autonomous.judge_policy", "autonomous.judge_timeout_seconds", "autonomous.judge_size_threshold_lines", judgeRoutingWording}},
		{"comm config-set", "internal/router/sx2_parity.go", []string{`"judge_policy"`, `"judge-policy-set"`}},
		{"comm judge verbs", "internal/router/judge_policy_cmd.go", []string{`"judge-policy"`, `"judge-policy-set"`, `"judge_timeout_seconds"`, `"judge_size_threshold_lines"`, judgeRoutingWording}},
		{"CLI", "cmd/datawatch/cli_autonomous.go", []string{`"judge-policy"`, `"judge-timeout-seconds"`, `"judge-size-threshold-lines"`, judgeRoutingWording}},
		{"web app.js", "internal/server/web/app.js", []string{"'autonomous.judge_policy'", "'autonomous.judge_timeout_seconds'", "'autonomous.judge_size_threshold_lines'", judgeRoutingWording}},
		{"config-reference.yaml", "docs/config-reference.yaml", []string{"judge_policy:", "judge_timeout_seconds:", "judge_size_threshold_lines:", judgeRoutingWording}},
		{"implementation.md", "docs/implementation.md", []string{"`autonomous.judge_policy`", "`autonomous.judge_timeout_seconds`", "`autonomous.judge_size_threshold_lines`", judgeRoutingWording, "`needs_review`"}},
		{"api/autonomous.md", "docs/api/autonomous.md", []string{"judge_policy:", "judge_timeout_seconds:", "judge_size_threshold_lines:", judgeRoutingWording}},
		{"api/openapi.yaml", "docs/api/openapi.yaml", []string{"`judge_policy`", "`judge_timeout_seconds`", "`judge_size_threshold_lines`", judgeRoutingWording}},
		{"mcp.md", "docs/mcp.md", []string{"`judge_policy`", "`judge_timeout_seconds`", "`judge_size_threshold_lines`", judgeRoutingWording, "`needs_review`"}},
		{"api-mcp-mapping.md", "docs/api-mcp-mapping.md", []string{"`judge_policy`", "`judge_timeout_seconds`", "`judge_size_threshold_lines`", judgeRoutingWording}},
		{"operations.md", "docs/operations.md", []string{"`autonomous.judge_timeout_seconds`", "`autonomous.judge_size_threshold_lines`", "`judge-timeout`", "`needs_review`"}},
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
	// YAML and JSON tags on the config struct, without omitempty for any of the
	// keys (an explicit clear, and an explicit 0 for the size rule, must
	// round-trip).
	cfg := readRepoFile(t, "internal/config/config.go")
	if !strings.Contains(cfg, `yaml:"judge_policy" json:"judge_policy"`) {
		t.Error("config struct lacks the judge_policy tags")
	}
	if !strings.Contains(cfg, `yaml:"judge_timeout_seconds" json:"judge_timeout_seconds"`) {
		t.Error("config struct lacks the judge_timeout_seconds tags")
	}
	if !strings.Contains(cfg, `yaml:"judge_size_threshold_lines" json:"judge_size_threshold_lines"`) {
		t.Error("config struct lacks the judge_size_threshold_lines tags")
	}
	for _, line := range strings.Split(cfg, "\n") {
		if strings.Contains(line, `"judge_policy`) || strings.Contains(line, `"judge_timeout_seconds`) || strings.Contains(line, `"judge_size_threshold_lines`) {
			if strings.Contains(line, "omitempty") {
				t.Errorf("config struct tag carries omitempty: %s", strings.TrimSpace(line))
			}
		}
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
			for _, bad := range staleJudgeWording {
				if strings.Contains(s, bad) || strings.Contains(s, "operator review") {
					t.Errorf("%s.json %s still describes the old judge behaviour: %q", lang, k, s)
				}
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

// Every surface that documents the judge time limit states the same default
// (1800 seconds, 30 minutes), so a surface that drifts to another number or
// omits the default fails here. The locale help texts and the web form
// placeholder are checked too.
func TestJudgeConfigDefaultWordingOnEverySurface(t *testing.T) {
	stating := func(src string) bool {
		return strings.Contains(src, "1800") || strings.Contains(src, "30m") ||
			strings.Contains(src, "30 min") || strings.Contains(src, "30-min")
	}
	for _, sf := range []struct{ name, rel string }{
		{"MCP schema", "internal/mcp/autonomous.go"},
		{"comm help", "internal/router/router.go"},
		{"comm judge verbs", "internal/router/judge_policy_cmd.go"},
		{"CLI", "cmd/datawatch/cli_autonomous.go"},
		{"web app.js", "internal/server/web/app.js"},
		{"config-reference.yaml", "docs/config-reference.yaml"},
		{"implementation.md", "docs/implementation.md"},
		{"api/autonomous.md", "docs/api/autonomous.md"},
		{"api/openapi.yaml", "docs/api/openapi.yaml"},
		{"mcp.md", "docs/mcp.md"},
		{"api-mcp-mapping.md", "docs/api-mcp-mapping.md"},
		{"operations.md", "docs/operations.md"},
	} {
		src := readRepoFile(t, sf.rel)
		// Look only at the lines that name the time limit, so an unrelated
		// 1800 elsewhere in the file cannot satisfy the check.
		found, ok := false, false
		for _, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "judge_timeout_seconds") || strings.Contains(line, "judge-timeout-seconds") {
				found = true
				if stating(line) {
					ok = true
					break
				}
			}
		}
		if !found {
			t.Errorf("%s (%s) never names the judge time limit", sf.name, sf.rel)
		} else if !ok {
			t.Errorf("%s (%s) names the judge time limit but not its 1800 second / 30 minute default", sf.name, sf.rel)
		}
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		var m map[string]any
		if err := json.Unmarshal([]byte(readRepoFile(t, "internal/server/web/locales/"+lang+".json")), &m); err != nil {
			t.Fatalf("%s.json: invalid JSON: %v", lang, err)
		}
		help, _ := m["settings_judge_timeout_help"].(string)
		if !strings.Contains(help, "1800") {
			t.Errorf("%s.json settings_judge_timeout_help does not state the 1800 second default: %q", lang, help)
		}
		label, _ := m["settings_judge_timeout"].(string)
		if !strings.Contains(label, "30") {
			t.Errorf("%s.json settings_judge_timeout does not show the 30 minute default: %q", lang, label)
		}
		policy, _ := m["settings_judge_policy"].(string)
		if strings.TrimSpace(policy) == "" {
			t.Errorf("%s.json settings_judge_policy is empty", lang)
		}
	}
	appjs := readRepoFile(t, "internal/server/web/app.js")
	if !strings.Contains(appjs, "placeholder: '1800'") {
		t.Error("web form judge time limit lacks the 1800 placeholder")
	}
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

// Every surface that documents the judge size threshold states the default
// (400 changed lines) and that 0 turns the size rule off, on the lines that
// name the key (or, for wrapped prose and YAML comments, the few lines around
// it), so a surface
// that omits either, or drifts to another default, fails here. The locale
// texts and the web form placeholder are checked too.
func TestJudgeSizeThresholdDefaultAndOffWordingOnEverySurface(t *testing.T) {
	if config.JudgeSizeThresholdDefaultLines != 400 {
		t.Fatalf("default = %d, want 400", config.JudgeSizeThresholdDefaultLines)
	}
	// A surface names the key on one line and may wrap its sentence over the
	// next lines, or put it in the comment above a YAML key, so the window is
	// the naming line, the seven before it and the three after it.
	const before, after = 7, 3
	// "0 = off", "`0` turns the rule off", "0 means off" and "when it is `0`"
	// all say it; the zero and the off must sit in the same sentence.
	offWording := regexp.MustCompile(`(?i)(\b0\b[^.\n]{0,40}\boff\b|\boff\b[^.\n]{0,40}\b0\b|when it is .?0.?\b)`)
	for _, sf := range []struct{ name, rel string }{
		{"MCP schema", "internal/mcp/autonomous.go"},
		{"comm help", "internal/router/router.go"},
		{"comm judge verbs", "internal/router/judge_policy_cmd.go"},
		{"CLI", "cmd/datawatch/cli_autonomous.go"},
		{"web app.js", "internal/server/web/app.js"},
		{"config-reference.yaml", "docs/config-reference.yaml"},
		{"implementation.md", "docs/implementation.md"},
		{"api/autonomous.md", "docs/api/autonomous.md"},
		{"api/openapi.yaml", "docs/api/openapi.yaml"},
		{"mcp.md", "docs/mcp.md"},
		{"api-mcp-mapping.md", "docs/api-mcp-mapping.md"},
		{"operations.md", "docs/operations.md"},
	} {
		lines := strings.Split(readRepoFile(t, sf.rel), "\n")
		found, hasDefault, hasOff := false, false, false
		for i, line := range lines {
			if !strings.Contains(line, "judge_size_threshold_lines") && !strings.Contains(line, "judge-size-threshold-lines") {
				continue
			}
			found = true
			start, end := i-before, i+after+1
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			text := strings.Join(lines[start:end], "\n")
			if strings.Contains(text, "400") {
				hasDefault = true
			}
			if offWording.MatchString(text) {
				hasOff = true
			}
		}
		switch {
		case !found:
			t.Errorf("%s (%s) never names the judge size threshold", sf.name, sf.rel)
		case !hasDefault:
			t.Errorf("%s (%s) names the judge size threshold but not its default of 400", sf.name, sf.rel)
		case !hasOff:
			t.Errorf("%s (%s) names the judge size threshold but not that 0 turns it off", sf.name, sf.rel)
		}
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		var m map[string]any
		if err := json.Unmarshal([]byte(readRepoFile(t, "internal/server/web/locales/"+lang+".json")), &m); err != nil {
			t.Fatalf("%s.json: invalid JSON: %v", lang, err)
		}
		help, _ := m["settings_judge_size_threshold_help"].(string)
		if !strings.Contains(help, "400") {
			t.Errorf("%s.json settings_judge_size_threshold_help does not state the 400 line default: %q", lang, help)
		}
		if !strings.Contains(help, "0") {
			t.Errorf("%s.json settings_judge_size_threshold_help does not say that 0 turns the rule off: %q", lang, help)
		}
		label, _ := m["settings_judge_size_threshold"].(string)
		if !strings.Contains(label, "0") {
			t.Errorf("%s.json settings_judge_size_threshold does not show that 0 turns the rule off: %q", lang, label)
		}
	}
	appjs := readRepoFile(t, "internal/server/web/app.js")
	if !strings.Contains(appjs, "key: 'autonomous.judge_size_threshold_lines'") || !strings.Contains(appjs, "min: 0, placeholder: '400'") {
		t.Error("web form judge size threshold lacks the 0 minimum and the 400 placeholder")
	}
}
