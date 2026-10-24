# Pi worker balancing across compute nodes: proposal

Date: 2026-10-24

Status: proposal. It records what is wrong, what the operator has already decided, and the
questions still open. It decides nothing new. Later work starts only after the operator confirms
it (Decision Interview Protocol in `AGENT.md`).

Scope: `internal/server/capacity.go`, `cmd/datawatch/capacity_wiring.go`,
`internal/server/api.go`, `internal/llm/backends/pi/backend.go`, `internal/compute/node.go`,
`internal/config/config.go`, `internal/server/compute.go`, `internal/server/web/app.js`,
`docs/howto/automata-capacity.md`.

## Problem

Two GPU machines are meant to serve pi workers as one pool, but the code treats an LLM entry's
`compute_nodes` list as an ordered failover list whose only live member is the first. Every
place below was read in the current tree.

- **Pool lookup uses only the first node.** `resolvePoolsForBackend` in
  `internal/server/capacity.go` appends `"node:"+l.ComputeNodes[0]` and nothing else, so a
  PRD-scoped capacity card and any admission preview see one node pool for the whole list.
- **The ledger key uses only the first node.** `engineNodes` in
  `cmd/datawatch/capacity_wiring.go` keeps the first `ComputeNodes` entry of the first LLM of
  each kind, and the `keys` closure in `wireCapacity` returns that one node. A session is
  admitted against `node:<first>` whatever the second node's load.
- **Session start uses only the first node.** The start handler in `internal/server/api.go`
  resolves `llm.ComputeNodes[0]` when no node is named (the `case len(llm.ComputeNodes) > 0`
  branch) and again for a backend arriving by name (`entry.ComputeNodes[0]`). Only an explicit
  `compute_node` in the request reaches another entry of the list, and the executor does not send one.
- **The node address reaches only opencode workers.** The block that turns the node address
  into a provider URL is gated on `isOpenCodeBackend`, which is true for the opencode and
  opencode-acp kinds alone. The pi backend in `internal/llm/backends/pi/backend.go` builds its
  command from `--model` and nothing else (`buildLaunchCmd`, `buildInteractiveCmd`), so pi
  resolves its host from its own provider table, which on this machine is the local one. A pi
  worker therefore never runs on the second machine, whatever node the ledger chose.
- **Node pools show 0 held while workers run.** A lease takes `node:<name>` only when
  `autonomous.LocalEngineGate` (`internal/autonomous/local_engine.go`) passes: the backend is
  in its local-engine list and the model carries a provider prefix that `engineNodes` resolves.
  A bare model name, or a prefix naming a kind with no compute node, takes no node lease, so
  the pool reads 0 held while the session runs. The start handler already documents bare
  names reaching it from per-task model overrides.
- **Two engines share one GPU.** The node pool counts sessions, not engines or GPUs. Its limit
  comes from `autonomous.max_engines_per_node` for a node that hosts an engine and from the
  node's `max_concurrent_sessions` otherwise (`syncPools`). Nothing ties an engine to a GPU,
  so two engines on one machine are separated only by that session count.
- **Capacity is per entry, not per node.** The only per-node figure is the single session
  ceiling in `internal/compute/node.go` (`MaxConcurrentSessions`, 0 = unlimited). The config
  seed type `ComputeNodeYAML` in `internal/config/config.go` has no such field.

## Already decided

These are the operator's decisions on record. They are restated here without change and are
not reopened by this proposal.

- The two GPU machines are one pool.
- At most one engine per GPU at a time.
- The supervisor never pins a machine to a lane.
- The node with the highest scheduling priority wins when several nodes have a free slot.
- A node that is unloaded or unreachable is loaded and waited on. A failed load raises an
  error alert and fails over to the other node when one exists. A one-node entry behaves
  exactly as today, with no new setting.
- The `compute_nodes` list on an LLM entry is the pool: no new flag and no second entry.
  Capacity is per node.
