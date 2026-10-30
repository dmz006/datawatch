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
	// WaitCauseAwaitDecision is a task parked on an open blocking worker
	// question until the operator decides.
	WaitCauseAwaitDecision WaitCause = "await_decision"
	WaitCauseTreeLock      WaitCause = "tree_lock"
	// WaitCauseRateCeiling is a wait on a pool's rolling-window rate ceiling
	// (requests or tokens per window).
	WaitCauseRateCeiling WaitCause = "rate_ceiling"
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
	rateCeilingInfix     = " rate ceiling: "

	// AwaitDecisionWaitReason is the bare sub-reason of a task parked on an
	// open blocking worker question; the full reason is
	// "await_decision: <question id>".
	AwaitDecisionWaitReason = "await_decision"
	// AwaitDecisionWaitReasonPrefix is the reason prefix.
	AwaitDecisionWaitReasonPrefix = "await_decision"
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
	case IsAwaitDecisionWaitReason(r):
		return WaitCauseAwaitDecision
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
		if strings.Contains(rest, rateCeilingInfix) {
			return WaitCauseRateCeiling
		}
		if strings.Contains(rest, poolFullInfix) {
			return WaitCausePoolFull
		}
	}
	return ""
}

// RateCeilingWaitText words a wait on a pool's rate ceiling: the pool, the
// dimension ("requests" or "tokens"), the usage against the ceiling, and when
// the window frees up. A zero resetsAt means the reset is not known, and the
// "resets in" part is left out. The "pool <p> rate ceiling: " prefix matches
// the ledger's own reason so ClassifyWaitReason recognises both.
func RateCeilingWaitText(now func() time.Time, pool, dimension string, used, ceiling int64, resetsAt time.Time) string {
	text := fmt.Sprintf("pool %s rate ceiling: %s %d/%d used", pool, dimension, used, ceiling)
	if !resetsAt.IsZero() {
		text += ", resets " + relativeIn(now, resetsAt)
	}
	return text
}

// parseRateCeilingWait splits a rate-ceiling reason, bare or wrapped by the
// backpressure path, into the pool, dimension, used and ceiling counts, and
// the reset time when the reason names one (zero otherwise). ok is false when
// the shape does not match. It reads both the ledger's wording ("requests 3/3
// in 1m0s, resets at <RFC3339>") and RateCeilingWaitText's.
func parseRateCeilingWait(reason string) (pool, dimension string, used, ceiling int64, resetsAt time.Time, ok bool) {
	i := strings.Index(reason, rateCeilingInfix)
	if i < 0 {
		return
	}
	j := strings.LastIndex(reason[:i], poolPrefix)
	if j < 0 {
		return
	}
	pool = reason[j+len(poolPrefix) : i]
	rest := reason[i+len(rateCeilingInfix):]
	var dim string
	var u, c int64
	if n, err := fmt.Sscanf(rest, "%s %d/%d", &dim, &u, &c); n != 3 || err != nil {
		return "", "", 0, 0, time.Time{}, false
	}
	const resetsAtMarker = ", resets at "
	if k := strings.Index(rest, resetsAtMarker); k >= 0 {
		ts := strings.TrimSpace(rest[k+len(resetsAtMarker):])
		if sp := strings.IndexAny(ts, " ,"); sp >= 0 {
			ts = ts[:sp]
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			resetsAt = t
		}
	}
	return pool, dim, u, c, resetsAt, true
}

// rateCeilingResetsAt returns the reset time a rate-ceiling reason names, or
// nil when it names none.
func rateCeilingResetsAt(reason string) *time.Time {
	if _, _, _, _, at, ok := parseRateCeilingWait(reason); ok && !at.IsZero() {
		return &at
	}
	return nil
}

