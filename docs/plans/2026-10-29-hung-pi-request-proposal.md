# Hung pi-prompt request — proposal (PRD f3420001, task b1f70001)

## Context

The open question is what, if anything, the datawatch daemon should do when a
one-shot pi-prompt worker sits on a single open inference request for an hour or
more with no output, no child process and about 0% CPU on the node, with nothing
detecting that state until a human notices. It matters now because it happened
twice in one evening (sessions ab4c and ef70, 2026-10-28, ~2h43m total on a node
with `max_concurrent_sessions=1` pinning the model the pipeline depends on), and
both sessions could only be ended by the generic session-idle detector after they
had already burned their time. This bound PRD, **f3420001**, already handles the
*descendant* stall — the shape where the runner has died but a hung child process
keeps the pane alive — with its default of 2700 seconds in
`internal/config/descendant_stall.go`; the hang investigated in
docs/plans/2026-10-29-hung-pi-request-findings.md is a different case, because the
worker's live runner and its open socket to the node are the only signs of life:
per the findings' "Why the detectors stayed quiet" section, every existing detector
keys off runner liveness, descendant count, log mtimes, state flips or error text,
and "runner still alive" is counted as evidence of work, so a wedged, output-less,
childless pi matches none of their fire conditions. The findings also make clear
that the leading mechanism — a wedged instance of the pinned
`qwen3.8-64k:27b` model in the node's Ollama engine, with the pi clients blocked
on it — is a *hypothesis* (inference, not fact): the sibling-model-answered-while-
pinned-model-timed-out evidence rests partly on operator recollection, and no
Ollama server-side request log or socket capture for the wedged window survived;
the findings say the evidence "supports" client-side waiting on a wedged instance
and rules out runaway generation and a wedged client runner. Whatever the exact
mechanism, the daemon currently has no observation that touches the request in
flight or the node side, and no remediation path that is targeted (killing one PID
or one model instance) rather than blanket (ending the whole session, as the
idle-detector did to ab4c). That gap — no in-flight-request observation, no
pid-precise or instance-precise remediation — is what this proposal asks the
operator to decide, and the constraint is that this PRD's own change surface
(descendant-only stall detection, cmd/datawatch/descendant_stall_wait.go) must not
be allowed to grow into any of those things by accident.

## Options

1. **Observe request progress (add an in-flight-request liveness signal for
   pi-prompt).**
   (a) Give the daemon a read-only observation that looks at the *request*, not
   just the runner process: the pane's tee'd output growth (the wrapper at
   `internal/llm/backends/pi/backend.go:516-563` writes stdout to a per-session
   scratch file), the pi session-log tail mechanism already present as
   `cmd/datawatch/pi_log_age.go` (currently a feed to the idle observation, never
   a verdict), and daemon-side read-only node/model probes of the same kind
   already used for `qwen3.8-64k:27b` (`/api/ps`) and for
   `internal/inference/model_load.go` residency state.
   (b) Detection signal: no new bytes in the pane/scratch output **and** no new
   pi session-log event **and** no node-side evidence of generation, sustained
   across a window. The window is a new operator-set configured value, taken from
   the operator the same way `descendant_stall_seconds` is taken at
   `internal/config/config.go:2326`; the trade-off is that a shorter window risks
   flagging a genuinely slow first token on a 27B model with 64k context, while a
   longer window leaves the single-slot node blocked for longer — the findings
   record both incident windows (5958 s and 4497 s) as the lower bound of what
   "bad" looked like, and nothing above that as a recommended number.
   (c) What the daemon does next: emit a timeline event and session
   classification ("wedged in flight", analogous in spirit to the
   `stalled (category infra)` verdict the findings quote from task bd730001), and
   make that verdict available to a remediation chosen from options 2-4 — the
   observation itself changes no session state.
   (d) Cost: one new config key with an operator-supplied value (no default
   number in code, and no timeout/cap/limit hardcoded); code surface in the
   probes (cmd/datawatch/session_idle_probe.go, cmd/datawatch/pi_log_age.go) plus
   the autonomous observation in internal/autonomous/ (session_idle.go shape);
   false-positive risk is the main cost (a legitimately slow first token reads
   like a wedge — mitigated by making this a notification/verdict, not a kill);
   reversible by setting the config off; parity impact: the new field must be
   reachable from YAML/config (internal/config/config.go), MCP (tool schema next
   to session_idle_timeout_seconds at internal/mcp/autonomous.go:117), the REST
   config surface, the CLI, a comm-channel event for the verdict, and the PWA
   settings form plus its tests (internal/server/web/ app-autonomous-settings
   tests, the missing app-autonomous-settings-field-values.test.js being the
   current gap the findings flag).