- Slots per node are derived from observer data (GPUs and free memory against the model). An
  operator value per node overrides the derived figure. With neither, a node has 1 slot.
- Verifier and guardrail judge sessions take node leases the same way workers do.

## How the pi worker reaches its node

This is an implementation note, not a question.

`pi --help` was run, along with `pi config --help` and `pi auth --help`. The help lists no flag
that sets a provider base URL or host. `--provider` and `--model` only choose among providers
pi already knows, and `pi config` toggles package resources in its settings file (global and
project-local); it is not documented as a way to repoint an endpoint. The one documented
setting that moves where pi reads its configuration is the environment variable
**`PI_CODING_AGENT_DIR`** ("Config directory", default `~/.pi/agent`). `PI_CODING_AGENT_SESSION_DIR`
and `--session-dir` move only session storage, so they do not help.

Mechanism the later tasks will use: at launch, for each pi session, write a small per-session
override of pi's provider table that points the model's provider at the chosen node's address,
place it in a per-session directory, and select that directory through `PI_CODING_AGENT_DIR` on
the launch command in `buildLaunchCmd` and `buildInteractiveCmd`. This is the same idea as the
provider block the start handler already writes for opencode workers, applied to pi.

What could not be confirmed from the command line: the file name inside that directory that
holds the provider table, and its schema, are not shown by any help output, and the
environment-variable route was not exercised because this task may write only under the
project directory. The first later task must confirm both by reading pi's own documentation,
and must also settle what else a fresh configuration directory needs copied in (credentials
and settings that the default directory supplies today). If the table cannot be overridden
this way, the fallback has to be raised with the operator before any code is written.

## Open points

Both are recommendations only. Nothing here is decided until the operator answers.

### 1. Capacity card layout beyond the node each worker holds

Today the card in `internal/server/web/app.js` (`capacityPoolsHtml`, `capacityHoldersLine`)
draws one bar per pool and lists holders by short id under it, and
`docs/howto/automata-capacity.md` ("Capacity and budget card") describes exactly that. Showing
the node each worker holds is the minimum: the node name beside each holder.

1. **Minimum only.** Put the node name beside each holder; keep the pool bars as they are.
   Smallest change, one new string, no new layout.
2. **Minimum plus a per-node row.** Add one row per node of the pool with its slots used of
   slots available and its priority, so two nodes read as one pool with two members.
3. **Minimum plus a pool summary line.** Add one line above the node bars with the total free
   slots across the pool and which node the next worker would take.

Recommendation: option 1 now, with option 2 considered once slots come from observer data.
Reason: the node name beside the holder answers the reported confusion (workers running while
a pool reads 0 held) and needs no layout change, which keeps the mobile follow-up small; the
extra rows are worth their space only after the slot figures are real.

### 2. Automatic slot counting when a GPU is busy with another program

Node detail in `internal/server/compute.go` (`handleComputeNodeDetail`) returns the observer's
snapshot, including GPU figures. `gpuNodeBackpressure` in `cmd/datawatch/capacity_wiring.go`
already blocks a node pool while cached GPU utilisation is at or over
`autonomous.capacity_gpu_util_pct`, but it looks at utilisation only, not memory, and it is off
at 0.

1. **Ignore it.** Count slots from the GPUs and the memory the node declares or reports as
   total. Simple and stable, but a node can be given more workers than its free memory holds.
2. **Subtract the memory already in use.** Count slots from free memory, so a program holding
   memory reduces the slots. Matches the decided rule (free memory against the model) but the
   figure moves while workers load, and the count can drop below the workers already running.
3. **Count the GPU as unavailable with a wait reason.** When another program holds more than a
   set share, the node offers no slots and waiting workers show why. Predictable, but coarse:
   one small foreign process idles the whole node.

