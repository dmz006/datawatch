# BL428 — Concurrency control & usage-budget design

**Date:** 2026-10-14
**Version:** v9.0.75 at time of writing (`var Version` in `cmd/datawatch/main.go`); implementation target TBD / next minor
**Status:** Phases 1–3, 5 and 6 implemented (Phases 1–2 shipped in the v9.4.0/v9.5.0 development builds, live smoke 2026-10-18 in `tests/live-smoke/REPORT.md`; Phase 3 complete 2026-10-22 in the unreleased development build; Phases 5 and 6 implemented 2026-10-22 in the unreleased development build, checklists below §Phases); Phase 4 not recorded as done in this doc (this doc's design pass otherwise unchanged)

Terminology note: operator-facing text says **Automaton/Automata**; the code
and this plan say **PRD** for the same object (`internal/autonomous`). The
multi-Automaton orchestration object designed in BL431 is called an
**Automation** (capital A). The Automation design-pass PRD was created in the
daemon under the title "BL430 — Automation…" (record `c187962a`), but the
backlog number is **BL431**. BL430 is the verification-LLM override that
already shipped in v9.0.72 (PRD `4d0ad909`). They are different items.

## Scope

**In scope:** a design for two separate concurrency-safety mechanisms and a
usage-budget throttle for the autonomous executor, grounded in what the code
does today:

- `internal/autonomous/manager.go`, `internal/autonomous/executor.go`,
  `internal/autonomous/api.go`: the executor goroutine for each PRD, per-PRD
  task fan-out, and the dead `max_parallel_tasks` knob.
- `internal/autonomous/capacity.go`, `internal/capacity/ledger.go`,
  `cmd/datawatch/capacity_wiring.go`: the existing admission ledger. This is
  the proposed home for the inference cap.
- `internal/autonomous/ci_watch.go`, `internal/autonomous/rules_review.go`,
  `internal/autonomous/guardrail_registry.go`,
  `internal/autonomous/verification_script_exec.go`: what each phase reads
  and writes.
- `internal/session/manager.go`, `internal/session/cooldown.go`,
  `internal/session/tracker.go`, `internal/session/state_engine.go`: how
  rate limits are detected today and what reacts to them.
- `internal/config/config.go`, `internal/server/api.go`,
  `internal/mcp/autonomous.go`, `internal/server/web/app.js`: config-surface
  plumbing.

**Out of scope:** any code. The Automation coordinator (BL431). Lifecycle
push notifications (BL427). Multi-backend budget logic (see the Scope
boundary section). Cost-observability dashboards (BL405 Phases 4/6/10,
BL414's observability half).

## 1. Terminology and resource classes

### Unit of execution: the task, not the story

An **Automaton (PRD)** contains **stories**, and each story contains
**tasks** (`internal/autonomous/models.go`). The executor's unit of work is
the **task**. `Manager.Run` flattens every story into one task list
(`flattenTasks(prd)` then `topoSort(tasks)`, `internal/autonomous/executor.go`
~235). It then calls `executeOne` once per task. `executeOne` spawns exactly
**one worker session per task attempt** (`spawn(ctx, SpawnRequest{TaskID: …})`,
executor.go ~861).

Stories do no execution of their own. They are used for:

- grouping,
- status roll-up (`StoryInProgress` is set when a story's first task starts,
  executor.go ~300),
- per-story guardrails (`runPerStoryGuardrails`, executor.go ~574), and
- `depends_on` ordering between stories.

**Granularity: the operator's original "story" framing maps to the task.**
Wherever the operator said "two stories running at once", the code-level
meaning is two **tasks**, each holding its own worker session. Every
mechanism in this doc admits, locks and throttles per task, not per story.

### Resource classes

| Class | What it is | Who draws it today |
|---|---|---|
| **ACTIVE-INFERENCE slot** | A live LLM worker or judge session that is generating tokens or waiting on the model. Bounded by Claude account usage, or by GPU/VRAM on a compute node. | Task worker sessions, decompose sessions, verifier/guardrail judge calls, and council calls. |
| **PROJECT-TREE WRITE lock** | Exclusive right to change the working tree and git index/refs of one canonical `project_dir`. | Task workers (edit, `git add`, `git commit`, push), `verification_script` side effects, `scan_fix`, and operator hand edits. **Not modeled at all today.** |
| **IDLE-EXTERNAL-WAIT** | Waiting on something outside datawatch (CI on GitHub, a release run) with no inference running. | `ci-watch` polling `gh run list` (`ci_watch.go` `pollCIRuns`). Also a task worker told to "wait for CI", which today holds a full inference slot while doing so (see worked example ii). |
| **USAGE-BUDGET draw** | Consumption of a time-windowed allowance: a Claude subscription's rolling 5-hour and weekly windows, or a self-imposed $/day/week/month ceiling on API billing. | Every phase that calls an LLM. Concurrency does not create usage, but it spends a fixed bucket faster. |

## 2. Phase-level concurrency matrix

Phases:

- **D** — decompose/planning.
- **T** — task execution.
- **V** — verification: the verifier diff plus the `verification_script`.
- **RR** — rules-review (PRD-completion guardrail).
- **CW** — ci-watch (PRD-completion guardrail).
- **S** — scan: the `scan` guardrails and `autonomous_prd_scan`.
- **M** — operator manual edits.

Evidence for each phase:

- **D** reads the tree. Its only designed write is the content-addressed
  `.decompose-output-<sha256(spec)[:8]>.json` in `project_dir`
  (`cmd/datawatch/main.go` ~4204), which `runOneShotJSONSession` moves into
  PRD-scoped storage (`internal/autonomous/scratch.go`). Because the filename
  is a hash of the spec, two PRDs never share an output file.
- **T** edits files, then runs `git add`/`git commit`, and sometimes push.
  This is the only phase designed to write the tree.
- **V** runs `git diff` over `pre_task_sha..HEAD` and runs the unit's
  `verification_script` inside `project_dir`
  (`runVerificationScript(script, projectDir)`,
  `internal/autonomous/verification_script_exec.go:32`, called from
  `prepareScriptEvidence`, executor.go ~1503). Scripts routinely run
  `go build`/`go test`/`docker build`, which write build artifacts, caches
  and test fixtures. These writes are write-ish side effects, not edits to
  the work itself.
- **RR** is an LLM judge over the PRD diff plus the rules file
  (`rules_review.go` `runPRDCompletionRulesReview`). It is read-only by
  design. A `contentFixVerdict` does not write: it synthesizes a new **task**
  (`synthesizeCorrectiveWork`, executor.go ~629), and that task is then a T.
- **CW** reads `HEAD` and the origin URL from `project_dir`
  (`defaultCIGit(ctx, projectDir)`, `ci_watch.go:133`), then polls
  `gh run list --commit <sha>` (`ci_watch.go:162`). It never writes the tree
  and never calls `admit()`, so it holds **no capacity lease** while
  polling. Only its failure-judge step (`ciWatchJudge` →
  `runGuardrailWithInfraRetry`) uses inference.
- **S** runs `scan.Run(projectDir, …)`
  (`guardrail_registry.go:155` `invokeScanGuardrail`;
  `manager.go:3155` `RunScan`) with deterministic SAST, secrets, deps and
  project-rules scanners. It reads the tree only. `scan_fix` writes, and is
  classed with T.
- **M** writes anything, any time. Nothing in datawatch can see it before
  it happens.

All verdicts below assume the **same canonical `project_dir`** unless the row
says otherwise. Two phases on **different** `project_dir`s are always safe to
overlap for the tree; they still compete for inference and usage budget
(mechanisms 3b and 4), which this table does not cover.

| Pair | Verdict | Read/write evidence |
|---|---|---|
| D × D | safe to overlap | Both only read. Output files are content-addressed per spec hash (main.go ~4204), so they cannot collide. |
| D × T | depends on whether T is mid-write | D reads while T writes. D cannot corrupt T, but D may plan against a half-edited tree or a commit that is about to land, so the plan can be stale. Acceptable when D's PRD runs later anyway. Unsafe for D's plan quality if both PRDs touch the same files. |
| D × V | depends on V's script side effects | D reads, and V's script may leave build output or caches in the tree. Harmless unless those artifacts are untracked and D's prompt reads them. |
| D × RR | safe to overlap | Both read only. |
| D × CW | safe to overlap | CW reads HEAD and calls GitHub; D reads files. |
| D × S | safe to overlap | Both read only. |
| D × M | depends on whether M is mid-edit | Same as D × T. Planning against an operator's uncommitted edit is a stale-read risk, not a corruption risk. |
| T × T, same project_dir | **unsafe** | Both edit the same tree and contend on `.git/index.lock`. Each one's `pre_task_sha..HEAD` range includes the other's commits. Live evidence: BL425's incident, where a task's `git add -A` swept an unrelated edit, and BL426 task `18bc04c1`, whose `files_touched` lists BL423's `internal/mcp/autonomous.go`, `internal/mcp/guardrail_tools.go`, `CHANGELOG.md` and others, because BL423's commits landed in the same tree during its window. |
| T × T, different project_dir | safe to overlap | Disjoint trees and disjoint git repos, or a worktree (see 3a). Shares only inference and budget. |
| T × V | **unsafe** (same tree, different PRDs); depends on the diff range for the same PRD | V's diff is `pre_task_sha..HEAD`, so a concurrent T's commits get judged as this task's work. Live evidence: BL426 task `18bc04c1`'s own verifier wrote "the supplied diff also includes CHANGELOG/main.go v9.0.64 … from earlier commits". V's script may build or test against a tree T is changing. Within one PRD, the sequential path never overlaps them. |
| T × RR | depends on whether T belongs to the same PRD | RR judges the PRD's full diff. Another PRD's T committing mid-review pollutes that diff. RR only runs after all of its own PRD's tasks are terminal (executor.go ~613). |
| T × CW | depends on who moves HEAD | CW resolves `HEAD` of `project_dir` (`ci_watch.go:133`). If another PRD's T commits or pushes in the same tree, CW watches **that** PRD's commit and credits or blames the wrong Automaton. |
| T × S | depends on whether a scan finding is judged against a half-written tree | S reads only, but a scan of a tree mid-edit can report transient findings, or miss findings in work not yet written. |
| T × M | **unsafe** | This is BL425's incident: an operator was hand-editing the tree where a direct-mode task ran `git add -A`. BL425 (`file_drift.go`, v9.0.75) **detects** this after the fact; it does not prevent it. |
| V × V | depends on the scripts | Two judge reads are safe. Two scripts that both `go build` or `docker build` in the same tree can race on shared outputs. |
| V × RR | safe to overlap | Both judge-read. RR runs only after V for the same PRD finishes. |
| V × CW | safe to overlap | CW does not write. V's script does not move HEAD. |
| V × S | safe to overlap | Both read; a script's build output may appear in a scan as untracked noise. |
| V × M | depends on M | A script run against an operator's in-progress edit gives evidence that does not describe the task. |
| RR × RR | safe to overlap | Read-only judges. Each holds an inference slot. |
| RR × CW | safe to overlap | Sequential within one PRD (executor.go ~628 → ~650). Across PRDs both are read-only. |
| RR × S | safe to overlap | Read-only. |
| RR × M | depends on M | Same as V × M: the judgment describes a tree that is still changing. |
| CW × CW | safe to overlap | Two PRDs polling GitHub. Wrong-commit attribution only happens if they share a tree and HEAD moves, which is the T × CW case. |
| CW × S | safe to overlap | Neither writes. |
| CW × M | depends on whether M commits or pushes | An operator commit in the tree moves HEAD under CW. |
| S × S | safe to overlap | Deterministic readers. |
| S × M | safe to overlap (findings may be stale) | Reads only. |
| M × M | out of datawatch's scope | Not observable. |

The pattern: **every unsafe or depends-on cell involves a tree writer (T,
V's script, M) or a HEAD reader (CW, V's diff, RR's diff) in the same
`project_dir`.** No cell is unsafe for any reason other than the shared tree.
That is why mechanism 3a is keyed by tree and is separate from the inference
cap.

## 3. Two separate mechanisms

### What gates concurrency today (the evidence)

- **`max_parallel_tasks` / `MaxParallelTasks` is dead config.** A repo-wide
  grep finds it in seven places, and every one is plumbing:
  - `internal/autonomous/manager.go:46`: the field.
  - `manager.go:261`: default 3.
  - `internal/config/config.go:1863-1864`: commented "per-PRD in-flight
    worker cap (default 3)".
  - `internal/server/api.go:5484`: GET.
  - `internal/server/api.go:6729-6731`: PUT.
  - `cmd/datawatch/main.go:4074` and `4167-4168`: bridge and default.
  - `internal/mcp/autonomous.go:53` and `85`: described as "Per-PRD worker
    cap (default 3)".
  - `internal/server/web/app.js:13942` and `28648-28649`: PWA field.

  **No executor code reads it.** Its own descriptions in MCP, config.go and
  the PWA are false. The live daemon has `autonomous.max_parallel_tasks: 1`,
  and that setting changes nothing.
- **`MaxConcurrentTasks` (BL370) is the real knob, but it is per-PRD only.**
  `executor.go:248-256` resolves the limit as follows: per-PRD
  `prd.MaxConcurrentTasks` wins; otherwise the global `m.cfg.MaxConcurrentTasks`
  applies; `<= 1` means sequential. The limit bounds fan-out **within one
  PRD's own `Run`**.

  The global field also has a gap: `config.AutonomousConfig` has **no**
  `MaxConcurrentTasks` field (zero hits in `internal/config/config.go`), and
  the `amgrCfg` literal in `cmd/datawatch/main.go` (~4072) does not set it. So
  the global default cannot be set from YAML and always starts at 0
  (sequential). The per-PRD override has a full surface:
  `SetPRDConcurrency` (`manager.go:3332`), REST at
  `internal/server/autonomous.go:1522-1543`, and the PWA at `app.js:13498`.
- **Nothing gates across PRDs.** `API.Run` (`internal/autonomous/api.go`
  ~305-345) starts one `go func(){ a.M.Run(ctx, id, …) }()` per PRD ID. Its
  only guard is the `runCancels[id]` dedupe, which stops the **same** PRD
  from getting two executor goroutines. Two different PRDs (or ten) each get
  their own goroutine, and each walks its own tasks with no shared lock.
  Approving a PRD does not start it: the `approve` REST case
  (`internal/server/autonomous.go` ~473-479) calls `Manager.Approve` and
  returns. `run` is a separate action.
- **The capacity ledger exists, and it gates inference-ish slots, not
  trees.** `executeOne` calls `m.admit(ctx, prd, t, backend, model)`
  (executor.go ~854) before every spawn, and releases with
  `defer m.releaseCapacity(t.ID)` (executor.go ~779). The lease is therefore
  held across spawn, the worker run, **and** verify, until `executeOne`
  returns. `admit` (`internal/autonomous/capacity.go` ~125) acquires the
  pools returned by `CapacityKeysFn(backend, model)`. In
  `cmd/datawatch/capacity_wiring.go:83-96` those pools are:
  - `host`, limited to `session.max_sessions − reserved_interactive`, which
    is 10 − 1 = 9 on the live daemon. Untracked interactive and
    `autonomous:decompose` sessions are added through the `External` callback
    (capacity_wiring.go:55-66).
  - `llm:<registry name>`, limited by the LLM's `max_inflight`
    (`internal/inference/llm.go:124-126`, "caps autonomous sessions in flight
    on this LLM across all PRDs (0 = unlimited)").
  - `node:<first compute node>`, limited by the node's
    `max_concurrent_sessions` (`internal/compute/node.go:230-232`), plus
    optional GPU-utilisation backpressure.

  The verifier takes a **second** lease on the same pools as holder
  `verify:<task>` (`verifierCapacityAdmit`, `cmd/datawatch/main.go` ~4927),
  and council does the same. Waiting is visible as `TaskWaitingCapacity`,
  bounded by `capacity_wait_timeout_seconds` (default
  `defaultCapacityWait` = 4h, capacity.go:26). A full session cap is retried
  every `capRetryPause` (15s) by `capacityRetrySpawn`. `ErrWorkerStalled`
  (capacity.go:25) keeps a stall inside the retry budget.
  `EnsureClaudeCode` (`internal/inference/store.go:173`) guarantees a
  `claude-code` registry entry, so **an `llm:claude-code` pool already
  exists and already applies across all PRDs**. It is unlimited only because
  `max_inflight` defaults to 0.
- **The ledger cannot host a tree lock as-is.** Its pools are counters (N
  holders up to `Limit`). It has no notion of "this holder has uncommitted
  work". Its lease lifetime is tied to one `executeOne` call. Locks must
  instead be keyed by a canonical path and must outlive one task, for
  example across an IDLE-EXTERNAL-WAIT with unpushed work. A `Limit: 1` pool
  named `tree:<canonical path>` could *represent* the lock, and that is the
  recommended representation for visibility (it appears in
  `capacity_status`). But acquisition and release need their own rules,
  covered in 3a.

### 3a. Project-tree write serialization (prevention)

**Key:** the canonical project tree.
`filepath.EvalSymlinks(filepath.Clean(prd.ProjectDir))`, then the
`git rev-parse --show-toplevel` of that path, so that `/home/user/workspace/datawatch`,
a symlink to it, and a subdirectory of it all map to the same key.

**Worktree mode (BL407).** A PRD with `Git.Branch` set runs in its own
`git worktree` under `~/.datawatch/prd-worktrees/<id>`
(`internal/autonomous/worktree.go:44-46`) on its own branch
(`branchNameFor`, worktree.go:23). Its checkout and index are separate, so
for the purposes of this lock it is a **distinct tree**: its key is the
worktree path and it does not serialize against the main checkout. It does
still share the repository's object store and `refs/`:

- Concurrent `git commit` in two worktrees is safe, because git's ref
  updates are atomic and lock-file guarded per ref.
- Two worktrees on the same branch is impossible, because git refuses it.
- `git gc`/`prune`, and pushes that touch shared remote-tracking refs, are
  the residual shared state. They are short and git's own locking covers
  them.

**Recommendation:** worktree PRDs take only a *shared* lock on the main
repository key, a reader-style lock that blocks nothing but is visible. They
take an *exclusive* lock on their own worktree key. Repo-wide maintenance,
such as orphan-worktree cleanup (B118), takes the main key exclusively. BL407
Phase 4's existing agents-side workspace lock (`IsWorkspaceLockError`,
capacity.go ~185) already does the cluster-dispatch equivalent for one
`(project_profile, branch)` pair. 3a is the local-tree counterpart and should
reuse that wording and the same retry-as-wait behavior.

**Who must hold the exclusive lock:**

- T: from spawn until the task's commit, and push if any, has landed, or
  the task ends.
- V: while its `verification_script` runs, because the script writes build
  output. A pure-diff verifier with no script does not need it.
- `scan_fix` and synthesized corrective work, because these are T.

**What is not gated:** D, RR, CW, S and the verifier's judge read are never
gated by the exclusive lock. They are readers. The depends-on cells in
section 2 are stale-read risks, which this design accepts, and CW's
HEAD-attribution risk is addressed separately below.

**CW HEAD attribution.** Fix this at the source rather than with the lock.
Record the SHA the PRD's own last task produced (the executor already has
`pre_task_sha`, and can record a `post_task_sha`), and have CW watch that SHA
instead of reading `HEAD` at completion time. This is called out because the
lock alone cannot fix it: CW runs after the PRD's writers are done, when the
lock is free for other PRDs.

**Operator manual edits cannot be enforced.** Datawatch does not see an
operator's editor. The design is:

- **(i)** An advisory lock file, `.datawatch/tree.lock`, in the canonical
  tree, git-ignored, holding the holder's PRD/task ID and start time. It is
  written on acquire and removed on release. Operators and their own chat
  sessions can check it, and a CLI `datawatch autonomous tree-lock status`
  prints it. Nothing stops a human from ignoring it.
- **(ii)** A PRD-level and global option to *refuse to start* a task when
  the tree is already dirty with files outside the task's plan. This uses the
  same pre-task dirty capture that BL425 widened (`PreTaskDirtyFiles`), but
  applied at admission instead of after the fact.
- **(iii)** BL425's foreign-file-drift check stays as the after-the-fact
  detector.

**3a prevents collisions between datawatch writers; BL425 detects what 3a
cannot prevent (humans, and anything outside datawatch).** These two are
complementary and neither replaces the other.

**Config:** `autonomous.project_write_serialization: per_project_dir | off`.
Global, with a per-PRD override; default `per_project_dir`. See 7b.

### 3b. Backend / compute inference-load cap

There are two different limits:

- A **Claude** inference limit is an **account-usage** limit, shared by
  every session on one subscription or API key, wherever it runs.
- An **Ollama/OpenWebUI** inference limit is a **hardware** limit: GPU and
  VRAM per `compute_node`.

These need **separate knobs, not one integer.** Ten parallel tasks across
three different repos on a Max-tier Claude account may be fine for the
trees and wrong for the weekly window. Two tasks on one 24 GB GPU may be
wrong for VRAM and irrelevant to any account.

**Do not invent a new mechanism; reuse the ledger pools that already
exist:**

- **Per backend:** `llm:<name>` with the existing registry field
  `max_inflight`. It is already enforced by the ledger across all PRDs and
  already has registry surfaces. The missing piece is a **default**: today
  it is 0, meaning unlimited, for `claude-code` too. Section 4d proposes
  deriving the default from a plan tier.
- **Per compute node:** `node:<name>` with the existing
  `max_concurrent_sessions` plus `capacity_gpu_util_pct` backpressure. These
  are already right for Ollama.
- **Gaps to close in the ledger:**
  - Decompose sessions are only counted into `host` via `External`. They
    never acquire `llm:`/`node:`, so a decompose can exceed `max_inflight`.
    It should `admit` like a task.
  - The verifier's second lease means one task in verification counts
    twice against `host`/`llm:`. Decide whether the verify lease should
    *replace* the task lease while the worker is idle (recommended:
    task-lease → verify-lease hand-off) rather than stack on it.
  - IDLE-EXTERNAL-WAIT must release the `llm:`/`node:` leases (section 6.ii).

The backend/model pair is already the ledger's key input
(`CapacityKeysFn(backend, model)`), so the **model is a first-class
dimension** for later per-model pools. The ledger request already carries
`Model`.

### 3c. Federation: shared compute nodes across peers

Real evidence, read live 2026-10-14 from this host's own registries:

- `compute_node_list` returns 4 nodes: `datawatch` (ollama), `datawatch-openai`
  (openai-compat, 1× B200 GPU), `local-node` (ollama, 1× 32 GB GPU), and
  `verdict` (ollama, `127.0.0.1:11435`, `tags: ["docker", "verdict-model",
  "imap-mcp", "shared-gpu-with-local-node"]`).
- That last tag is a **free-text string an operator typed by hand.** Nothing
  in the capacity ledger (3b's `node:<name>` pools) reads it. `node:verdict`
  and `node:local-node` are enforced as two fully independent pools
  (`capacity_status` tracks each `limit`/`held` separately) even though the
  operator's own tag says they are the same physical GPU. Two tasks each
  admitted "within limit" on their own node can still collide on hardware
  that cannot run both.
- `federation_peer_list` returns one peer (`ralfthewise`, `capabilities:
  ["full-control"]`). Peer config has no field for "this peer's compute node
  X is the same hardware as my compute node Y." Each daemon's `ComputeNode`
  registry is local and opaque to every peer.

This is the same gap at two scopes: within one daemon (the `verdict` /
`local-node` case today, worked around by an unread tag) and across
federated peers (two daemons' registries pointing at the same box under
different names/addresses, with neither aware the other exists).

**Discovery mechanism — fingerprint reconciliation, not raw disclosure**
(operator requirement 2026-10-14: peers must be able to tell whether they
share a resource *without exchanging the resource's own identifying
details*). For each compute node, each daemon computes a keyed hash
(`HMAC-SHA256`, keyed by a per-peer-pair secret — reuse the existing
federation auth material rather than invent a new secret channel) over the
most stable self-identifying value the resource itself can expose: a GPU
UUID where the node's driver exposes one (e.g. via an `nvidia-smi`-style
query the compute node answers), falling back to a locally generated,
disk-persisted node-instance ID when no hardware UUID is available. Critically,
**this value must come from querying the node itself**, not from the
daemon's own locally-configured name/address for it — two daemons pointing
at the same physical Ollama install over a LAN IP and a Tailscale IP
respectively would otherwise hash two different strings for one real node,
and a mismatch there is a false negative, not a safe default. Periodically
(piggybacking on the existing federation heartbeat/`federation_peer_test`
cadence — no new polling loop), each daemon pushes its list of `{resource_type,
local_name, hash}` tuples to each peer it is paired with for this purpose,
over the existing authenticated federation transport. A peer comparing
incoming hashes against its own sees a match only when the underlying
resource is genuinely the same, without either side ever learning the
other's raw GPU UUID, hostname, or address.

**A hash match is a detected candidate, not an automatic merge.** It is
surfaced to the operator (or an auto-accept policy, configurable) before the
capacity ledger actually starts treating the two nodes as one combined pool.
This mirrors BL428's existing stance ("explicit config over inference")
while removing the toil of hand-typing `peer:node` pairs — the operator
confirms a *discovered* pairing instead of composing one from scratch.
Confirmed pairs are recorded as a structured field (replacing today's
free-text tag), e.g. `ComputeNode.SharesHardwareWith []string`, entries being
bare node names for an intra-daemon pairing (`"local-node"`) or
`peer:<node>` for a cross-federation one (`"ralfthewise:gpu-box-1"`). Once
confirmed, the ledger computes `node:<name>` admission over the merged set,
not each name independently.

**Live coordination while sessions are actually running** (operator
requirement 2026-10-14: a confirmed pairing needs more than a periodic
fingerprint sync — the peers need to know about each other's *current* load
on the shared resource). The fingerprint exchange above answers "is this
shared," on a slow cadence; it is not the channel for "is it busy right
now." For that, reuse BL427's event-dispatch mechanism (PRD `fe2e2110`) once
implemented: a lease acquired/released on a confirmed-shared `node:<name>`
pool fires a lightweight cross-daemon event to every peer sharing that node,
so each side's own ledger reflects the other's current holders without
polling. Until BL427 ships, the fallback is a synchronous pre-admission
check over the existing federation peer HTTP path (same shape as
`federation_peer_test`) — higher latency, no new dependency, and it must
fail open (log and admit locally) rather than fail closed, so a peer being
briefly unreachable never stalls local work indefinitely; that fail-open
choice is an explicit trade-off, not a hidden default (see Open questions).

## 4. Usage-budget throttling

### 4a. Subscription allocation (Claude Pro/Max)

- Opaque and enforced by Anthropic. It has rolling windows, which the
  scraped wording calls a "5-hour limit" plus a "weekly limit". **Remaining
  quota cannot be queried.** Nothing in this codebase reads it, and the only
  signal is the CLI's own prose once a threshold is hit.
- Concurrency does not create extra usage. It **spends a fixed bucket
  faster**: three parallel tasks reach a 5-hour cap roughly three times
  sooner, and then **all** of them stall together.
- Plan tiers differ several-fold, so **no single safe default concurrency
  exists**. A default that is comfortable on one tier will exhaust a lower
  tier overnight.

### 4b. API / dollar billing

Self-imposed day/week/month ceilings **are computable**:

- Per-session `EstCostUSD` is live and accurate for claude-code. B98 changed
  it to scrape the real transcript, and `internal/session/cost.go` sums it at
  ~77-82.
- Rates come from `session.cost_rates` (BL6) via the
  `cost_summary`/`cost_usage`/`cost_rates` surfaces.
- The gap is the rollup to a PRD (BL414: `Decision.CostUSD` has zero
  writers) and to a time window.

### 4c. The shared concept, and BL414

Both models are the same thing: **a time-windowed usage ceiling, configured
per operator, that concurrency must not blow through early.**

- For subscription allocation, the meter is invisible and the limit is
  discovered by hitting it.
- For dollar billing, the meter is computable and the limit is chosen by the
  operator.

This is **not** BL414's framing. BL414 (docs/plans/README.md item 14) is
about *observability and minimizing total cost*: Decision rollup, dashboards,
and "token guardrails (budget ceilings, alerts, maybe hard stops)". Its own
text says it is "not yet designed" and must be scoped against BL405. BL405
(`docs/plans/2026-10-10-llm-enhancements.md`, Status "Planned — not started")
already plans the relevant pieces:

- **Phase 2:** an optional **rate-ceiling dimension per ledger pool**
  ("requests/tokens over a rolling window, alongside the existing
  concurrency `Limit`").
- **Phase 4:** per-call usage records.
- **Phase 9:** alert metrics `cost_burn_rate` / `token_budget_remaining`.

**Recommendation (one choice): BL428 absorbs BL414's budget-guardrail half.
BL414 keeps observability.** This doc supersedes "token guardrails / budget
ceilings / hard stops" in BL414's text. BL414 narrows to the Decision
`CostUSD` rollup, reporting and dashboards. The reasons:

1. A ceiling only does anything at an **admission point**, and this doc
   creates that point (the executor's `admit` and the dispatch gate in 4d).
   Designing ceilings in BL414 would need a second admission path or would
   depend on this one anyway.
2. One concept with two meters (allocation-reactive and dollar-computed)
   behind one admission check is simpler than two features that each almost
   cover it.
3. BL414 is undesigned, so absorbing its guardrail half costs no rework.
   Making BL414 grow a second "allocation mode" would force a cost-reporting
   feature to own dispatch throttling, which it has no code path for.
4. BL405 Phase 2's rate-ceiling pools are the natural *storage and
   enforcement* for the dollar meter: a `budget:<backend>` pool with a
   windowed ceiling. The follow-up PRD should implement that slice of BL405
   Phase 2, scoped to cost/usage, or land it first, rather than duplicate
   it. See open question 2.

**BL431 boundary:** BL431's Automation-level **BudgetPolicy** (a per-Automation
scoping of these same ceilings) is designed in BL431, not here. This doc
defines the base mechanics, meaning the ceiling, the meters, the admission
check and the backoff. BL431 scopes them to a named batch of work.

### 4d. Proactive and reactive mechanisms

**Proactive: account-tier configuration.** Add a registry field on the LLM
entry: `plan_tier: unset | pro | max_5x | max_20x | api` on `claude-code`,
and on any future subscription-type backend. When `max_inflight` is unset,
the tier supplies its default, plus an optional suggested model tier for
autonomous work (feeding BL422's results via the BL430 per-unit verification
override and the existing per-task/story LLM overrides).

- **Feasibility:** high. It is a static lookup feeding an existing ledger
  limit.
- **Caveat:** the tier→number table is operator policy, not something
  Anthropic publishes. The default for `unset` should stay
  conservative (`1`). The table must be overridable, and its values belong
  in open question 4.

**Reactive: daemon-wide back-off on a real rate-limit signal.** This is what
the code actually surfaces today:

- **Detection is screen-scraping.** `processOutputLine` in
  `internal/session/manager.go` (~6105-6125) lower-cases each output line
  shorter than 2048 characters and substring-matches it against
  `effectiveRateLimitPatterns()` (manager.go:222). That function returns the
  operator's `detection.rate_limit_patterns` if set, else the built-in
  `rateLimitPatterns` (manager.go:131-168).
- **That list is real, maintained prior art, not dead code.** Its comments
  record being patched as Claude's wording drifted:
  - v5.27.4's "5-hour limit reached" / "weekly limit reached",
  - BL240/v6.2.0's "claude usage limit" / "plan limit",
  - BL262/v6.11.3's "out of extra usage".

  `internal/session/ratelimit_parser_test.go` pins real prose formats,
  for example "You're out of extra usage · resets 11:50am
  (America/New_York)" (~121-150).
- **On a match, the reaction is to that one session only, and only from
  `StateRunning`:**
  - Set `StateRateLimited`.
  - Parse `RateLimitResetAt` with `parseRateLimitResetTime`
    (manager.go:5262).
  - `tracker.RecordRateLimit(resetAt)` (`internal/session/tracker.go:150`).
  - Auto-send `1`+Enter for "stop and wait".
  - Schedule a resume message at the reset time, or now + 60 minutes
    (manager.go ~6155-6166).
  - Fire `onRateLimitFallback` if a profile fallback chain is configured
    (`cmd/datawatch/main.go` ~1668).

  `StateRateLimited` is also a sticky state in
  `internal/session/state_engine.go` (~99).
- **There is no automatic global reaction.** The BL30 global cooldown
  (`internal/session/cooldown.go`, `ErrGlobalCooldown`) is only ever set by
  `SetGlobalCooldown`, which is called only from the manual REST/MCP handler
  `internal/server/cooldown.go:54` (`cooldown_set`). `Start` refuses new
  sessions only when that cooldown is active **and**
  `session.rate_limit_global_pause` is on (manager.go ~1656). The config
  comment ("hitting a rate limit … pauses new session creation",
  `config.go:1706-1709`) promises more than the code does: nothing calls
  `SetGlobalCooldown` on detection.
- **The autonomous executor does not react at all.** `internal/autonomous`
  has zero references to rate limits. A task whose worker goes
  `RateLimited` sits there holding its capacity lease. The one-shot wait
  loop's stall check keys on `waiting_input`, not `rate_limited`
  (main.go ~4592). Every **other** PRD's goroutine keeps spawning new
  claude-code tasks onto the same exhausted account. Each of those hits the
  same wall, and each costs a spawn and a wasted attempt. If an operator had
  set the cooldown by hand with the pause flag on, `Start` would return
  `ErrGlobalCooldown`. Because `IsCapacityError` only matches "max sessions"
  (capacity.go:178), that would likely surface as a spawn **failure**, not a
  wait. The follow-up must confirm this and fix it.
- **Not observable:** remaining subscription quota, how close the account
  is to the cap (the "Approaching usage limit" warning is the only early
  signal and is scraped the same way), and usage drawn by Claude sessions
  outside datawatch on the same account.

**Reactive design.** On the first `StateRateLimited` transition of any
session whose backend family is `claude-code`:

1. Mark `backoff:claude-code` with `until = RateLimitResetAt`, or a
   configured fallback when the reset time cannot be parsed.
2. Make the autonomous dispatch gate (in `admit`, before spawn) treat that
   as a **wait** (`TaskWaitingCapacity`, reason "usage limit — resets at
   …"), not a failure.
3. Do not affect sessions already running; they get their own
   per-session handling.
4. Audit the event.
5. Clear automatically at `until`, or manually via `cooldown_clear`.

Feasibility is high: the signal, the reset time and the wait state all exist
today. The work is wiring an `onStateChange` subscriber to a backend-scoped
flag that `admit` checks. The flag is keyed by backend so that a future
Ollama or API backend is unaffected.

**Pattern-drift risk (open item).** This doc did **not** verify the scraped
strings against today's live Claude Code CLI output; doing so requires
triggering a real limit. A pattern list that silently stops matching turns
the reactive mechanism off with no error. Shipped prior art shows that
misrecognized CLI states are a real and recurring class of bug: BL429
(v9.0.66, commit `507c0ccb`, PRD `c5774d3f` completed 2026-10-14) added
`selfUpdatePatterns` (manager.go ~205) so the "✔ Update installed ·
/restart to apply" footer is recognised as a benign passive notice. It is
never treated as a prompt or a rate limit and never sent a key. The
investigation, `docs/plans/2026-10-14-cli-self-update-dialog-recovery.md`,
found the real cause of the re-paste was the channel-ready heartbeat
re-delivering the task, not the footer. That fix is shipped and is not an
open gap.

The design should include **some** drift guard. The options, in increasing
cost:

- **(a)** A documented manual recheck cadence, with
  `detection.rate_limit_patterns_verified_at` recorded in config.
- **(b)** A staleness flag surfaced in `autonomous_status`/`get_stats` when
  `claude --version` has changed since that timestamp.
- **(c)** A periodic self-test that feeds captured real dialog fixtures,
  like the BL429 `testdata/claude_self_update_dialog.txt`, through the live
  matcher.

The recommendation is (a)+(b) in the follow-up, with (c) as a stretch. See
open question 3.

### 4e. Federation: shared cloud/billing accounts across peers

The subscription-allocation meter in 4a is **account-scoped, not
daemon-scoped**: a Claude Pro/Max plan or an API key belongs to a person or
org, not to one `datawatch` process. Two federated daemons (this host and
`ralfthewise`, per `federation_peer_list`) can be configured with the
**same** `claude-code` credentials — logged into the same account, or
holding the same API key — in which case each daemon's own `backoff:claude-code`
flag (4d) and `llm:claude-code` pool (3b) each track only their own half of
a quota the two are actually splitting. A daemon well under its own
`max_inflight` can still be the one that tips a shared 5-hour window over,
because the other daemon's usage is invisible to it.

**Same discovery mechanism as 3c, applied to accounts instead of hardware.**
Raw credentials must never cross the federation link — an API key or a
Claude Code session token is a secret, and a naive unsalted hash of a
*low-entropy* value (a plan-tier name, a short PIN) would be brute-forceable
offline even if only the hash is transmitted. The same `HMAC-SHA256`,
keyed by a per-peer-pair secret, over a **high-entropy** stable identifier is
safe here precisely because API keys and session tokens have enough entropy
that offline guessing is infeasible even from a disclosed hash — the keyed
HMAC is still the right default (defense in depth, and it scopes the hash to
one specific peer pairing so it cannot be replayed against an unrelated
third party). Where the backend exposes a stable, non-secret account
identifier instead (to be confirmed per backend — e.g. whether `claude`'s
local credential store exposes an account ID separately from the session
token itself), prefer hashing that over the credential. **Do not hash
anything whose entropy has not been checked first** — this is the one place
in this mechanism where getting it wrong has a real security cost, unlike
3c's hardware UUIDs, which are not meaningfully secret to begin with.

As in 3c, a hash match is a **detected candidate surfaced for operator
confirmation**, not a silent merge — a confirmed pairing is recorded as a
`billing_group` string (operator-chosen label) on the LLM registry entry
(`llm_list`'s `claude-code` record today; any future subscription-backed
entry later), shared by every LLM entry — on this daemon or a confirmed peer
— that the fingerprint exchange matched. Two entries sharing a non-empty
`billing_group` are treated by 4d's reactive back-off as **one shared
`backoff:<billing_group>` flag** instead of one `backoff:<backend>` flag per
daemon.

**Live coordination while sessions are running**, same requirement as 3c:
the first daemon to see a real `StateRateLimited` signal on a
`billing_group`-linked backend must tell every peer in that group *now*, not
on the next slow fingerprint-sync tick. Reuse BL427's dispatcher once it
ships (push); until then, a synchronous check at admission time (pull) with
the same fail-open-but-logged default as 3c. Propagation is therefore
eventually consistent, not transactional — a short window where one daemon
has not yet heard the other hit the limit remains possible, and the design
must say so rather than imply perfect coordination.

**BL431 boundary, same shape as 4c's:** this doc defines the base mechanics
(the fingerprint protocol, `SharesHardwareWith`, `billing_group`, and the
cross-daemon lease/back-off propagation). BL431's Automation-level
`BudgetPolicy` narrows these to a named batch of work and is designed there,
not here.

### 4b. Operator-reported usage snapshots (calculator input)

The subscription quota cannot be read programmatically (see 4a), so the
usage calculator (BL472) runs on periodic operator-reported snapshots plus
inferred token and cost rollups. This is the snapshot log it starts from.
Record each new snapshot here, in this doc, not in any assistant memory.

Two numbers must both be modelled: the session-local meter (percent of this
session's own budget) and the shared weekly ceiling across all models. They
can disagree sharply, and the shared ceiling is the real constraint.

| When (local) | Reading | Note |
|---|---|---|
| 2026-10-14 ~16:02 | 81% weekly | |
| 2026-10-16 (overnight session) | 87%, then 90%, then 91% | informational only |
| 2026-10-16 ~16:3x | 97% used, operator estimate ~3 hours left | first time-remaining estimate; anchor for burn rate |
| 2026-10-16 ~17:3x | 99% used; session meter at 15% with 2 hours left, all models at 99% | the two numbers disagreed; session stopped |
| 2026-10-17 ~13:4x | usage ran out; operator bought $20 of extra usage to reach 2026-10-18 | purchase recorded as a snapshot event |
| 2026-10-17 ~14:30 | 7.22 credits left of the 20 purchased | the supervisor catch-up and audit after switching to Sonnet 5.5 (read of both rule docs, the 79-commit audit, the v9.5.0 release work) cost about 12.78 credits, a one-time cost to keep apart from steady supervision |
| 2026-10-17 ~15:20 | 2.12 credits left | steady supervision ticks plus resets and fix work cost roughly 0.60 credits per tick (approximate) |
| 2026-10-17 16:29 | all 20 credits used | the last supervision tick consumed the remainder |
| 2026-10-18 08:00 | weekly limit reset | back on the weekly percent limit, not credits; operator allows one Claude development thread on the backlog in addition to the two GPU threads; the Claude thread's usage is tracked as its own line, apart from supervision |
| 2026-10-18 ~10:20 | 2% weekly, 8% session, session resets in 2h40m | about two hours of supervision plus the first Claude-lane fix PRDs (Haiku worker, Sonnet verifier); first data point for the Claude-lane line |
| 2026-10-18 ~14:3x | 4% weekly | this Claude dev-lane session (BL449 pi-switch + BL472 mockup filing below); session stopped here on operator instruction, handed back to the supervisor |
| 2026-10-18 ~21:05 | 7% weekly | operator-reported to the supervisor (Sonnet 5.5) after the 20:18 tmux loss and recovery; Claude lane running one PRD (stall detector) |
| 2026-10-19 ~09:55 | 9% weekly | operator-reported; Claude lane running one PRD (stale-task detector, 10 of 12 tasks) with Haiku workers and a Sonnet verifier |
| 2026-10-19 ~17:50 | 13% weekly | operator-reported for tracking; two Claude lanes running (BL515 `d4c50001`, BL514 `da660001`), `06ae0001` queued |
| 2026-10-19 ~20:15 | 14% weekly | operator-reported; operator decision: use both Claude lanes, with Claude decompose run serially (one decompose thread) when a Claude lane is free, to clear backlog for the GPU workers. Provisional: the operator tracks Claude usage for the next 24 hours and adjusts the lane model on 2026-10-20 |
| 2026-10-20 ~04:31 | 16% weekly | operator-reported for the calculator; operator proposes a third Claude lane (not yet decided; the lane model still has two) |
| 2026-10-20 ~16:40 | 19% weekly, used since the Sunday 2026-10-18 reset | operator-reported for accounting (calculator). Lanes: two GPU lanes on qwen plus Claude lanes on decompose and Haiku workers |
| 2026-10-18 ~17:00 | 5% weekly; session meter at 15%, session resets in about 50 minutes | operator-reported. One Claude builder thread plus two qwen GPU threads; operator decided to hold at one Claude thread (decision 2026-10-18 17:02) and revisit after a per-PRD cost is measured |

Restricted window: while the weekly ceiling was exhausted, autonomous PRD
decompose, run, verify and smoke used pi-prompt with ollama/qwen3.8-64k:27b
only. The window runs until Sunday 2026-10-18.

### 4c. Other consumers of tracked models (calculator input)

Plugins, integrated services, scheduled jobs (the hourly mail daemon), web UIs and helper
models (embedder, summariser) use the same models and nodes as the build. They tie into
the same accounting: consumer name and kind, model, node, tokens where known, wall-clock
seconds, and whether the call evicted another model. The calculator reports PRD work and
all other consumers separately. Details in
[model host requirements](2026-10-18-model-host-requirements.md) section 3.

### 4b-i. Calculator visualization mockups (design reference, not data)

Operator supplied four dashboard mockups (2026-10-18) as design reference
for whoever builds BL472's actual UI/output once it leaves "not started" —
these are illustrative layouts with placeholder numbers (14.29%/day decay,
a $1000/week example, etc.), **not real operator-reported snapshots**; the
table above stays the only source of real readings. Filed under
`docs/plans/assets/bl472-calculator-mockups/`:

- `daily-decay-and-weekly-refresh.jpeg` — a single day's 8am→8am decay
  gauge (100%→86%, -14%) next to a Sun→Sat weekly bar/line chart showing
  the same 14%/day compounding decay down to a Saturday low, with a
  "refresh to 100%" arrow into the next Sunday. Models the two numbers
  this doc already tracks as separate (session-local meter vs. shared
  weekly ceiling) as one combined visual.
- `weekly-dashboard-sections.jpeg` — a 4-section dashboard layout: weekly
  totals/cost summary, a Sun-Sat daily-decay strip (gauge per day), an
  hourly-rate section (bar chart + $/hr), and a system-metrics section
  (heatmap by hour×day, cumulative-cost line, average-rate gauges). A
  candidate shape for a BL472 summary view.
- `daily-breakdown-dashboard.jpeg` — single-day drill-down: a uniform
  hourly-consumption-rate bar chart (modelling flat ~0.595%/hr, i.e.
  100%/7days/24h), a cumulative progressive-decay line (100%→85.71% over
  24h), a cumulative-total-consumed area chart, and a 4-way time-of-day
  phase breakdown (morning/afternoon/evening/night, each ~2.4-4.8%
  consumed). The flat-hourly-rate assumption is a mockup simplification,
  not a finding — real usage is bursty (see the irregular readings in the
  table above), so BL472's actual rollup should not assume uniform
  hourly draw.
- `3-shift-plan-dashboard.jpeg` — the same day broken into three 8-hour
  shifts (day/evening/night) with per-shift and per-block consumption,
  plus the same cumulative-decay line annotated with shift-end markers.
  Useful if BL472 ever needs to attribute weekly burn to the three
  concurrent lanes this doc already tracks (GPU/pi lanes + the Claude
  dev lane) rather than to time-of-day.

None of this is a spec — BL472 is still "not started" (README row 53) and
these are inputs for whoever designs it, not a commitment to any specific
chart library, cadence, or the flat-hourly-rate model the mockups use as
a simplification.

## 5. Scope boundary

- **Claude only for now.** The reactive back-off, the tier defaults and the
  allocation meter are designed and built for `claude-code` only.
- **The backend/model pair is a first-class key dimension everywhere.**
  This applies to `llm:<name>`, `node:<name>` and `backoff:<backend>`, and to
  budget pools keyed `budget:<backend>[:<model>]`. BL422's forward pointer
  ("a separate future rerun comparing against Qwen/other open models via the
  existing Ollama/OpenWebUI backends") therefore slots in by adding pool
  limits for those backends, with no structural change.
- **No multi-backend logic in this pass.** That excludes failover between
  Claude and an open model when the budget runs out, cross-backend budget
  pooling, and translating dollars to GPU-hours. BL405 Phases 3, 5 and 7
  own those.

## 6. Worked examples

### (i) BL423 `9f311580` and BL426 `ff67e611` running at once, with three concurrent tasks

**Facts** (from the daemon's own PRD records, read live 2026-10-14):

- BL423 `9f311580` ("ci-watch: completion guardrail…"), project_dir
  `/home/user/workspace/datawatch`, direct mode (`git: {}`).
  - Approved 20:52:31 (all times are local, -04:00, 2026-10-13).
  - Ran its 7 tasks sequentially from 20:53 onward.
  - A `boot_reconcile` decision at 22:53:15 and a `run` at 22:53:17
    resumed it after a daemon restart.
  - Final task `6e3cf717`, "Live end-to-end ci-watch run…", ran
    22:53:17-23:08:42 on session `f42f`.
- BL426 `ff67e611`, same project_dir, direct mode.
  - Approved 22:27:16, **while BL423 was still running.**
  - Its release task `18bc0bcc` ran 22:47:50-22:59:12 on session `e913`.
  - Its `run` decision was also re-issued at 22:53:17.
- Scratch probe `392b2603` ("ci-watch guardrail live probe (v9.0.65)"), same
  project_dir.
  - Created 22:54:09 by BL423's live-E2E verification task.
  - Created through the ordinary create API, so `parent_prd_id` is empty and
    `autonomous_prd_children 9f311580` returns `[]`. That makes it invisible
    as a child.
  - Its task `13e8599c` ran 22:55:51-22:57:35 on session `b9c4`.
- **From 22:55:51 to 22:57:35, three tasks from three PRDs were
  `in_progress` against one working tree.** The live config at that time was
  `autonomous.max_parallel_tasks: 1`, with no global
  `max_concurrent_tasks`, `capacity_enabled: true`, `session.max_sessions:
  10` and `reserved_interactive: 1`.

**Which code allowed each:**

1. Each PRD got its own executor goroutine from `API.Run`
   (`internal/autonomous/api.go` ~305-345). The `runCancels[id]` dedupe is
   per ID, so three IDs gave three goroutines.
2. Within each PRD, `executor.go:248-256` resolved concurrency to 0 or 1,
   which is sequential, so each PRD had one task in flight. That was
   correct per PRD, and it never consults any other PRD.
3. `max_parallel_tasks: 1` was read by nothing. It is dead config.
4. Each `executeOne` → `admit` acquired `host`, limit 9, holding about 3,
   and `llm:claude-code`, whose `max_inflight` is 0 (unlimited). All were
   granted instantly.
5. No tree lock exists, so nothing considered that the three shared
   `/home/user/workspace/datawatch`.

**Consequences visible in the records:**

- BL426 task `18bc04c1`'s `files_touched` includes BL423's files.
- Its verifier flagged that the diff included other commits.
- BL426's release found v9.0.64 already consumed in both `var Version`
  strings and shipped v9.0.65. That is version-number collision between
  concurrent release work in one tree.

**Operator/process gap:** the second PRD was approved and run while the first
was active, and the first PRD's own verification task spawned a third PRD in
the same tree. Even if the operator had intended serialization, **nothing in
the daemon would have stopped it**: no setting exists that expresses "one
writer per tree", and the one setting that sounds like it is dead.

### (ii) BL426's final task stalling during a planned ~90-minute CI wait

**Facts:**

- BL426 task `18bc0cee`, "Record the real Release CI outcome…", told its
  worker to poll the Release run with a 90-minute bound.
- It started 23:01:13 and ended `failed` at 23:02:20, with `retry_count: 1`
  and failure category `infra`.
- The error was the one-shot stall message from `cmd/datawatch/main.go`
  ~4601: "worker stopped and asked an interactive question … worker
  stalled", wrapping `ErrWorkerStalled`.
- Its own checkpoint read "Now: waiting for run 38503350068 (90 min bound…)".

**Mechanism:** for `claude-code` one-shots, the wait loop kills a session
that sits in `waiting_input` for about 3 ticks, roughly 9 seconds
(`waitingInputTick`, main.go ~4592). A worker that ends its turn to wait on
CI looks exactly like that. Both attempts died within about a minute, long
before the planned 90-minute wait.

The structural problem is not that the stall detector is too strict. **An
LLM session is the wrong vehicle for a long external wait:**

- While waiting it held a task lease on `host` and `llm:claude-code` (the
  deferred release at executor.go ~779), consuming an ACTIVE-INFERENCE
  slot.
- If its loop had kept generating tokens, it would also have drawn USAGE
  BUDGET for no work.

`ci-watch` (`ci_watch.go`) is the counter-example. It polls `gh run list`
from daemon code with no session and no `admit()`. When it times out, it
completes the PRD as `pending` and runs one background follow-up
(`ciWatchFollowUp`).

**Decision:** IDLE-EXTERNAL-WAIT is a distinct resource class.

- **It must not hold an ACTIVE-INFERENCE slot or draw usage budget.**
  Waits run daemon-side as in ci-watch, or a task can end with a typed
  "await external" result that releases its `llm:`/`node:` leases and is
  re-admitted when the condition resolves.
- **It keeps the PROJECT-TREE WRITE lock only while the tree holds that PRD's
  uncommitted changes or commits not yet pushed.** That is: `git status`
  dirty with its files, or its HEAD not reachable from a remote-tracking
  ref, which is the same reachability test `defaultCIGit` already uses.

**Justification:** if the work is committed and pushed, another PRD writing
the tree cannot corrupt it, and holding the lock for 90 minutes would block
the tree for nothing. If the work is unpushed, another writer could
interleave commits on the same branch, or run `git add -A` and sweep the
unpushed changes in (the BL425 failure mode), so the lock must be held.

For `18bc0cee` specifically, the release push was already done by
`18bc0bcc`, so the wait should have held no lock. Its later docs-only commit
should re-acquire the lock as a new short T.

### (iii) The overnight queue stall, 2026-10-14

**What happened, from the daemon records and the operator's account:**

- During the night, a batch of related PRDs was being advanced one at a time
  **by an operator's interactive LLM chat session, not by datawatch.** That
  session drafted a bash polling script to advance the queue, said it would
  launch it, and **did not.**
- Queue advancement stopped. The operator-observed stall window is
  02:23-07:37.
- In the records:
  - PRD `4d0ad909` (verification LLM override) completed its last task at
    02:35:02.
  - PRD `180a01a0` (foreign-file drift) had been **approved since 00:33:14**
    and was ready the whole time. Its first task did not start until
    07:49:30.
  - PRD `30e6f153` (task effort → `--effort`), created 01:14:44, was not
    approved until 07:37:58 and started at 07:38:00.
- **For over five hours an approved PRD sat idle, and nothing in datawatch
  noticed:**
  - Approve does not run (`internal/server/autonomous.go` ~473-479).
  - No daemon object knows that PRD B should follow PRD A.
  - No notification fires on completion (BL427's confirmed gap).

**Why it belongs in this doc:** the operator was hand-sequencing those PRDs
because the daemon gives no safe way to let them overlap. Example (i) shows
what happens when they do overlap: three writers in one tree, an
unattributed child PRD and a version collision. With nothing on the server
side to rely on, serialization lived in a chat session, and that failed
silently. The mechanics here, a per-tree write lock plus inference and
budget caps with visible queued/waiting state, are what make it **safe** for
a daemon-side coordinator to start the next PRD without a human. They are
also what would have made "approved but not running" visible as a queued
state with a reason.

**The orchestration fix itself is BL431, not this doc.**

## Recommendation

### 7a. BL431 pointer (Automation coordinator)

The daemon-native multi-PRD orchestration object is designed in its own
dedicated PRD, **BL431**
(`docs/plans/2026-10-14-automation-orchestration-design.md`, once it exists).
The "Automation" covers ordered or graph-shaped groups of Automata,
auto-advance, cost policy, multi-model policy, dynamic mid-flight editing and
a PWA surface.

**This doc is the foundation BL431 builds on.** An Automation member's task
execution must respect this doc's per-`project_dir` write serialization (3a)
and its per-backend / per-compute-node inference caps (3b), exactly like any
other PRD. No special-casing is needed, because **an Automation member is an
ordinary PRD as far as sections 1-6 are concerned.** BL431's Automation-level
BudgetPolicy (see 4c) narrows this doc's usage-budget ceiling (section 4) to
a named batch of work. This doc therefore does not need its own separate
per-batch concept.

### 7b. Fate of `max_parallel_tasks`, and the full key list

**Decision: deprecate and remove `autonomous.max_parallel_tasks`. Do not
repurpose it.**

The name sounds like a cross-PRD limit, and its documented default of 3 is
wrong for every real meaning. Mapping its value onto any new axis would
silently change behavior on daemons that set it expecting nothing: the live
daemon has `1`, which would become a real limit overnight.

Migration:

- On config load, if the key is present, log a one-time warning naming the
  replacement keys, and record an audit entry.
- `PUT /api/config` and MCP `autonomous_config_set` accept and ignore it for
  one minor release, returning a `deprecated` note.
- Then remove it from:
  - `manager.go:46`/`261`,
  - `config.go:1863-1864`,
  - `api.go:5484`/`6729`,
  - `main.go:4074`/`4167-4168`,
  - `internal/mcp/autonomous.go:53`/`85`,
  - `app.js:13942`/`28648`.

New and changed config keys:

| Key | Scope | Default | Notes |
|---|---|---|---|
| `autonomous.project_write_serialization` | global + per-PRD override | `per_project_dir` | `per_project_dir \| off`. Key = canonical tree (3a). Worktree PRDs key on their worktree. |
| `autonomous.refuse_dirty_tree_start` | global + per-PRD override | `false` | Admission-time companion to BL425 (3a ii). |
| `autonomous.max_concurrent_tasks` | global (**new YAML plumbing**) + existing per-PRD | `1` | Existing BL370 knob. Fix the missing `config.AutonomousConfig` field and the main.go bridge. Fan-out *within* one PRD is still bounded by 3a when tasks share a tree. |
| `llm.<name>.max_inflight` (existing) | per-backend | 0, or the tier-derived value when `plan_tier` is set | Existing ledger pool `llm:<name>`, already cross-PRD. |
| `llm.<name>.plan_tier` (new) | per-backend | `unset` (→ conservative `1` for subscription kinds) | `pro \| max_5x \| max_20x \| api \| unset`. Proactive defaults (4d). |
| `compute_nodes.<name>.max_concurrent_sessions` (existing) | per-compute_node | unchanged | Existing `node:` pool. |
| `autonomous.capacity_gpu_util_pct` (existing) | global | unchanged | Existing node backpressure. |
| `autonomous.usage_backoff` (new) | global, keyed per backend at runtime | `pause_backend` | `pause_backend \| off`. Reactive back-off (4d). |
| `autonomous.usage_backoff_fallback_minutes` (new) | global | `60` | Used when the reset time cannot be parsed. Matches today's per-session fallback (manager.go ~6157). |
| `llm.<name>.budget` (new) | per-backend (model optional) | unset | `{window: day\|week\|month, ceiling_usd: N}` for API billing (4b). Enforced as a BL405-Phase-2-style windowed pool `budget:<backend>`. |
| `autonomous.idle_wait_releases_inference` (new) | global | `true` | IDLE-EXTERNAL-WAIT releases `llm:`/`node:` leases (6.ii). |
| `detection.rate_limit_patterns_verified_at` (new) | global | unset | Pattern-drift guard (4d). |

## Phases

These are the phases of the **follow-up implementation PRD**. Phase 1, 2,
3 and 5 are **Done**; the rest are **Planned**, not approved. Every phase
carries AGENT.md's Phase Completion Checklist, and none is done until that
checklist is.

1. **Phase 1 — Dead-knob removal plus the global `max_concurrent_tasks`
   plumbing.** Deprecation warning and accept-and-ignore for
   `max_parallel_tasks`; correct the descriptions; add
   `autonomous.max_concurrent_tasks` to YAML, REST, MCP, CLI, comm and PWA.
   Status: Done (v9.0.86) — max_parallel_tasks removal deferred one release.
2. **Phase 2 — Project-tree write lock (3a).** Canonical key resolution,
   worktree keying, the `tree:<key>` representation, the advisory lock file,
   the `refuse_dirty_tree_start` check, CW watching the PRD's own recorded
   SHA. Status: **Done (v9.4.0/v9.5.0 development builds, 2026-10-18)** —
   `internal/autonomous/tree_lock.go` (BL428 §3a writer lock),
   `executor.go` admission order (capacity → dirty-tree gate → tree lock →
   spawn), `tree_admission.go` (BL469 PRD-level same-tree gate, the
   2026-10-13-incident shape pinned by
   `same_tree_serialization_test.go`), `ci_watch.go` own-commit pin
   (`prdLastPostVerifySHA`), config keys
   `autonomous.project_write_serialization` / `autonomous.refuse_dirty_tree_start`.
   Operator guide: `docs/operations.md` → "Project-tree write serialization";
   mechanism: `docs/implementation.md` → "Per-tree write lock (Automata)";
   tests: `docs/testing-tracker.md` → "Per-tree write lock + same-tree
   admission + ci-watch own-commit pin". Live smoke 2026-10-18
   (`tests/live-smoke/tree-serialization-smoke.sh`, evidence
   `tests/live-smoke/EVIDENCE/20261019-1637/`): same-tree serialized PASS,
   cross-tree parallel PASS. Known gap (carried to the Phase 2 follow-up,
   not a checklist-failing item): a PRD refused by the admission gate needs a
   manual re-kick today — PRD-level auto-wait/auto-resume is not implemented;
   the task-level wait+resume works.

   **Phase Completion Checklist (recorded 2026-10-21, evidence re-verified
   same day):**
   - Functionality per §3a: ✅ `internal/autonomous/tree_lock.go` (key
     resolution, advisory lock, stale reclamation, release guard),
     `executor.go` (admission order: capacity → dirty-tree gate → tree lock →
     spawn), `tree_admission.go` (BL469 PRD-level, shipped v9.5.0),
     `ci_watch.go` (own-commit pin via `prdLastPostVerifySHA`),
     `config.go` (`autonomous.project_write_serialization`,
     `autonomous.refuse_dirty_tree_start`).
   - Unit tests: ✅ 54 dedicated tests green on 2026-10-21 (20
     `tree_lock_test.go`, 9 `executor_tree_lock_test.go`, 2
     `same_tree_serialization_test.go`, 13 `bl451_rollup_race_test.go`,
     10 in `ci_watch_plumbing_test.go` including the 5 incident pin tests).
   - Full suites: ✅ `go test ./...` 4,517 passed / 84 packages, 0 fail
     (2026-10-21); ✅ `node --test internal/server/web/*.test.js` 257/257
     pass (2026-10-21).
   - Live smoke against a real daemon: ✅ 2026-10-18, v9.5.4, isolated
     scratch daemon (port 18912, temp data dir — never production),
     `tests/live-smoke/tree-serialization-smoke.sh`, verdicts in
     `tests/live-smoke/REPORT.md`, raw evidence in
     `tests/live-smoke/EVIDENCE/20261019-1637/`.
   - 8-item documentation checklist (2026-10-21): `CHANGELOG.md` ✅ (v9.4.0
     entry); `docs/config-reference.yaml` ✅ (both keys + cross-references);
     `docs/operations.md` ✅ ("Project-tree write serialization");
     `README.md` ✅ (per-tree serialization line in Orchestration & autonomy);
     doc index N/A (no new doc added, only existing sections grown);
     `docs/testing-tracker.md` ✅ (dedicated section added 2026-10-21);
     no leaked tracker IDs ✅ (README/locales/howto surfaces clean per the
     2026-10-07 rule — BL428/BL469 ids appear only in reference docs,
     CHANGELOG and plans, which are exempt); Parity-surface section of this
     plan still accurate ✅.
   - Conditional docs: N/A — the operator-visible pieces are the two config
     keys (in `docs/config-reference.yaml`) and the refusal/wait strings
     (in `docs/operations.md`); no new user-followable workflow beyond the
     force-clear procedure documented there, and no new diagram-worthy
     data flow (the lock file is one advisory file under the project's
     already-documented `.datawatch/` directory).
   - Mobile-Parity: covered by existing open datawatch-app issue #263
     ("Parity: … tree-busy refusal (datawatch v9.5.0)"); the task-level
     wait reason flows through the existing task message/status fields, so
     no new API contract for mobile clients.
   - Localization: N/A — no `app.js` change in this phase.
   - Plans README line updated: ✅ row 8 status `blocked` → implemented
     (2026-10-21).
   - Version: in the v9.4.0/v9.5.0 development builds; not in a public
     release yet (ships with the first complete release, tracked as 9.6.0 in
     `docs/plans/README.md` row 9a).
3. **Phase 3 — Inference-cap gap closure (3b).**
   - Decompose admits on `llm:`/`node:`.
   - Hand off the task lease to the verify lease instead of stacking them.
   - IDLE-EXTERNAL-WAIT lease release with an "await external" task result.
   - `plan_tier`-derived `max_inflight` defaults.

   Status: **Done (unreleased development build, 2026-10-22)** —
   `internal/inference/llm.go` (`PlanTier`, kind inflight class,
   `EffectiveMaxInflight`, `InflightSource`), `internal/config/config.go`
   (`autonomous.plan_tier_max_inflight`, default `pro: 1, max_5x: 2,
   max_20x: 4, api: 0`, negative caps and unknown tiers rejected),
   `internal/autonomous/capacity.go` (planner role + `decomposeAdmitted`
   admission, `HandoffVerifyLease`), `internal/autonomous/await_external.go`
   (`DATAWATCH_AWAIT_EXTERNAL:` cycle — park, lease release, tree lock
   retained while unpushed, re-admit through the bounded wait, boot resume),
   `cmd/datawatch/main.go` (marker detection, await re-prompt),
   `cmd/datawatch/capacity_wiring.go` (hand-off wiring). Operator guide:
   `docs/operations.md` → "Per-LLM concurrency limits and plan tiers" and
   "Parking a task while waiting on an external condition"; howto:
   `docs/howto/automata-capacity.md` (per-LLM caps, one slot from spawn
   through verification, decompose waits, park on external condition);
   flow: `docs/flow/automata-capacity-flow.md` (verify hand-off, planner
   admission and await-external sequences). Tests:
   `docs/testing-tracker.md` → "Inference-cap gap closure — per-LLM
   `plan_tier` caps, verify lease hand-off, decompose admission,
   await-external".

   **Phase Completion Checklist (recorded 2026-10-22):**
   - Functionality per §3b: ✅ the four bullets above, all in the files
     listed in the status line.
   - Unit tests: ✅ all new tests green on 2026-10-22 —
     `internal/inference/plan_tier_test.go` (8), `plan_tier_source_test.go`
     (1), `internal/config/plan_tier_max_inflight_test.go` (5),
     `internal/server/plan_tier_api_test.go` (6),
     `internal/mcp/plan_tier_tools_test.go` (6),
     `cmd/datawatch/cli_llm_plan_tier_test.go` (5),
     `internal/autonomous/decompose_admission_test.go` (7),
     `capacity_handoff_test.go` (6), `await_external_test.go` (8), and the
     PWA `app-plan-tier-fields.test.js` (8).
   - Full suites: ✅ `go build ./...` clean; `go test ./internal/inference/...
     ./internal/config/... ./internal/autonomous/... ./cmd/datawatch/...`
     all pass (2026-10-22); `node --test internal/server/web/app-plan-tier-fields.test.js`
     8/8 pass.
   - Live smoke: No — this is an unreleased development build; the tracker's
     live rows stay No until a sandbox daemon run (plan-tier table
     round-trip and capacity-panel read-back, one full await-external
     cycle, a decompose wait reason on a saturated pool).
   - 8-item documentation checklist (2026-10-22): `CHANGELOG.md` ✅
     (Unreleased entries: plan_tier caps + config validation + tests,
     decompose capacity gating, verify lease hand-off, the await-external
     cycle); `docs/config-reference.yaml` ✅ (`plan_tier_max_inflight` block,
     LLM `plan_tier`/`max_inflight` fields, operator-policy caveat);
     `docs/operations.md` ✅ (two new sections); `README.md` ✅ (one line
     in Orchestration & autonomy); doc index N/A (no new top-level doc —
     existing howto and flow sections were grown); `docs/testing-tracker.md`
     ✅ (dedicated section added 2026-10-22); no leaked tracker IDs ✅
     (`scripts/check-no-internal-refs.sh` passes — IDs appear only in
     plans, CHANGELOG and reference docs, all of which are exempt); this
     plan's Parity-surface section updated ✅.
   - Conditional docs: ✅ howto and flow updated (above); the §Data-model
     decision is recorded: await-external is a sub-reason under
     `TaskWaitingCapacity`, not a new status.
   - Mobile-Parity: a `datawatch-app` issue is needed — the LLM-edit
     `plan_tier` / `max_inflight` fields and the new wait reasons
     (`await_external: …`, `capacity: planner waiting …`) must parse and
     display on Android/iOS. Not filed yet; tracked here and in
     `docs/plans/README.md` row 12.
   - Localization: ✅ the new PWA strings exist in all five locale bundles
     (en/de/es/fr/ja — asserted by `app-plan-tier-fields.test.js`).
   - Plans README line: ✅ row 12 status updated 2026-10-22.
   - Version: in the unreleased development build; not in a public release
     yet (the first complete release after v9.0.87 is tracked as 9.6.0 in
     `docs/plans/README.md` row 9a).
4. **Phase 4 — Reactive usage back-off (4d).** Backend-scoped back-off flag
   set from `StateRateLimited`; `admit` waits on it; `ErrGlobalCooldown` is
   classified as a wait; the pattern-drift guard is (a)+(b). Status: Planned.
   (Not started — Phase 3 is complete; Phase 4 and later require operator
   approval.)
5. **Phase 5 — Dollar-window budget (4b/4c).** `llm.<name>.budget` enforced
   as a windowed ledger pool, coordinated with BL405 Phase 2.

   Status: **Implemented (unreleased development build, 2026-10-22)** —
   `internal/inference/budget.go` (`BudgetWindow`: rolling `hours` or weekly
   `weekday` + `time` + `zone`, validation, window and rollover math) and
   `llm.go` (`LLM.Budget`); `internal/config/config.go`
   (`autonomous.plan_tier_allocation_usd`, unknown tiers and negative values
   rejected); `internal/session/cost.go` + `manager.go` (usage-event hook);
   `internal/autonomous/budget.go` (persisted spend ledger, `Blocked`
   with the `budget window full:` reason, `Status`), `budget_format.go` (the
   text the CLI and comm channel print), `peer_budget.go` (per-peer opt-in
   spend counting behind `PeerShareConfirmer`), `manager.go` + `models.go`
   (`budgets` in the autonomous status); `cmd/datawatch/capacity_wiring.go`
   (budget gate composed with the usage back-off gate on `llm:` pools,
   autonomous requests only; audit events on each fill and resume);
   `internal/server/budget.go` (`GET /api/budget`, `GET /api/budget/usage`,
   the LLM `budget` field), `plan_tier_allocation.go`, `capacity.go`
   (`budgets` array); `internal/mcp/budget.go` (`budget_status`, the `budget`
   argument on `llm_add`/`llm_update`, `plan_tier_allocation_usd` on
   `autonomous_config_set`); `cmd/datawatch/cli_budget.go` (`datawatch budget
   status`, `--budget-*` flags); `internal/router/` (`budget` comm command);
   PWA `app.js` and all five locale bundles. Operator guide:
   `docs/operations.md` → "Per-LLM dollar-window budget"; howto:
   `docs/howto/automata-capacity.md` → "Per-LLM dollar budget"; config:
   `docs/config-reference.yaml`; API: `docs/api-mcp-mapping.md`
   ("Per-LLM dollar budget") and both OpenAPI specs. Tests:
   `docs/testing-tracker.md` → "BL428 Phase 5 — per-LLM dollar-window
   budget".

   **Operator decisions recorded for this phase (2026-10-22):**
   - The ledger is the first slice of the later shared usage work (BL405
     Phase 2, which builds on it) and owns ceilings and hard stops. The older
     cost-guardrail item (BL414) is narrowed to reporting and the Decision
     rollup only.
   - `allocation_pct` is opt-in per budget, always marked estimated, and never
     blocks anything; only `ceiling_usd` does. The notional dollar figure per
     plan tier is operator policy (`autonomous.plan_tier_allocation_usd`).
   - Peer spend counting is opt-in per peer (`peer_window`), default off. An
     unreachable peer fails open (the local spend alone decides).

   **Finding (updated 2026-10-22):** the resource fingerprint exchange
   (§3c/§4e) now exists in code — it was built on this branch (unreleased),
   see "Resource fingerprint exchange (sections 3c and 4e) — shipped"
   below the phase list. The Phase 5 ledger interface is unchanged: peer
   spend still counts only via the operator's explicit `peer_window` list
   (`PeerWindowConfirmer` remains the default `PeerShareConfirmer`). A
   *confirmed* fingerprint match does not add a peer to the ledger by
   itself — it tells the operator which peer to put in `peer_window`
   (billing-group detection, §4e) and drives the §3c shared-node pool gate
   directly. The two surfaces stay separate and are connected only at the
   level of operator judgment.

   **Phase Completion Checklist (recorded 2026-10-22):**
   - Functionality per §4b/§4c: ✅ the budget model and window math, the
     persisted ledger fed by the usage-event hook, the admission gate (a full
     window is a wait with `budget window full: …`, resumed on rollover;
     interactive starts never gated), the estimated allocation figure, the
     per-peer opt-in, and the surfaces — all in the files listed in the
     status line. Known limit: admission cannot know what a task will cost, so
     work admitted just before the ceiling is crossed can overshoot it.
   - Unit tests: ✅ the 27 tests named in `docs/testing-tracker.md` plus
     `TestUsageEventHookNilSafe`, `TestBudgetPeerSpendCountsTowardBlocked`,
     `TestBudgetTransitionsAuditedOncePerChange`,
     `TestPeerBudgetConfirmerCanVeto`,
     `TestPeerBudgetFailureLoggedOncePerEpisode`,
     `TestPeerBudgetLocalOnlyNoLoop` and the PWA
     `app-llm-budget.test.js` (14 tests, 14 pass 2026-10-22). The Go budget
     tests pass across `internal/inference`, `config`, `session`,
     `autonomous`, `server`, `mcp`, `router` and `cmd/datawatch`
     (2026-10-22, `-run` filtered).
   - Full suites: not re-run for this documentation pass; the supervisor runs
     the full Go and node suites at merge.
   - Live smoke: No — this is an unreleased development build; the tracker's
     live rows stay No until a sandbox daemon run (a tiny ceiling on a sandbox
     LLM filling and resuming, spend surviving a restart, a two-daemon peer
     opt-in).
   - 8-item documentation checklist (2026-10-22): `CHANGELOG.md` — left to
     the supervisor at merge time, by instruction;
     `docs/config-reference.yaml` ✅ (the `budget` block shape and
     `plan_tier_allocation_usd`); `docs/operations.md` ✅ (new section);
     `README.md` — not edited in this pass (the `check-no-internal-refs.sh`
     audit already reports one earlier README line, not from this phase);
     doc index N/A (no new top-level doc — existing howto and operations
     sections were grown); `docs/testing-tracker.md` ✅ (dedicated section);
     no leaked tracker IDs in operator-facing prose ✅ (IDs appear only in
     plans, the tracker and reference-doc comments); this plan's
     Parity-surface section updated ✅.
   - Conditional docs: ✅ howto updated (`docs/howto/automata-capacity.md`);
     `docs/api-mcp-mapping.md` rows added, and the OpenAPI specs
     (`docs/api/openapi.yaml`, `internal/server/web/openapi.yaml`) already
     carry the two routes; no per-route page exists for the usage back-off
     routes under `docs/api/`, so none was added for these. The flow diagram
     `docs/flow/automata-capacity-flow.md` is unchanged: it does not list the
     back-off gate, so there was no gate list to extend.
   - Mobile-Parity: a `datawatch-app` issue is needed — the LLM `budget`
     object, the `budgets` arrays in the capacity API and the autonomous
     status, and the `budget window full` wait reason must parse and display
     on Android and iOS. Not filed yet (it cannot be filed from this
     session); tracked here and in `docs/plans/README.md` row 17.
   - Localization: ✅ the new PWA strings exist in all five locale bundles
     (en/de/es/fr/ja — asserted by `app-llm-budget.test.js`).
   - Plans README line: ✅ row 17 status updated and the cost-guardrail item
     narrowed (2026-10-22).
   - Version: in the unreleased development build; not in a public release
     yet.
6. **Phase 6 — Visibility, audit and parity close-out.** `autonomous_status`
   shows queued-vs-running with a reason; PWA; `datawatch-app` issue; howto;
   release-smoke section.

   Status: **Implemented (unreleased development build, 2026-10-22)** —
   `internal/autonomous/wait_reason.go` (the seven wait causes and the plain
   reason text for each), `task_visibility.go` (`TaskWaitView` and
   `TaskHoldView`: cause, reason, since, reset time, holder, and a running
   task's pools, node and session), `capacity_format.go` (the text the CLI and
   comm channel print), `capacity_audit.go` (audit events with an injectable
   clock), with small changes to `capacity.go`, `backoff.go`, `budget.go`,
   `await_external.go`, `tree_lock.go`, `manager.go`, `models.go` and `api.go`
   in the same package, and `internal/capacity/ledger.go` (the pool lines split out so the capacity text reuses them);
   `internal/audit/events.go`, `cef.go`, `log.go` and `query.go` (the three new
   events, their CEF signatures, the entry `subject`, the `subject` filter, the
   trailing-`*` action prefix and one shared `ParseQuery`); `internal/server/capacity.go` (`tasks`
   object, `reset_known`), `session_status_automaton.go` (`wait_cause`,
   `wait_resets_at`, `holds`, `holds_note`), `usage_backoff.go` and `audit.go`
   (operator back-off events carry the actor and the subject; RFC 3339 bounds
   are validated); `internal/mcp/sx_parity.go` (`audit_query` filters),
   `internal/router/router.go` and `sx2_parity.go` (the `capacity` reply and the
   `audit` words); `cmd/datawatch/cli_capacity.go`, `cli_sx_parity.go`,
   `capacity_wiring.go` and `usage_backoff_wiring.go`; PWA `app.js` (capacity
   and budget card, cause chips, Holds line) and all five locale bundles;
   `scripts/release-smoke.sh` section 77. Operator guide:
   `docs/operations.md`; howto: `docs/howto/automata-capacity.md`; config:
   `docs/config-reference.yaml` (comment only, no new keys); API:
   `docs/api-mcp-mapping.md` and both OpenAPI specs. Tests:
   `docs/testing-tracker.md` → "Visibility, audit and parity close-out". App
   note: [`2026-10-22-datawatch-app-capacity-visibility-note.md`](2026-10-22-datawatch-app-capacity-visibility-note.md).

   **Phase Completion Checklist (recorded 2026-10-22):**
   - Functionality per the phase description: ✅ queued-vs-running with a
     reason and a cause in the capacity API, the autonomous per-task views and
     the session status; the CLI and comm text; audit events for admission,
     back-off and budget; one audit query filter set on REST, MCP, CLI and
     comm; the PWA card and chips.
   - Unit tests: ✅ the `TestWaitReason_`, `TestTaskVisibility_`,
     `TestCapacityVisibility_`, `TestCapacityText_`, `TestCapacityAudit_` and
     `TestAuditQuery_` families, `TestEmitBackoffAudit_RecordsActorAndSubject`
     and the two node files `app-capacity-card.test.js` and
     `app-wait-reason-text.test.js`, all listed by exact name in the tracker.
   - Live smoke: No — this is an unreleased development build; release-smoke
     section 77 is written but has not been run against a sandbox daemon, and
     the tracker's live rows stay No until it is.
   - Full suites (`go test ./...`, `node --test internal/server/web/*.test.js`):
     not run for this documentation pass; the supervisor runs them at merge.
   - 8-item documentation checklist: `CHANGELOG.md` — left to the supervisor
     at merge time, by instruction; `docs/config-reference.yaml` ✅ (comment
     listing the audit actions; no new keys); `docs/operations.md` ✅;
     `README.md` — not edited in this pass; doc index N/A (no new top-level
     doc — the howto and operations sections were grown);
     `docs/testing-tracker.md` ✅ (dedicated section); no leaked tracker IDs in
     operator-facing prose ✅ (IDs appear only in plans and the tracker); this
     plan's Parity-surface section updated ✅.
   - Conditional docs: ✅ howto updated (`docs/howto/automata-capacity.md`);
     `docs/api-mcp-mapping.md` capacity and audit rows updated and both
     OpenAPI specs carry the new `tasks`, `reset_known` and `subject`/prefix
     fields; the flow diagram is unchanged (the gate order did not change).
   - Mobile-Parity: a `datawatch-app` issue is needed and is **not filed** — it
     cannot be filed from the session that wrote this phase. The issue text is
     the note linked above; the supervisor or the operator files it. This also
     covers the Phase 3 and Phase 5 items still open for the apps.
   - Localization: ✅ the new PWA strings exist in all five locale bundles
     (asserted by `app-capacity-card.test.js` and `app-wait-reason-text.test.js`).
   - Plans README line: ✅ row 19 updated.
   - Version: in the unreleased development build; not in a public release
     yet, and no version file was changed by this phase.

### Resource fingerprint exchange (sections 3c and 4e) — shipped

**Status: built on this branch, unreleased (2026-10-22).** The §3c
reconciliation loop and the §4e `billing_group` detection went from design
to code in the same branch work as Phase 5. Open questions 10–13 below were
decided in the operator conversation of 2026-10-22 and are implemented as
documented here. Operator guide: `docs/howto/federation-fingerprint.md`;
status and test inventory in `docs/testing-tracker.md` (the BL568 section);
endpoints and MCP tools in `docs/api-mcp-mapping.md`.

**Implementing files:**

- `internal/fingerprint/` — the testable core, no daemon imports:
  `hash.go` (pair key = HMAC-SHA256 of the shared bearer token over
  `datawatch-fingerprint-v1`; item hash = HMAC-SHA256 of kind + value under
  that key, so the same two resources collide for exactly one peer pair),
  `identity.go` (node identity value: GPU UUID, else instance ID; names and
  addresses never count), `export.go` (responder's own hash list — one item
  per resolvable node, one per LLM with a non-empty `billing_group`; the
  operator's local label is the only plain-text field), `sync.go` (the
  reconciliation loop: pull each opted-in peer every 15 minutes, a code
  constant; exact collisions become `pending` matches; a rejection sticks;
  a peer outage fails soft and logs once per episode; it **never
  confirms**), `matches.go` (the persisted MatchStore, atomic JSON under
  `<data_dir>/autonomous/fingerprint/matches.json`, pending/confirmed/
  rejected transitions are idempotent at the target and 404 on unknown id,
  `OnDecision` hook), `gate.go` (the admission-time shared-node check,
  fail-open).
- `internal/server/federation_fingerprints.go` —
  `GET /api/federation/fingerprints` (method → `federation:fingerprint`
  capability → registered peer identity, admin token gets 400 → the peer
  record's `fingerprint_sync`, off gets 403; the answer is keyed to the
  caller's bearer token).
- `internal/server/federation_fingerprint_matches.go` (+ tests) —
  `GET /api/federation/fingerprint-matches[?state=]`,
  `POST …/{id}/confirm`, `POST …/{id}/reject`.
- `internal/federation/capabilities.go` — `federation:fingerprint` is
  granted only by full-control (`FULL_CONTROL`) control groups
  (`TestCapFederationFingerprintOnlyInFullControl`).
- `cmd/datawatch/fingerprint_wiring.go` — daemon wiring: one MatchStore
  under the data dir, the exporter behind the endpoint, the slow Syncer,
  the gate, and the `OnDecision` audit hook (the
  `fingerprint_match_confirmed` / `fingerprint_match_rejected` audit
  events of `internal/audit/events.go`).
- `cmd/datawatch/capacity_wiring.go` — `sharedNodeBackpressure()` composes
  the gate into `node:` pool admission for autonomous work only; never
  `host`/`llm:`/`external` pools, never interactive session starts; a
  daemon with no opted-in peers and no confirmed matches is fully idle.
- Surfaces: `cmd/datawatch/cli_fingerprint.go` (`datawatch fingerprint
  list|confirm|reject`), `cli_federation.go` (`--fingerprint-sync` on
  `federation peer add/update`), `cli_llm.go` (`--billing-group` on
  `llm add/update`), `internal/router/bl316_federation.go` (the
  `federation peer fingerprint …` / `federation fingerprint …` comm verbs),
  `internal/mcp/federation_fingerprint.go` (`federation_fingerprint_matches`,
  `federation_fingerprint_confirm`, `federation_fingerprint_reject`; the
  `fingerprint_sync` and `billing_group` arguments ride the peer and LLM
  tools), `internal/config/config.go` (peer entry `fingerprint_sync`),
  `internal/inference/llm.go` (LLM entry `billing_group`), PWA
  `internal/server/web/app.js` (+ `app-fingerprint.test.js`): the
  pending-match panel under Settings → Comms → Federation Peers with
  per-row confirm/reject, the peer form's `fingerprint_sync` toggle, the
  LLM form's billing-group field, and seven new keys in all five locale
  bundles (en/de/es/fr/ja) — the bundles currently carry English
  placeholders for those, awaiting translation.

**Operator decisions implemented (2026-10-22):**

1. Off by default; opt-in is per peer and must be set on **both** daemons
   (the caller's peer entry and the responder's record of that peer) —
   question 10's recommendation, as built.
2. Key material: the pair's existing federation bearer token, with
   fingerprint-specific HMAC domain separation — no separately exchanged
   pairing secret (question 11 resolved in favor of reuse plus
   domain-separation).
3. Always confirm, never auto-accept — question 12 as recommended: the
   loop only ever creates `pending` records and the operator decides on
   any surface; a rejection sticks across syncs and a rejection after a
   confirmation un-merges; every decision is audited.
4. Fail-open in both cases — question 13 as recommended, one shared
   behavior rather than two settings: an unreachable or malformed peer
   contributes zero to the shared-node gate and is logged once per failure
   episode, so a peer outage never stalls local work.
5. Wire minimality: only keyed hashes plus the operators' own local
   resource labels cross the wire — never the identity value, a node
   address, a billing label, or a credential (`TestExportItemsCarryNoRawValues`;
   the endpoint is keyed per caller — `TestFingerprintEndpointKeyedPerCaller`).
6. The Phase 5 ledger interface is untouched: `PeerShareConfirmer` still
   confirms exactly the operator's `peer_window` list; a confirmed
   billing-group match informs that list, it does not modify it.

**Not delivered (intentional):**

- No peer spend rollup: a peer's spend still enters a Phase 5 ceiling only
  through the explicit `peer_window` opt-in; the exchange informs that
  decision, it does not wire it.
- No enforcement of a billing-group match: the shared-node gate is the
  one enforcement surface; a billing match is decision context for the
  operator's windows.
- The sync interval (15 minutes), the poll timeout (5 s) and the 64 KiB
  body cap are code constants, not config keys — deliberate for this
  slice.
- `datawatch-app` parity and the live two-daemon smoke are the open rows
  in `docs/testing-tracker.md` (Tested=Yes, Validated=No for this
  unreleased build).
- `docs/operations.md` and the budget-facing prose that still say the
  exchange "does not exist yet" (e.g. the `peer_window` caveat sentence)
  remain worded for the ledger caveat, which is still true: confirmation
  never modifies `peer_window`.

## Follow-up implementation PRD — what to build

This is the **mechanics-level** implementation PRD. BL431's own
implementation PRD (the Automation coordinator) is a separate, later PRD that
builds on this one's config keys and admission points. Its build list is not
repeated here; only the dependency is named.

### Data model

- `autonomous.Task`:
  - `PostTaskSHA string` (the SHA its own work produced, so CW watches the
    right commit).
  - `WaitReason` already exists (`setWaiting`, capacity.go ~107). Extend its
    reason vocabulary with `tree_lock`, `usage_backoff`, `budget` and
    `await_external`. (Phase 3 resolved this: `await_external` is a
    sub-reason under `TaskWaitingCapacity` — no new task status.)
  - New `TaskAwaitingExternal` status, or a sub-reason under
    `TaskWaitingCapacity` (decide in Phase 3).
- `autonomous.PRD`: `ProjectWriteSerialization *string` and
  `RefuseDirtyTreeStart *bool` overrides, persisted in the existing PRD store
  like the other per-PRD pointer overrides.
- `capacity.Ledger`:
  - Exclusive/shared lock pools `tree:<canonical>` with holder = PRD+task,
    able to outlive one `executeOne` while `await_external` holds unpushed
    work.
  - A backend back-off map `backoff:<backend>` with `until` and `reason`.
  - Windowed-ceiling `budget:<backend>` pools (shared with BL405 Phase 2).
  - All in-memory, rebuilt on boot from `in_progress` tasks, the same way
    `Reap` reconciles leases today (capacity_wiring.go ~186).
- `inference.LLM`: `PlanTier string`, `Budget *BudgetWindow`.
- `config.AutonomousConfig`: the new keys from 7b, plus the missing
  `MaxConcurrentTasks`.

### Executor admission points

- `executeOne` → `admit` (executor.go ~854). The order is:
  1. back-off check;
  2. budget check;
  3. `tree:` lock;
  4. the existing `host`/`llm:`/`node:` pools.

  All four are all-or-nothing, as `Request.Pools` already is.
- Verify: hand off from the task lease to `verify:<task>`, and take the
  `tree:` lock only while a `verification_script` runs.
- Decompose (main.go ~4196): admit on `llm:`/`node:`, with no tree lock.
- `synthesizeCorrectiveWork` and `scan_fix` go through the same T admission.
- The `StateRateLimited` → `backoff:<backend>` subscriber chains into the
  existing state-change hook (`SetStateChangeHandler` →
  `onStateChange`, `internal/session/manager.go:899-900`), which is
  single-slot, so it must chain rather than replace. It is wired in
  `cmd/datawatch/main.go`.

### Surfaces (per AGENT.md's Configuration Accessibility Rule)

- `handleGetConfig` / `handlePutConfig` in `internal/server/api.go`.
- `autonomous_config_get`/`_set` and a new `autonomous_tree_locks` status
  tool, plus the existing `capacity_status`, which gains `tree:`, `backoff:`
  and `budget:` pools.
- `datawatch config set …` and `datawatch autonomous tree-lock status`.
- Comm `configure autonomous.project_write_serialization=…`.
- PWA `GENERAL_CONFIG_FIELDS`/`LLM_CONFIG_FIELDS`, and registry LLM-edit
  fields for `plan_tier`/`budget`.
- A `datawatch-app` issue for Android and iOS.

### Audit logging

New audit events:

- `autonomous_tree_lock_acquire` / `_wait` / `_release`, each with key,
  holder and wait duration.
- `autonomous_usage_backoff_set` / `_cleared`, with backend, reset time and
  the source session.
- `autonomous_budget_ceiling_wait`.
- `autonomous_idle_wait_release`.
- `config_deprecated_key` (`max_parallel_tasks`).

### Status visibility

`autonomous_status` and `autonomous_prd_get` show, per task, `running` versus
`queued` with its reason (for example "waiting for tree lock held by PRD
X/task Y since …", or "Claude usage limit — resets 11:50"). Per PRD, they show
`approved (not running)` as distinct from `running`. That distinction is the
thing example (iii) could not see.

### Tests

- **Unit:**
  - canonical key resolution (symlink, subdirectory, worktree);
  - lock exclusivity and shared/exclusive semantics;
  - lock retained across `await_external` only while work is unpushed;
  - back-off set and clear from a fake `StateRateLimited` transition;
  - `ErrGlobalCooldown` classified as a wait;
  - the deprecated-key warning;
  - `max_concurrent_tasks` YAML round-trip.
- **E2E story ideas:**
  - two direct-mode PRDs on one sandbox tree, where the second's task shows
    `waiting: tree_lock` until the first commits;
  - two PRDs on different trees run in parallel;
  - a worktree PRD alongside a direct PRD;
  - an injected `DATAWATCH_RATE_LIMITED:` line from one worker pauses
    dispatch for every PRD on `claude-code` and resumes at the reset time;
  - a CI-await task releasing its inference slot while keeping the tree lock
    over unpushed work.

## Parity surface

| Surface | Include / exclude | What |
|---|---|---|
| REST | Include | `GET/PUT /api/config` for every new key. `max_parallel_tasks` accepted-and-ignored with a deprecation note. Lock/back-off/budget state in `GET /api/capacity`, plus queued reasons in autonomous PRD JSON. **(Phase 3, implemented)** `GET /api/llms` and the `llm:` pools of `GET /api/capacity` carry `plan_tier`, `max_inflight`, `effective_max_inflight` and `inflight_source`; `PUT /api/llms/{name}` and `PUT /api/config` (`autonomous.plan_tier_max_inflight`) accept them. **(Phase 5, implemented)** `GET /api/budget` (and `?llm=`), `GET /api/budget/usage` (own spend only, used by a peer that opted this daemon in), the LLM `budget` object on `PUT /api/llms/{name}`, a `budgets` array in `GET /api/capacity` and `GET /api/autonomous/status`, and `autonomous.plan_tier_allocation_usd` on `PUT /api/config`. **(Phase 6, implemented)** `GET /api/capacity` gains a `tasks` object (`waiting` with `cause`, `reason`, `since`, `resets_at`, `holder`; `running` with `pools`, `node`, `session_id`, `note`; `?prd_id=` narrows it) and `reset_known` on each `backoff` entry; the session status `automaton` block gains `wait_cause`, `wait_resets_at`, `holds`, `holds_note`; `GET /api/audit` gains the `subject` filter, the trailing-`*` prefix form of `action`, and a 400 for a bad `since`/`until`. |
| MCP | Include | `autonomous_config_get`/`_set` with corrected descriptions (remove the false "Per-PRD worker cap (default 3)"). `capacity_status` gains `tree:`/`backoff:`/`budget:` pools. New read-only `autonomous_tree_locks`. **(Phase 3, implemented)** `llm_add`/`llm_update` carry `plan_tier` and `max_inflight`, `autonomous_config_set` accepts the tier table (object and pair forms); the `llm_update` `budget` field is still Phase 5. **(Phase 5, implemented)** `budget_status`; `llm_add`/`llm_update` carry the `budget` object; `autonomous_config_set` accepts `plan_tier_allocation_usd`; `capacity_status` includes the budgets. **(Phase 6, implemented)** `capacity_status` carries `tasks` and `reset_known`; `audit_query` gains `subject` and the action prefix form, with the capacity actions named in its description. |
| CLI | Include | `datawatch config set autonomous.project_write_serialization …` and the other keys. `datawatch autonomous tree-lock status`. **(Phase 3, implemented)** `datawatch llm add/update --plan-tier` (with `--max-inflight`); invalid tiers rejected before send. **(Phase 5, implemented)** `datawatch budget status [--llm]`; `datawatch llm add/update --budget-*` and `--clear-budget`, invalid combinations rejected before send. **(Phase 6, implemented)** `datawatch capacity` prints queued reasons and holdings; `datawatch audit` gains `--subject` and the action prefix form, and rejects a bad `--since`/`--until` before sending. |
| comm channel | Include | `configure <key>=<value>` for every new key. A status verb reply shows queued reasons and the active back-off. **(Phase 3, implemented)** `configure autonomous.plan_tier_max_inflight=…` through the generic configure path. **(Phase 5, implemented)** `budget [status] [<llm>]` replies with the same text as the CLI; `configure autonomous.plan_tier_allocation_usd=…` through the generic path. **(Phase 6, implemented)** `capacity` replies with the same text as the CLI; `audit` accepts `subject=`, `since=`, `until=` and `action=<prefix>*`. |
| YAML/config | Include | New keys in `config.AutonomousConfig` and in the LLM registry entry, annotated in `docs/config-reference.yaml`. `max_parallel_tasks` deprecation documented. The missing `max_concurrent_tasks` YAML field is added. **(Phase 6, N/A for new keys)** no key was added; a comment in `docs/config-reference.yaml` lists the audit actions the capacity gate, back-off and budget emit. |
| PWA | Include | Settings fields for the new keys; the `max_parallel_tasks` field replaced by `max_concurrent_tasks` in Settings (General list and Autonomous card) in Phase 1 (v9.0.86); the PWA no longer edits `max_parallel_tasks`, while the server still accepts and ignores it until the deferred removal. **(Phase 3, implemented)** LLM-edit *Plan tier* and *Max in flight* fields plus the autonomous *Plan tier caps* table setter (`app-plan-tier-fields.test.js`), all five locale bundles. Task chips for queued reasons. Automata list distinguishes approved-not-running. Capacity panel shows locks and back-off, plus (3c/4e) a per-peer "resource fingerprint sync" toggle and a confirmation list of detected shared-node/billing-group matches awaiting operator accept/reject. **(Phase 5, implemented)** LLM-edit budget group (ceiling, hours, weekday, time, zone, allocation estimate, peers), the Capacity card budget line (spend, window end, rollover, `budget window full`, estimated allocation) and the plan-tier allocation table in Settings (`app-llm-budget.test.js`), all five locale bundles. **(Phase 6, implemented)** Capacity and budget card on the Automata list (pools with holders, back-off flags with a reset time or "reset time unknown", budget spend bars with the estimate label), cause chips, the reset time and a Holds line in the task detail and on the session Status tab (`app-capacity-card.test.js`, `app-wait-reason-text.test.js`), all five locale bundles. |
| Android | Include, via a `datawatch-app` issue per the Mobile-Parity Rule | Queued-reason display and new config fields mirror the PWA. The REST contract change (new task wait reasons) must be parsed. **(Phase 3, needs a datawatch-app issue — not filed yet)** the LLM-edit `plan_tier`/`max_inflight` fields and the new wait reasons (`await_external: …`, `capacity: planner waiting …`) must parse and display. **(Phase 5, needs a datawatch-app issue — not filed yet)** the LLM `budget` object, the `budgets` arrays in the capacity API and the autonomous status, and the `budget window full` wait reason must parse and display. **(Phase 6, needs a datawatch-app issue — not filed yet, cannot be filed from the writing session)** parse the `tasks` object, `reset_known`, the session status cause fields and the audit `subject`/prefix filters; the issue text is `2026-10-22-datawatch-app-capacity-visibility-note.md`. |
| iPhone/iOS | Include, via the same `datawatch-app` issue | Same as Android, including the Phase 3 items. No iOS-specific behavior. Same Phase 5 items as Android (same issue, not filed yet). **(Phase 6)** Same as Android, same issue, not filed yet. |

## Open questions

1. **BL414: absorb, or grow a second mode?** This doc recommends that BL428
   absorbs BL414's budget-guardrail half (ceilings, hard stops) and that
   BL414 narrows to cost observability and Decision rollup (4c). Does the
   operator confirm this, and should BL414's backlog text be edited to say
   so?
   **Decided by the operator 2026-10-22: absorb.** BL428 owns ceilings and hard stops; BL414 narrows to cost observability and the Decision rollup, and its backlog text is edited to say so.
2. **BL405 Phase 2 overlap.** Should the windowed-ceiling ledger dimension be
   built inside this doc's follow-up PRD (Phase 5) as the first slice of
   BL405 Phase 2, or should Phase 5 wait for BL405 Phase 2?
   **Decided by the operator 2026-10-22: build it inside Phase 5** as the first slice of BL405 Phase 2; BL405 Phase 2 later builds on that ledger.
3. **Pattern-list drift guard.** Do the screen-scraped pattern lists
   (`rateLimitPatterns`, and by extension `selfUpdatePatterns` and the
   prompt patterns) need a staleness-check mechanism of their own? This doc
   recommends a manual recheck cadence plus a `claude --version`-change
   staleness flag, with a fixture self-test as a stretch (4d). Is that
   enough, or should the self-test be required?
   **Decided by the operator 2026-10-22: recheck cadence plus a version-change staleness flag.** The fixture self-test stays a stretch item, not required for 10.0.0.
4. **Tier defaults.** What `max_inflight` value should each `plan_tier` map
   to for `claude-code` (`pro`, `max_5x`, `max_20x`, `api`)? Is `1` the
   right conservative default for `unset`? — **Resolved (Phase 3,
   2026-10-22):** the table is documented as operator policy, overridable
   in one place (`autonomous.plan_tier_max_inflight`, default
   `pro: 1, max_5x: 2, max_20x: 4, api: 0`); `unset` falls back to the kind
   default, which is 1 for subscription kinds.
5. **Default for `project_write_serialization`.** Should `per_project_dir`
   be on by default, which changes today's behavior by serializing
   same-tree PRDs, or opt-in for one release?
   **Confirmed by the operator 2026-10-22: keep `advisory`** (the value shipped in BL428 Phase 1/2).
6. **Shared lock for worktree PRDs on the main repo key.** Is a
   visible-but-non-blocking shared lock wanted, or should worktree PRDs be
   fully independent of the main checkout?
7. **Back-off reach.** Should the reactive back-off also refuse *interactive*
   claude-code session starts (which effectively turns on BL30's
   `rate_limit_global_pause` automatically), or gate autonomous dispatch
   only?
   **Decided by the operator 2026-10-22: automatic work only.** Reactive back-off gates autonomous dispatch; interactive claude-code session starts are never blocked by it.
8. **Sequencing with BL431.** Should this doc's follow-up implementation PRD
   land before BL431's Automation-coordinator implementation PRD? BL431's
   BudgetPolicy and its per-member execution depend on these mechanics
   existing first. Cross-reference BL431's own open questions on this
   sequencing. This doc does not decide it. The 2026-10-14 operator addendum
   in `docs/plans/README.md` already orders implementation
   BL427 → BL428 → BL431.
9. **Child-PRD attribution.** Should PRDs created by a running task (like
   the scratch probe `392b2603`) record the creating PRD/task as a parent,
   so they show up in `autonomous_prd_children` and the lock holder graph?
   Or is that BL431's concern?
10. **Federation fingerprint-sync scope (3c/4e).** Should resource-fingerprint
    exchange be on by default for every federated peer, or opt-in per peer
    (this doc recommends opt-in — a peer with only `full-control` capability
    today, like `ralfthewise`, is not automatically the right trust level for
    resource fingerprinting, which is a separate decision from control
    capability)?
    **Decided 2026-10-22, as recommended and as built:** opt-in per peer,
    off by default, on both daemons — the peer entry's `fingerprint_sync`
    plus the responder's record of that peer; the `federation:fingerprint`
    capability (full-control only) is a precondition, not the trust decision.
11. **Hash key material (3c/4e).** Should the per-peer-pair HMAC key reuse
    existing federation auth/TLS material directly, or be a separately
    exchanged pairing secret generated once at opt-in time? The latter is
    more work but avoids coupling resource-fingerprinting security to
    whatever the control-plane auth happens to be.
    **Decided 2026-10-22, as built:** reuse the pair's existing federation
    bearer token as key material, with fingerprint-specific HMAC domain
    separation (the `datawatch-fingerprint-v1` tag) so fingerprint hashing
    stays distinct from control-plane auth; no separately exchanged pairing
    secret.
12. **Auto-accept vs. always-confirm for a fingerprint match.** This doc
    recommends surfacing every match for explicit operator confirmation
    before the ledger treats two resources as one pool. Is there a safe
    auto-accept case (e.g., only when both sides are on `full-control`
    federation already), or should confirmation always be manual?
    **Decided 2026-10-22, as recommended and as built:** always confirm,
    never auto-accept — the reconciliation loop only ever creates `pending`
    records; the operator confirms or rejects on any surface (PWA / CLI /
    comm / REST / MCP); each decision is a one-way audit event and a
    rejection sticks across syncs.
13. **Fail-open vs. fail-closed on an unreachable billing-group/shared-node
    peer at admission time (3c/4e).** This doc recommends fail-open (log and
    admit locally) so a peer outage never stalls local work. An operator
    managing a hard subscription ceiling may prefer fail-closed for the
    billing-group case specifically, even if 3c's hardware case stays
    fail-open. Should this be one shared setting or two independent ones?
    **Decided 2026-10-22, as recommended and as built:** fail-open, one
    shared behavior rather than two settings — an unreachable peer
    contributes zero to the shared-node gate and is logged once per
    failure episode; there is no fail-closed option in this slice, and the
    billing-group case enforces nothing at admission at all. See the
    "Resource fingerprint exchange (sections 3c and 4e) — shipped" section
    above for the shipped status, and `docs/howto/federation-fingerprint.md`
    for the operator guide.
14. **The tree lock (3a) should assess real conflict, not just same-tree
    exclusivity (operator-raised 2026-10-15, live judgment call this doc's
    own 3a doesn't currently cover).** Live example: two PRDs both
    targeting the main (non-worktree) tree — one doing real Go code changes
    under `internal/autonomous/`, the other writing only to `docs/plans/`.
    The operator's own call was to let the docs-only one proceed
    concurrently rather than block it behind the first, reasoning from
    actual file footprint rather than treating "same tree" as automatically
    exclusive. 3a as currently scoped is a binary per-canonical-tree lock —
    it does not have this nuance. Open: should 3a gain a lighter-weight
    "declared file/path footprint, non-overlapping footprints admit
    concurrently" mode alongside the exclusive lock, or is a binary lock
    with an explicit operator (or BL431 coordinator) override the right
    level of sophistication for v1, deferring smarter conflict detection to
    a later phase? Whatever is decided here is what BL431's coordinator
    (`onMemberTerminal`/advance logic) must consume faithfully — this
    judgment call should not keep being made ad hoc by whoever is driving
    PRDs by hand.
    **Decided by the operator 2026-10-22: opt-in per peer.** Resource-fingerprint exchange and shared-budget counting are off for every peer until switched on for that peer.

### 4d. Verifier split (operator decision 2026-10-18)

Decision: qwen is the worker on both GPU lanes and verifies routine tasks; Claude Sonnet
verifies the live-smoke and final-gate tasks and any task qwen fails twice. This keeps the
open models in the build (they find the harness defects) while the long, expensive smoke runs
stop holding a GPU for hours. It is a policy setting, not a hard rule (see BL498 and the
automation policy, BL483).

Measured on 2026-10-18 (tasks completed that day, worker and verifier time together):
Haiku worker with Sonnet verifier, 17 tasks, median 4.3 minutes, p90 6.5; qwen worker and
verifier, 5 tasks, median 18.5 minutes, p90 54.6 (small sample, long smokes included).
Verdict review (re-running each task's own verification script on the final branch state):
no sign of a wrong qwen verdict among 33 BL431 tasks and 3 BL479 tasks; the failures were
fragile scripts (a `tee | grep -q` pipe that exits 141) and two real gaps (embedded docs
drift, a file that is unformatted on main). Open: the rate of wrong verdicts in the other
direction (a correct task failed) is not measured, and the credit cost of one Sonnet
verification is not yet measured; BL479's cost is recorded at the next usage reading.

**2026-10-20 rule note (operator):** the weekly ceiling was exhausted on 2026-10-17 and reset on Sunday 2026-10-18 at 08:00. The section 3 "pi only" rule in `docs/plans/2026-10-17-supervisor-rules-and-lessons.md` was written during the exhaustion and is corrected there. Claude lanes are allowed under the lane model; this timeline is the usage record.

**Time and accuracy log (operator request, 2026-10-20).** One row per PRD that runs on a Claude lane or a GPU lane, so Claude and qwen can be compared. Fill `time` from start to verdict and `accuracy` from the verifier result and any rework.

| PRD | lane | worker model | verifier | start | verdict | time | accuracy (pass first try / rework) |
|---|---|---|---|---|---|---|---|
| (first entry: the next PRD run on a Claude lane) | | | | | | | |

**Tick 16:58 (2026-10-20):** weekly % not reported since 16:40 (19%). Pace allows about 34% at 2.4 days since the 10-18 08:00 reset, so one Claude-lane run is inside pace. Claude lane 1 runs `148b0001` (Haiku worker, Sonnet verifier). Operator to report the next weekly % reading.

**Tick 17:35 (2026-10-20):** no new weekly % reading since 16:40 (19%); pace allows about 34% at 2.4 days since the reset. Claude lane 1 runs `148b0001` (Haiku worker, Sonnet verifier); Claude lanes 2 and 3 replan `677d0001` and `51670001`. Operator to report the next weekly % reading.
**Tick 17:50 (2026-10-20):** no new weekly % reading since 16:40 (19%). Pace allows about 35% at 2.42 days since reset. Claude lane 1 runs 148b0001; lanes 2 and 3 replan 677d0001 and 51670001 after second revisions. Operator to report the next weekly % reading.
**Tick 18:10 (2026-10-20):** no new weekly % since 16:40 (19%); pace allows about 35% at 2.5 days since reset. Claude lanes: 148b0001, 51670001, 677d0001 (all Haiku workers, Sonnet verifier). GPU lanes: cece0001 (P0) and 49ac0001. Operator to report the next weekly % reading.
**Tick 18:30 (2026-10-20):** weekly % still 19% (16:40); pace about 35%. Finding: the three Claude-lane PRDs (148b0001, 51670001, 677d0001) verify task-by-task on qwen 64k, not Sonnet, because the PRD verifier can only be set before a run; their guardrail judge is Sonnet. Also 49ac0001's guardrail session ran on qwen 27b, not the pinned 64k. Running PRDs are locked for LLM overrides (HTTP 400), so neither can be changed in place.
**Tick 18:50 (2026-10-20):** weekly % still 19% (16:40); pace about 35%. GPU lane 1 runs 17880001 (main-checkout decompose fix); GPU lane 2 reruns cece0001 (P0) after a verdict-parse failure on its last task. 49ac0001 completed. Claude lanes: 148b0001, 51670001, 677d0001 running. Operator to report the next weekly % reading.
**Tick 22:35 (2026-10-20):** no new weekly % since 16:40 (19%); pace allows about 38% at 2.6 days since reset. Claude lanes: df790001 (tmux guard, Sonnet), decomposes 215f0001 and 2f1b0001. GPU lanes idle by the BL529 hold. Operator to report the next weekly % reading.
**Tick 22:57 (2026-10-20):** no new weekly % since 16:40 (19%); pace allows about 38% at 2.6 days since reset. Claude lane 1 runs df790001 (Sonnet); decomposes running for 4cbc0001, 9db60001, 81b70001. GPU lanes idle by the BL529 hold. Operator to report the next weekly % reading.
**Tick 23:15 (2026-10-20):** no new weekly % since 16:40 (19%); pace allows about 39% at 2.6 days since reset. Claude lanes run df790001 (Sonnet), 9db60001 (Haiku) and 4cbc0001 (Sonnet), each with a Sonnet verifier. GPU lanes idle by the hold. Operator to report the next weekly % reading.
**Tick 00:00 (2026-10-21):** no new weekly % since 16:40 (19%); pace allows about 40% at 2.7 days since reset. Claude lanes: 4cbc0001, 9db60001, 60270001. GPU lanes: 215f0001 and the verifies of the Claude PRDs. 2f1b0001 paused for the two-engine limit. Operator to report the next weekly % reading.
**Reading 14:40 (2026-10-21), operator-reported:** weekly 30%. Days since reset (Sun 2026-10-18 08:00): 3.27, so the pace rule allows 46.7%; headroom about 16 points. Supervisor error recorded: from 2026-10-21 about 11:40 the free Claude lanes were held back waiting for a reading, and the GPU lanes were left idle by defaulting new PRDs to Claude workers; both cost throughput while pace had room. Rule: a stale reading is asked for once, then work continues at the last pace-safe rate; free lanes are filled from the roadmap (rows 10, 11, 12 of docs/plans/README.md) and GPU lanes take mechanical PRDs on pi-prompt.
**Reading 18:30 (2026-10-21), operator-reported:** weekly 33%. Days since reset 3.44, so the pace rule allows 49.2%; headroom about 16 points. The supervisor's earlier projection (about 40% at 18:16) was 7 points high: the +4 per 24 hours and +2 per Claude PRD coefficients overestimate. Calibration so far: 30% at 14:40 to 33% at 18:30 is +3 points over 3.8 hours with three full Claude PRDs, three decomposes and several merges, about +1 per full Claude PRD. Tick projection from this reading: +3 per 24 hours elapsed, +1 per full Claude PRD started, +0.5 per decompose or merge session.
**Reading 19:40 (2026-10-21), operator-reported:** weekly 34% (reset Sunday 08:00 EST, unchanged), session 34% (the hourly session window; resets 20:10 daily). Weekly pace allows 49.9% at 3.49 days; headroom about 15 points. The session window is the short-term limit: at 34% there is room, and it resets at 20:10. Operator allowed one extra Claude BUILD lane (workers only; no extra decompose or validate lane).

## Inflight caps set to match the lane model (2026-10-22 00:25)

After the Phase 3 capacity admission shipped (9.5.68) and the daemon restarted onto it, the `claude-code` LLM entry fell to the kind default of one inflight task, so a second Claude PRD, and every Claude-backed decompose, waited in `waiting_capacity` ("pool llm:claude-code full (1/1)"). The explicit per-LLM cap wins over the plan-tier table, so the supervisor set `max_inflight` to match the operator's lane model: `claude-code` = 3 (three Claude lanes), `pi-prompt` = 2 (two GPU lanes; the per-node pool still limits each compute node to one engine). The plan-tier table (`pro` 1, `max_5x` 2, `max_20x` 4) is untouched. If the operator wants a fourth build lane, raise `claude-code` to 4.

**Reading 12:36 (2026-10-22), operator-reported:** weekly 45%. Days since the 10-18 08:00 reset: 4.19, so the pace rule allows 59.8%; headroom about 15 points. Compared with the 19:40 reading on 10-21 (34%) this is +11 points in 16.9 hours, about 15.6 points per 24 hours, against the earlier planning figure of +3 per 24 hours. The cause is the 10-21 evening to 10-22 midday run: three to four Claude PRDs at once, twelve merges and many decompose sessions. The allowance itself accrues at 14.3 points per 24 hours, so the recent burn is about equal to the allowance, not above it. Recalibrated projection coefficients (replace the old ones): about +1.5 points per full Claude PRD started, +0.3 per decompose or merge session, +4 per 24 hours of idle baseline. The earlier projections (55 to 58%) were too high because they used the old per-PRD figure and had no reading since yesterday. With 45% and 2.8 days left, a steady three-lane Claude load (about 15 points per day) ends the week near 87%, inside the allowance.

**Reading 20:44 (2026-10-22), operator-reported, saved for the usage calculator (BL472):** weekly 55%. Days since the 10-18 08:00 reset: 4.53, so the pace rule allows 64.7%; headroom about 9.7 points. Since the 12:36 reading (45%) that is +10 points in 8.1 hours, about 29.6 points per 24 hours, roughly double the allowance accrual of 14.3 per 24 hours. Load in that window: two to three Claude PRDs at once, six merges (9.5.95 to 9.5.98), about twenty decompose attempts (most killed at the launch stall and retried, which spent usage for nothing) and many reset-and-retry worker launches. My projection for this time was 61%, so the projection ran 6 points high and the coefficients above overstate the cost of a full PRD run slightly; recalibrate from the next reading. At 29.6 per 24 hours the week would end near 128% against the 100% limit at the 10-25 08:00 reset, so the Claude load has to fall to about 18 points per 24 hours or less: the launch-stall fix (BL569) removes the largest waste.

**Reading 02:40 (2026-10-23), operator-reported:** weekly 58%. Days since the 10-18 08:00 reset: 4.78, so the pace rule allows 68.3%; headroom about 10.3 points. Since the 20:44 reading (55%) that is +3 points in 5.9 hours, about 12.2 points per 24 hours, below the allowance accrual of 14.3 per 24 hours. Load in that window: two to three Claude PRDs running at once (BL393, BL431 Phase 7, the earlier BL570 and BL571), about eight merges and releases, six decompose sessions, and every verifier on the local pi slots rather than Claude. My projection for this time was 68%, so it ran 10 points high: the per-PRD and per-decompose coefficients above overcount, and the 20:44 reading shows the earlier 29.6 per 24 hours was driven by the launch-stall retries that BL569 removed. Recalibrated for planning: about +0.4 points per full Claude PRD started and +0.15 per decompose or merge session, +3 per 24 h baseline, until the next reading. At 12.2 per 24 hours the week would end near 85% at the 10-25 08:00 reset, so no Claude hold is needed.

**Reading 17:09 (2026-10-23), operator-reported:** weekly 67%. Days since the 10-18 08:00 reset: 5.38, so the pace rule allows 76.9%; headroom about 9.9 points. Since the 02:40 reading (58%) that is +9 points in 14.5 hours, about 14.9 points per 24 hours, just above the allowance accrual of 14.3 per 24 hours. Load in that window: Claude PRDs BL580 and BL583 plus Claude workers on two Observer tasks, seven merges and releases (9.5.116 to 9.5.121), a decompose, and the supervisor session itself, which has run a tick every 20 minutes. At 14.9 per 24 hours the week would end near 91% at the 10-25 08:00 reset (1.62 days away), which is inside the cap but with less margin than before. The Claude hold held since 02:40 is lifted: lanes fill from the normal sources again, with Claude kept for hard work and decomposes and the GPU lanes taking mechanical PRDs. Planning coefficients stay at about +0.4 per full Claude PRD, +0.15 per decompose or merge, +3 per 24 h baseline until the next reading.

**Reading 22:08 (2026-10-23), operator-reported:** weekly 72%. Days since the 10-18 08:00 reset: 5.59, so the pace line is 79.9% and the headroom is about 7.9 points. Since the 17:09 reading (67%) that is +5 points in 5.0 hours, about 24 points per 24 hours (the week-to-date average is 12.9 per 24 hours). The reset is at 10-25 08:00, 1.41 days away. Projections to the reset: 106.0% at the recent 24 per 24 hours (the cap would be reached around Sunday 02:02, about six hours before the reset), 90% at the week-to-date average. *Corrected 2026-10-25 (operator decision, "1" on the BL472 calculator question): this entry first said 1.39 days, 105% and a run-out around 05:00; the timestamps give 1.41 days, 106.0% and 02:02, as the usage calculator design's worked example notes.* The supervisor reported this to the operator as over budget at the current burn and let the operator choose the number of Claude lanes.

**Operator decision, 2026-10-23: the usage policy is the operator's choice, never a standing supervisor hold.** The running calculator must offer the policy as an option, because operators differ: run until the allowance is used up; pace the work to last until the end of the week; or run flat out because the work will finish before the allowance does. On every usage reading the supervisor advises whether the current burn is within or over budget for the week and lets the operator choose how many lanes to run (for example one, two or three Claude lanes). The supervisor never stops all Claude lanes on its own projection. The 02:40 to 17:09 hold on 2026-10-23 broke this: it left Claude lanes idle for about 14 hours and was recorded as the wrong call.

**Reading 23:40 (2026-10-23), operator-reported:** weekly 76%. Days since the 10-18 08:00 reset: 5.65, so the pace line is 80.7% and the headroom is about 4.7 points. Since the 22:08 reading (72%) that is +4 points in 1.5 hours, a burst of about 63 points per 24 hours, far above the week-to-date average of 13.4 per 24 hours (the burst was three Claude lanes plus many decomposes and merges). The reset is 10-25 08:00, 1.35 days away. Projections to the reset: about 94% at the week-to-date average, and over 100% if the burst rate continued (the cap would be reached early Saturday morning). Reported to the operator as over budget at the current burn. **Operator decision:** drop to 2 Claude lanes now (`llm update claude-code --max-inflight 2`), so the Saturday morning switch to 2 is already done; the return to 3 at Sunday 08:00 stands.

**Reading about 14:17 (2026-10-24), operator-reported:** weekly 86%. Days since the 10-18 08:00 reset: 6.26, so the pace line is 89.4% and the headroom is about 3.4 points: within the pace line. The 06:45 reading was 79%, so the burn is +7 points in 7.5 hours, about 0.93 points per hour with two Claude lanes. At that burn the weekly allowance reaches 100% at about 05:30 on Sunday 2026-10-25, about 2.5 hours before the 08:00 reset (projected 102.5% at reset): over budget at the current burn. The operator chooses the lane count; a one-lane setting for the last 17 hours would land near 94%. Verifier routing was changed at 14:13 to prefer-free (pi-prompt first, a Claude verifier when no GPU slot is free), which adds Claude sessions; no cap is set on it.

**Reading about 07:25 (2026-10-25), operator-reported:** weekly 87%. Days since the 10-18 08:00 reset: 6.98, so the pace line is 99.7% and the headroom is about 12.7 points: within the pace line and within budget. Since the 14:17 reading (86%) that is +1 point in 17 hours, because the lanes were idle for most of that time (see the lesson of 2026-10-24 in the supervisor rules). The allowance resets at 08:00, 35 minutes from now, so there is no budget reason to hold the 3-lane setting planned for 08:00.

**Reading 08:00 (2026-10-25), operator-reported, new week:** weekly 0% after the reset. Days since the reset: 0.0, so the pace line starts at 0% and rises 14.3 points a day. The previous week used 87% over 6.98 days, an average of 12.5 points a day with two to three Claude lanes, which is under the 14.3 points a day pace line: within budget at the current burn with 3 lanes. The lane count stays the operator's choice; 3 lanes are set.

**Reading about 19:45 (2026-10-25), operator-reported:** weekly 15%. Days since the 08:00 reset: 0.49 (11.7 hours, not 24), so the pace line is 6.9% and the reading is about 8 points over it: **over budget at the current burn**. The burn is about 1.3 points an hour (about 31 a day, against a pace line of 14.3 and last week's 12.5), so the allowance would run out in about 66 hours (Wednesday afternoon). Operator, quoted: "Claude is at 15%, might have to stick with 2 lanes and decompose and validate, we aren't 24 hours in are we?" Applied the same tick as `datawatch llm update claude-code --max-inflight 2` (running tasks finish; new admissions are capped at 2). Claude is kept for decompose, validate and design-heavy work; mechanical execution goes to the GPU lanes once the pi-prompt idle-detector fix (BL614) ships. Lane count stays the operator's call.

**Operator clarification, 2026-10-25 about 19:50:** lanes stay at 2, not 3 ("No, 2 claude lanes"; "Not 3. We were at 3 right, 15% is more than 14 we need to stick to on 24 hr"). The operator reads the budget as about 14 points per 24 hours, so 15% after 11.7 hours is already past the whole first day's allowance (14.3), not only past the prorated pace line (6.9%). At 2 lanes the burn would still be roughly 20 points a day by the 31-a-day, 3-lane rate, so 2 lanes alone does not bring the week back to 14 a day; the next lane-count step is the operator's call. Both supervisor crons were recreated with the 2-lane cap (the old ones removed).

**Operator decision, 2026-10-25 about 20:00 (lane count question 1):** option 2, one Claude lane until 08:00 Monday 2026-10-26, then re-evaluate. Operator, quoted: "Option 2 for now". Applied as `datawatch llm update claude-code --max-inflight 1` (running tasks finish; new admissions capped at 1). Claude covers decompose, validate and the pi-prompt idle-detector fix (BL614); GPU lanes take mechanical execution once BL614 ships.

**Reading about 06:25 (2026-10-26), operator-reported:** weekly 22%. Days since the 2026-10-25 08:00 reset: 0.93, so the pace line is 13.3% and the reading is about 8.7 points over it: **over budget at the current burn**. Since the 19:45 reading (15%) that is +7 points in 10.7 hours, about 0.65 points an hour or 15.7 a day, against the 14.3 a day pace line. At that rate the remaining 78 points last about 120 hours, so the allowance runs out around Saturday 06:30, about 26 hours before the next reset. Operator decision, quoted: "22%, keep one lane". The Claude cap stays at `max-inflight 1` after 08:00. Note: that burn is higher than one worker lane alone would explain; the supervisor session itself (a tick every 30 minutes plus an hourly backstop, each with a long prompt) is a large part of it, and the lane count does not change that.

**Reading about 13:35 (2026-10-26), operator-reported:** weekly 24%. Operator, quoted: "claude at 24%". Days since the 2026-10-25 08:00 reset: 1.23, so the pace line is 17.6% and the reading is about 6.4 points over it, but the burn has fallen sharply: +2 points in 7.2 hours since the 06:25 reading (22%), about 0.28 points an hour or 6.7 a day, against 15.7 a day before it. The change that preceded it is the move to a 20 minute cheap watch plus an hourly full tick (supervisor schedule of 2026-10-26, rules doc section 11a) with the Claude cap at one lane. At 6.7 a day the week would end near 63% at the reset (5.76 days away): **within budget at the current burn**. The remaining allowance is about 13.2 points a day, so there is room for roughly double the current Claude draw. The lane count is the operator's choice; the cap stays at one lane until the operator answers.

**Operator decision, 2026-10-26 about 13:40 (lane count question 2):** two Claude lanes. Operator, quoted: "Lays go with 2 lanes, I'll keep an eye on it". Applied as `datawatch llm update claude-code --max-inflight 2`. The projection at two lanes is roughly 10 to 13 points a day, ending near 85 to 100% at the reset, so the operator watches the usage and the supervisor asks for a reading at each hourly tick. The headroom goes first to the CodeQL alert triage (BL625) and to the live debugging of the GPU node placement (BL624 follow-up).

**Operator correction, 2026-10-26 about 13:45 (lane count question 2):** option 3, not two lanes. Operator, quoted: "Actual 3 2ad a better option" (read as: option 3 is the better option). Option 3 is one Claude lane with the headroom spent on specific items. Reverted to `datawatch llm update claude-code --max-inflight 1`, replacing the two-lane decision recorded a few minutes earlier. The headroom goes to the CodeQL alert triage (BL625, PRD 5a390001) and to the live debugging of the GPU node placement. The 24% reading stands: burn about 6.7 points a day on one lane, projected near 63% at the reset.

**Operator refinement, 2026-10-26 about 13:50:** operator, quoted: "Stay at 1 but use 2nd for special". The standing Claude cap stays at 1 (`max-inflight 1`). The supervisor may raise it to 2 for a special item and returns it to 1 when that item's Claude work is done. Special means: a defect that blocks other work or throughput (for example the GPU node placement), a security finding that needs triage (BL625), or a merge that cannot wait. Every use of the second lane is stated in the next report with its reason and the time it was opened and closed. Ordinary feature work stays on the one lane.

**Incident, 2026-10-26 about 14:00: three pi workers ran on node datawatch at once.** Operator, quoted: "3 gwen on datawatch at once, it was suppose to be 1 pi on each gpu, how did this get broken again". Causes, in order: (1) from the node balancing release (2026-10-25 18:41) a worker's own lease carried no node pool, so nothing held a node's one engine for it (noted that evening as a gap to watch, not fixed); (2) every daemon restart re-adopted the surviving workers with only a pool lease, so the ledger showed the node as free and admitted more work onto it (about eight restarts on 2026-10-26: deploys and re-placement experiments); (3) the supervisor raised the pi pool cap from 2 to 3 and 4 several times to put the idle local GPU to work, but placement always chose datawatch, so each extra session landed there; (4) the root cause: placement resolves the node through the provider-kind (ollama) LLM entries, which are single-node, so the pi-prompt entry's two-node list was never consulted (BL626, PRD fe9c0001). The earlier placement fixes passed their unit tests with fake registries that did not have this live shape. Action: pi cap returned to 2 (the one-pi-per-GPU lane model) so nothing more is admitted until BL626 ships; no further cap raises as a workaround.

**Reading about 19:30 (2026-10-26), operator-reported:** weekly 26%. Operator, quoted: "26% used". Days since the 2026-10-25 08:00 reset: 1.48, so the pace line is 21.1% and the reading is about 4.9 points over it. Since the 13:35 reading (24%) that is +2 points in 5.9 hours, about 8.1 points a day (6.7 before it). That window included the second Claude lane from 13:43 to 15:33 for the GPU placement fix (BL626), the CodeQL triage (BL625) and the BL616 retry. The remaining 5.52 days at 8.1 a day end near 71% at the reset: **within budget at the current burn**. The standing cap stays at one Claude lane; the second lane is opened for special items only.

### Usage reading 2026-10-27 about 04:00

Operator: "Claude is at 27%". The week started at the 2026-10-25 08:00 reset, so 1.83 days have passed and the pace line is 26.2%: the reading is 0.8 points over it, essentially on the line. Since the 26% reading at 19:30 on 10-26 the burn is 1 point in 8.5 hours (about 2.8 a day) because the Claude lane was nearly idle. The remaining 5.17 days end near 41% at that burn and near 69% at the earlier 8.1 a day: within budget either way, with room for a second Claude lane on real work.

### Decisions 2026-10-27 about 05:30 (operator answers, quoted)

- Local-model time limit: operator, quoted: "Local model can continue, it's there any way to tell if it is still running?" Decision: no automatic hand-off on elapsed time; instead the supervisor needs a reliable liveness signal (open item: log growth plus child process CPU and wait state plus GPU use), so a stalled worker is told apart from one still generating.
- CodeQL alerts: operator, quoted: "2 yes fix the issue". Decision: triage each open alert's location, fix the code or dismiss with a recorded reason; no GitHub setting changes.
- Second Claude lane: operator, quoted: "4 yes open 2nd lane". Decision: Claude cap raised from 1 to 2 for ordinary work at 05:32 on 10-27 (usage 27%, on the pace line). Supersedes "Stay at 1 but use 2nd for special" (2026-10-26).
- Design questions: operator, quoted: "5 if there are decisions ask". Decision: the supervisor asks, one at a time, when a design decision is needed.
- Housekeeping: operator, quoted: "6 housekeeping is build supervisor job". Done: issue #210 closed with a note naming v9.5.84. The duplicate `v9.0.0` CHANGELOG heading is a non-exempt file, so it ships with the next release's version bump. No stable tag is named yet (no soak period has run).

### Usage reading 2026-10-27 about 07:40 and judge routing decision

Operator: "Claude is at 30%". Days since the 2026-10-25 08:00 reset: 1.98, so the pace line is 28.3% and the reading is 1.7 points over it. Since 27% at about 04:00 the burn is 3 points in about 3.7 hours (about 19 points a day) with the second Claude lane open since 05:32 and both slots almost always busy. If both lanes stayed that busy the week would end well over 100%; at the one-lane rate of about 8 a day it ends near 70%. The operator picks the lane count; the supervisor reports this at the next tick and does not close the lane on its own projection.

Final judge routing: operator, quoted: "size aware rooting sound good". Decision: a diff above a size threshold (proposed 400 changed lines; the setting defaults to 400, with 0 meaning off) goes to the Claude backend first instead of spending the judge time limit on the local model; a judge that times out on every backend still holds the PRD for review.

### Usage reading 2026-10-27 about 10:55

Operator: "33% claude". Days since the 2026-10-25 08:00 reset: 2.12, so the pace line is 30.3% and the reading is 2.7 points over it. Since 30% at about 07:40 the burn is 3 points in about 3.25 hours (about 22 points a day) with the second Claude lane open. The remaining 4.88 days end near 72% at the one-lane rate of about 8 a day, and well over 100% if both lanes stay this busy. The lane-count question (keep two until the in-flight PRDs finish, drop to one, or special items only) is still open with the operator. Interim supervisor rule, within the cap of 2: the second lane takes only defects, in-flight PRDs and retries of held tasks, and no new feature PRD is started on Claude until the operator answers or usage is back on the pace line. The supervisor closes the second lane at the next reading if usage is more than 3 points over the line.

**Decision 2026-10-27 about 11:00, operator, quoted: "Keep 2 for now, so see how it goes in 24 hours".** The Claude cap stays at 2 for 24 hours, until about 11:00 on 2026-10-28, with normal lane fill (the interim restriction on new feature PRDs and the automatic close at more than 3 points over the line are withdrawn). The supervisor reports the usage reading and burn against the pace line at every hourly tick and asks for a fresh reading, and the operator reviews the result at about 11:00 on 10-28 (keep two, drop to one, or special items only).

### Operator answers 2026-10-27 about 12:00

- Usage: operator, quoted: "2 still 33%" (still 33% at about 12:00, 5 hours after the 07:40 reading; the transcript-based burn since 10:55 is about 0.3 points).
- Supervisor tick schedule: operator, quoted: "3 no" (keep the 20-minute watch and the hourly full tick through the 24-hour trial).
- CodeQL alerts: operator, quoted: "1 option 1" (get a local CodeQL CLI, iterate the fixes against it, push only code that clears the alerts; no dismissals of product alerts and no GitHub setting changes).

### Usage reading 2026-10-27 about 13:55

Operator: "Claude is at 36%". Days since the 2026-10-25 08:00 reset: 2.25, so the pace line is 32.1% and the reading is 3.9 points over it. Since 33% at 10:55 the burn is 3 points in 3 hours (about 24 points a day). Normalized units from the transcripts: 3.52 million units for those 3 points (1.17 million per point, the heaviest window per point so far, with merge helpers and two large Claude PRDs running), and 24.4 million units since the reset for 36 points, a pooled 0.68 million per point (was 0.655 million). At the week-average burn of about 16 points a day the week ends near 112 percent. The two-lane trial the operator asked for runs until about 11:00 on 2026-10-28.

### Usage reading 2026-10-28 about 03:50

Operator: "45 % used". Days since the 2026-10-25 08:00 reset: 2.83, so the pace line is 40.4% and the reading is 4.6 points over it. Since 36% at about 13:55 on 10-27 the burn is 9 points in 13.9 hours (about 15.5 points a day, mostly during the busy evening; the overnight rate is under 2 points an hour). Normalized units: 11.99 million for those 9 points (1.33 million per point); 51.0 million since the reset for 45 points (1.13 million per point, refit; the earlier 0.65 million figure was wrong because the extraction left out the first day). At the week-average rate of about 16 points a day the week ends near 110 percent; at the one-lane rate of about 8 a day it ends near 78 percent. The 24-hour two-lane trial ends at about 11:00 today; the operator reviews it then.

### Usage reading 2026-10-28 about 05:55

Operator: "Claude is at 46%". Days since the 2026-10-25 08:00 reset: 2.91, so the pace line is 41.6% and the reading is 4.4 points over it. Since 45% at about 03:50 the burn is 1 point in about 2 hours, the quiet overnight rate; the token estimate for the same span was about 0.4 points, within the rounding of whole-percent readings. The refit calibration (1.13 million units per point) tracks the readings. The 24-hour two-lane trial ends at about 11:00 today.

### Usage reading 2026-10-28 18:17 (saved by the operator in the usage calculator)

Operator, quoted: "I just saved% claude in calculator in automation, could you set in the data we've been capturing and your can get data i put in and make sure it looks right". The reading: 50% weekly, 6% session, at 18:17 EDT (22:17 UTC), source typed. The supervisor added the ten earlier readings from this file (15% 10-25 19:45 through 46% 10-28 05:55, EDT times converted to UTC) to the calculator series so it has history.

Check of the calculator's output against this file: with history, recent burn is 7.8 points per 24 h (4 points from 05:55 to 18:17, matching the supervisor's own figure) and the week average is 13.9. Two things do not look right. (1) `usage.calculator.reset_zone` is UTC, so the calculator puts the reset at Sunday 08:00 UTC (04:00 EDT) and counts 3.6 days since the reset; the reset the operator has used all week is Sunday 08:00 EDT, 3.43 days elapsed, pace line 49.0%, so the 50% reading is 1.0 point over the line, not 1.4 under, and the week-average projection is about 102% (over budget) instead of 97.4%. (2) The lane advice ("3 lanes end the week at 60.2%") rests on the defaults of 0.1 points per task and 3.0 points idle burn, and its remaining work counts one queued task; neither matches this week's measured burn. Both are for the operator to decide (the zone is a setting; the per-task cost is a calibration).

### Calculator settings applied 2026-10-28 18:25

Operator, quoted: "Yes set reset zone to New York And calibrate and make sure the time zone can be set for other users". Applied through `datawatch config set`: `usage.calculator.reset_zone` = America/New_York; `usage.calculator.baseline_per_24h` = 4.4; `usage.calculator.cost_per_task_points` = 0.05. Zone effect: the calculator then follows the daylight-saving change (the reset is Sunday 08:00 EST on 2026-11-01, so the week is 169 hours), pace line 48.7% at the 18:17 reading, 3.43 days elapsed; the week-average projection became 102.7% (over budget) and the recent-burn projection 78% (within budget).

Calibration data (transcripts since the 2026-10-25 08:00 EDT reset, normalized units over 1.13 million per point): total 50.5 points against the typed 50% (the weights hold); autonomous task sessions 35.3 points over 909 Claude sessions, so 0.039 points per session (workers 0.040, rules-review judges 0.026, verifiers 0.044); the supervisor and operator chat in the datawatch directory 15.1 points, which is 4.4 points per 24 h of non-task burn. Settings chosen: baseline 4.4; per-task cost 0.05 (0.040 for a worker session plus the judge, verifier and retry share, about 1.25 sessions per task). The old defaults were 3.0 and 0.1.

Time zone for other users: `reset_zone` is settable on YAML, REST (a bad name is refused with "not an IANA zone name"), the CLI, the PWA settings card and the comm channel, and `docs/howto/usage-calculator.md` tells users to use a zone name, never an offset. The default stays UTC, which is wrong for most users; whether the default should be the daemon's own time zone is an open question for the operator.

Time zone default for other users, operator 2026-10-28, quoted: "Default to zone server is in". The calculator's reset zone default becomes the zone the daemon runs in (registry row 241); an explicit value, including UTC, always wins.

### Usage reading 2026-10-29 about 03:46

Operator: "Claude is at 53%, pi only is good, what else is needed to get everything done, are automation ask done, how many prd and bugs left to 10.0?" The reading was typed into the usage calculator at 03:46 EDT. The calculator then showed: pace line 54.3 points at 3.82 days elapsed (the reset is Sunday 08:00 EST), so 53% is 1.3 points under the line; recent burn 7.6 points per 24 h, week average 13.9; projected end 77.4% at the recent burn and 97.6% at the week average, both within budget, nothing running out before the reset. The supervisor's own estimate overnight was 51 to 52%. The "pi only is good" part answers the eval matrix question: the matrix runs on pi workers only, the opencode roles are dropped (registry row 243).

## Reading 2026-10-30 05:05 EDT: 67%

Operator, 2026-10-30, quoted: "Claude is at 67%". The calculator records 67% at Fri 05:05 EDT against a pace line of 69.3 (headroom 2.3 points). Recent burn is 19.6 points per 24 h (the 11 points since 56% at 15:35 on 10-29 came mostly from the overnight Claude work: three attempts at the search-before-create final gate, the large security-failure auto-fix, and decomposes), the week average is 13.7. At the recent burn the week ends at 109% and runs out Sat 21:35 EDT; at the week average it ends at 96.7%. The advice for three lanes is 76.5%. The Claude cap stays at one lane and I will not start Claude work that the pi lanes can do.

## Reading 2026-10-30 13:28 EDT: 70%, and the decision on Claude lanes to Sunday

Operator, 2026-10-30, quoted: "Just saved 70%, not sure why it asks how many lanes running, it's that how many i want or how many are running?" The lanes field of a reading is how many Claude lanes were running when it was read (a fact used to work out the burn per lane); the number of lanes to run is a separate choice. The reading was saved with 2 lanes; only one Claude lane was running, so it was corrected to 1 (the reading was deleted and added again at the same time).

With 1 lane running the calculator says: pace line 74.2, headroom 4.2 points, 1 lane ends the week at 85.6 to 94.3%, 2 lanes end at 93.2%, 3 lanes run out about 1.2 hours before the Sunday reset. The choice was put in the question queue (`q-18e35eec6f27ca03cafd`, "Claude lanes to Sunday"; the first version, `q-18e35eeae5ae704ca8d9`, used figures from the wrong lane count and was withdrawn).

Operator answer, 2026-10-30, quoted: "2 lanes, go ahead". Two Claude lanes run until the Sunday 08:00 reset. The second lane takes the slowest feature work (first the worktree and temp cleanup, with a Claude judge for verification, then the automatic held-state questions). Re-read the percentage every few hours and drop back to one lane if the recent burn would run the week out before the reset.
