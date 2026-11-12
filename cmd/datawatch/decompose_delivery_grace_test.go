package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestDecomposeDeliveryGrace(t *testing.T) {
	const grace = 5 * time.Minute // comfortably inside a realistic delivery budget

	t.Run("waiting_input inside grace never stalls", func(t *testing.T) {
		ticks := 0
		var stalled bool
		spawned := time.Now()
		for i := 0; i < waitingInputStallThreshold*10; i++ {
			since := time.Since(spawned) // always well under grace in this test
			ticks, stalled = decomposeWaitingInputTick(ticks, session.StateWaitingInput, since, grace)
			if stalled {
				t.Fatalf("tick %d: stalled=true inside the delivery grace", i)
			}
		}
		if ticks != 0 {
			t.Fatalf("ticks = %d after waiting_input inside grace, want 0 (grace suppresses and holds the counter)", ticks)
		}
	})

	t.Run("after grace, three consecutive waiting_input ticks stall", func(t *testing.T) {
		spawned := time.Now().Add(-grace - time.Second) // spawn time in the past, past the grace
		ticks := 0
		var stalled bool
		for i := 0; i < waitingInputStallThreshold-1; i++ {
			ticks, stalled = decomposeWaitingInputTick(ticks, session.StateWaitingInput, time.Since(spawned), grace)
			if stalled {
				t.Fatalf("stalled=true after only %d tick(s) past the grace", i+1)
			}
		}
		ticks, stalled = decomposeWaitingInputTick(ticks, session.StateWaitingInput, time.Since(spawned), grace)
		if !stalled {
			t.Fatalf("stalled=false at %d consecutive waiting_input ticks past the grace", waitingInputStallThreshold)
		}
		if ticks != waitingInputStallThreshold {
			t.Fatalf("ticks = %d past the grace, want %d", ticks, waitingInputStallThreshold)
		}
	})

	t.Run("running tick after grace resets the counter", func(t *testing.T) {
		spawned := time.Now().Add(-grace - time.Second) // spawn time in the past, past the grace
		ticks, stalled := decomposeWaitingInputTick(0, session.StateWaitingInput, time.Since(spawned), grace)
		if stalled {
			t.Fatal("stalled after one tick, want the debounce to hold")
		}
		ticks, stalled = decomposeWaitingInputTick(ticks, session.StateWaitingInput, time.Since(spawned), grace)
		if stalled {
			t.Fatal("stalled after two ticks, want the debounce to hold")
		}
		if ticks != 2 {
			t.Fatalf("ticks = %d before the reset, want 2", ticks)
		}
		ticks, stalled = decomposeWaitingInputTick(ticks, session.StateRunning, time.Since(spawned), grace)
		if stalled || ticks != 0 {
			t.Fatalf("running tick: got (%d, %v), want (0, false)", ticks, stalled)
		}
	})

	t.Run("grace follows the config keys", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Session.TaskDeliveryWindowSeconds = 90
		cfg.Session.TaskDeliveryAttempts = 4
		got := decomposeDeliveryGrace(cfg)
		want := cfg.Session.TaskDeliveryBudgetTotal()
		if got != want {
			t.Fatalf("grace = %v with window=90 attempts=4, want %v", got, want)
		}
		if got <= decomposeDeliveryGrace(nil) {
			t.Fatalf("window=90/attempts=4 grace %v is not larger than the default grace %v", got, decomposeDeliveryGrace(nil))
		}
	})

	t.Run("default grace is at least 180s", func(t *testing.T) {
		if got := decomposeDeliveryGrace(nil); got < 180*time.Second {
			t.Fatalf("default grace = %v, want at least 180s", got)
		}
	})

	t.Run("nil config gives the default grace", func(t *testing.T) {
		wantCfg := config.SessionConfig{}
		want := wantCfg.TaskDeliveryBudgetTotal()
		if got := decomposeDeliveryGrace(nil); got != want {
			t.Fatalf("nil-config grace = %v, want defaults %v", got, want)
		}
	})
}
