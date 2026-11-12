# Model request progress: is a request hung or slow

**Date:** 2026-10-31
**Version:** none on the branch. This work does not change `var Version` in `cmd/datawatch/main.go` or `internal/server/api.go`; the merge helper assigns the next patch above `main` and bumps a minor version at merge time.

**Terminology.** Operator-facing text says **Automaton / Automata**, following the Terminology
Rule in `AGENT.md`. The internal Go type is `PRD` (`internal/autonomous`); this document uses
"PRD" only for code identifiers, API paths and tool names.

## Problem

A worker or verifier session of an Automaton can wait a long time on one model request. From the
outside a hung request and a slow one look the same: the session prints nothing either way. When
the model runs on a registered compute node the daemon can look at the node, so it can say whether
the node still shows the model working on the request and for how long it has shown nothing.

## Scope

New package `internal/requestwatch/`:

- `types.go`, `text.go`: the observation, its four states (`generating`, `idle`, `unreachable`,
  `unknown`) and the plain-text summary wording.
- `source_ollama.go`: the signal source. It reads the node's running-models listing and, from an
  attached observer, the GPU activity. It never calls a generate, chat or completion route.
- `tracker.go`: the per-session progress tracker (state, last change, no-progress time,
  threshold, recovery).
- `watcher.go`, `registry.go`, `status.go`: the poller, the registry of current observations and
  the status read model.

Configuration, daemon, alert and question:

- `internal/config/request_watch.go` and `internal/config/config.go`: the
  `autonomous.request_watch` block (`config.RequestWatchConfig`, field
  `Autonomous.RequestWatch`) with eleven keys. Booleans are `*bool`; the option list keeps unset
  distinct from an explicit empty list, the same shape as `InfraRetryConfig`.
- `cmd/datawatch/request_watch_wiring.go`: which sessions are watched, the signal source chosen
  by node kind, and the GPU reading taken from the node's attached observer.
- `internal/autonomous/request_watch_question.go`: the alert, the question and the recorded
  answer.

Surfaces:

- `internal/server/request_watch.go`, `request_watch_config.go`, `request_watch_views.go`: the
  route `GET /api/autonomous/request-watch` (`session` and `automaton` query parameters), the
  config keys on `GET` and `PUT /api/config`, and the `model_request` field on session and task
  views.
- `internal/mcp/request_watch_tools.go`: the read-only tool `autonomous_request_watch` and the
  eleven `request_watch_*` arguments of `autonomous_config_set`.
- `cmd/datawatch/cli_request_watch.go`: `datawatch autonomous request-watch [--session ID]
  [--automaton ID] [--json]`.
- `internal/router/request_watch_comm.go`: the chat verb `autonomous request-watch` and the
  `configure` keys.
- `internal/server/web/app.js` and the five locale bundles: the card on the Automation Overview,
  the chips on session and task views and the settings fields.

Documentation: `docs/howto/model-request-progress.md`, `docs/datawatch-definitions.md`,
`docs/config-reference.yaml`, `docs/operations.md`, `docs/api/autonomous.md`,
`docs/testing-tracker.md`, `CHANGELOG.md` and this plan.

## Phases

The work is four stories. All four are Done. The version is assigned at merge time, not on this branch.

| Story | What it delivers | Status |
|---|---|---|
| Observe the in-flight model request | The configuration block, the node signal source, the progress tracker, the poller wired into the daemon, and the alert and question at the threshold | Done |
| Show the observation on REST, MCP, CLI and the comm channel | The route, the config keys on the REST config, the session and task views, the MCP tool and its config arguments, the CLI command and the chat verb | Done |
| Show the observation in the web app | The settings fields with help links, the Automation card, and the chips on session rows, the session header and task rows, in all five locale bundles | Done |
| Document, hand off and prove the feature | The glossary, how-to, config reference and operations text, this tracker, changelog and plan, the mobile parity issue, the sandbox live check and the final gate | Done |

## Status

