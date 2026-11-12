// Package burnguard estimates how fast the Claude sessions the daemon runs are using the
// weekly allowance, and whether that burn will run past the weekly pace line before the
// reset.
//
// It has three parts, one per file:
//
//   - tokens.go reads the session transcripts under a directory it is given (never a
//     hard-coded path), de-duplicates the assistant turns by request id, and buckets the
//     input, output, cache-write and cache-read tokens per hour, split between worker and
//     verifier sessions (those that ran in a task worktree) and everything else.
//   - fit.go turns tokens into points of the weekly allowance with two coefficients, points
//     per million cache-write tokens and points per million cache-read tokens, and refits
//     them by least squares against the points between consecutive usage readings whenever
//     a newer reading arrives. It reports the fit error so a poor refit is visible.
//   - project.go projects the points to the weekly reset and compares the result with the
//     pace line of the newest reading, using usagecalc's advice for the week position, the
//     pace line and the ceiling. It returns the projection, the line, whether the projection
//     is over or under by the margin, and the numbers an alert needs (points per hour and
//     allowed per hour). It also projects the readings alone with usagecalc's advice (the
//     recent-burn projection, AdviceProjected). When the two differ by more than the
//     configured gap limit, the ladder decides on the lower of them.
//
// The package does no network I/O, reads no clock (every moment comes in as an argument) and
// does not import the server package.
package burnguard
