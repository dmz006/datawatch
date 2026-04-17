package router

import "testing"

func TestIsNewerSemver(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		// B24: downgrade must not advertise an update
		{"2.4.3", "2.4.4", false},
		{"v2.4.3", "v2.4.4", false},
		{"2.4.4", "2.4.4", false},
		{"2.4.5", "2.4.4", true},
		{"2.5.0", "2.4.9", true},
		{"", "2.4.4", false},
		{"garbage", "2.4.4", false},
	}
	for _, c := range cases {
		if got := isNewerSemver(c.latest, c.current); got != c.want {
			t.Errorf("isNewerSemver(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