// RateCeilingWaitFacts returns the pool and reset time a rate-ceiling reason
// names (zero time when none), for audit and view code. ok is false when the
// reason is not a rate-ceiling one.
func RateCeilingWaitFacts(reason string) (pool string, resetsAt time.Time, ok bool) {
	pool, _, _, _, resetsAt, ok = parseRateCeilingWait(reason)
	return
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

// ReadableWaitText turns a wait reason plus the holder ids it names into an
// operator-readable sentence: the same fact worded the same way, but with
// the holder names ("<PRD title> / <task title> (<short id>)", "Verifying:
// <task title> (<short id>)") instead of raw holder ids. holderName maps a
// raw holder id to its readable form; a nil or empty result falls back to
// the raw id (the shared holder-title resolver, capacity_titles.go, already
// falls back to "(untitled <short id>)" when a title is missing). An empty
// holder list reads "other sessions", same as the raw text. Only reasons
// that name holders — a full pool and a busy node — are reworded; every
// other reason is returned unchanged. The original reason text and its
// prefix stay intact; this is a companion sentence for operator-facing
// views, not a replacement.
func ReadableWaitText(reason string, holders []string, holderName func(holderID string) string) string {
	if holderName == nil {
		holderName = func(holderID string) string { return holderID }
	}
	switch ClassifyWaitReason(reason) {
	case WaitCausePoolFull:
		pool, used, limit, ok := parsePoolWait(reason)
		if !ok {
			return reason
		}
		return fmt.Sprintf("pool %s full (%d/%d), held by %s", pool, used, limit, readableHolderList(holders, holderName))
	case WaitCauseNodeBusy:
		node, used, limit, ok := parseNodeWait(reason)
		if !ok {
			return reason
		}
		return fmt.Sprintf("waiting for node %s: %d/%d engines in use, held by %s", node, used, limit, readableHolderList(holders, holderName))
	case WaitCauseRateCeiling:
		pool, dim, used, ceiling, at, ok := parseRateCeilingWait(reason)
		if !ok {
			return reason
		}
		return RateCeilingWaitText(time.Now, pool, dim, used, ceiling, at)
	}
	return reason
}

// WaitReasonHolders returns the raw holder ids a reason names (a full pool
// or a busy node), in listing order. Nil when the reason names no holders at
// all — the other reasons never do — so the caller can skip re-wording.
func WaitReasonHolders(reason string) []string {
	switch ClassifyWaitReason(reason) {
	case WaitCausePoolFull, WaitCauseNodeBusy:
	default:
		return nil
	}
	const marker = "held by "
	i := strings.Index(reason, marker)
	if i < 0 {
		return nil
	}
	rest := strings.TrimSpace(reason[i+len(marker):])
	if ClassifyWaitReason(reason) == WaitCausePoolFull {
		// The holder list is the tail of the last "(u/l, held by …)" group.
		if j := strings.LastIndex(rest, ")"); j >= 0 {
			rest = rest[:j]
		}
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || rest == holderList(nil) {
		return nil
	}
	return strings.Split(rest, ",")
}

// parsePoolWait splits a pool-full reason ("pool <p> full (<u>/<l> …") into
// the pool name, used and limit. ok is false when the shape does not match.
func parsePoolWait(reason string) (pool string, used, limit int, ok bool) {
	rest := strings.TrimSpace(reason)
	if !strings.HasPrefix(rest, poolPrefix) {
		return
	}
	rest = rest[len(poolPrefix):]
	i := strings.Index(rest, poolFullInfix)
	if i < 0 {
		return
	}
	pool = rest[:i]
	nums := rest[i+len(poolFullInfix):]
	if j := strings.Index(nums, ")"); j >= 0 {
		nums = nums[:j]
	}
	if n, err := fmt.Sscanf(nums, "%d/%d", &used, &limit); n != 2 || err != nil {
		return "", 0, 0, false
	}
	return pool, used, limit, true
}

// parseNodeWait splits a node-busy reason ("waiting for node <n>: <u>/<l>
// engines …") into the node name, used and limit. ok is false when the shape
// does not match.
func parseNodeWait(reason string) (node string, used, limit int, ok bool) {
	rest := strings.TrimSpace(reason)
	if !strings.HasPrefix(rest, nodeBusyPrefix) {
		return
	}
	rest = rest[len(nodeBusyPrefix):]
	name, rest, found := strings.Cut(rest, ": ")
	if !found {
		return
	}
	node = name
	if j := strings.Index(rest, " engines"); j >= 0 {
		rest = rest[:j]
	}
	if n, err := fmt.Sscanf(strings.TrimSpace(rest), "%d/%d", &used, &limit); n != 2 || err != nil {
		return "", 0, 0, false
	}
	return node, used, limit, true
}

// readableHolderList renders a wait reason's holder list readably: one name
// alone, two as "A and B", three or more as "A, B and C". An empty list
// reads "other sessions", like the raw text does. Blank names fall back to
// the raw holder id.
func readableHolderList(holders []string, holderName func(holderID string) string) string {
	if len(holders) == 0 {
		return holderList(nil)
	}
	named := make([]string, len(holders))
	for i, id := range holders {
		named[i] = strings.TrimSpace(id)
		if s := strings.TrimSpace(holderName(id)); s != "" {
			named[i] = s
		}
	}
	if len(named) < 3 {
		return strings.Join(named, " and ")
	}
	return strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1]
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

// AwaitDecisionWaitText words a wait on an open blocking worker question:
// "await_decision: <question id>". An empty id yields the bare sub-reason.
func AwaitDecisionWaitText(questionID string) string {
	id := strings.TrimSpace(questionID)
	if id == "" {
		return AwaitDecisionWaitReason
	}
	return AwaitDecisionWaitReasonPrefix + ": " + id
}

// IsAwaitDecisionWaitReason reports whether a Task.WaitReason value is the
// await-decision sub-reason of waiting_capacity.
func IsAwaitDecisionWaitReason(reason string) bool {
	r := strings.TrimSpace(reason)
	if r == "" {
		return false
	}
	if r == AwaitDecisionWaitReasonPrefix {
		return true
	}
	return strings.HasPrefix(r, AwaitDecisionWaitReasonPrefix+":") ||
		strings.HasPrefix(r, AwaitDecisionWaitReasonPrefix+" — ")
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
