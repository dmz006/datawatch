// Question quality check on the raise path: POST /api/questions runs the
// quality check under autonomous.questions.quality_mode, folds the optional
// tradeoffs object into the options, and GET /api/questions/quality reports
// the effective policy.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// qualityTestServer wires a fake store and the quality keys into a Server
// with the operator-questions switch on. An empty mode leaves the key unset
// (the shipped default, warn).
func qualityTestServer(t *testing.T, mode string, minChars *int, pattern string) (*Server, *fakeQuestions) {
	t.Helper()
	cfg := questionRESTTestConfig(true)
	if mode != "" {
		cfg.Autonomous.Questions.QualityMode = &mode
	}
	if minChars != nil {
		cfg.Autonomous.Questions.QualityMinTextChars = minChars
	}
	if pattern != "" {
		cfg.Autonomous.Questions.QualityTrackerIDPattern = &pattern
	}
	f := newFakeQuestions()
	f.stored = nil
	return &Server{cfg: &cfg, questionStore: f}, f
}

const qualityLongText = "Should the retry backoff be capped at five minutes, or keep growing until the operator steps in?"

// goodQualityBody is a question every check passes: long text, trade-offs on
// every option, no identifier.
func goodQualityBody() map[string]any {
	return map[string]any{
		"label":   "decision",
		"context": "none",
		"text":    qualityLongText,
		"options": []map[string]any{
			{"id": "yes", "label": "Yes, cap it", "tradeoffs": "Bounded delay, but a long outage retries more often."},
			{"id": "no", "label": "No", "tradeoffs": "Fewer retries, but a stuck task can wait for hours."},
		},
		"default": "yes",
	}
}

type qualityRaiseResp struct {
	Question autonomous.Question `json:"question"`
	Warnings []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"warnings"`
	Findings []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"findings"`
}

func decodeQualityRaise(t *testing.T, rr *httptest.ResponseRecorder) qualityRaiseResp {
	t.Helper()
	var out qualityRaiseResp
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %d response: %v (body=%q)", rr.Code, err, rr.Body.String())
	}
	return out
}

func qualityCodes(items []struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}) []string {
	var codes []string
	for _, it := range items {
		codes = append(codes, it.Code)
	}
	return codes
}

func hasCode(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func TestQuestionQuality_RaiseWarn(t *testing.T) {
	s, f := qualityTestServer(t, "warn", nil, "")
	body := goodQualityBody()
	body["options"] = []map[string]any{{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}}

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rr.Code, rr.Body.String())
	}
	got := decodeQualityRaise(t, rr)
	if len(got.Warnings) == 0 || got.Warnings[0].Code == "" || got.Warnings[0].Message == "" {
		t.Fatalf("warnings = %+v, want at least one with code and message", got.Warnings)
	}
	if len(f.stored) != 1 {
		t.Fatalf("stored %d questions, want 1: warn mode stores the question", len(f.stored))
	}
}

func TestQuestionQuality_RaiseRefuse(t *testing.T) {
	s, f := qualityTestServer(t, "refuse", nil, "")
	body := goodQualityBody()
	body["options"] = []map[string]any{{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}}

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%q)", rr.Code, rr.Body.String())
	}
	got := decodeQualityRaise(t, rr)
	if !hasCode(qualityCodes(got.Findings), autonomous.QualityCodeNoTradeoffs) {
		t.Fatalf("findings = %+v, want %s", got.Findings, autonomous.QualityCodeNoTradeoffs)
	}
	if len(f.stored) != 0 {
		t.Fatalf("stored %d questions, want 0: refuse mode stores nothing", len(f.stored))
	}

	// The asker fixes the question and resends it: it is stored.
	rr = doQ(t, s, http.MethodPost, "/api/questions", goodQualityBody())
	if rr.Code != http.StatusCreated {
		t.Fatalf("resend status = %d, want 201 (body=%q)", rr.Code, rr.Body.String())
	}
	if len(f.stored) != 1 {
		t.Fatalf("stored %d questions after the resend, want 1", len(f.stored))
	}
}

func TestQuestionQuality_RaiseOff(t *testing.T) {
	s, f := qualityTestServer(t, "off", nil, "")
	body := map[string]any{
		"context": "none",
		"text":    "Cap it? See AB123.",
		"options": []map[string]any{{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}},
		"default": "yes",
	}

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "warnings") {
		t.Fatalf("off mode must not report warnings: %q", rr.Body.String())
	}
	if len(f.stored) != 1 {
		t.Fatalf("stored %d questions, want 1", len(f.stored))
	}
}

func TestQuestionQuality_RaiseNoTradeoffs(t *testing.T) {
	s, _ := qualityTestServer(t, "warn", nil, "")
	body := goodQualityBody()
	body["options"] = []map[string]any{
		{"id": "yes", "label": "Yes", "tradeoffs": "Bounded delay."},
		{"id": "no", "label": "No"},
	}

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rr.Code, rr.Body.String())
	}
	got := decodeQualityRaise(t, rr)
	codes := qualityCodes(got.Warnings)
	if len(codes) != 1 || codes[0] != autonomous.QualityCodeNoTradeoffs {
		t.Fatalf("warning codes = %v, want only %s", codes, autonomous.QualityCodeNoTradeoffs)
	}
	if !strings.Contains(got.Warnings[0].Message, "no") {
		t.Fatalf("message %q should name the option without trade-offs", got.Warnings[0].Message)
	}
}

