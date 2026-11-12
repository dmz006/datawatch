// Package platformcontext holds the fixed platform context text that tells a
// session what datawatch is and where to look first.
package platformcontext

// PlatformContextVersion must be incremented whenever PlatformContext changes.
const PlatformContextVersion = 1

// PlatformContext is the platform context text. It is a constant and is not
// configurable.
const PlatformContext = `This session runs on datawatch.
- Sessions: running AI coding sessions.
- Projects: the folders sessions work in.
- Automata and Automations: planned multi-step work datawatch runs itself.
- Memory: facts kept between sessions.
- Federation: peers and compute nodes sharing work.
- Messaging channels: how datawatch talks to the operator.
- Secrets: stored credentials, never printed.
- Alerts: notices about sessions and the system.
Look first: run docs search, then docs read on the matching howto and definitions, before reading code or guessing a setting. Use the scope and project tools to learn where this session is, and the status tools for state.
Many more tools exist for infrastructure, observability, models, plugins and configuration; search the tool list and docs to find them.`

// EstimateTokens returns the rough token estimate used by the budget test.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }
