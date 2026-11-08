package server

import (
	"strings"
	"testing"
)

func TestValidateDiscussionID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"single letter", "a", false},
		{"128 characters", strings.Repeat("a", 128), false},
		{"mixed punctuation", "disc-abc_1.2", false},
		{"all digits", "1234567890", false},
		{"empty", "", true},
		{"single dot", ".", true},
		{"double dot", "..", true},
		{"forward slash", "disc/abc", true},
		{"backslash", `disc\abc`, true},
		{"NUL byte", "disc\x00abc", true},
		{"129 characters", strings.Repeat("a", 129), true},
		{"space", "disc abc", true},
		{"non-ASCII letter", "discé", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDiscussionID(tc.id)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("validateDiscussionID(%q) = %v, want nil", tc.id, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateDiscussionID(%q) = nil, want error", tc.id)
			}
			if err.Error() != "invalid discussion id" {
				t.Fatalf("error text = %q, want %q", err.Error(), "invalid discussion id")
			}
		})
	}
}
