// Package delivery keeps one list of the work that has not been delivered or
// finished, checks it, and tells the operator.
//
// It reads existing records (Automata, tasks, bug batches, unfinished branches,
// schedules, sessions, pipelines, held answers) through sources; it never keeps
// a second copy of them. Its own records hold only IDs, kinds, states, reason
// codes and times, and are sealed with the delivery store key on a secure
// install. The check reports: it blocks nothing, and it never types into a
// session.
package delivery
