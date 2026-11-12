package config

import "testing"

func TestNormalizeStuckRunningSeconds(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, StuckRunningDefaultSeconds},
		{-5, StuckRunningDefaultSeconds},
		{1, 1},
		{1800, 1800},
	}
	for _, c := range cases {
		if got := NormalizeStuckRunningSeconds(c.in); got != c.want {
			t.Errorf("NormalizeStuckRunningSeconds(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if StuckRunningDefaultSeconds != 900 {
		t.Errorf("default = %d, want 900", StuckRunningDefaultSeconds)
	}
}