Recommendation: option 2, with a floor so running workers are never counted out from under
themselves. Reason: it follows the operator's decision that slots come from free memory against
the model, and it degrades smoothly. Option 3 can be added later as the answer when free memory
cannot hold even one worker.

## Configuration surfaces for the per-node slot override

Where the operator sets the per-node slot value, as the files stand today:

- **Config file node record** (`ComputeNodeYAML`, `internal/config/config.go`): no session
  ceiling field exists, and the seed conversion in `cmd/datawatch/main.go` does not carry one.
  A file-seeded node cannot set it.
- **Node record** (`Node` in `internal/compute/node.go`): `MaxConcurrentSessions`, YAML and
  JSON name `max_concurrent_sessions`, 0 = unlimited, validated as not negative.
- **REST** (`internal/server/compute.go`): create and update take the node record as JSON, so
  `max_concurrent_sessions` already works.
- **MCP**: `compute_node_update` has a `max_concurrent_sessions` argument; `compute_node_add`
  does not, so a node cannot be created with a ceiling through MCP.
- **CLI** (`cmd/datawatch/cli_compute.go`): the add and update commands have flags for
  capacity, priority and pinning but none for the session ceiling.
- **Comm channel** (`internal/router/compute.go`): `parseKVPairs` has no case for it, so the
  default branch would send the value as text and a numeric field would not accept it.
- **PWA node form** (`internal/server/web/app.js`): the edit form already has a
  *Max concurrent sessions* input; the add flow does not.
- **Mobile app follow-up**: the app mirrors the node form, so a new field needs an issue in the
  mobile repository per the Mobile App Sync Rule in `AGENT.md`.

Reuse or new field? Reuse would make 0 mean two things: today 0 is "unlimited" and the value
is a ceiling that applies only when smaller than `autonomous.max_engines_per_node` on a hosting
node, while the decided rule needs 0 to mean "derive from observer data". Any node whose ceiling
is set today would silently turn into an override.

Recommendation: add a new field and keep the existing ceiling as a hard cap on top of it. Name:
**`session_slots`** (YAML and JSON), whole number, 0 = derive from observer data, validated as
not negative, with the effective count never above `max_concurrent_sessions` when that is set.
Reason: it keeps existing nodes behaving as they do and lets the two meanings stay separate.
Whichever way the operator answers, the field must be added to `ComputeNodeYAML` and the seed
conversion, and given a CLI flag and a comm channel case, because those two surfaces cannot set
the existing ceiling today.

## Operator decisions

Pending operator confirmation.

## Parity surface

Each surface that will show or set the new behaviour, following the Mobile-Parity Rule and the
configuration accessibility rules in `AGENT.md`:

- **REST**: the node record and node detail carry the slot override and the derived slot count;
  the capacity snapshot names the node each lease holds; `openapi.yaml` updated to match.
- **MCP**: `compute_node_add` and `compute_node_update` take the slot override; `capacity_status`
  and `compute_node_detail` return the node per holder and the derived count.
- **CLI**: a flag for the slot override on the compute node add and update commands; the
  `capacity` command prints the node per holder.
- **Comm channel**: `parseKVPairs` recognises the slot override as a number; the `capacity`
  reply names the node per holder.
- **YAML/config**: the field on `ComputeNodeYAML`, carried by the seed conversion, with an
  entry in `docs/config-reference.yaml`.
- **PWA**: the node form (add and edit) sets the override; the capacity card shows the node
  beside each holder (open point 1 decides anything more).
- **Mobile app follow-up (Android and iPhone/iOS)**: capability parity is required, so an issue
  is filed in the mobile repository for the new node field and the holder-node display, as the
  Mobile App Sync Rule requires.
- **Docs**: `docs/howto/automata-capacity.md` (setup, the card section and pitfalls),
  `docs/config-reference.yaml`, `docs/operations.md`, `docs/testing-tracker.md`, the changelog,
  and the doc index, with no internal tracker ids in operator-facing text.
