// Package placementtags derives the automatic placement tags for a piece of
// work from the facts of where and how it runs.
//
// A placement tag is a single lowercase string of the form "axis:value"
// (for example "compute:node1" or "lane:gpu"), matching the style of the
// existing automatic tags (lowercase axis name, a colon, then the value).
// From a set of PlacementFacts — which node runs the work, which federated
// peer, which backend, lane, role and project, plus the surrounding
// automation context (Automaton, automation, stage and work kind) and any
// custom axes — the package produces a sorted, de-duplicated set of such
// tags for whichever axes are enabled in the Settings and whose fact value
// is non-empty.
//
// The ten built-in axes are compute, peer, backend, lane, role, project,
// automaton, automation, stage and kind.
//
// The package is pure: it performs no I/O and imports no other datawatch
// package, so it can be used and tested in isolation.
package placementtags
