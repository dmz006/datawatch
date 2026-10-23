// Operator-readable wait reasons: one place that words each way an
// autonomous task can wait, and one classifier that maps a reason string back
// to its cause. Builders that show a time take an injectable clock so the
// text is testable; every existing reason prefix is preserved because other
// code and tests match on them.
package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// WaitCause names why a task is waiting.
type WaitCause string

// Wait causes. The empty WaitCause means the reason was not recognised.
const (
	WaitCausePoolFull       WaitCause = "pool_full"
	WaitCauseNodeBusy       WaitCause = "node_busy"
	WaitCauseBackoff        WaitCause = "backoff"
	WaitCauseBudget         WaitCause = "budget"
	WaitCauseGlobalCooldown WaitCause = "global_cooldown"
	WaitCauseAwaitExternal  WaitCause = "await_external"
	WaitCauseTreeLock       WaitCause = "tree_lock"
)

const (
	poolFullInfix        = " full ("
	nodeBusyPrefix       = "waiting for node "
	backoffPrefix        = "backend "
	backoffInfix         = " backed off"
	budgetPrefix         = "budget window full:"
	globalCooldownPrefix = "global cooldown"
	treeLockPrefix       = "tree lock:"
	poolPrefix           = "pool "
	backpressureInfix    = " backpressure: "
)

// ClassifyWaitReason maps a wait reason produced anywhere in the daemon back
// to its cause, or "" when it is not one of the known reasons. A pool
// backpressure reason ("pool <p> backpressure: <why>") is classified by the
// gate that produced <why>.
func ClassifyWaitReason(reason string) WaitCause {
	r := strings.TrimSpace(reason)
	switch {
	case r == "":
		return ""
	case IsAwaitExternalWaitReason(r):
		return WaitCauseAwaitExternal
	case strings.HasPrefix(r, treeLockPrefix):
		return WaitCauseTreeLock
	case strings.HasPrefix(r, budgetPrefix):
		return WaitCauseBudget
	case strings.HasPrefix(r, globalCooldownPrefix):
		return WaitCauseGlobalCooldown
	case strings.HasPrefix(r, nodeBusyPrefix):
		return WaitCauseNodeBusy
	case strings.HasPrefix(r, backoffPrefix) && strings.Contains(r, backoffInfix):
		return WaitCauseBackoff
	case strings.HasPrefix(r, poolPrefix):
		rest := r[len(poolPrefix):]
		if i := strings.Index(rest, backpressureInfix); i >= 0 {
			return ClassifyWaitReason(rest[i+len(backpressureInfix):])
		}
		if strings.Contains(rest, poolFullInfix) {
			return WaitCausePoolFull
		}
	}
	return ""
}

// PoolFullWaitText words a wait on a full capacity pool: the pool, its
// used/limit, and the holders. No holders reads as "other sessions".
func PoolFullWaitText(pool string, used, limit int, holders []string) string {
	return fmt.Sprintf("pool %s full (%d/%d, held by %s)", pool, used, limit, holderList(holders))
}

// NodeBusyWaitText words a wait on a compute node whose engines are all in
// use: the node, used/limit engines, and the holders.
func NodeBusyWaitText(node string, used, limit int, holders []string) string {
	return fmt.Sprintf("waiting for node %s: %d/%d engines in use, held by %s", node, used, limit, holderList(holders))
}

func holderList(holders []string) string {
	if len(holders) == 0 {
		return "other sessions"
	}
	return strings.Join(holders, ",")
}

// BackoffWaitText words a wait on a backed-off backend. With a known reset it
// shows the reset time and how long away it is; with an unknown one it says
// so plainly and shows when the next attempt is allowed. note is optional
// extra operator text.
func BackoffWaitText(now func() time.Time, backend string, until time.Time, resetKnown bool, note string) string {
	rel := relativeIn(now, until)
	var detail string
	if resetKnown {
		detail = "resets " + rel
	} else {
		detail = "reset time unknown; next attempt allowed then, " + rel
	}
	text := fmt.Sprintf("backend %s backed off until %s (%s)", backend, until.Format("15:04 MST"), detail)
	if n := strings.TrimSpace(note); n != "" {
		text += " — " + n
	}
	return text
}

// BudgetWaitText words a wait on a full budget window: the LLM, the spend
// against the ceiling, and when the window rolls over. peerUSD > 0 splits the
// spend into local and peer parts. A nil rollsOver means the window frees up
// only as peer spend ages out.
func BudgetWaitText(now func() time.Time, llm string, localUSD, peerUSD, ceilingUSD float64, rollsOver *time.Time) string {
	when := "when peer spend ages out"
	if rollsOver != nil {
		when = rollsOver.Format(time.RFC3339) + " (" + relativeIn(now, *rollsOver) + ")"
	}
	total := localUSD + peerUSD
	if peerUSD > 0 {
		return fmt.Sprintf("budget window full: %s spent $%.2f (local $%.2f, peers $%.2f) of $%.2f ceiling; window rolls over at %s",
			llm, total, localUSD, peerUSD, ceilingUSD, when)
	}
	return fmt.Sprintf("budget window full: %s spent $%.2f of $%.2f ceiling; window rolls over at %s",
		llm, total, ceilingUSD, when)
}

// GlobalCooldownWaitText words a wait on the daemon-wide cooldown; a zero
// until means the end is not known.
func GlobalCooldownWaitText(now func() time.Time, until time.Time) string {
	if until.IsZero() {
		return "global cooldown active"
	}
	return fmt.Sprintf("global cooldown until %s (%s)", until.Format("15:04 MST"), relativeIn(now, until))
}

// AwaitExternalWaitText words a wait on an external condition. An empty
// condition yields the bare sub-reason; a long one is truncated.
func AwaitExternalWaitText(condition string) string {
	cond := strings.TrimSpace(condition)
	if cond == "" {
		return AwaitExternalWaitReason
	}
	if len(cond) > maxAwaitExternalConditionChars {
		cond = cond[:maxAwaitExternalConditionChars] + "…"
	}
	return AwaitExternalWaitReasonPrefix + ": " + cond
}

// TreeLockWaitText words a wait on a working-tree lock, naming the holder.
// An empty holderTaskID means the lock record could not be read; acquiredAt
// is the holder's acquire time in unix seconds (0 = unknown).
func TreeLockWaitText(key, holderTaskID, holderPRDID string, acquiredAt int64) string {
	if holderTaskID == "" {
		return fmt.Sprintf("tree lock: %s held by unknown holder (record unreadable)", key)
	}
	since := "unknown time"
	if acquiredAt > 0 {
		since = time.Unix(acquiredAt, 0).UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("tree lock: %s held by task %s (PRD %s) since %s", key, holderTaskID, holderPRDID, since)
}

// relativeIn renders how far until is from now as "in 42m" ("in 1h05m",
// "now" once reached). Sub-minute remainders round up to a whole minute.
func relativeIn(now func() time.Time, until time.Time) string {
	d := until.Sub(now())
	if d <= 0 {
		return "now"
	}
	mins := int((d + time.Minute - 1) / time.Minute)
	if mins < 60 {
		return fmt.Sprintf("in %dm", mins)
	}
	if mins%60 == 0 {
		return fmt.Sprintf("in %dh", mins/60)
	}
	return fmt.Sprintf("in %dh%02dm", mins/60, mins%60)
}
