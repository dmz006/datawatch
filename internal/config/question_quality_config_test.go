package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func qqStr(v string) *string { return &v }
func qqInt(v int) *int       { return &v }

func TestQuestionQuality_ConfigDefaultsWhenAbsent(t *testing.T) {
	for name, q := range map[string]AutonomousQuestionsConfig{
		"zero value":     {},
		"default config": DefaultConfig().Autonomous.Questions,
	} {
		if got := q.EffectiveQualityMode(); got != "warn" {
			t.Errorf("%s: quality_mode = %q, want warn", name, got)
		}
		if got := q.EffectiveQualityMinTextChars(); got != 60 {
			t.Errorf("%s: quality_min_text_chars = %d, want 60", name, got)
		}
		if got := q.EffectiveQualityTrackerIDPattern(); got != DefaultQuestionQualityTrackerIDPattern {
			t.Errorf("%s: quality_tracker_id_pattern = %q, want the default", name, got)
		}
		if got := q.EffectiveQueuePreviewLines(); got != 3 {
			t.Errorf("%s: queue_preview_lines = %d, want 3", name, got)
		}
		if q.QualityMode != nil || q.QualityMinTextChars != nil || q.QualityTrackerIDPattern != nil || q.QueuePreviewLines != nil {
			t.Errorf("%s: stored values must be absent (nil), got %+v", name, q)
		}
	}
	// An empty mode or pattern reads as the default too.
	q := AutonomousQuestionsConfig{QualityMode: qqStr(""), QualityTrackerIDPattern: qqStr("")}
	if q.EffectiveQualityMode() != "warn" || q.EffectiveQualityTrackerIDPattern() != DefaultQuestionQualityTrackerIDPattern {
		t.Errorf("empty values must read the defaults, got %q and %q", q.EffectiveQualityMode(), q.EffectiveQualityTrackerIDPattern())
	}
}

func TestQuestionQuality_ConfigExplicitValuesWin(t *testing.T) {
	q := AutonomousQuestionsConfig{
		QualityMode:             qqStr("refuse"),
		QualityMinTextChars:     qqInt(0),
		QualityTrackerIDPattern: qqStr(`\bX-[0-9]+\b`),
		QueuePreviewLines:       qqInt(7),
	}
	if q.EffectiveQualityMode() != "refuse" {
		t.Errorf("mode = %q, want refuse", q.EffectiveQualityMode())
	}
	if q.EffectiveQualityMinTextChars() != 0 {
		t.Errorf("min chars = %d, want an explicit 0 to stay 0", q.EffectiveQualityMinTextChars())
	}
	if q.EffectiveQualityTrackerIDPattern() != `\bX-[0-9]+\b` {
		t.Errorf("pattern = %q", q.EffectiveQualityTrackerIDPattern())
	}
	if q.EffectiveQueuePreviewLines() != 7 {
		t.Errorf("preview lines = %d, want 7", q.EffectiveQueuePreviewLines())
	}
}

func TestQuestionQuality_ConfigYAMLAndJSONRoundTrip(t *testing.T) {
	in := AutonomousQuestionsConfig{
		QualityMode:             qqStr("off"),
		QualityMinTextChars:     qqInt(0),
		QualityTrackerIDPattern: qqStr(`\b[A-Z]{2}-[0-9]+\b`),
		QueuePreviewLines:       qqInt(1),
	}

	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"quality_mode:", "quality_min_text_chars: 0", "quality_tracker_id_pattern:", "queue_preview_lines: 1"} {
		if !strings.Contains(string(data), key) {
			t.Errorf("yaml lacks %q (an explicit value must not be elided):\n%s", key, data)
		}
	}
	var fromYAML AutonomousQuestionsConfig
	if err := yaml.Unmarshal(data, &fromYAML); err != nil {
		t.Fatal(err)
	}
	checkQualityRoundTrip(t, "yaml", fromYAML, in)

	js, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON AutonomousQuestionsConfig
	if err := json.Unmarshal(js, &fromJSON); err != nil {
		t.Fatal(err)
	}
	checkQualityRoundTrip(t, "json", fromJSON, in)

	// An absent config marshals the keys as null/empty and reads back absent.
	empty, _ := yaml.Marshal(AutonomousQuestionsConfig{})
	var back AutonomousQuestionsConfig
	if err := yaml.Unmarshal(empty, &back); err != nil {
		t.Fatal(err)
	}
	if back.QualityMinTextChars != nil || back.QueuePreviewLines != nil || back.EffectiveQualityMinTextChars() != 60 {
		t.Errorf("absent keys must stay absent after a round trip, got %+v", back)
	}
}

