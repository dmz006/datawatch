package session

import (
	"testing"
	"time"
)

func TestDeliveryWindowForPrompt(t *testing.T) {
	cases := []struct {
		name  string
		lines int
		want  time.Duration
	}{
		{"3000 lines", 3000, 4 * time.Minute},
		{"zero lines", 0, time.Minute},
		{"cap holds", 100000, 15 * time.Minute},
		{"partial thousand rounds up", 1001, 3 * time.Minute},
	}
	for _, c := range cases {
		if got := deliveryWindowForPrompt(60, 60, c.lines); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}