Done. Every phase above is Done and covered by unit and integration tests on this branch
(`docs/testing-tracker.md`, section "Model request progress"). Tested: yes. Validated: yes, by the
sandbox live check (`docs/plans/2026-10-31-model-request-progress-live-check.md`). The final gate
ran and passed (`docs/plans/historical-plans/2026-10-31-model-request-progress-final-gate-report.md`). The version is
assigned at merge time: this branch changes neither `var Version` line, and the merge helper takes
the next patch above `main` and bumps a minor version.

## Decision notes

- **Observation only.** The feature reports; it does not act. It never ends a worker, never
  retries the work on another backend and never unloads or reloads a model. Those are operator
  decisions, and a daemon that took them on a guess about "no progress" could throw away a request
  that was about to finish.
- **The question is non-blocking and records the choice only.** At the threshold the daemon raises
  one warning alert and one question offering `keep_waiting` (always offered, always the default),
  `end_worker`, `retry_other_backend` and `unload_reload_model`. The question does not park or
  change the task: parking a running worker would itself be an end of the worker, which is out of
  scope. Answering writes one decision that says no remediation was performed. If the operator
  picks anything other than `keep_waiting` they carry it out themselves. Alert and question are
  raised once per stretch without progress, and an open question is withdrawn when the request
  recovers or the session ends.
- **Only Ollama nodes have a signal source.** The running-models listing is read from an Ollama
  node, and the GPU reading from the node's attached observer when `use_gpu_signal` is on. A
  session on any other kind of node is not watched and shows no observation. A new kind is added
  by giving it a source, with no change to the tracker, the surfaces or the settings.
- **No model call is made.** The watch only asks the node what it holds loaded and how busy the
  GPU is. Sending a probe prompt would add load to the very request it is trying to judge, could
  queue behind it, and would answer a different question (can the node take a new request) than
  the one asked (is this request moving). A stand-in node in the tests records that no generate,
  chat or completion route is ever called.
- **A long first response looks idle.** A large model can take many minutes to answer, and neither
  signal moves meanwhile. The state reads `idle` and the time grows, the same as a hung request;
  `no_progress_minutes` and the GPU signal are the levers, and the how-to says so.
- **Nothing is tied to this repository.** No repository, model, node or backend family name and
  no path is hard-coded. Every behaviour is one of the eleven config keys, each with an
  `Effective` accessor so an unset key reads its default.

## Parity surface

| Surface | Included? | Intent |
|---|---|---|
| REST | Yes | `GET /api/autonomous/request-watch` with optional `session` and `automaton`; `GET` and `PUT /api/config` carry the eleven `autonomous.request_watch.*` keys, the list taking a JSON array or a comma list and a bad value refusing the whole put; the session and task views gain `model_request`. |
| MCP | Yes | The read-only `autonomous_request_watch` tool (`session`, `automaton`) proxies the route; `autonomous_config_set` takes the eleven `request_watch_*` arguments and sends them as dotted keys; the federation capability table lists the tool. |
| CLI | Yes | `datawatch autonomous request-watch [--session ID] [--automaton ID] [--json]` prints one line per observation; `datawatch config set autonomous.request_watch.<key>` covers every key. |
| comm channel | Yes | `autonomous request-watch [session <id> \| automaton <id>]` answers one line per observation; `configure autonomous.request_watch.<key>=<value>` sets the keys; the question is answered with the existing question verbs. |
| YAML/config | Yes | The `autonomous.request_watch` block with all eleven keys and their defaults in `docs/config-reference.yaml`. |
| PWA | Yes | The Automata model request progress card on the Automation Overview, the chip on session rows, the session header and task rows, the eleven settings fields with description and glossary link, and the locale keys in all five bundles. |
| Android | Excluded in this work | This work does not change the apps. The single mobile parity record is `docs/plans/parity-issues/model-request-progress.md`, filed as one sync issue on the datawatch-app repository, which covers the new route, the `model_request` field, the settings block and the locale keys. |
| iPhone/iOS | Excluded in this work | Same single sync issue as Android: `docs/plans/parity-issues/model-request-progress.md`. The apps pick the change up from that issue, not from this branch. |
