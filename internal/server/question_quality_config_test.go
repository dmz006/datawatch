// Question quality and queue preview config keys — Configuration Accessibility
// round trip for autonomous.questions.quality_mode, quality_min_text_chars,
// quality_tracker_id_pattern and queue_preview_lines. GET /api/config returns
// the effective values, an explicit value survives a PUT and reaches
// config.yaml, and a bad value is a 400 that names the key, applies nothing
// and writes nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type questionQualitySurface struct {
	Mode    string `json:"quality_mode"`
	MinText int    `json:"quality_min_text_chars"`
	Pattern string `json:"quality_tracker_id_pattern"`
	Preview int    `json:"queue_preview_lines"`
}

func getQuestionQualitySurface(t *testing.T, s *Server) questionQualitySurface {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["questions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks questions")
	}
	var got questionQualitySurface
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	return got
}

func TestQuestionQuality_ConfigRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	// Defaults come out of GET with nothing stored.
	got := getQuestionQualitySurface(t, s)
	want := questionQualitySurface{Mode: "warn", MinText: 60, Pattern: autonomous.DefaultQualityTrackerIDPattern, Preview: 3}
	if got != want {
		t.Fatalf("GET defaults = %+v, want %+v", got, want)
	}
	q := s.cfg.Autonomous.Questions
	if q.QualityMode != nil || q.QualityMinTextChars != nil || q.QualityTrackerIDPattern != nil || q.QueuePreviewLines != nil {
		t.Fatalf("stored values must be absent by default, got %+v", q)
	}

	// The shipped config defaults and the quality check's defaults are one set.
	if pol := autonomous.QualityPolicyFromConfig(s.cfg.Autonomous.Questions); pol != autonomous.NormalizeQualityPolicy(autonomous.QualityPolicy{}) {
		t.Fatalf("policy from default config = %+v, want the quality check's own defaults %+v",
			pol, autonomous.NormalizeQualityPolicy(autonomous.QualityPolicy{}))
	}

	// Explicit values, including 0 minimum chars, survive a PUT and a GET.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.questions.quality_mode":               "refuse",
		"autonomous.questions.quality_min_text_chars":     0,
		"autonomous.questions.quality_tracker_id_pattern": `\bQ-[0-9]+\b`,
		"autonomous.questions.queue_preview_lines":        6,
	})
	got = getQuestionQualitySurface(t, s)
	want = questionQualitySurface{Mode: "refuse", MinText: 0, Pattern: `\bQ-[0-9]+\b`, Preview: 6}
	if got != want {
		t.Fatalf("GET after PUT = %+v, want %+v", got, want)
	}
	q = s.cfg.Autonomous.Questions
	if q.QualityMinTextChars == nil || *q.QualityMinTextChars != 0 {
		t.Fatalf("an explicit 0 must be stored as 0, got %v", q.QualityMinTextChars)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, frag := range []string{"quality_mode: refuse", "quality_min_text_chars: 0", "quality_tracker_id_pattern:", "queue_preview_lines: 6"} {
		if !strings.Contains(string(raw), frag) {
			t.Errorf("config.yaml lacks %q:\n%s", frag, raw)
		}
	}

	// The policy built from the stored config carries the same values.
	pol := autonomous.QualityPolicyFromConfig(s.cfg.Autonomous.Questions)
	if pol.Mode != "refuse" || pol.MinTextChars != 0 || pol.TrackerIDPattern != `\bQ-[0-9]+\b` {
		t.Fatalf("policy from config = %+v", pol)
	}
}

func TestQuestionQuality_ConfigRefusesBad(t *testing.T) {
	cases := []struct {
		key, value, named string
	}{
		{"autonomous.questions.quality_mode", `"strict"`, "off"},
		{"autonomous.questions.quality_mode", `"WARN"`, "refuse"},
		{"autonomous.questions.quality_mode", `true`, "warn"},
		{"autonomous.questions.quality_mode", `3`, "warn"},
		{"autonomous.questions.quality_min_text_chars", `-1`, ">= 0"},
		{"autonomous.questions.quality_min_text_chars", `"many"`, ">= 0"},
		{"autonomous.questions.quality_min_text_chars", `"60"`, ">= 0"},
		{"autonomous.questions.quality_min_text_chars", `12.5`, ">= 0"},
		{"autonomous.questions.quality_min_text_chars", `null`, ">= 0"},
		{"autonomous.questions.quality_tracker_id_pattern", `"[unclosed"`, "regular expression"},
		{"autonomous.questions.quality_tracker_id_pattern", `"(?P<"`, "regular expression"},
		{"autonomous.questions.quality_tracker_id_pattern", `7`, "regular expression"},
		{"autonomous.questions.queue_preview_lines", `0`, ">= 1"},
		{"autonomous.questions.queue_preview_lines", `-3`, ">= 1"},
		{"autonomous.questions.queue_preview_lines", `"three"`, ">= 1"},
		{"autonomous.questions.queue_preview_lines", `2.5`, ">= 1"},
	}
	for _, c := range cases {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		// Sent beside a valid key: the whole PUT is refused, so that key must
		// not land either.
		body := `{"autonomous.questions.decision_gate": false, "` + c.key + `": ` + c.value + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s=%s: want 400, got %d (%s)", c.key, c.value, rr.Code, rr.Body.String())
		}
		if msg := rr.Body.String(); !strings.Contains(msg, c.key) || !strings.Contains(msg, c.named) {
			t.Errorf("400 for %s=%s should name the key and %q; got %q", c.key, c.value, c.named, msg)
		}
		q := s.cfg.Autonomous.Questions
		if q.DecisionGate != nil || q.QualityMode != nil || q.QualityMinTextChars != nil ||
			q.QualityTrackerIDPattern != nil || q.QueuePreviewLines != nil {
			t.Fatalf("a rejected PUT (%s=%s) changed the stored config: %+v", c.key, c.value, q)
		}
		if got := getQuestionQualitySurface(t, s); got.Mode != "warn" || got.MinText != 60 || got.Preview != 3 {
			t.Fatalf("GET after rejected PUT %s=%s: %+v", c.key, c.value, got)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("a rejected PUT (%s=%s) wrote config.yaml", c.key, c.value)
		}
	}
}
