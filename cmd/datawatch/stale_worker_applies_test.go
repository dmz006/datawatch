package main

import "testing"

func TestStaleWorkerAppliesTo(t *testing.T) {
	cases := []struct {
		family string
		want   bool
	}{
		{"pi-prompt", false},
		{"claude-code", true},
		{"goose-prompt", true},
		{"", true},
	}
	for _, tc := range cases {
		t.Run(tc.family, func(t *testing.T) {
			if got := staleWorkerAppliesTo(tc.family); got != tc.want {
				t.Errorf("staleWorkerAppliesTo(%q) = %v, want %v", tc.family, got, tc.want)
			}
		})
	}
}
