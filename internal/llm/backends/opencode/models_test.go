package opencode

import "testing"

func TestModelLabel(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"opencode/big-pickle", "Big Pickle (Free)"},
		{"opencode/deepseek-v4-flash-free", "Deepseek v4 Flash (Free)"},
		{"opencode/nemotron-3-super-free", "Nemotron 3 Super (Free)"},
		{"anthropic/claude-sonnet-4-6", "Claude Sonnet 4 6"},
		{"openai/gpt-4o", "Gpt 4o"},
		{"google/gemini-2-5-pro", "Gemini 2 5 Pro"},
		{"ollama/llama3", "Llama3"},
		{"plain-model", "Plain Model"},
	}
	for _, tc := range tests {
		got := ModelLabel(tc.id)
		if got != tc.want {
			t.Errorf("ModelLabel(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestProviderLabel(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"opencode", "Free (Built-in)"},
		{"ollama", "Ollama (Local / Compute)"},
		{"anthropic", "Anthropic"},
		{"openai", "OpenAI"},
		{"google", "Google"},
		{"", "Other"},
		{"custom", "Custom"},
	}
	for _, tc := range tests {
		got := ProviderLabel(tc.provider)
		if got != tc.want {
			t.Errorf("ProviderLabel(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

func TestIsDigits(t *testing.T) {
	if !isDigits("123") {
		t.Error("isDigits(\"123\") = false, want true")
	}
	if isDigits("") {
		t.Error("isDigits(\"\") = true, want false")
	}
	if isDigits("12a") {
		t.Error("isDigits(\"12a\") = true, want false")
	}
}
