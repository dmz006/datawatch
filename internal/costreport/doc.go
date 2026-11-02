// Package costreport renders the cost report: one view over the spend the
// daemon already records, grouped by automaton, automation, backend or week
// over a since/until window.
//
// Tokens come first: every row always carries sessions, tokens_in and
// tokens_out. A USD estimate appears on a row only when a rate is
// configured for every backend in that row — the rates come from the
// per-backend table in internal/session (cost.go); a row whose backends
// have no rate simply has no usd_estimate and rate_configured false.
//
// Spend arrives through the Source interface, so a later per-call or
// long-history store can replace the current data source without changing
// the report or its surfaces: the report, its contract and its tests only
// know Source.
//
// Nothing here is hard-coded. Every knob — default group_by, the default
// window in days, USD visibility, the week start for week grouping and
// peer-spend inclusion — comes from the cost_report section of Config in
// internal/config.
package costreport
