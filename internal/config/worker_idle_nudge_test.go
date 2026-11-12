package config

import "testing"

func TestNormalizeWorkerIdleNudge(t *testing.T) {
	for _, v := range []int{-5, 0} {
		if got := NormalizeWorkerIdleNudgeSeconds(v); got != 180 {
			t.Fatalf("nudge(%d) = %d, want 180", v, got)
		}
		if got := NormalizeWorkerIdleNudgeStallSeconds(v); got != 600 {
			t.Fatalf("stall(%d) = %d, want 600", v, got)
		}
	}
	if got := NormalizeWorkerIdleNudgeSeconds(30); got != 30 {
		t.Fatalf("got %d, want 30", got)
	}
	if got := NormalizeWorkerIdleNudgeStallSeconds(45); got != 45 {
		t.Fatalf("got %d, want 45", got)
	}
}