func TestQuestionQuality_RaiseShortText(t *testing.T) {
	s, _ := qualityTestServer(t, "warn", nil, "")
	body := goodQualityBody()
	body["text"] = "Cap the backoff?"

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rr.Code, rr.Body.String())
	}
	codes := qualityCodes(decodeQualityRaise(t, rr).Warnings)
	if len(codes) != 1 || codes[0] != autonomous.QualityCodeTextTooShort {
		t.Fatalf("warning codes = %v, want only %s", codes, autonomous.QualityCodeTextTooShort)
	}

	// The minimum is the configured one: lowered to 5, the same text passes.
	five := 5
	s, _ = qualityTestServer(t, "warn", &five, "")
	rr = doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated || strings.Contains(rr.Body.String(), "warnings") {
		t.Fatalf("with min 5: status %d body %q, want 201 and no warnings", rr.Code, rr.Body.String())
	}
}

func TestQuestionQuality_RaiseTrackerID(t *testing.T) {
	s, _ := qualityTestServer(t, "warn", nil, "")
	body := goodQualityBody()
	body["text"] = qualityLongText + " This follows item AB123."

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rr.Code, rr.Body.String())
	}
	codes := qualityCodes(decodeQualityRaise(t, rr).Warnings)
	if len(codes) != 1 || codes[0] != autonomous.QualityCodeTrackerID {
		t.Fatalf("warning codes = %v, want only %s", codes, autonomous.QualityCodeTrackerID)
	}

	// A configured pattern replaces the default one.
	s, _ = qualityTestServer(t, "warn", nil, `ticket-[0-9]+`)
	rr = doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated || strings.Contains(rr.Body.String(), "warnings") {
		t.Fatalf("custom pattern: status %d body %q, want 201 and no warnings", rr.Code, rr.Body.String())
	}
}

func TestQuestionQuality_Read(t *testing.T) {
	min := 0
	s, _ := qualityTestServer(t, "refuse", &min, `ticket-[0-9]+`)
	rr := httptest.NewRecorder()
	s.handleQuestionQuality(rr, httptest.NewRequest(http.MethodGet, "/api/questions/quality", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{"mode": "refuse", "min_text_chars": float64(0), "tracker_id_pattern": `ticket-[0-9]+`, "queue_preview_lines": float64(3)}
	if len(got) != len(want) {
		t.Fatalf("body = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	// Unset keys read their defaults.
	s, _ = qualityTestServer(t, "", nil, "")
	rr = httptest.NewRecorder()
	s.handleQuestionQuality(rr, httptest.NewRequest(http.MethodGet, "/api/questions/quality", nil))
	var def questionQualityResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &def); err != nil {
		t.Fatalf("decode defaults: %v", err)
	}
	if def.Mode != "warn" || def.MinTextChars != 60 || def.TrackerIDPattern != autonomous.DefaultQualityTrackerIDPattern || def.QueuePreviewLines != 3 {
		t.Fatalf("defaults = %+v", def)
	}

	// The switch is honoured, and only GET is accepted.
	cfg := questionRESTTestConfig(false)
	off := &Server{cfg: &cfg}
	rr = httptest.NewRecorder()
	off.handleQuestionQuality(rr, httptest.NewRequest(http.MethodGet, "/api/questions/quality", nil))
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "autonomous.questions.enabled") {
		t.Fatalf("switch off: status %d body %q, want 503 naming the switch", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	s.handleQuestionQuality(rr, httptest.NewRequest(http.MethodPost, "/api/questions/quality", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rr.Code)
	}
}

// The route is matched before the /api/questions/{id} subtree.
func TestQuestionQuality_ReadRouteBeforeID(t *testing.T) {
	s, _ := qualityTestServer(t, "warn", nil, "")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/questions/quality", s.handleQuestionQuality)
	mux.HandleFunc("/api/questions/", s.handleQuestions)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/questions/quality", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"mode"`) {
		t.Fatalf("status %d body %q, want the quality policy", rr.Code, rr.Body.String())
	}
}

func TestQuestionQuality_TradeoffsMerge(t *testing.T) {
	s, f := qualityTestServer(t, "refuse", nil, "")
	body := goodQualityBody()
	body["options"] = []map[string]any{
		{"id": "yes", "label": "Yes, cap it", "tradeoffs": "Own text wins."},
		{"id": "no", "label": "No"},
	}
	body["tradeoffs"] = map[string]string{
		"yes": "Ignored, the option has its own.",
		"no":  "Fewer retries, but a stuck task can wait for hours.",
	}

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: the merged trade-offs satisfy refuse mode (body=%q)", rr.Code, rr.Body.String())
	}
	if len(f.stored) != 1 {
		t.Fatalf("stored %d questions, want 1", len(f.stored))
	}
	opts := f.stored[0].Options
	if opts[0].Tradeoffs != "Own text wins." {
		t.Errorf("yes tradeoffs = %q, want the option's own text kept", opts[0].Tradeoffs)
	}
	if opts[1].Tradeoffs != "Fewer retries, but a stuck task can wait for hours." {
		t.Errorf("no tradeoffs = %q, want the merged text", opts[1].Tradeoffs)
	}
}

func TestQuestionQuality_TradeoffsUnknownOption(t *testing.T) {
	s, f := qualityTestServer(t, "off", nil, "")
	body := goodQualityBody()
	body["tradeoffs"] = map[string]string{"maybe": "No such option."}

	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "maybe") {
		t.Fatalf("body %q should name the unknown option id", rr.Body.String())
	}
	if len(f.stored) != 0 {
		t.Fatalf("stored %d questions, want 0", len(f.stored))
	}
}
