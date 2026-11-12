package audit

import (
	"fmt"
	"strconv"
	"time"
)

// QueryParamNames is the one filter set every audit query surface exposes:
// REST query parameters, MCP audit_query arguments, CLI flags (with '_'
// written as '-') and comm-channel key=value words.
var QueryParamNames = []string{"actor", "action", "subject", "session_id", "since", "until", "limit"}

// DefaultQueryLimit is the entry cap applied when a query names no limit.
const DefaultQueryLimit = 100

// CapacityActions lists the audit actions the autonomous capacity gate
// emits, in the order query descriptions show them.
var CapacityActions = []string{
	ActionCapacityWait,
	ActionCapacityAdmitted,
	ActionUsageBackoffSet,
	ActionUsageBackoffCleared,
	ActionUsageBackoffAutoCleared,
	ActionUsageBackoffPatternsRechecked,
	ActionBudgetCeilingWait,
	ActionBudgetWindowResumed,
	ActionBudgetWindowRollover,
}

// ParseQuery builds a QueryFilter from a name→value lookup over
// QueryParamNames, so every surface reads and validates its input the same
// way. An empty value means the filter was not given. since and until must
// be RFC 3339; a bad value is an error rather than a silently dropped
// bound. A limit that is not a positive integer keeps DefaultQueryLimit.
func ParseQuery(get func(name string) string) (QueryFilter, error) {
	f := QueryFilter{
		Actor:     get("actor"),
		Action:    get("action"),
		Subject:   get("subject"),
		SessionID: get("session_id"),
		Limit:     DefaultQueryLimit,
	}
	if v := get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Limit = n
		}
	}
	for _, b := range []struct {
		name string
		dst  *time.Time
	}{{"since", &f.Since}, {"until", &f.Until}} {
		v := get(b.name)
		if v == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return QueryFilter{}, fmt.Errorf("invalid %s %q: want RFC3339 (e.g. 2026-10-22T15:04:05Z)", b.name, v)
		}
		*b.dst = t
	}
	return f, nil
}

// QueryActionHelp is the filter-description text shared by the surfaces
// that document the action filter.
func QueryActionHelp() string {
	s := "Filter by action: exact match, or a prefix match when it ends in '*' " +
		"(e.g. autonomous_budget_* or autonomous_*). Capacity actions:"
	for _, a := range CapacityActions {
		s += " " + a
	}
	return s
}
