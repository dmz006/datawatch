# [sync] A session's cost now includes its subagents — parity draft for the datawatch-app native clients (Android, iPhone)

**Not filed yet.** This is the tracked draft; the issue is opened when the operator files it. This draft is a file write only: it runs no hosting CLI and creates nothing on any host.

**World this changes.** A Claude Code subagent (a Task or Agent tool run inside a session) has no datawatch session of its own, so its tokens were never in the Sessions list, the cost summary or the spend ledger. The daemon now reads the subagent's transcript and records the usage under the parent session.

**What the client sees.**
- A session's `tokens_in`, `tokens_out`, `est_cost_usd`, `cache_read_tokens`, `cache_creation_tokens` and `tool_calls` now include its subagents. Totals of a supervisor session rise accordingly; this is the correction, not a regression.
- New optional session fields: `subagent_tokens_in`, `subagent_tokens_out`, `subagent_tool_calls` (omitted when zero). They say how much of the totals came from subagents.
- `GET /api/cost` carries `subagent_tokens_in` and `subagent_tokens_out` in the rollup and in the per-session form (`?session=`).

**Client work.** Show the subagent share next to a session's token count where the app shows tokens (for example "12,400 tokens, 3,100 from subagents"), and in the cost summary line. No new setting, endpoint or permission. Clients that ignore the new fields keep working.

**Open.** Subagent tokens are priced at the parent backend's rate; a subagent that ran another model is priced as the parent.