2. **End only the exact recorded worker pid (precision remediation).**
   (a) At launch, record the pid of the exact `pi` process (Launch at
   `internal/llm/backends/pi/backend.go:472`; the wrapper template it builds can
   surface the child's pid), and when a wedge verdict lands, end that one pid —
   not the pane, not the session, not the node — so pi exits with the same
   143/SIGTERM shape the findings record for ab4c and ef70, the wrapper writes
   its `DATAWATCH_PI_FAILED` marker, and the existing one-shot failure path runs.
   (b) Detection signal: the wedge verdict from option 1, or an explicit operator
   directive; the option as a stand-alone still needs its action window to be an
   operator-set configured value — shorter risks killing a slow-but-live request
   (the false positive is now an irreversible lost attempt of the
   auto_fix_retries budget that internal/mcp/autonomous.go manages), longer risks
   the node sitting blocked as it did in the 07:51–10:35 window.
   (c) What the daemon does next: reap the marker, record the kill attribution
   (the findings note both incident kills are attributed to
   `autonomous.verifier.idle-detector` with a conflicting story for ab4c — a
   precise kill should produce an unambiguous `by=`), and let existing retry
   policy decide the next attempt.
   (d) Cost: no new duration config if it is always verdict- or directive-gated
   (a gating flag is the only possible new key); code surface concentrated in
   the pi backend launch/exit path plus the attribution constant at
   cmd/datawatch/end_attribution.go; false-positive risk is low *if and only if*
   the kill is reachable only via a verdict or operator action, never via a bare
   elapsed-time check — that restriction should be explicit; reversible by not
   wiring the trigger; parity: if exposed at all it needs the same plumbing as
   option 1 across REST, MCP, CLI, comm channel, YAML/config and PWA, or it
   stays an internal remediation with operator-visible audit entries only.

3. **Retry on a stronger backend (reroute the attempt, not the node).**
   (a) On a wedge verdict against the pinned model, classify the attempt as an
   infra failure and route the next attempt to a different backend or model — a
   node/model the router can already express (internal/router/compute.go
   `node_pinned_model`, `scheduling_priority`, and the existing
   `allow_model_swap` knob at internal/router/compute.go:31) — instead of
   re-queueing onto the same pinned `qwen3.8-64k:27b` instance that the findings
   hypothesize was wedged.
   (b) Detection signal: the wedge verdict from option 1; how many consecutive
   wedged attempts count as "reroute now" versus "retry same model" would be an
   operator-set count, with the trade-off that a lower count leaves the failing
   instance less chance to recover on its own while a higher count burns more of
   the single concurrency slot; no number is proposed here.
   (c) What the daemon does next: write the failure into the per-task verdict
   stream exactly like the ab4c/ef70 "stalled (category infra)" entries the
   findings quote, mark the node/model as suspect in the roster, and have the
   router prefer an alternate for the next attempt (the sibling-model-answered
   evidence in the findings is operator recollection only, so the fallback
   target is whatever the operator says is the stronger backend).
   (d) Cost: minimal new config (allow_model_swap and node priority already
   exist in internal/router/compute.go; at most a per-node "prefer alternate on
   wedge" flag and the count above, operator-valued); code surface in the
   verdict/routing seam (internal/mcp/autonomous.go retry handling,
   internal/router/); false-positive risk is low for the node but the *result*
   risk is real — a stronger backend may produce a different answer for the same
   task, changing what downstream REST/MCP/CLI/PWA consumers see, so the verdict
   record must name the backend used, as the findings' verdict entries already do
   ("Worker: pi-prompt/ollama/qwen3.8-64k:27b"); reversible by removing the
   flag; parity: the strongest cross-surface issue, because a "retry" that
   silently changes the model is a capability difference visible differently in
   YAML/config vs PWA vs comm-channel.

4. **Node-instance recovery (unload/reload the wedged model) — included because
   the findings support it.**
   (a) When the wedge verdict names a specific node's pinned model, act on the
   *instance* rather than the client: unload it (keep_alive 0) and reload it,
   which is the recovery the operator reported actually worked on 2026-10-28 —
   the findings record this as operator recollection and keep
   `keep_alive`/residency as the daemon's existing lever via
   internal/inference/model_load.go (`loadKeepAlive`, `requestLoad`), or mark
   that node's model slot as degraded so the router stops sending work to it.
   (b) Detection signal: again the option-1 verdict; an automatic trigger window
   would be operator-set with the same slow-vs-cruel trade-off as above, and the
   option can be gated so it always requires a verdict (or an operator
   directive) rather than a bare duration.
   (c) What the daemon does next: issue the unload/reload through the existing
   model-load path, or write a node-degraded mark that internal/router/compute.go
   respects, and record the action in the session timeline so a future
   investigation does not hit the ef70-style "no by= attribution" gap.
   (d) Cost: no new duration if verdict-gated; code surface reuses
   internal/inference/model_load.go and the node roster; false-positive risk is
   the collateral one — unloading the instance drops *all* sessions and future
   loads on that node and costs reload time for a 17.95 GB model (the findings'
   2026-10-29 `/api/ps` reading), which on a `max_concurrent_sessions=1` node is
   bounded but on a shared node is not; reversible (just don't trigger it);
   parity: node-level, so it is visible uniformly across REST, MCP, CLI, comm
   channel, YAML/config and PWA — the cheapest option on the parity axis.

## Recommendation

Option 1 — observe request progress — because options 2, 3 and 4 are all
remediations whose safety depends on the wedge verdict they would consume, and
the findings' "Why the detectors stayed quiet" section shows that every current
detector is silent for exactly this shape, so building one of them without the
in-flight-request signal first would be acting blind on the only node the
autonomous pipeline can currently use (PRD f3420001;
docs/plans/2026-10-29-hung-pi-request-findings.md).

## Question for the operator

Which option do you prefer?

1. Observe request progress (option 1; PRD f3420001)
2. End only the exact recorded worker pid (option 2)
3. Retry on a stronger backend (option 3)
4. Node-instance recovery, unload/reload (option 4)
5. Let's chat about this instead.

## Parity surface

No surface changes are made by this investigation and proposal. Each surface is
intentionally excluded (this is an investigation and a proposal, so no implementation
surface is touched):

- **REST** — no new endpoints or config fields; nothing to expose.
- **MCP** — no new tools or tool-schema fields added.
- **CLI** — no new flags or commands added.
- **Comm channel** — no new events emitted beyond existing session/timeline events.
- **YAML/config** — no new config keys introduced.
- **PWA** — no settings-form or test changes.
- **Android** — not affected; investigation/proposal only.
- **iPhone** — not affected; investigation/proposal only.
