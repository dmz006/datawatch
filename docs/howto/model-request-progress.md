---
docs:
  index: true
  topics: [model request, progress, hung, slow, stall, compute node, ollama, gpu, worker, verifier, automata, autonomous, observation, no progress, request watch]
exec_params:
  - {name: automaton_id, required: false, description: "Automaton ID to narrow the read to; leave empty to read every watched Automaton"}
  - {name: session_id, required: false, description: "Session ID to narrow the read to one worker or verifier session; leave empty to read them all"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the daemon-wide Automata settings, including the autonomous.request_watch block with defaults filled in
    args: {}
    read_only: true
  - tool: autonomous_request_watch
    description: Read the watch settings in effect and the observation of every watched worker or verifier session (state, time without progress, summary). Observation only, no model call
    args: {automaton: "{{params.automaton_id}}", session: "{{params.session_id}}"}
    read_only: true
---
# How-to: Model request progress

A worker or verifier session can sit for a long time waiting on one model
request. From the outside, a hung request and a slow one look the same: the
session shows no output either way. Model request progress answers one
question, **is it hung or slow?**, so you can decide whether to keep waiting.

The feature is also called the *request watch*. It is for the sessions of
Automata (the workers that do a task and the verifiers that check it) whose
model runs on a registered compute node.

## What it answers

When the model runs on one of your compute nodes, the daemon can look at the
node and tell whether the node is still working on the request. It reports one
of four states for each watched session and how long it has been since the last
sign of progress. A request that is generating is not stuck. A request that
shows no progress for longer than you allow is worth a look.

It is **observation only**. The request watch:

- makes no model call, so watching a request never adds load to it;
- never ends a worker, never retries the work on another backend and never
  unloads a model;
- never changes a task or a session.

It reports, raises one alert and one question when a request passes your limit,
and you decide what to do.

## What is observed, and what is not

On every poll the daemon asks the compute node for its own **running-models
signal**: which models it holds loaded, and for how long. A change in that
signal counts as progress. When the node has an attached observer and
`autonomous.request_watch.use_gpu_signal` is on, the daemon also reads the
**GPU activity** of the node. A GPU at or above
`autonomous.request_watch.gpu_busy_percent` counts as progress too.

That is all. The watch does not read the request, the prompt or the answer, it
does not read the session's output, and it does not send anything to the model.
It never calls a generate, chat or completion route on the node.

A session is watched only when all of these hold:

- it is running or waiting for input;
- its model runs on a registered compute node (it carries a compute node
  reference that resolves in the node registry);
- it is the worker of a task that is in progress, or the verifier of a task of
  an Automaton;
- the node is of a kind the watch can read (see [Limits](#limits)).

Every other session is left alone, and so is a session whose model does not run
on a registered compute node.

## The states

| State | What it means |
|---|---|
| `generating` | The node shows the model working on the request: the running-models signal changed, or the GPU is busy. |
| `idle` | The node answers, but shows no sign of the model working on the request. |
| `unreachable` | The node could not be read at the last poll. |
| `unknown` | No reading has been taken yet, right after the session is first seen. |

The time without progress is counted from the last sign of progress. For a
session the watch has just started to see, the clock starts at the first poll,
so a request that was already stuck when the watch first looked is measured
from that moment.

## The summary wording

Every observation carries a plain-text `summary`:

| State | Summary |
|---|---|
| generating | `waiting on a model request: generating` |
| idle | `waiting on a model request: no progress for N minutes` |
| unreachable | `waiting on a model request: node unreachable, no progress for N minutes` |
| unknown | `waiting on a model request: no reading yet` |

`N` is whole minutes, rounded down; under a minute it reads `less than a
minute`, and one minute reads `1 minute`.

## What an observation holds

| Field | Meaning |
|---|---|
| `session_id` | The watched session. |
| `automaton_id`, `task_id` | The Automaton and the task the session works for. |
| `role` | `worker` or `verifier`. |
| `node`, `model` | The compute node and the model the session asked it for. |
| `state` | `generating`, `idle`, `unreachable` or `unknown`. |
| `last_change` | When the last sign of progress was seen (RFC 3339). |
| `no_progress_seconds` | Seconds since `last_change`. |
| `last_poll` | When the node was last read (RFC 3339). |
| `over_threshold` | `true` once the time without progress has reached `no_progress_minutes`. |
| `error` | Why the node could not be read; left out when there is no error. |
| `summary` | The plain-text line above. |

The read also carries `enabled`, `poll_seconds` and `no_progress_minutes`, the
settings in effect with defaults filled in. With the watch off, or when no
session is watched, the list is empty.

## Reading it

Every surface reads the same data. The optional `session` and `automaton`
arguments narrow the list to one session or one Automaton; an id that matches
nothing gives an empty list.

- **Web app.** On the **Events** tab, the **Automata model request
  progress** card lists each watched session with its role, task, node and
  summary, and refreshes every 10 seconds. A small chip with the same wording sits on the session
  rows, the session detail header and the task rows. The card links to this
  guide.
- **REST.** `GET /api/autonomous/request-watch`, with optional `session` and
  `automaton` query parameters.
- **MCP.** The read-only tool `autonomous_request_watch`, with optional
  `session` and `automaton` arguments.
- **CLI.** `datawatch autonomous request-watch [--session ID] [--automaton ID] [--json]`.
  One line per session; `--json` prints the raw response.
- **Chat channel.** The verb `autonomous request-watch`, optionally followed by
  `session <id>` or `automaton <id>`. The reply is cut to 20 lines.

```sh
# Everything that is being watched
datawatch autonomous request-watch

# One Automaton, as JSON
datawatch autonomous request-watch --automaton <automaton-id> --json

# REST
curl -sk "https://localhost:8443/api/autonomous/request-watch?session=<session-id>"
```

```
You: autonomous request-watch automaton <automaton-id>
Bot: <session-id> worker task <task-id> on <node>: waiting on a model request: no progress for 12 minutes
```

## What happens at the threshold

When a request has shown no progress for `no_progress_minutes`, the observation
is over the threshold, and the daemon does two things, once:

1. **One alert**, titled "Automaton model request is not progressing", at
   warning level. It names the Automaton, the task, the role, the node, the
   model and how long there has been no progress. Turn it off with
   `raise_alert`.
2. **One question** to the operator, when `raise_question` is on and the
   operator question channel (`autonomous.questions.enabled`) is on. Turn it off
   with `raise_question`.

The question offers up to four choices, set by `remediation_options`:

| Choice | Meaning |
|---|---|
| `keep_waiting` | Leave everything as it is; a slow request may still finish. Always offered, and always the default answer. |
| `end_worker` | End only the stuck worker and leave the model on the node alone. |
| `retry_other_backend` | Run the work again on another, stronger backend. |
| `unload_reload_model` | Unload and reload the model on its node. |

**The question does not block anything, and answering it does nothing but record
the choice.** The task is not parked and its status does not change, because
parking a running worker would itself be an end of the worker. The worker keeps
running. Your answer is written to the Automaton's decision log as a
`request_watch_answer` entry that says the daemon performed no remediation. If
you pick `end_worker`, `retry_other_backend` or `unload_reload_model`, you do it
yourself; nothing is ended, retried or unloaded for you.

Both are raised **once per episode**. An episode is one stretch without
progress. A request that shows progress again and then stalls again starts a new
episode, with a new alert and a new question. A question still open when the
request recovers or the session ends is withdrawn on its own.

When no question is asked (the question setting is off, the question channel is
off, or fewer than two choices are offered), the alert lists the choices in
plain words, so it alone carries them.

## Configuration

Every key lives under `autonomous.request_watch`. They are read on every poll,
so a change applies at once with no restart. An unset key reads its default.

| Key | Default | Accepted values | Effect |
|---|---|---|---|
| `autonomous.request_watch.enabled` | `true` | `true`, `false` | Turns the whole feature on. Off: no sample, no observation, no alert or question, and an empty list on every read. |
| `autonomous.request_watch.poll_seconds` | `30` | whole number, 1 or more | How often each watched request is sampled. |
| `autonomous.request_watch.no_progress_minutes` | `30` | whole number, 1 or more | Minutes without progress before a request is over the threshold. |
| `autonomous.request_watch.observe_workers` | `true` | `true`, `false` | Watch worker sessions. |
| `autonomous.request_watch.observe_verifiers` | `true` | `true`, `false` | Watch verifier sessions. |
| `autonomous.request_watch.use_gpu_signal` | `true` | `true`, `false` | Read the node's GPU activity as evidence of progress, when the node reports it. |
| `autonomous.request_watch.gpu_busy_percent` | `10` | whole number, 1 to 100 | The GPU load at or above which the node counts as generating. |
| `autonomous.request_watch.unreachable_counts` | `true` | `true`, `false` | Time with the node unreachable counts as no progress. When off, an unreachable node never takes a request over the threshold. |
| `autonomous.request_watch.raise_alert` | `true` | `true`, `false` | Raise the alert at the threshold. |
| `autonomous.request_watch.raise_question` | `true` | `true`, `false` | Raise the non-blocking question at the threshold. Needs `autonomous.questions.enabled`. |
| `autonomous.request_watch.remediation_options` | unset: all four | any of `keep_waiting`, `end_worker`, `retry_other_backend`, `unload_reload_model` | The choices the question offers. `keep_waiting` is always offered, first, and is always the default. An explicit empty list offers `keep_waiting` only (no question is raised, because there is nothing to choose between). |

A bad value sent through `PUT /api/config`, MCP, the CLI, the chat channel or the
web app is refused and nothing is written: the refusal names the key and the
valid values. A bad value written by hand into `config.yaml` is refused when the
file loads. The list accepts a JSON array or a comma list, for example
`end_worker,unload_reload_model`. A value of `[]` or an empty string is kept as
an explicit empty list, which is different from leaving the key unset.

### Tuning

- **Slow hardware or a big model: raise `no_progress_minutes`.** See
  [Limits](#limits). Ninety minutes is a reasonable start for a model that is
  slow to answer.
- **Poll less often: raise `poll_seconds`.** The poll is one small read per
  node and model, shared by every session on that node, so the default is
  cheap. Raise it only to cut the noise in a node's own log.
- **Only workers, or only verifiers: switch the other off** with
  `observe_workers` or `observe_verifiers`.
- **A node with no attached observer: set `use_gpu_signal` to `false`.** It has
  no effect there, but then the setting says what is true.
- **A GPU that idles above the default: raise `gpu_busy_percent`** so only real
  work counts as progress. A GPU that is used lightly by a small model: lower it.
- **A node that drops off the network now and then: set `unreachable_counts` to
  `false`** so a short outage is not mistaken for a stall.
- **A quiet watch: set `raise_alert` and `raise_question` to `false`.** The read
  surfaces keep working.
- **Fewer choices: list only the ones you would use**, for example
  `datawatch config set autonomous.request_watch.remediation_options end_worker`.

## Access methods

Each way of reading and changing the settings takes the same eleven keys.

| Method | Read | Change the settings |
|---|---|---|
| **YAML** | not applicable | `autonomous.request_watch:` in `~/.datawatch/config.yaml` with any of the eleven keys |
| **CLI** | `datawatch autonomous request-watch [--session ID] [--automaton ID] [--json]` | `datawatch config set autonomous.request_watch.no_progress_minutes 45` |
| **Web UI** | the **Automata model request progress** card on the **Events** tab; the chips on the session and task views | Settings → General → Automata settings → **Model request watch** |
| **REST API** | `GET /api/autonomous/request-watch` with optional `session` and `automaton`; the settings are also under `autonomous.request_watch` in `GET /api/config` | `PUT /api/config` with `{"autonomous.request_watch.no_progress_minutes": 45}` |
| **Comm channel** | `autonomous request-watch [session <id> \| automaton <id>]` | `configure autonomous.request_watch.no_progress_minutes=45` |

The MCP tool `autonomous_request_watch` reads the same data (read-only;
`session`, `automaton`). `autonomous_config_set` takes the eleven keys as
`request_watch_enabled`, `request_watch_poll_seconds`,
`request_watch_no_progress_minutes`, `request_watch_observe_workers`,
`request_watch_observe_verifiers`, `request_watch_use_gpu_signal`,
`request_watch_gpu_busy_percent`, `request_watch_unreachable_counts`,
`request_watch_raise_alert`, `request_watch_raise_question` and
`request_watch_remediation_options`; `config_set` takes the dotted keys.

```yaml
autonomous:
  request_watch:
    enabled: true
    poll_seconds: 30
    no_progress_minutes: 30
    observe_workers: true
    observe_verifiers: true
    use_gpu_signal: true
    gpu_busy_percent: 10
    unreachable_counts: true
    raise_alert: true
    raise_question: true
    remediation_options: [keep_waiting, end_worker, retry_other_backend, unload_reload_model]
```

```sh
# Allow a slow model ninety minutes before it is flagged
datawatch config set autonomous.request_watch.no_progress_minutes 90

# Offer only two ways to act besides keep_waiting
datawatch config set autonomous.request_watch.remediation_options end_worker,unload_reload_model

# REST
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.request_watch.no_progress_minutes": 90}' \
  https://localhost:8443/api/config
```

## Limits

- **Only Ollama nodes have a signal source today.** The running-models listing
  is read from an Ollama node. A session on a node of another kind is not
  watched, and shows no observation. Another kind can be added later without
  changing anything on this page.
- **A long first response on a big model looks idle.** A large model can take
  many minutes to give its first response, and during that time neither signal
  moves. The request reads as `idle` with a growing time without progress, the
  same as a hung one. It is only a stall once the threshold has passed and
  nothing has changed. Raise `no_progress_minutes` for slow hardware, and attach
  an observer to the node so that GPU activity can show the work.
- **The GPU signal needs an attached observer.** Without one, the running-models
  signal is the only evidence of progress.
- **The clock starts when the watch first sees the session.** A request that
  began before the daemon started, or before the setting was turned on, is
  measured from the first poll.
- **Observation, not diagnosis.** The watch tells you the node shows no sign of
  work. It cannot tell you why.
- **Nothing is performed.** Answering the question records your choice only.
  Ending a worker, retrying on another backend and unloading a model are done by
  you.

## See also

- [infrastructure-retry.md](infrastructure-retry.md): tasks that fail because
  of the machinery around them, which are retried on their own track
- [failure-escalation.md](failure-escalation.md): the worker ladder and the
  review actions you use after you have ended a stuck worker yourself
- [compute-nodes.md](compute-nodes.md): registering a compute node and
  attaching an observer to it
- [../api/autonomous.md](../api/autonomous.md): REST reference
