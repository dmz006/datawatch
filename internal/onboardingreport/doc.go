// Package onboardingreport builds the Automation onboarding report: a
// read-only account of how far a datawatch install is set up for Automation.
//
// The report answers three questions in order:
//
//  1. What is configured: the backends and models available, the project
//     directories registered, the comm channels, memory, and which merge
//     pipeline source each project uses.
//  2. What is missing: each check has an id, a plain title, a status of ok,
//     missing or optional, and a one-sentence reason.
//  3. What to try next: ordered suggestions, each linking a how-to and a
//     definitions anchor and naming the command, the MCP tool or the PWA
//     place that does it.
//
// The report is built from live daemon state through small interfaces (see
// Sources), so tests use fakes. It never changes configuration. The checks
// are data: a list that can be switched off per check through the
// onboarding_report.disabled_checks setting. Text renders the same words for
// the REST route, MCP, the CLI, comm channels and the PWA.
package onboardingreport
