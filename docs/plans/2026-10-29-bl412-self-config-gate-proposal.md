# BL412 Self-config permission-gate generalization: design proposal

**Date:** 2026-10-29

**Version:** none (planning document; plan-document commits are exempt from the version bump)

**Tracker:** BL412 (registry row 42)

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

The operator's direction (recorded 2026-10-11): the control behind `mcp.allow_self_config` should apply across every function, API, MCP tool and CLI path, so that what an LLM session may change about its own daemon is enforced in one place, in the spirit of federation access control. The default posture should be enabled, with clear documentation on how to harden it. Today the gate covers a handful of MCP tools, while a second, newer mechanism already restricts sessions by capability. The two overlap and neither is the whole answer.

## What exists today (verified)

EXISTS TODAY, mechanism 1, the boolean gate (BL110):

| Piece | Proof |
|---|---|
| Config flag `mcp.allow_self_config`, default false | `internal/config/config.go` (`AllowSelfConfig`) |
| The generic MCP `config_set` tool is refused unless the flag is true, and refuses to change the flag itself (bootstrap protection) | `internal/mcp/memory_tools.go` (`handleConfigSet`) |
| The shared helper `allowSelfConfigCheck` guards `detection_config_set`, `dns_channel_config_set`, `proxy_config_set` and the compute pin-settings write | `internal/mcp/bl220_mcp_tools.go`, `internal/mcp/compute.go` |
| Each approved `config_set` is logged to stderr and to an audit file | `internal/mcp/self_config_audit.go` |

EXISTS TODAY, mechanism 2, capability-based access (federation and Design A3):

| Piece | Proof |
|---|---|
| Every REST config write checks a capability: `PUT /api/config` needs `config:write`, `PUT /api/cost/rates` needs `config:write` | `internal/server/api.go` (`handleConfig`), `internal/server/cost.go` |
| A per-session scoped credential carries a capability set; the default group `session-default` deliberately omits config writes, autonomous writes, secrets, LLM and compute registry, federation, identity and daemon control | `internal/federation/capabilities.go`, `internal/server/federation_cap.go` (`fedCap`, `callerCaps`) |
| Every MCP tool maps to a required capability (about 490 entries) and is checked per call; operators can grant a session more through custom groups | `internal/federation/mcp_tool_caps.go` (`MCPToolCap`) |
| An admin caller (no peer, no session credential) is unrestricted | `fedCap`: "admin — unrestricted" |

Surface-by-surface answer to "can a session or worker change daemon configuration, and is it gated":

| Surface | Boolean gate | Capability check |
|---|---|---|
| REST `PUT /api/config` | no | yes, `config:write` |
| MCP `config_set` | yes | yes |
| MCP detection, DNS channel, proxy config set; compute pin settings | yes | yes |
| MCP `autonomous_config_set`, `orchestrator_config_set` | no | `autonomous:write` |
| MCP `council_config_set`, `channel_routing_config_set`, `usage_backoff_set` | no | `config:write` |
| MCP `observer_config_set` | no | `observers:write` |
| MCP `llm_add`, `llm_update`, `llm_delete`; `secret_set`; `server_add` | no | per-tool write capability |
| Comm channel `configure <key>=<value>` | no | none found; it calls the same configure function the router is given (`internal/router/router.go`) |
| CLI `datawatch config ...` | no | none; it is the operator's own shell command |

MISSING:

| Gap | Evidence |
|---|---|
| The boolean covers about 5 of the roughly 45 MCP tools that write configuration or registries | the table above; grep of `allowSelfConfigCheck()` finds four call sites plus `config_set` |
| No audit of config changes made through any path except `config_set` | `auditSelfConfig` is called only from `handleConfigSet` |
| No single place that classifies "this call changes daemon configuration" | classification lives per tool in `MCPToolCap`, but the boolean does not use it |
| The two mechanisms disagree on defaults: the boolean defaults closed, `session-default` denies config writes. The operator's stated direction is default enabled | `config.go`, `capabilities.go` |

Not verified: whether a worker with a plain shell can reach the admin token or edit the config file directly (which would bypass both mechanisms), and whether the comm-channel `configure` path is limited to the authenticated operator. Both need a short audit before any option is built.

One incidental observation, reported and not acted on: `POST /api/cost/usage`, which writes a session's counters, requires only the read capability `analytics:read` (`internal/server/cost.go`).

## Options

**A. Document and close.** Declare mechanism 2 the enforcement and keep the boolean as is. Add a how-to on hardening and on granting a session `config:write`. Cost: 1 docs-only PRD, about 3 tasks. Risk: the 2026-10-11 direction (gate everywhere, default enabled) is not met; the two mechanisms remain.

**B. Central gate on top of both.** One check, run once per call in the MCP dispatcher and the REST config handlers, that treats any tool or route classified as a configuration write as gated by `allow_self_config`, with a per-call audit record, bootstrap protection kept. Cost: 1 to 2 PRDs, about 8 tasks. Risk: layers a third rule on two existing ones; the classification list must be kept in step with new tools (a test that fails when an unclassified tool appears solves this, as the capability map already has one).

**C. Fold the boolean into capabilities.** Add a built-in group (for example `self-config`) holding the config-write capabilities; `allow_self_config=true` becomes an alias that adds that group to `session-default`; operators who want hardening remove it or define a narrower custom group. One mechanism, federation-style, with one audit hook in `fedCap` and the MCP per-tool check. Cost: 2 PRDs, about 10 to 12 tasks (group, alias and migration, audit in the shared check, comm-channel and CLI session paths, docs, tests). Risk: changes the default grant of every spawned session, so it needs a staged rollout and a live check that existing sessions keep working.

**D. Do nothing.** Cost: none. Risk: the direction stays open and the boolean keeps implying more protection than it gives.

## Recommendation

Option C. It matches the operator's request to model this on federation access control, ends with one mechanism and one audit point instead of three, and gives a documented hardening path (drop or narrow the group). Run the two unverified checks (shell access to the token or config file, comm-channel authority) first, as the first task of the build, because they decide whether C actually closes the paths.

## Open questions for the operator

1. Which option? A / B / C / D.
2. Default posture for new sessions: config writes allowed (the 2026-10-11 direction) or denied until granted? (allowed / denied)
3. If allowed by default, should that cover secrets, LLM registry and federation changes too, or only non-secret settings? (all / non-secret only)
4. Should every configuration change by a session, on any surface, be written to the audit log, not only `config_set`? (yes / no)
5. Should the audit of shell and token reach (the unverified item above) be filed as its own item ahead of the build? (yes / no)

## Dependencies and order

No hard dependency. It touches the same capability map as the federation work (shipped) and the session scoped-credential design (shipped). It should land before BL473 and BL483 turn more Automation policy into config keys that a session can write. Order: audit tasks, then the central check, then the default change last.

## Parity surface

| Surface | What a build touches |
|---|---|
| Config | `mcp.allow_self_config` kept as alias; new group in the capability config |
| REST | audit event on config writes; capability error text |
| MCP | per-call check and audit; no new tools |
| CLI | no gate (operator shell); a `config` view of the effective session grant is optional |
| Comm channel | `configure` needs an authority decision (question 2 and the unverified item) |
| PWA | settings page text and a view of a session's grant |
| Mobile | datawatch-app parity issue required if the PWA settings change |
