package main

import (
	"strings"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// backoffReserveGate builds the review-reserve gate whose effective slot
// count and one-hour token cap read the live config at decision time, so a
// config change needs no daemon restart. The gate's own state (per-backend
// rolling token stamps, the bounded verdict queue) is in memory only: it may
// be decided on the capacity ledger's lock path.
func backoffReserveGate(cfg *config.Config) *autonomouspkg.BackoffReserve {
	return autonomouspkg.NewBackoffReserve(
		time.Now,
		func() int { return config.ReserveForReviewSlots(cfg.Autonomous.BackoffReserveForReview) },
		func() int64 { return config.ReserveTokensPerHour(cfg.Autonomous.BackoffReserveTokensPerHour) },
	)
}

// backoffReserveBackpressure is the role-aware back-off gate installed as the
// ledger's RoleBackpressure hook. Like the pool-only back-pressure gates it
// touches only "llm:<backend>" pools — host, node: and external pools are
// never blocked — and it is consulted for autonomous requests only (the
// ledger skips Interactive requests, so operator session starts are never
// held back).
//
// While the backend is backed off a worker is refused exactly as it was
// before ("backoff: <why>"), but a review role — the guardrail judges and
// verifiers (capacity.RoleVerifier) or decompose/planning sessions
// (capacity.RolePlanner) — may still be admitted through the backend's
// review reserve, decided by the gate: the pool's live leases held by
// review roles come in heldByRole, and the gate bounds the flow further by
// a rolling one-hour token budget (see autonomouspkg.BackoffReserve). A
// refused verdict reports the ledger's "back-off, review reserve in use"
// sub-reason, classified as the back-off wait cause.
//
// It runs under the capacity ledger lock (rule on capacity.Options):
// Backoff.Blocked reads in-memory entries only, the gate's functions read
// the config values through plain field reads and its token stamps under
// its own mutex, and the verdict is only queued — the audit emission
// happens on the sync tick, outside any lock.
func backoffReserveBackpressure(bo *autonomouspkg.Backoff, gate *autonomouspkg.BackoffReserve) func(pool string, role capacity.Role, heldByRole map[capacity.Role]int) (bool, string) {
	return func(pool string, role capacity.Role, heldByRole map[capacity.Role]int) (bool, string) {
		backend, ok := strings.CutPrefix(pool, "llm:")
		if !ok || bo == nil || gate == nil {
			return false, ""
		}
		blocked, why := bo.Blocked(backend)
		if !blocked {
			return false, ""
		}
		if !autonomouspkg.ReserveRole(role) {
			return true, "backoff: " + why
		}
		held := heldByRole[capacity.RoleVerifier] + heldByRole[capacity.RolePlanner]
		admit, reason := gate.Admit(backend, role, held)
		if admit {
			return false, ""
		}
		return true, reason
	}
}

// drainBackoffReserveAudit emits the review-reserve verdicts the gate queued
// since the last call: one operator audit entry per admission or refusal,
// under the autonomous actor, with the slot and token figures the gate
// recorded at decision time. The syncPools tick calls it, so it is the one
// place that leaves the ledger's lock path without emitting audit writes.
func drainBackoffReserveAudit(gate *autonomouspkg.BackoffReserve) {
	if gate == nil {
		return
	}
	for _, ev := range gate.DrainEvents() {
		action := auditpkg.ActionUsageBackoffReserveRefused
		if ev.Admitted {
			action = auditpkg.ActionUsageBackoffReserveAdmitted
		}
		emitBackoffAudit(action, map[string]any{
			"backend":     ev.Backend,
			"role":        ev.Role,
			"slots":       ev.Slots,
			"used_slots":  ev.UsedSlots,
			"tokens_used": ev.TokensUsed,
			"token_cap":   ev.TokenCap,
			"at":          ev.At.Format(time.RFC3339),
		})
	}
}
