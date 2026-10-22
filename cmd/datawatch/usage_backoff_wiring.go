// BL428 Phase 4 — daemon wiring for the per-backend usage back-off: rate-limit
// detection sets flags on the shared registry, a sweeper clears expired ones,
// and every automatic set/clear is recorded in the operator audit log.
// Operator-driven set/clear is audited by the REST surface.

package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// autonomousBackoff is the daemon-wide per-backend back-off registry
// (in-memory only; rebuilt from live sessions on start).
var autonomousBackoff = autonomouspkg.NewBackoff()

// autonomousBudget is the daemon-wide dollar-spend ledger behind the per-LLM
// budget windows. It is nil until wireCapacity constructs it with a
// persistence file under the daemon data directory; budgetBackpressure and
// the usage hook both treat nil as "no budgets".
var autonomousBudget *autonomouspkg.BudgetLedger

// backoffSweepInterval is how often expired back-off flags are cleared.
const backoffSweepInterval = 15 * time.Second

// backoffAuditFunc records one audit event.
type backoffAuditFunc func(action string, details map[string]any)

// backoffAuditSink is the audit writer for automatic back-off events. It is
// installed once the operator audit log is open (after wireUsageBackoff has
// already started), so it is an atomic pointer; nil means no log is open and
// events are dropped.
var backoffAuditSink atomic.Pointer[backoffAuditFunc]

// setBackoffAuditLog routes automatic back-off audit events to l (nil
// detaches).
func setBackoffAuditLog(l *auditpkg.Log) {
	if l == nil {
		backoffAuditSink.Store(nil)
		return
	}
	var fn backoffAuditFunc = func(action string, details map[string]any) {
		_ = l.Write(auditpkg.Entry{Timestamp: time.Now(), Actor: "autonomous", Action: action, Details: details})
	}
	backoffAuditSink.Store(&fn)
}

func emitBackoffAudit(action string, details map[string]any) {
	if fn := backoffAuditSink.Load(); fn != nil {
		(*fn)(action, details)
	}
}

// rateLimitBackoffHandler returns the session manager's rate-limit listener:
// it sets a flag for the backend until the parsed reset time, or for the
// configured maximum back-off when the message carried none. The mode and
// maximum are read at event time, so a config change applies without a
// restart; "off" skips the automatic path (operator-set flags are unaffected).
func rateLimitBackoffHandler(cfg *config.Config, bo *autonomouspkg.Backoff, now func() time.Time, audit backoffAuditFunc) func(backend string, resetAt time.Time, hasReset bool) {
	return func(backend string, resetAt time.Time, hasReset bool) {
		mode, maxWaitMin := config.EffectiveUsageBackoff(cfg.Autonomous.UsageBackoff, cfg.Autonomous.BackoffMaxWaitMinutes)
		if mode != config.UsageBackoffPauseBackend {
			return
		}
		var until time.Time
		var reason string
		if hasReset {
			until = resetAt
			reason = "rate-limit — reset " + resetAt.Format("15:04")
			fmt.Printf("[backoff] %s paused until %s (rate limit)\n", backend, until.Format(time.RFC3339))
		} else {
			until = now().Add(time.Duration(maxWaitMin) * time.Minute)
			reason = autonomouspkg.BackoffReasonNoResetTime
			fmt.Printf("[backoff] %s paused until %s (rate limit, no reset time — max back-off)\n", backend, until.Format(time.RFC3339))
		}
		bo.Set(backend, until, reason, autonomouspkg.BackoffSourceRateLimit)
		audit(auditpkg.ActionUsageBackoffSet, map[string]any{
			"backend": backend,
			"until":   until.UTC().Format(time.RFC3339),
			"source":  autonomouspkg.BackoffSourceRateLimit,
		})
	}
}

// sweepBackoff clears flags whose deadline has passed, logging and auditing
// each one.
func sweepBackoff(bo *autonomouspkg.Backoff, now time.Time, audit backoffAuditFunc) {
	for _, e := range bo.SweepExpired(now) {
		fmt.Printf("[backoff] %s back-off expired — cleared\n", e.Backend)
		audit(auditpkg.ActionUsageBackoffAutoCleared, map[string]any{
			"backend": e.Backend,
			"until":   e.Until.UTC().Format(time.RFC3339),
		})
	}
}

// wireUsageBackoff connects rate-limit detection to the back-off registry:
// it registers the session manager's rate-limit listener, rebuilds in-flight
// flags from sessions already rate-limited at daemon start (when
// autonomous.usage_backoff is "pause_backend"), and runs a sweeper tied to
// ctx that clears expired flags.
func wireUsageBackoff(ctx context.Context, cfg *config.Config, mgr *session.Manager, bo *autonomouspkg.Backoff) {
	mgr.AddRateLimitListener(rateLimitBackoffHandler(cfg, bo, time.Now, emitBackoffAudit))

	if mode, _ := config.EffectiveUsageBackoff(cfg.Autonomous.UsageBackoff, cfg.Autonomous.BackoffMaxWaitMinutes); mode == config.UsageBackoffPauseBackend {
		// In-flight rebuild: sessions that were rate-limited before this start.
		now := time.Now()
		latest := map[string]time.Time{}
		for _, s := range mgr.ListSessions() {
			if s.State != session.StateRateLimited || s.RateLimitResetAt == nil || !s.RateLimitResetAt.After(now) {
				continue
			}
			if s.RateLimitResetAt.After(latest[s.BackendFamily]) {
				latest[s.BackendFamily] = *s.RateLimitResetAt
			}
		}
		for backend, until := range latest {
			bo.Set(backend, until, "rate-limit — reset "+until.Format("15:04"), autonomouspkg.BackoffSourceRateLimit)
			fmt.Printf("[backoff] rebuilt: %s paused until %s\n", backend, until.Format(time.RFC3339))
		}
	}

	go func() {
		t := time.NewTicker(backoffSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweepBackoff(bo, time.Now(), emitBackoffAudit)
			}
		}
	}()
}