func checkQualityRoundTrip(t *testing.T, via string, got, want AutonomousQuestionsConfig) {
	t.Helper()
	if got.EffectiveQualityMode() != *want.QualityMode ||
		got.QualityMinTextChars == nil || *got.QualityMinTextChars != *want.QualityMinTextChars ||
		got.EffectiveQualityTrackerIDPattern() != *want.QualityTrackerIDPattern ||
		got.QueuePreviewLines == nil || *got.QueuePreviewLines != *want.QueuePreviewLines {
		t.Errorf("%s round trip changed the values: got %+v want %+v", via, got, want)
	}
}

func TestQuestionQuality_ConfigLoadsFromFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  questions:\n    quality_mode: refuse\n    quality_min_text_chars: 0\n" +
		"    quality_tracker_id_pattern: '\\bZ[0-9]+\\b'\n    queue_preview_lines: 5\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	q := cfg.Autonomous.Questions
	if q.EffectiveQualityMode() != "refuse" || q.EffectiveQualityMinTextChars() != 0 ||
		q.EffectiveQualityTrackerIDPattern() != `\bZ[0-9]+\b` || q.EffectiveQueuePreviewLines() != 5 {
		t.Errorf("loaded values wrong: %+v", q)
	}
}

func TestQuestionQuality_ConfigBadValuesRefused(t *testing.T) {
	cases := []struct {
		name, yaml, key string
	}{
		{"bad mode", "quality_mode: strict", "quality_mode"},
		{"negative min chars", "quality_min_text_chars: -1", "quality_min_text_chars"},
		{"non-numeric min chars", "quality_min_text_chars: lots", ""}, // the YAML decoder refuses it before validate() runs
		{"uncompilable pattern", "quality_tracker_id_pattern: '[unclosed'", "quality_tracker_id_pattern"},
		{"zero preview lines", "queue_preview_lines: 0", "queue_preview_lines"},
		{"negative preview lines", "queue_preview_lines: -2", "queue_preview_lines"},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte("autonomous:\n  questions:\n    "+c.yaml+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil {
			t.Errorf("%s: Load accepted %q", c.name, c.yaml)
			continue
		}
		if c.key != "" && !strings.Contains(err.Error(), "autonomous.questions."+c.key) {
			t.Errorf("%s: error %q does not name the key", c.name, err)
		}
	}

	if err := ValidateQuestionQualityMode("strict"); err == nil ||
		!strings.Contains(err.Error(), `"off"`) || !strings.Contains(err.Error(), `"warn"`) || !strings.Contains(err.Error(), `"refuse"`) {
		t.Errorf("mode error must list the valid values, got %v", err)
	}
	for _, ok := range []string{"", "off", "warn", "refuse"} {
		if err := ValidateQuestionQualityMode(ok); err != nil {
			t.Errorf("mode %q refused: %v", ok, err)
		}
	}
	if ValidateQuestionQualityMinTextChars(0) != nil || ValidateQuestionQualityMinTextChars(-1) == nil {
		t.Error("min chars: 0 must pass and -1 must be refused")
	}
	if ValidateQuestionQueuePreviewLines(1) != nil || ValidateQuestionQueuePreviewLines(0) == nil {
		t.Error("preview lines: 1 must pass and 0 must be refused")
	}
	if ValidateQuestionQualityTrackerIDPattern("") != nil || ValidateQuestionQualityTrackerIDPattern(DefaultQuestionQualityTrackerIDPattern) != nil {
		t.Error("empty and the default pattern must pass")
	}
	if ValidateQuestionQualityTrackerIDPattern("(") == nil {
		t.Error("an uncompilable pattern must be refused")
	}
}
