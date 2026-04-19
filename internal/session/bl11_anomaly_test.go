// BL11 — anomaly detector tests.

package session

import (
	"strings"
	"testing"
	"time"
)

func TestBL11_DetectStuckLoop_ThresholdReached(t *testing.T) {
	out := strings.Repeat("waiting...\n", 50)
	a := DetectStuckLoop("aa", out, 50)
	if a == nil {
		t.Fatal("expected stuck loop detection")
	}
	if a.Kind != AnomalyStuckLoop {
		t.Errorf("kind=%s want stuck_loop", a.Kind)
	}
}

func TestBL11_DetectStuckLoop_BelowThreshold(t *testing.T) {
	out := strings.Repeat("waiting...\n", 5)
	if DetectStuckLoop("aa", out, 50) != nil {
		t.Error("should not detect with only 5 repeats")
	}
}

func TestBL11_DetectStuckLoop_VariedTail(t *testing.T) {
	// Tail of last 5 lines is c c c b c — varied, should not flag.
	out := strings.Repeat("a\n", 10) + "c\nc\nc\nb\nc\n"
	if DetectStuckLoop("aa", out, 5) != nil {
		t.Error("varied tail should not flag")
	}
}

func TestBL11_DetectStuckLoop_DisabledByThresholdZero(t *testing.T) {
	out := strings.Repeat("x\n", 100)
	if DetectStuckLoop("aa", out, 0) != nil {
		t.Error("threshold=0 should disable")
	}
}

func TestBL11_DetectLongInputWait(t *testing.T) {
	sess := &Session{
		FullID: "h-aa", State: StateWaitingInput,
		UpdatedAt: time.Now().Add(-30 * time.Second),
	}
	a := DetectLongInputWait(sess, 10)
	if a == nil || a.Kind != AnomalyLongInputWait {
		t.Errorf("expected long input wait, got %+v", a)
	}
}

func TestBL11_DetectLongInputWait_NotWaiting(t *testing.T) {
	sess := &Session{
		FullID: "h-aa", State: StateRunning,
		UpdatedAt: time.Now().Add(-30 * time.Second),
	}
	if DetectLongInputWait(sess, 10) != nil {
		t.Error("running session should not flag input wait")
	}
}

func TestBL11_DetectDurationOutlier(t *testing.T) {
	sess := &Session{
		FullID:    "h-aa", State: StateRunning,
		CreatedAt: time.Now().Add(-30 * time.Minute),
	}
	a := DetectDurationOutlier(sess, 10*time.Minute, 2.0)
	if a == nil {
		t.Fatal("expected duration outlier")
	}
}

func TestBL11_DetectDurationOutlier_WithinThreshold(t *testing.T) {
	sess := &Session{
		FullID:    "h-aa", State: StateRunning,
		CreatedAt: time.Now().Add(-5 * time.Minute),
	}
	if DetectDurationOutlier(sess, 10*time.Minute, 2.0) != nil {
		t.Error("within threshold should not flag")
	}
}
