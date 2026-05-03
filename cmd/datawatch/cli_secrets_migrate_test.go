package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestMaskValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ab", "**"},
		{"abcde", "*****"},
		{"abcdef", "ab**ef"},
		{"ghp_1234567890abcdef", "gh****************ef"},
		{"", ""},
	}
	for _, tc := range cases {
		got := maskValue(tc.in)
		if got != tc.want {
			t.Errorf("maskValue(%q): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestSensitiveFieldsNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range sensitiveFields {
		if f.secretName == "" {
			t.Errorf("sensitiveField has empty secretName: %+v", f)
		}
		if seen[f.secretName] {
			t.Errorf("duplicate secretName: %q", f.secretName)
		}
		seen[f.secretName] = true
	}
}

func TestSensitiveFieldsGetSetSymmetry(t *testing.T) {
	for _, f := range sensitiveFields {
		cfg := &config.Config{}
		f.set(cfg, "test-value-xyz")
		got := f.get(cfg)
		if got != "test-value-xyz" {
			t.Errorf("field %q: set/get asymmetry: got %q", f.secretName, got)
		}
	}
}
