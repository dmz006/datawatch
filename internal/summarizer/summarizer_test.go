package summarizer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// TestSummarize_Disabled verifies that a disabled summarizer returns empty
// string without error.
func TestSummarize_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = false
	cfg.Session.Summarizer.LLMRef = "my-llm"

	svc := New(cfg, nil)
	result, err := svc.Summarize(context.Background(), "some session output")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string when disabled, got %q", result)
	}
}

// TestSummarize_NoLLMRef verifies that a missing LLMRef returns empty string.
func TestSummarize_NoLLMRef(t *testing.T) {
	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = ""

	svc := New(cfg, nil)
	result, err := svc.Summarize(context.Background(), "some session output")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string when LLMRef is empty, got %q", result)
	}
}

// TestSummarize_OllamaRoundTrip uses an httptest.Server that mimics Ollama's
// API, verifies the request format, and confirms the response is parsed correctly.
func TestSummarize_OllamaRoundTrip(t *testing.T) {
	const wantSummary = "The session completed successfully with no errors."
	var gotModel, gotPrompt string
	gotStream := true // should be set to false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		gotModel = body.Model
		gotPrompt = body.Prompt
		gotStream = body.Stream
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": wantSummary}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	// Build a minimal inference registry with an ollama entry.
	reg := inference.NewRegistry()
	err := reg.Add(&inference.LLM{
		Name:  "test-ollama",
		Kind:  inference.KindOllama,
		Model: "llama3",
	})
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	result, err := svc.Summarize(context.Background(), "session output here")
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if result != wantSummary {
		t.Errorf("summary mismatch: got %q, want %q", result, wantSummary)
	}
	if gotModel != "llama3" {
		t.Errorf("model sent: got %q, want %q", gotModel, "llama3")
	}
	if gotStream {
		t.Errorf("stream should be false, got true")
	}
	if !strings.Contains(gotPrompt, DefaultSummarizerPrompt) {
		t.Errorf("prompt should contain DefaultSummarizerPrompt, got: %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "session output here") {
		t.Errorf("prompt should contain the session text, got: %q", gotPrompt)
	}
}

// TestSummarize_DefaultPrompt verifies that the DefaultSummarizerPrompt
// constant is not empty.
func TestSummarize_DefaultPrompt(t *testing.T) {
	if strings.TrimSpace(DefaultSummarizerPrompt) == "" {
		t.Fatal("DefaultSummarizerPrompt must not be empty")
	}
}

// TestParseDualSummary covers every branch of the parseDualSummary fallback chain.
func TestParseDualSummary(t *testing.T) {
	shortText := "Working on auth. Tests passed. Waiting for review."
	longText := "The session implemented OAuth2 login using PKCE. All unit tests passed and the integration suite ran clean. The PR is now open and awaiting code review from the team."

	tests := []struct {
		name      string
		raw       string
		wantShort string
		wantLong  string
		// wantLongNonEmpty just checks long is non-empty (for fallbacks where exact value varies)
		wantLongNonEmpty bool
	}{
		{
			name:      "primary markers ===SHORT=== / ===LONG===",
			raw:       "===SHORT===\n" + shortText + "\n===LONG===\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "primary markers with preamble before ===SHORT===",
			raw:       "Here is my analysis:\n\n===SHORT===\n" + shortText + "\n===LONG===\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "primary markers with trailing text after long",
			raw:       "===SHORT===\n" + shortText + "\n===LONG===\n" + longText + "\n\nEnd.",
			wantShort: shortText,
			wantLong:  longText + "\n\nEnd.",
		},
		{
			name:      "[SHORT] / [LONG] alternate markers",
			raw:       "[SHORT]\n" + shortText + "\n[LONG]\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "**SHORT** / **LONG** alternate markers",
			raw:       "**SHORT**\n" + shortText + "\n**LONG**\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "line-header: SHORT: / LONG: on own lines",
			raw:       "SHORT:\n" + shortText + "\nLONG:\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "line-header: ## SHORT / ## LONG markdown headers",
			raw:       "## SHORT\n" + shortText + "\n## LONG\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "line-header: case insensitive SHORT/LONG",
			raw:       "Short\n" + shortText + "\nLong\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:             "blank-line paragraph split",
			raw:              shortText + "\n\n" + longText,
			wantShort:        shortText,
			wantLong:         longText,
		},
		{
			name:             "sentence-split last resort: 3+ sentences",
			raw:              "First sentence. Second sentence. Third sentence. Fourth sentence which becomes long.",
			wantShort:        "First sentence. Second sentence. Third sentence.",
			wantLongNonEmpty: true,
		},
		{
			name:      "sentence-split last resort: fewer than 3 sentences returns full text as short",
			raw:       "Only one sentence here.",
			wantShort: "Only one sentence here.",
			wantLong:  "",
		},
		{
			name:      "think-tag stripped before parsing primary markers",
			raw:       "<think>internal reasoning here</think>\n===SHORT===\n" + shortText + "\n===LONG===\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "think-tag stripped before parsing paragraph split",
			raw:       "<think>some thought</think>\n" + shortText + "\n\n" + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "unclosed think-tag drops everything after it",
			raw:       "===SHORT===\n" + shortText + "\n===LONG===\n" + longText + "<think>unfinished",
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "empty input returns empty short and long",
			raw:       "",
			wantShort: "",
			wantLong:  "",
		},
		{
			name:      "whitespace-only input returns empty short and long",
			raw:       "   \n\n  ",
			wantShort: "",
			wantLong:  "",
		},
		{
			name:      "markers present but long before short (invalid) falls through to paragraph",
			raw:       "===LONG===\n" + longText + "\n\n===SHORT===\n" + shortText,
			wantShort: "===LONG===\n" + longText,
			wantLong:  "===SHORT===\n" + shortText,
		},
		{
			name:      "primary markers with extra blank lines around content",
			raw:       "===SHORT===\n\n" + shortText + "\n\n===LONG===\n\n" + longText + "\n\n",
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "inline SHORT:/LONG: labels on same line as content",
			raw:       "SHORT: " + shortText + "\nLONG: " + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "inline labels with decoration: ## SHORT: / ## LONG:",
			raw:       "## SHORT: " + shortText + "\n## LONG: " + longText,
			wantShort: shortText,
			wantLong:  longText,
		},
		{
			name:      "inline labels with multi-line long content",
			raw:       "SHORT: " + shortText + "\nLONG: Line one.\nLine two.\nLine three.",
			wantShort: shortText,
			wantLong:  "Line one.\nLine two.\nLine three.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotShort, gotLong := parseDualSummary(tc.raw)
			if gotShort != tc.wantShort {
				t.Errorf("short mismatch\n  got:  %q\n  want: %q", gotShort, tc.wantShort)
			}
			if tc.wantLongNonEmpty {
				if strings.TrimSpace(gotLong) == "" {
					t.Errorf("long expected non-empty, got %q", gotLong)
				}
			} else if gotLong != tc.wantLong {
				t.Errorf("long mismatch\n  got:  %q\n  want: %q", gotLong, tc.wantLong)
			}
		})
	}
}

