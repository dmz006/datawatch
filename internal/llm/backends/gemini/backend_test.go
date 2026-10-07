// B98 — tests for the gemini backend's Go-mediated usage reporting.

package gemini

import (
	"encoding/json"
	"testing"
)

func TestExtractUsage_SnakeCase(t *testing.T) {
	stats := json.RawMessage(`{"input_tokens": 15, "output_tokens": 182, "total_tokens": 197}`)
	in, out := extractUsage(stats)
	if in != 15 || out != 182 {
		t.Fatalf("got in=%d out=%d, want 15/182", in, out)
	}
}

func TestExtractUsage_CamelCase(t *testing.T) {
	stats := json.RawMessage(`{"inputTokens": 15, "outputTokens": 182, "totalTokens": 197}`)
	in, out := extractUsage(stats)
	if in != 15 || out != 182 {
		t.Fatalf("got in=%d out=%d, want 15/182", in, out)
	}
}

func TestExtractUsage_GoogleAPINaming(t *testing.T) {
	stats := json.RawMessage(`{"promptTokenCount": 15, "candidatesTokenCount": 182, "totalTokenCount": 197}`)
	in, out := extractUsage(stats)
	if in != 15 || out != 182 {
		t.Fatalf("got in=%d out=%d, want 15/182", in, out)
	}
}

func TestExtractUsage_EmptyOrNestedIsZero(t *testing.T) {
	if in, out := extractUsage(nil); in != 0 || out != 0 {
		t.Errorf("nil stats: got %d/%d, want 0/0", in, out)
	}
	// A nested "models" breakdown must NOT be summed -- see the doc
	// comment on extractUsage for why a tree-wide scan would double-
	// count against a flat aggregate in the same object.
	stats := json.RawMessage(`{"models": {"gemini-pro": {"tokens": {"prompt": 15, "candidates": 182}}}}`)
	in, out := extractUsage(stats)
	if in != 0 || out != 0 {
		t.Errorf("nested-only stats: got %d/%d, want 0/0 (must not guess into nested shape)", in, out)
	}
}

func TestJSONResult_ParsesRealCapturedErrorShape(t *testing.T) {
	// Captured verbatim from a real unauthenticated `gemini -p "say hi"
	// --output-format json` run (stderr, not stdout -- see backend.go's
	// Launch comment on why both are tried).
	raw := `{
  "session_id": "6755122b-29d8-4099-9583-022058e29871",
  "error": {
    "type": "Error",
    "message": "Please set an Auth method in your /home/dmz/.gemini/settings.json or specify one of the following environment variables before running: GEMINI_API_KEY, GOOGLE_GENAI_USE_VERTEXAI, GOOGLE_GENAI_USE_GCA",
    "code": 41
  }
}`
	var result jsonResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.SessionID != "6755122b-29d8-4099-9583-022058e29871" {
		t.Errorf("got session_id %q", result.SessionID)
	}
	if result.Error == nil || result.Error.Code != 41 || result.Error.Type != "Error" {
		t.Fatalf("got error %+v", result.Error)
	}
	if result.Response != "" {
		t.Errorf("response should be empty on error, got %q", result.Response)
	}
}
