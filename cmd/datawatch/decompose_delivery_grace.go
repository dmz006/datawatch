package main

import (
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// decomposeDeliveryGrace is how long after a one-shot session's spawn the
// waiting_input stall check stays exempt: while the daemon is still
// delivering the task (its TaskDelivery budget), the Claude main prompt is
// not necessarily ready yet and the session legitimately sits in
// waiting_input. It mirrors cfg.Session.TaskDeliveryBudgetTotal() and is
// nil-safe: a nil config falls back to the config defaults
// (3 * 60 s window + 15 s backoff = 195 s >= 180 s).
func decomposeDeliveryGrace(cfg *config.Config) time.Duration {
	if cfg == nil {
		def := config.SessionConfig{}
		return def.TaskDeliveryBudgetTotal()
	}
	return cfg.Session.TaskDeliveryBudgetTotal()
}

// decomposeWaitingInputTick is waitingInputTick with a delivery-grace
// prefix: while sinceSpawn < grace it reports (0, false) — never stalled,
// and the debounce counter is held at 0, since waiting_input during the
// task-delivery window is expected, not a stall. Once sinceSpawn >= grace
// it delegates to waitingInputTick unchanged (running ticks still reset
// the counter, three consecutive waiting_input ticks still stall).
func decomposeWaitingInputTick(ticks int, state session.State, sinceSpawn, grace time.Duration) (int, bool) {
	if sinceSpawn < grace {
		return 0, false
	}
	return waitingInputTick(ticks, state)
}
