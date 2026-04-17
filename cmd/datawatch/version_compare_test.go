package main

import "testing"

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		// B24: this is the case the user reported. Ensure a downgrade
		// (current ahead of release) never reports "update available".
		{"2.4.3", "2.4.4", false},
		{"v2.4.3", "v2.4.4", false},

		// Equal versions
		{"2.4.4", "2.4.4", false},

		// Real upgrades
		{"2.4.5", "2.4.4", true},
		{"2.5.0", "2.4.9", true},
		{"3.0.0", "2.99.99", true},

		// Different lengths
		{"2.4.4.1", "2.4.4", true},
		{"2.4.4", "2.4.4.1", false},
		{"2.4", "2.4.0", false},

		// Pre-release suffix is stripped before compare
		{"2.4.4-rc1", "2.4.4", false},
		{"2.4.5-rc1", "2.4.4", true},

		// Garbage input must not advertise an update
		{"", "2.4.4", false},
		{"latest", "2.4.4", false},
		{"2.4.4", "garbage", false},
	}
	for _, c := range cases {
		got := isNewerVersion(c.latest, c.current)
		if got != c.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
