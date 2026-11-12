// Package changerecord captures what a task or an Automaton changed — files,
// Go packages, settings, routes, locale keys, tests and the commands that were
// run — as a ChangeRecord, so later steps can generate documentation and
// reports from facts instead of from memory.
//
// BuildFromDiff derives a record from a DiffSource (a fake in tests, a
// git-backed one in production), Merge combines task records into one
// Automaton record, and Save/Load persist it as JSON under the Automaton data
// directory.
package changerecord
