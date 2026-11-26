// Package acceptance keeps the "done when" list of a request: what was asked,
// how each statement is checked, and whether it is met, not met or cannot tell.
//
// The rules exist so that a request never reads "done" when its ask was not
// met. Only an explicit result counts. An error, a missing check method, a
// missing reviewer, silence from a source or the doer's own word are never
// "met". A result belongs to one version of its subject.
//
// The package takes no result from the Automaton engine's verifier, guardrail,
// final gate, landing or shipped records: an item is evaluated only by its own
// saved check, by an independent reviewer datawatch starts, or by a person's
// confirmation. It therefore does not import those packages (a test enforces
// this).
//
// The setting acceptance.mode is "off" (the default) or "report". In report
// mode lists are kept and checked; nothing is blocked, held or merged
// differently because of them. Who may change a list is recorded here and is
// not enforced by this package.
//
// Design: docs/plans/2026-11-26-done-when-acceptance.md.
package acceptance
