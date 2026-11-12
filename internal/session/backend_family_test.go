package session

import "testing"

func TestIsPromptBackend(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"pi-prompt", true},
		{"goose-prompt", true},
		{"opencode-prompt", true},
		{"  Pi-Prompt  ", true}, // mixed case + surrounding whitespace
		{"claude-code", false},
		{"pi", false},
		{"opencode", false},
		{"opencode-acp", false},
		{"goose", false},
		{"shell", false},
		{"", false},
		{"prompt", false}, // contains the word prompt, does not end in "-prompt"
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPromptBackend(tc.name); got != tc.want {
				t.Errorf("IsPromptBackend(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