// TestExtractFirstNSentences verifies the sentence-boundary scanner.
func TestExtractFirstNSentences(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		n     int
		want  string
	}{
		{"exactly n sentences", "One. Two. Three.", 3, "One. Two. Three."},
		{"more than n sentences", "One. Two. Three. Four. Five.", 3, "One. Two. Three."},
		{"fewer than n sentences", "One. Two.", 3, "One. Two."},
		{"single sentence", "Just one.", 3, "Just one."},
		{"exclamation and question marks", "Yes! Really? Always.", 3, "Yes! Really? Always."},
		{"empty string", "", 3, ""},
		{"no punctuation", "no sentence endings here", 3, "no sentence endings here"},
		{"n=1 stops at first period", "First. Second. Third.", 1, "First."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFirstNSentences(tc.text, tc.n)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStripThinkTags verifies that <think>...</think> blocks are removed.
func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no tags", "hello world", "hello world"},
		{"single tag", "<think>hidden</think>visible", "visible"},
		{"multiple tags", "<think>a</think>X<think>b</think>Y", "XY"},
		{"unclosed tag drops tail", "before<think>unfinished", "before"},
		{"nested-like tag (outer only)", "<think>outer<think>inner</think>middle</think>after", "middle</think>after"},
		{"whitespace trimmed", "  <think>thought</think>  result  ", "result"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripThinkTags(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSummarizeDual_OllamaRoundTrip tests the full SummarizeDual pipeline
// with a well-formed LLM response using primary markers.
func TestSummarizeDual_OllamaRoundTrip(t *testing.T) {
	const wantShort = "Auth feature implemented. All tests passed. PR awaiting review."
	const wantLong = "The session focused on OAuth2 PKCE implementation. Integration tests ran clean."
	llmResp := "===SHORT===\n" + wantShort + "\n===LONG===\n" + wantLong

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": llmResp}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "test-ollama", Kind: inference.KindOllama, Model: "llama3"}); err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	short, long, err := svc.SummarizeDual(context.Background(), "session output", "")
	if err != nil {
		t.Fatalf("SummarizeDual error: %v", err)
	}
	if short != wantShort {
		t.Errorf("short: got %q, want %q", short, wantShort)
	}
	if long != wantLong {
		t.Errorf("long: got %q, want %q", long, wantLong)
	}
}

// TestSummarizeDual_UnstructuredResponse verifies that an LLM that ignores
// the format still populates current_status_long via sentence-split fallback.
func TestSummarizeDual_UnstructuredResponse(t *testing.T) {
	// LLM ignores the format and returns one big paragraph of 4 sentences.
	llmResp := "The agent refactored the auth module. Tests are passing. The PR is open. It needs one more approval before merge."

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": llmResp}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "test-ollama", Kind: inference.KindOllama, Model: "llama3"}); err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	short, long, err := svc.SummarizeDual(context.Background(), "session output", "")
	if err != nil {
		t.Fatalf("SummarizeDual error: %v", err)
	}
	// short should be the first 3 sentences, not the full text
	wantShort := "The agent refactored the auth module. Tests are passing. The PR is open."
	if short != wantShort {
		t.Errorf("short: got %q, want %q", short, wantShort)
	}
	// long should be the full text
	if long != llmResp {
		t.Errorf("long: got %q, want %q", long, llmResp)
	}
}

// TestSummarizeDual_PrevShortIncludedInPrompt verifies that prevShort is
// injected as "Previously reported" before the dual-summary prompt.
func TestSummarizeDual_PrevShortIncludedInPrompt(t *testing.T) {
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Prompt string `json:"prompt"` }
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		gotPrompt = body.Prompt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": "===SHORT===\nA. B. C.\n===LONG===\nFull narrative."}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "test-ollama", Kind: inference.KindOllama, Model: "llama3"}); err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	_, _, err := svc.SummarizeDual(context.Background(), "new output", "previous summary here")
	if err != nil {
		t.Fatalf("SummarizeDual error: %v", err)
	}
	if !strings.Contains(gotPrompt, "Previously reported") {
		t.Errorf("prompt should contain 'Previously reported', got: %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "previous summary here") {
		t.Errorf("prompt should contain prevShort text, got: %q", gotPrompt)
	}
}

// TestSummarize_CustomPrompt verifies that a custom prompt is used when set.
func TestSummarize_CustomPrompt(t *testing.T) {
	const customPrompt = "One sentence summary only."
	var gotPrompt string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		gotPrompt = body.Prompt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": "done"}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Session.Summarizer.Prompt = customPrompt
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	reg := inference.NewRegistry()
	err := reg.Add(&inference.LLM{Name: "test-ollama", Kind: inference.KindOllama, Model: "llama3"})
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	_, _ = svc.Summarize(context.Background(), "output")

	if !strings.Contains(gotPrompt, customPrompt) {
		t.Errorf("expected custom prompt %q in request, got %q", customPrompt, gotPrompt)
	}
	if strings.Contains(gotPrompt, DefaultSummarizerPrompt) {
		t.Errorf("should not use default prompt when custom prompt is set")
	}
}
