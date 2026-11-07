# Agent mail between peers: design pass

Status: design pass, documents only, nothing decided.

## Scope

This record answers one question for six requests that came in as GitHub issue 211. For each request, can it be done with what datawatch already has, can it be done as a skill or howto with no code change, or does it need a feature? Where a feature is needed, the record names the smallest one and its downsides.

The requester runs several AI agents (siblings) on three machines. Each agent has two or three simultaneous sessions (seats). The siblings need to leave each other messages and know those messages arrived and were read.

In scope: reading the shipped building blocks (the work queue, discussion scopes with the write ahead log and peer replication, discussion subscribe, exit hooks, alert rules, schedule spawn, memory handoff, the identity tools, channel routing, federation) and judging each request against them.

Out of scope: any code, any config change, any new Automaton beyond this one, and any decision. The operator decides. The sequencing rule from the 2026-10-15 answer stands: this work comes after the 10.0.0 release work, and it builds on the work queue and the discussion scopes instead of adding a parallel messaging layer.

The checks behind each verdict were made by reading the how-to pages for discussion scopes, queue advance and cross-agent memory, and the discussion code in the server (the write handler, the write ahead log append and read, the peer receive handler, the conflict detector) and in the MCP and session packages (subscribe and dispatch).

## The operator's words

The public answer of 2026-10-15, as the operator gave it: this is genuine new scope, not a duplicate of the work queue or the discussion scopes; it is a design pass that builds on those, not a parallel messaging layer, and it is sequenced after the 10.0.0 release work.

The words of 2026-11-07, exactly: "that needs to be validated and checked against queue of prd completed and open, but get it in the queue, but let me know if i need design decisions and is it can be implemented with anything we already have or a skill or if it needs a feature and what down side of feature"

Validation against the queue of completed and open Automata found no Automaton that delivers any of the six requests. Three neighbours exist: the federation-as-mail-transport item (not started), the identity scaffold work that already shipped (it bootstraps the operator identity and does not hand an identity from seat to seat), and a placeholder for personal agents with personality that needs a real design session first. The first and third are the closest overlaps and should be read with this record before anything is filed.

## Summary of verdicts

| # | Request | Verdict |
|---|---|---|
| 1 | Read-back delivery receipts | needs a feature |
| 2 | Per-seat read state, change wins merge | needs a feature |
| 3 | Writer provenance | needs a feature |
| 4 | Wake on arrival | needs a feature |
| 5 | Small per-seat views of large stores | possible as a skill or howto with no code change |
| 6 | Multi-aspect identity handoff | possible as a skill or howto with no code change |

Nothing in the six is already possible with existing tools in the full sense the requester means. Request 5 comes closest, and its skill is the cheapest win.

## How the building blocks behave today

These facts, found in the code, drive several verdicts.

- The discussion write handler saves the entry to the memory store and appends it to the write ahead log on the writing node. It then pushes the entry to each participant peer in the background. The push ignores the peer's reply and drops every error. The writer never learns whether any peer stored anything.
- The peer receive handler appends to the write ahead log only. It does not save the entry to the memory store, so semantic recall on the receiving node does not show a replicated entry. Only the write ahead log read does. The handler discards the append result and always answers ok.
- The sequence number is the count of lines already in that node's log. The receiving node assigns its own number and does not keep the sender's. The payload carries the sender's sequence number, but the receiver drops it. The same message therefore has a different number on each replica.
- Replication is a one-way push per writing node, from the participant list set on that node. There is no catch-up, no anti-entropy and no pull. A push lost to a down peer is lost for good.
- Conflict detection groups entries by the first 64 characters of content from different origin peers within five seconds. Resolving a conflict appends marker records that tombstone the losers. It is a pick-a-winner tool for duplicate writes. It is not a field merge.
- The origin peer and the role on an entry are values the caller sends in the request. The server does not stamp them from the authenticated identity.
- Discussion subscribe delivers an entry to a named session by sending it as input. It runs only from the MCP write path on the node where the write happened. A write by REST, and an entry arriving from a peer, never trigger it. If no session with that name is running, the entry is skipped without a trace.
- The write ahead log read through MCP takes a count, an after-sequence cursor and an optional long-poll block with a timeout. Semantic recall takes only a result count.
- The work queue is role based with claim, lease, complete and fail. It lives on one daemon. Completion is recorded on that daemon only, and a claim is an acknowledgement by a worker, not a delivery proof. It has no per-recipient addressing and no cross-node replication of its own.
- Discussion writes are throttled to 60 per minute per bearer token.

## 1. Read-back delivery receipts

What the request means. When a message is sent to a sibling on another machine, the sender gets a receipt that is a read-back. The message is proven present in the recipient's store because the recipient's node read it back after storing it. A receipt that only says the request was accepted is not enough.

What exists today. Participant sync pushes an entry to each peer's receive endpoint. The reply is thrown away, and the receive handler answers ok even when the append failed. The work queue records completion on its own node only. The write ahead log read on the recipient node can show the entry, so the raw material for a read-back exists. The log read can also be called through federation by a sender with the right capability, but nothing ties that read to the original send, and the entry's sequence number differs per replica, so the sender has no stable way to name what to look for.

Verdict: needs a feature.

What is still missing. The queue cannot give a read-back across nodes, and the discussion scopes cannot give one at all. The smallest feature is a receipt step on the receive path. After appending, the receiving node reads the entry back from its own log, compares a content hash, and returns the hash with its own sequence number. The sender records the reply against the entry, with a state of pending, confirmed or failed, and exposes it through a status read. A stable message identifier carried in the payload, separate from the line-count sequence number, is the one prerequisite. The same identifier would serve requests 2 and 3.

Downsides. It adds a synchronous round trip to a write that is fire-and-forget today, or an asynchronous receipt store to keep the write fast. It turns the silent loss of a push into a visible failed state, which is good, but it also needs a retry policy and a decision on how long a pending receipt waits. A receipt proves storage on the node, not that any seat looked at it, which is what request 2 is for.

## 2. Per-seat read state

What the request means. For each recipient and each of its seats, the system tracks what has been read. When two replicas disagree about a read marker, the change that happened last wins, and this holds in both directions: a read on node A reaches node B and a read on node B reaches node A.

What exists today. Nothing stores read state. Discussion subscribe pushes content to a session and keeps no record that the session handled it. The log read has an after-sequence cursor, but the cursor lives in the caller and is per node, because sequence numbers differ per replica. The conflict resolver is a pick-a-winner tool over duplicate content, not a merge of per-field changes. Replication is one-way push from each writer, with no anti-entropy, so a marker written on one node reaches the other only if a participant list happens to cover it and the push does not fail.

Verdict: needs a feature.

What is still missing. A convention could approximate this by having each seat append read-marker entries to the discussion and letting readers take the latest per recipient and seat. That falls short of the request, because a lost push means a marker that never arrives, clocks on three machines can disagree, and the throttle caps marker traffic. The smallest feature is a read-marker record type in the write ahead log, in the same way the conflict-resolved marker is an administrative record. It carries the stable message identifier or a high-water mark, the recipient, the seat, a change timestamp and the writing node. Replicas merge by keeping the record with the latest change timestamp per recipient and seat, with the node name as a tie breaker. It also needs a catch-up pull so that both directions converge after an outage.

Downsides. Last-change-wins depends on clocks, so a machine with a wrong clock can overwrite a correct marker, and a bounded skew tolerance or a logical clock has to be chosen. A high-water mark is cheap to store and cannot express "read this one, skip that one". Per-message flags can, and they grow with the store. The catch-up pull is new replication behaviour, not a small addition.

## 3. Writer provenance

What the request means. Every message records how it was written and by which seat. The how has three values: transport stamped (the system recorded the writer from the authenticated connection), self declared on the command line (the writer said who it was), or a direct append (a write straight into the store with no daemon in the path). The seat identifies which of the two or three sessions wrote it.

What exists today. An entry has an origin peer and a role. Both are filled from the request body, so a caller can write anything, and the server never overwrites them from the token. Nothing records a seat. The log is a plain line file, so a direct append is possible, and nothing marks such a line as direct: it has no number check and, when the log is not encrypted, no integrity check. The federation capability system does know which peer a token belongs to, and the session manager knows the session behind a call, so the facts needed for a transport stamp exist on the server. They are not written into the entry.

Verdict: needs a feature.

What is still missing. A skill can only produce the self-declared kind, by a content header convention, and cannot produce the other two. The smallest feature is three fields on the write ahead log entry, set by the server and not by the caller: the stamp kind, the writing seat (the session name or identifier from the authenticated call) and the writing node. A caller-supplied origin or role is kept as a separate declared field, so a declared value can never pass for a stamped one. Peer replication carries all three unchanged. A direct append is detected, not prevented: a line with no server stamp, or a number that does not follow, reads back as direct.

Downsides. Existing entries have none of the fields, so readers must treat missing as unknown and never as trusted. The stamp is only as strong as the token behind it, so two seats that share a token cannot be told apart unless the seat comes from the session identity. A tamper-evident chain over the entries would be stronger, but it is a larger change and should be a separate decision.

## 4. Wake on arrival

What the request means. When a message addressed to a named agent arrives, start or resume that agent's session so it can act. Each recipient has a kill switch that turns this off, and a cooldown so that a burst of messages wakes the agent once.

What exists today. Discussion subscribe sends an arriving entry to a session that is already running under a given name, and only for a write made through MCP on the same node. It does nothing for an entry that arrives from a peer, and it skips a missing session silently. Exit hooks have the right shape for kill switch and cooldown (each hook can be enabled or disabled and has a cooldown in seconds), but they fire on a session crash or a failed or killed state, not on a message. Alert rules fire on metric thresholds from observer data, not on message arrival. Schedule spawn starts an ephemeral session on a cron schedule. Channel routing sends inbound messages from the chat channels to sessions by rule. It does not read discussion entries or peer pushes, so mail between siblings never passes through it. None of them starts a session in response to an arrival.

Verdict: needs a feature.

What is still missing. A wake can start a paid model session, so the kill switch and the cooldown are part of the smallest feature and not later additions. The smallest feature extends the existing subscription. When an entry arrives for a subscribed discussion and no session with the subscribed name is running, the subscription starts or resumes that session with a stored task, subject to two fields kept on the subscription: an enabled flag (the per-recipient kill switch) and a cooldown in seconds. The dispatch also has to run from the peer receive path and from the REST write path, which it does not today. A polling arrangement built on schedule spawn would approximate this, but it trades arrival latency for the poll interval and spends a session start on every poll, so it is a stopgap and not an answer to the request.

Downsides. Starting model sessions from an incoming message is a cost and abuse surface: any peer allowed to write can spend the recipient's model budget. The kill switch and cooldown limit that, and a per-sender allow list and a daily cap are the likely next asks. The daemon's usage guard and capacity limits already apply to session starts, and the design should say how they interact with a wake so that a wake is refused cleanly when capacity is full. A session that wakes on a message and replies can trigger a wake in the other direction, so a loop guard is needed.

## 5. Per-seat views

What the request means. A seat should not have to read the whole store to see its own mail. It should page through a store, or have a per-seat inbox with its own read state.

What exists today. The write ahead log read through MCP takes a count and an after-sequence cursor, and it can long-poll for new entries with a timeout. That is paging, forward from any point, and it is enough for one seat on one node. Discussion identifiers are free strings and each discussion has its own log and its own participant list, so a discussion per seat is a normal use of the feature. The queue list tool filters by role and state only. It has no count, offset or cursor, so it cannot page, and it is a work queue with no per-recipient addressing, so it is not a per-seat inbox. Semantic recall takes only a result count, with no cursor, and it does not show entries that arrived from a peer, so a seat reading on a recipient node must use the log read.

Verdict: possible as a skill or howto with no code change.

Outline of the skill.

- Name: agent-inbox-per-seat.
- Trigger: a seat is told to check its mail, or its startup routine runs; also when a sibling needs to address one specific seat.
- Steps: first, agree an inbox naming rule, for example one discussion identifier per recipient seat with the sibling and seat in the name. Second, set the participant list on the sending side so only the nodes that need the inbox receive it. Third, the sender writes to the recipient's inbox discussion with the memory discussion write tool. Fourth, on start the seat reads its inbox from the last position it saved, using the log read with the after-sequence cursor and a small count, and processes what it finds. Fifth, the seat saves its new position in its own memory as a note, so it survives a session restart. Sixth, an optional long-poll read keeps the seat responsive while it works.
- Tools used: memory_discussion_write, memory_discussion_wal (count, after-sequence and block options), memory_discussion_participants, memory_remember for the saved position, and optionally discussion_subscribe for live delivery to a running seat.

What is still missing. The saved position is local to the seat and is not replicated, because sequence numbers differ per replica, so a seat that moves to another node starts over. A shared, merged read state is request 2. The skill also inherits the 60 writes per minute throttle and the lost-push behaviour described above.

Downsides. Many small discussions mean many log files and participant lists to manage. Every write scans its whole log to number the entry, so a very large inbox slows its writers and wants a retention or archive rule that this record does not decide.

## 6. Identity handoff

What the request means. When one seat takes over from another, several aspects of identity move with the work: who the agent is (role, goals, current focus), what it knows (working memory), and what it owes (open questions, unread mail, tasks in flight).

What exists today. The identity tools read, set, merge, back up, restore and reset one identity record, with backups that can be listed and restored. Memory handoff writes a task-completion summary to the story scope so the next task in the same story inherits it, and it needs a project and a story. Scoped memory has borrow, seed and promote. A discussion scope can carry a free-form note between nodes. Session tools can bind an agent to a session. The two identity howtos (identity and telos, and identity scaffold bootstrap) cover setting the operator's role, goals and focus, and creating a new person in its own project directory with its own memory scopes. Neither hands an identity from one seat to another. Read the identity as the operator-level record for a daemon: it is not a per-seat or per-sibling persona, and a placeholder item for personal agents with personality already says that area needs its own design session.

Verdict: possible as a skill or howto with no code change.

Outline of the skill.

- Name: seat-handoff.
- Trigger: a seat is about to end, be replaced, or be moved to another machine; or a new seat starts and is told which seat it relieves.
- Steps: first, the outgoing seat reads the identity and takes a backup of it. Second, it writes a structured handoff note to a handoff discussion for the receiving sibling, with sections for identity aspects, working knowledge, open obligations (unread inbox position, pending replies, tasks in flight) and pointers to the memory scopes to borrow. Third, where the work belongs to a story, it also records a memory handoff summary so the next task inherits it. Fourth, the incoming seat reads the note, merges only the identity fields the note names with the update call, borrows the named scopes, and confirms by writing an acknowledgement entry to the same discussion. Fifth, the outgoing seat checks for the acknowledgement before it exits, and restores the backup if the handoff failed.
- Tools used: get_identity, identity_backups, update_identity, restore_identity, memory_handoff, memory_scope_borrow, memory_scope_seed, memory_discussion_write, memory_discussion_wal.

What is still missing. The handoff is a convention and has no integrity check, no receipt (request 1) and no recorded writer (request 3). If "identity" means a separate persona per sibling, the identity record cannot hold it, and that need belongs to the personal agents design. This is the one open question for the operator below.

Downsides. The note is only as complete as the outgoing seat makes it. Nothing checks that every aspect was covered.

## Design decisions the operator may need to make

These are the points where the verdict depends on a choice. Nothing here is decided.

1. Stable message identifier. Requests 1, 2 and 3 all need one, separate from the per-node line number. Decide once whether it is a generated identifier or a content hash.
2. Replication model. Requests 1 and 2 need catch-up after an outage. Decide whether the discussion scope gets a pull step, or whether replication stays push only and the receipts show the gaps.
3. Clock policy for change wins. Decide whether to trust wall clocks within a stated skew or use a logical clock.
4. Wake safety. Decide the default for the kill switch (off until enabled is the safer choice), whether a per-sender allow list is required, and what cap applies.
5. Identity scope. Decide whether the handoff in request 6 is about the operator identity record or a per-sibling persona. The second needs the personal agents design.
6. Whether to do anything about request 5 beyond publishing the howto. It needs no feature.

## Smallest build, if the operator chooses to build

This is a possible order, not a plan. One shared piece first: the stable message identifier and the server-set stamp fields (requests 1 and 3). Receipts then follow on the receive path. The read-marker record and the catch-up pull (request 2) are the largest part and could be left until the first two prove out. The wake extension to subscribe (request 4) is small but carries the cost and abuse risk, so it should come with its safety decisions. The two skills (requests 5 and 6) can be written at any time and need no feature.

## Parity surface

Nothing is built by this record, so no surface is touched now. If features follow, each surface and its intent is as follows.

- REST: yes. Receipt status, read markers and the stamp fields are exposed on the discussion routes.
- MCP: yes. The same reads and writes as tools, with the existing capability checks.
- CLI: yes. Matching discussion subcommands for receipt status, read state and the wake settings.
- Comm channel: yes. A read-only status command and the wake kill switch, since an operator needs to stop a wake from a phone.
- YAML and config: yes for the wake defaults, the cooldown, the throttle and the clock skew bound. Not for per-message data, which is runtime state.
- PWA: yes for a read-only view of receipts and read state on the discussion card and the wake switch in settings. The placement of any new card or sub-tab is a design question for the operator and is not decided here.
- Android: follows the PWA through a parity issue filed at the merge that changes the PWA.
- iPhone: same as Android.
- Excluded for now: a graphical editor for read markers, because read state is written by the seats and the operator only needs to see it.

## Cross-check against Automata and the registry

Status: validation record, documents only, nothing decided. This section answers the operator's instruction of 2026-11-07 (registry row 412, part c) to check GitHub issue 211 against the queue of completed and open Automata. It lists options and a recommendation. It changes no code, no settings and no version, and it runs no release.

### How the check was made

- The registry `docs/plans/README.md` was searched for every row and tracker item that touches mail, messaging, discussion, queue, federation, exit hooks, channel routing, identity, personal agents, plugins and issue 211. Rows 390, 412 and 414 and the tracker items BL446, BL456 and BL403 are the ones that matter. The registry is very large, so the check was a targeted search of it, not a line-by-line read of every row; the unrelated rows (for example the row numbered 211, which is about LLM node placement and has nothing to do with GitHub issue 211) were checked and set aside.
- Completed work was read in `docs/plans/HISTORY.md` and `docs/plans/historical-plans/`.
- The overlap check named in the instruction was done by reading the registry rows that mention GitHub issue 211. There are two: row 390 (issue 211 stays open, "the owner is still reviewing the requests") and row 412 (the 2026-11-07 answer that queued this design pass as Automaton `ac690001`). No other row mentions issue 211, and no row files an Automaton that builds any of the six requests. The search tool for overlaps exists as the MCP tool `autonomous_overlap_search` (read from the tool list in `internal/mcp`); it was not run here because it needs a running daemon, and this pass may not start or call one.
- Each existing-behaviour claim below was checked by reading the file named beside it.

### Identifiers found

| Identifier | What it is | Status in the registry or history | Source read |
|---|---|---|---|
| BL357 | Durable role based work queue (queue_push, queue_claim, queue_complete, queue_fail, queue_list) | shipped, within BL344-BL361 (v8.10.3 to v8.10.15) | `docs/plans/HISTORY.md` line 76; `internal/mcp/bl357_queue.go` |
| BL332 | Discussion scopes for federated shared memory: write, write ahead log, participants, conflicts | shipped v8.4.0 | `docs/plans/HISTORY.md` line 72; `docs/howto/discussion-scopes.md`; `internal/server/bl332_discussion_scope.go`, `internal/server/bl332_discussion_sync.go`; `internal/mcp/bl332_discussion_tools.go` |
| BL358 | Discussion subscribe: deliver a discussion entry to a named session | shipped, within BL344-BL361 | `docs/plans/HISTORY.md` line 76; `internal/mcp/bl358_discussion_subscribe.go` |
| BL332 sync (peer replication) | One-way background push of each write to the participant peers; no catch-up | shipped inside BL332 | `internal/server/bl332_discussion_sync.go` (push at the sync function, receive handler) |
| BL331 | Channel routing: inbound chat messages (Telegram, Signal, Matrix) go to a federation peer by rule | shipped v8.3.0 | `docs/plans/HISTORY.md` line 71; `docs/howto/channel-routing.md`; `internal/mcp/bl331_channel_routing_tools.go` |
| BL356 | Exit hooks: restart or notify on a session crash or failed or killed state; enable flag and cooldown | shipped, within BL344-BL361 | `docs/plans/HISTORY.md` line 76; `internal/mcp/bl356_exit_hooks.go` |
| BL353 | Schedule spawn (recurring and one-shot ephemeral sessions) | shipped, within BL344-BL361 | `docs/plans/HISTORY.md` line 76; `docs/plans/harness-research/datawatch-mapping.md` |
| BL360 | Structured agent result store | shipped, within BL344-BL361 | `internal/mcp/bl360_result_store.go` |
| BL329 | Identity endpoints | shipped v8.2.0 | `docs/plans/HISTORY.md` line 69; `internal/mcp/identity.go` |
| Registry row 272, Automaton `28bd0001` | Identity backups, empty-write refusal and restore on every surface | done, shipped 9.49.0 | `docs/plans/README.md` row 272 |
| Identity scaffold records (no tracker number assigned) | Plans and designs only, archived: gap analysis, skill, memory extension, multi-person, continuity, refusal and ethics | archived, planning only | `docs/plans/historical-plans/2026-09-21-identity-scaffold-*.md` (the gap analysis says "planning only, ships no code" and that BL numbers are assigned at implementation time) |
| `docs/howto/identity-scaffold-bootstrap.md`, `docs/howto/identity-and-telos.md` | The shipped bootstrap and operator identity howtos | shipped | the two files |
| BL456 | Personal agents with personality and decision making, for a real community user | placeholder only; the first Automaton (`51c531b6`) produced no output; needs a real design session; held for v11.0.0 planning | `docs/plans/README.md` (community and personal agent design table) |
| BL446 | Federation as a sibling agent mail transport (external feature request) | not started | `docs/plans/README.md` (feature requests table) |
| BL403, Automaton `784f0001`, row 414 | Plugin extension surfaces: manifest v3, supervised service plugins, widget model and renderer, capability gating; imap-mcp as first adopter | phases 1 to 5 shipped; phases 6 to 9 are Automaton `784f0001`, held for an overlap review | `docs/plans/README.md` rows 397 and 414; `docs/plans/2026-10-09-plugin-extension-surfaces.md` |
| BL404, row 195 | imap_mcp channel configuration parity | done, released 9.14.0 | `docs/plans/README.md` row 195 |
| Row 149, BL568 | Resource fingerprint exchange between federated peers | done, shipped 9.5.103 | `docs/plans/README.md` row 149 |
| Row 362, Automaton `396a0001` | Federated peer placement design document | merged docs only; the implementation Automata wait for the operator | `docs/plans/README.md` row 362 |
| GH#201 and `internal/federation/hopchain.go` | Hop chain for federation nesting, capability grants | landed 2026-10-09 | `docs/plans/2026-10-10-llm-enhancements.md` |
| Automaton `ac690001` | This design pass | filed as a draft 2026-11-07 (row 412) | `docs/plans/README.md` row 412 |

### The six requests against what exists

| # | Request | What already covers it (identifier) | What does not | Overlapping or neighbouring work |
|---|---|---|---|---|
| 1 | Read-back delivery receipts | BL332 sync pushes an entry to each participant and the receive handler appends it to the log; the log read on the recipient (`memory_discussion_wal`) can show it. BL357 records completion on its own node. | The push ignores the peer reply and drops errors (`internal/server/bl332_discussion_sync.go`, push and receive handlers); the receive handler answers ok even when the append failed. The BL332 howto says `origin_wal_seq` lets a receiver skip re-syncs, but the receive handler does not pass it to the append, so the sender's number is lost. No stable message identifier exists. BL357 has no cross node receipt. | BL446 (federation as mail transport, not started) would own a receipt if it is ever specified. BL568 (row 149) shows the opt-in, hash only exchange pattern between peers that a receipt could reuse. |
| 2 | Per-seat read state, change wins merge | BL332 log read has an after-sequence cursor and a long-poll block (`internal/mcp/bl332_discussion_tools.go`). BL332 conflict resolver picks a winner among duplicate content. | No read state is stored anywhere. The cursor is per node because numbers differ per replica. The resolver is not a field merge. No catch-up or anti-entropy in replication. | BL446. The shipped hop chain (GH#201) and the federated peer placement design (row 362) are about routing and capacity, not about merged per-seat state. |
| 3 | Writer provenance | BL332 entries carry `origin_peer` and a role. The federation capability system and the session manager know the token owner and session. BL399 (HISTORY line 108) audit logging records access, including federation hops. | `origin_peer` and role come from the request body and are not stamped from the token. No seat field. No marker for a direct append. Audit logs (BL399) record access, not per entry provenance. | BL399 audit trail is the nearest thing and could be the place a stamp is also mirrored; it does not replace a stamp on the entry itself. |
| 4 | Wake on arrival | BL358 delivers an entry to a running named session from the MCP write path on the node of the write. BL356 has an enable flag and a cooldown in seconds. BL353 starts sessions on a schedule. BL331 routes chat channel messages to peers. | BL358 does not run for a REST write or a peer push, and skips a missing session silently. BL356 fires on crash or failed state, not on a message. BL331 does not read discussion entries. Nothing starts a session on an arrival. | BL446. The usage guard and capacity limits (rows on lane fill and capacity, for example BL514 row 95 and BL428 phases) apply to any session start and must be named in the design. |
| 5 | Small per-seat views of large stores | BL332 log read pages forward with count, after-sequence and long-poll; discussion identifiers are free strings, so one discussion per seat works; `memory_remember` can hold the saved position. | BL357 `queue_list` filters by role and state only, so it cannot page. Semantic recall has a count only and does not show peer entries. The saved position is not replicated. | None. No Automaton covers it; it needs a skill or howto only. The skills registry (`skills_registry_*` tools, BL255) is where the skill would live. |
| 6 | Multi-aspect identity handoff | BL329 identity tools and the row 272 backups and restore (`get_identity`, `update_identity`, `identity_backups`, `restore_identity`); memory handoff, scope borrow and seed; the two shipped identity howtos. | The identity is the operator level record for one daemon, not a per-seat persona. No handoff convention or acknowledgement exists. The identity scaffold work (archived) is plans only and bootstraps a new person; it does not move identity from seat to seat. | The archived identity scaffold designs (continuity and multi-person are the two closest) and BL456, the personal agents placeholder, which already says this area needs its own design session. |

### Overlap judgement

- No completed or open Automaton delivers any of the six requests. This confirms the validation in row 412.
- BL357 and BL332 with BL358 are real and shipped, and they are the base the requests build on. They are not duplicates, because none of them gives a read-back receipt, a merged read state or a stamped writer.
- BL446 is the closest overlap. It has no spec, so filing request 1 to 4 work without reading it would risk two Automata owning the same transport. The options below keep one owner.
- BL456 and the archived identity scaffold designs are the overlap for request 6 if the requester means a persona per sibling.
- The plugin extension plan (BL403, row 414) and the imap mail integration (BL404, row 195, and Automaton `784f0001`) are neighbours, not overlaps. They put an external mail service (imap-mcp) beside the daemon and show it on a service card. They carry human email, not messages between agents. The one useful link is that a service plugin could be the route for a future wake step, so the plugin plan should be read before a wake feature chooses where its settings live. The held status of `784f0001` is unaffected by this record.
- Federation work that stays separate: BL568 fingerprint exchange (row 149), the federated peer placement design (row 362) and the hop chain (GH#201). They share the peer and capability machinery but answer different questions.
- Channel routing (BL331) is not mail between agents. It routes human chat messages to a peer. Reusing it for sibling mail would mix the two and is not recommended.

### Options

1. Treat BL446 as the owner. Rewrite BL446 from "not started" into the design record's scope (the six requests), file the features under it in the order given in "Smallest build", and keep the two skills outside it. This keeps one owner and one transport.
2. File the features as separate small Automata under issue 211 and leave BL446 as a link only. This is faster per item and risks overlapping with whatever BL446 later says.
3. Do the two skills now and hold every feature until the operator decides the open points above. This costs almost nothing and delivers requests 5 and 6 at once.
4. Do nothing yet and reply to issue 211 with this record. This leaves the requester without the two cheap wins.

### Recommendation

Option 3 first, then option 1 once the operator has answered the decision list in this record. Concretely: publish the two skills (requests 5 and 6, after the operator answers the identity scope question, since a persona per sibling belongs to BL456), and make BL446 the single owner of the features for requests 1 to 4. Do not file any feature Automaton until the operator has decided the stable message identifier and the replication model, because three of the four features depend on them. The sequencing rule stands: this work follows the 10.0.0 release work and uses no parallel messaging layer. Whether a design is needed from the operator is answered by the decision list above; the cross-check adds one more decision: whether BL446 absorbs these requests or stays separate (options 1 and 2).

### Corrections found while checking

- The BL332 howto (`docs/howto/discussion-scopes.md`, "What happens on write") says the write ahead log records `origin_wal_seq` so a receiver can detect re-syncs. The receive handler in `internal/server/bl332_discussion_sync.go` calls the append with content, role and origin peer only, so the sender's number is not kept. This record's earlier statement stands. The howto wording is worth a docs fix when someone next edits it; this record does not change it.
- The earlier paragraph of this record names "the federation-as-mail-transport item" as a neighbour. Its registry identifier is BL446 and its state is not started.

## Downsides of each proposed feature

Status: documents only, nothing decided. This section lists, for each feature proposed above as "needs a feature" and for each skill that carries a cost, what it would cost and what happens if it is left out. It recommends nothing. The recommendation stays where it was given, in the cross-check section.

### What was read for this section

- `internal/server/bl332_discussion_scope.go`: the log entry has the fields sequence, content, role, timestamp, origin peer and an optional administrative op. It has no message identifier, no seat and no stamp kind. Each append is a line written to `~/.datawatch/discussions/<id>/wal.jsonl` and, when an encryption key is set, each line is encrypted. The write path numbers the entry from the existing log.
- `internal/server/bl332_discussion_sync.go`: the push to each participant runs in a background goroutine, returns on any error without a trace, and closes the reply body without reading it. The receive handler appends content, role and origin peer only, and discards the append result. The write throttle is 60 writes per minute per bearer token, kept in memory.
- `internal/session/discussion_subs.go` and `internal/mcp/bl358_discussion_subscribe.go`: a subscription is two strings, the discussion identifier and the session name, kept in a JSON file. Dispatch looks up a running session by name, skips it when none is found, and sends the entry as input.
- `internal/mcp/bl356_exit_hooks.go`: exit hooks carry an enabled flag and a cooldown in seconds (default 300), and have tools to enable and disable them.
- `internal/mcp/bl357_queue.go`: the work queue is role based (push, claim, complete, fail, list) on the local daemon.
- `docs/howto/discussion-scopes.md`: the log location and the participant sync description.

### Costs that apply to every feature below

- Flat rate plan. The plan is a flat monthly fee. Every dollar figure the daemon's cost tools show is an API-equivalent estimate, not a charge. The real limit is the plan's usage window. A feature that starts or lengthens model sessions spends that window, and so it can cause a usage back-off, even though no money is billed. The features that spend none of it (receipts, read state, provenance) cost only disk, memory and processor time on the daemon.
- Settings. Every tunable a feature adds would need a documented setting, a config reference entry and a way to read it through the MCP tools and the docs, which is more work and more surface than the code alone.
- Release work. Each feature is a minor version bump with the parity items listed in the parity section (REST, MCP, CLI, comm channel, config, PWA, plus parity issues for Android and iPhone). That is documentation, test and surface cost beyond the daemon change.
- Sequencing. All of it follows the 10.0.0 release work.

### 1. Read-back delivery receipts: downsides

- Complexity added to the daemon: a stable message identifier on the log entry; a receipt step on the receive handler (append, read back, hash, reply); a sender-side receipt store with pending, confirmed and failed states; a retry policy and a timeout for pending receipts; a status read. The push today is a fire-and-forget goroutine, so this turns it into a tracked operation with a lifecycle.
- New state to keep consistent across peers: one receipt record per message and peer, held on the sender only. The receiver keeps nothing new except the identifier on the entry. The receipt can disagree with reality if the reply is lost after the receiver stored the entry (the sender sees pending or failed while the entry is present), so a retry must be safe to repeat and the receiver must treat a repeat of the same identifier as already stored.
- Security and privacy surface: the receipt carries a hash of private message content. A hash of a short or guessable message can be tested by anyone who sees it, so the receipt should travel only on the authenticated reply and not be logged or shown beyond the sender. A peer can lie in a receipt, so it proves storage only as far as that peer is trusted. The status read exposes who received what, and that should sit behind the same capability checks as the log read.
- Cost: no model use. Extra network round trip per participant per write, extra disk for the receipt store. The 10 second push timeout in the current code means a slow peer holds a receipt pending for that long. Nothing here is billed.
- Upgrade and migration: entries written before the change have no identifier, so they can never get a receipt and a receipt reader must show them as unknown. A peer that has not been upgraded cannot return a hash, so mixed-version pairs show every message as failed or pending unless the sender treats an old reply as unknown. The receive route changes shape, and old and new peers must keep working together for a release or more.
- If left out: a push to a peer that is down or that fails to append is lost with no sign, the writer never learns whether any peer stored the entry, and the receive handler keeps answering ok after a failed append. Siblings must fall back to a manual read of the other node's log to check.

### 2. Per-seat read state: downsides

- Complexity added to the daemon: a new read-marker record type in the log next to the existing administrative records; a merge rule (latest change per recipient and seat, node name as tie break); a catch-up pull, which is new replication behaviour because replication today is a one-way push with no pull; a clock policy. This is the largest of the four features.
- New state to keep consistent across peers: one marker per recipient and seat, replicated both ways, plus the sequence or identifier it points at. This is the only proposed feature whose state must converge in both directions. Sequence numbers differ per replica today, so markers must point at the stable identifier from request 1 and not at a line number, which makes request 2 depend on request 1. Markers grow with the store if kept per message, and are cheap but coarse if kept as a high-water mark.
- Security and privacy surface: a marker reveals when a seat read a message and so reveals activity. A peer that may write markers can mark another seat's mail as read, hide mail from it, or overwrite a correct marker with a later timestamp. With change wins, a forged future timestamp beats every real marker until the clock passes it, so a bounded skew limit is a security need as well as a correctness need. The catch-up pull is a new endpoint that returns log content to a peer and needs its own capability check.
- Cost: no model use. Disk and network for markers and the pull; marker writes would count against the 60 per minute write throttle unless they are exempted, and an exemption opens a flood path. Nothing is billed.
- Upgrade and migration: existing discussions have no markers, so everything reads as unread on first use unless a seat sets a starting position. Entries without identifiers (see request 1) cannot be marked individually. A mixed-version group cannot converge, so all peers in a discussion would need the upgrade before the feature is trusted. Rolling back leaves marker records in the log that old code does not understand; whether old code skips or rejects an unknown record type was not checked in this pass.
- If left out: seats keep the position of their last read in their own notes, which is local to one node and one session. A seat that moves to another node starts over, two seats of the same agent can handle the same message twice, and no one can see what a sibling has read.

### 3. Writer provenance: downsides

- Complexity added to the daemon: three server-set fields on the log entry (stamp kind, writing seat, writing node) plus a separate declared field for what the caller claimed; the write path must resolve the authenticated peer and the session behind each call, through the MCP path and the REST path, and the replication payload must carry the fields unchanged; direct-append detection needs a rule for what an unstamped or out-of-order line looks like.
- New state to keep consistent across peers: nothing new that needs merging, because the fields travel with the entry and are never edited. The one consistency risk is a receiver stamping its own values over the sender's. The receive handler today builds a new entry from content, role and origin peer, so it would have to be changed to keep the sender's stamp, and a receiver must then decide whether to trust a stamp it did not make, since it can only check the token of the peer that delivered it, not the seat behind that peer.
- Security and privacy surface: this is the feature that addresses forging, and it only narrows it. Today the origin peer and role are whatever the request body says. A stamp is as strong as the token: two seats sharing one token are told apart only if the seat comes from the session identity, and a compromised peer can stamp anything it likes on entries it forwards. A direct append is detected, not prevented, and an unencrypted log has no integrity check, so someone with file access can still write a line that merely looks stamped. A tamper-evident chain would close that and is a larger change. Seat names and node names become visible to every reader of the log, which leaks which sessions exist.
- Cost: no model use. A few bytes per entry; a small lookup on each write. Nothing is billed.
- Upgrade and migration: existing entries have none of the fields and must read as unknown and never as trusted. Readers and tools that show provenance need that third state. Old peers drop the new fields on receive, so entries that pass through an old peer lose their stamp. There is no way to back-fill the past.
- If left out: the origin peer and role stay caller-declared, a peer can write an entry that claims to come from a sibling, no entry records which of a sibling's seats wrote it, and a direct write to the log file looks the same as any other line.

### 4. Wake on arrival: downsides

- Complexity added to the daemon: the subscription record grows from two strings to include a stored task, an enabled flag and a cooldown; dispatch must start or resume a session, not only send input to a running one; dispatch must also run from the peer receive path and the REST write path, which it does not today, so the receive handler gains a side effect it never had; loop protection between two waking sessions; interaction with the capacity and usage guards when a wake is refused.
- New state to keep consistent across peers: the wake settings are per recipient and live on the node that runs the session. They are not replicated, so a kill switch set on one node does nothing on another node that also holds a subscription for the same agent. Last-wake times for the cooldown are runtime state on one node.
- Security and privacy surface: this is the highest-risk feature. A message from a peer can start a paid session on the recipient's node, and the entry text goes into that session as input, so a peer that may write can steer a model session (prompt injection) and can spend the recipient's usage window. The message content is private and enters the session transcript and any memory the session saves. A forged origin (request 3) makes a sender allow list unreliable until stamps exist. The kill switch has to be reachable from a comm channel, since an operator needs to stop a wake from a phone. The silent skip of a missing session today is safe in one respect: nothing starts. Removing it removes that safety.
- Cost: this is the only feature of the six that spends model usage. Each wake starts or resumes a model session, and that session reads the message and then works. The dollar cost the tools report is API-equivalent only; the real cost is the plan's usage window, which a burst of wakes, a ping-pong between two agents, or one noisy peer can drain, and which can trigger the usage back-off for every other session on the plan. The cooldown limits the rate, not the length of each session. A per-sender allow list and a daily cap are not in the smallest design and are the likely next asks.
- Upgrade and migration: the subscription file is a list of two-field records, so old records load with the new fields empty. Whether an empty enabled field must mean off, so that an upgrade never starts waking sessions that were only receiving input before, is a design choice that this record leaves open. Old peers do not know about wake, so nothing changes for them, but a wake on a peer-delivered entry needs the receive path change on the recipient's node.
- If left out: a message reaches a session only if that session is already running under the subscribed name and the write was made through MCP on the same node. A message from a peer, or a write by REST, never reaches a session, and a missing session drops the message from delivery with no trace (it stays in the log). The stopgap is a scheduled spawn that polls, which spends a session start on every poll whether or not mail arrived, and that cost is real usage-window spend.

### 5. Skill: agent-inbox-per-seat: downsides

- Complexity added to the daemon: none. It is a skill that uses existing tools.
- New state to keep consistent across peers: the saved read position is a memory note local to the seat and is not replicated, so it can be lost or can differ between nodes. One discussion per seat means many logs and many participant lists that someone must create, name consistently and clean up. There is no tool to list or retire them in bulk.
- Security and privacy surface: the skill depends on a naming convention, not enforcement. Any peer allowed to write to a discussion can write to any seat's inbox, and any participant reads the whole inbox. Seats on the same node can read each other's inbox logs. Messages are stored in the log and, on the writing node, also in the memory store, so private content is held in more than one place. The skill adds no stamp, so the sender claimed in an entry is unverified (request 3).
- Cost: no model use beyond the seat's own reading. A seat that long-polls holds its session open while it waits, and an open session spends usage window only while it is generating, but it occupies a session slot under the capacity limits. A poll loop that wakes a model to check an empty inbox spends usage on nothing. The skill inherits the 60 per minute write throttle and the lost-push behaviour of request 1.
- Upgrade and migration: none for the daemon. The skill would be a registry or local skill, and a later rename of its tools or a change to the log read would need the skill to be revised. If features 1 to 3 are built later, the skill's saved position would have to be rewritten to use the stable identifier.
- If left out: request 5 has no documented answer and each seat invents its own naming and position-keeping, with no shared convention across siblings. Nothing breaks.

### 6. Skill: seat-handoff: downsides

- Complexity added to the daemon: none. It is a skill that uses existing tools.
- New state to keep consistent across peers: a handoff note and an acknowledgement in a handoff discussion, plus an identity backup on the outgoing node. If the incoming seat runs on another node, the note arrives by the one-way push and can be lost, so the outgoing seat can wait for an acknowledgement that never comes.
- Security and privacy surface: the handoff note holds identity, working knowledge and open obligations, which is the densest private content in the whole design, and it travels over peer push to every participant of the handoff discussion. The identity update call changes the daemon's identity record, which belongs to the operator. A note that can name which identity fields to merge lets a forged or mistaken note rewrite them. The restore call on a failed handoff overwrites the current identity with the backup, so a handoff that races with an operator edit can lose that edit. The identity backups already exist to limit this, and the skill must use them.
- Cost: no model use beyond the two seats doing the work. The note-writing and note-reading steps are model turns on both sides, and they are billed against the plan's usage window like any other turns. A long handoff note is a long read.
- Upgrade and migration: none for the daemon. If the identity is meant to be per sibling and not per daemon, the skill has no place to put it and would have to be rewritten against the personal agents design (BL456), so some of its content could be discarded.
- If left out: handoffs are ad hoc. The outgoing seat's identity, knowledge and open obligations reach the incoming seat only if someone remembers to carry them, there is no acknowledgement, and the identity record is not protected by a backup taken at the right moment.

### Features and skills compared on one point

| Item | Model usage spent | New state across peers | Needs a peer upgrade to be trusted |
|---|---|---|---|
| 1 Receipts | none | sender-side receipt store | yes |
| 2 Read state | none | both-way merged markers | yes |
| 3 Provenance | none | none (fields travel with the entry) | yes |
| 4 Wake on arrival | yes, per wake | per-node wake settings | receive path on the recipient's node |
| 5 Skill: inbox per seat | seat's own turns only | local saved position | no |
| 6 Skill: seat handoff | both seats' turns | handoff note and acknowledgement | no |

### Corrections found while checking

- The earlier section "How the building blocks behave today" says the push drops every error. The code in `internal/server/bl332_discussion_sync.go` does drop errors (the comment on the function says they are logged, but no log call exists in it). The earlier statement stands and the comment is wrong. This record does not change the code.
- Whether old code skips or rejects an unknown log record type was not verified in this pass, and is marked above as unchecked.

## Decisions for the operator

Status: questions only, documents only, nothing decided. Each question is one decision. Each lists the options and a recommendation. The recommendation is advice, and no option is chosen until the operator answers. The facts behind the questions come from the sections above and were checked against `docs/howto/discussion-scopes.md`, `docs/plans/README.md` (rows 390, 412 and 414, and the tracker item BL446), `internal/server/bl332_discussion_scope.go`, `internal/server/bl332_discussion_sync.go`, `internal/mcp/bl357_queue.go`, `internal/mcp/bl358_discussion_subscribe.go` and `internal/server/web/app.js` (the Discussion Scopes card in Settings, General).

### Question 1: Build a separate mail feature, or extend the work queue and the discussion scopes?

The work queue (`queue_push`, `queue_claim`, `queue_complete`, `queue_fail`, `queue_list`) is role based, lives on one daemon and has no per-recipient addressing (`internal/mcp/bl357_queue.go`). The discussion scopes already replicate entries between peers, keep a write ahead log and page by cursor (`internal/server/bl332_discussion_sync.go`, `internal/mcp/bl332_discussion_tools.go`). The operator's public answer of 2026-10-15, quoted in "The operator's words" above, said the work is a design pass that builds on those and is not a parallel messaging layer. That answer comes from the issue thread and is not recorded in any file under `docs/plans/`, so this pass could not check it against a file; the operator should confirm that it still stands, which is part of this question.

Options:

- A. Extend the discussion scopes (stable message identifier, receipts, read markers, server-set stamps, wake on the existing subscription). The work queue stays as it is.
- B. Extend the work queue with per-recipient addressing and cross-node replication, and leave the discussion scopes as they are.
- C. Build a separate mail feature with its own store and routes.
- D. Build nothing yet and publish only the two skills (requests 5 and 6).

Recommendation: A, with D done first. The discussion scopes already carry the replication and the log, and the queue would need both added. C contradicts the 2026-10-15 answer as quoted above, if it still stands. Nothing is decided.

### Question 2: Is wake on arrival in scope?

Wake on arrival is the only one of the six requests that spends model usage (see "Downsides of each proposed feature", item 4). Today a subscription only sends input to a session that is already running, and only for a write made through MCP on the same node (`internal/mcp/bl358_discussion_subscribe.go`).

Options:

- A. In scope, in the first release of the work, with the kill switch off by default, a cooldown and a loop guard.
- B. In scope, but as a later step after receipts and provenance exist, so a sender allow list can be trusted.
- C. Out of scope. Use a scheduled spawn that polls, as a stopgap the requester accepts.

Recommendation: B. A wake that trusts a caller-declared sender is unsafe, and provenance comes first. The stopgap in C costs a session start on every poll. Nothing is decided.

### Question 3: How is provenance forgery by a peer handled?

The origin peer and the role on an entry are values the caller sends. The server does not set them from the authenticated identity (the write handler in `internal/server/bl332_discussion_scope.go` reads `origin_peer` and `role` from the request body; the peer receive handler in `internal/server/bl332_discussion_sync.go` appends them as sent). A peer allowed to write can therefore claim to be a sibling.

Options:

- A. Server-set stamp fields (stamp kind, writing seat, writing node) kept apart from a caller-declared field, as described in request 3. This narrows forgery to the strength of the token.
- B. Option A plus a tamper-evident chain over the entries, so a direct append to the log file is caught as well as detected.
- C. Leave the fields as declared and treat provenance as advisory only, shown with an unverified label.
- D. Restrict who may write to a discussion with a per-discussion allow list of peers, and add no stamp.

Recommendation: A now, with B as its own later decision, because a chain is a larger change. A is the minimum that makes a sender allow list for wake meaningful. Nothing is decided.

### Question 4: Where does any new view sit in the web interface?

This record does not assume a place. The current discussion view is the Discussion Scopes card in Settings, General (`internal/server/web/app.js`, the `discussion_scopes` section). A receipt and read state view, and a wake switch, would each need a home. Per the operator's rule, placement of any new card, tab or sub-tab is the operator's call, and the question is asked here before anything is filed.

Options (listed for the operator to choose among, or to replace with their own):

- A. Extend the existing Discussion Scopes card with a receipts and read state panel, and put the wake switch in the same card.
- B. A new card in Settings, General, beside the Discussion Scopes card.
- C. A new sub-tab on a page the operator names.
- D. No new web view in the first release. Status is read through REST, MCP, the command line and the comm channel only, and the web view follows later.

Recommendation: no placement is recommended, because this layout choice belongs to the operator; please say where it goes. If a default is needed to size the work, D is the cheapest and defers the choice. Nothing is decided.

### Question 5: Is the requester's change wins merge code adopted?

The requester offered their change wins merge code and tests. That code is not in this repository, and this pass has not read it. The discussion conflict resolver today is a pick-a-winner tool over duplicate content, not a field merge (`discussionDetectConflicts` and `handleDiscussionConflictResolve` in `internal/server/bl332_discussion_sync.go`).

Options:

- A. Adopt it as offered, after a review of its licence, its clock handling, its tests and how it fits the log record type, and keep the requester as its author.
- B. Use it as a reference and its tests as a specification, and write the merge in the project's own code.
- C. Decline it and write the merge from the design in request 2.
- D. Decide after reading it. Ask the requester to supply it (see "What the requester must provide") and judge it then.

Recommendation: D, then B unless the code fits cleanly. The clock policy (wall clock within a stated skew, or a logical clock) is decided first, because it decides whether the offered code can be used at all. Nothing is decided.

### Question 6: Which replication model and message identifier?

Requests 1, 2 and 3 all need a stable message identifier separate from the per-node line number, and requests 1 and 2 need catch-up after an outage. Replication today is a one-way push with no pull (`internal/server/bl332_discussion_sync.go`).

Options:

- A. A generated identifier, with push only and receipts showing the gaps.
- B. A generated identifier, with a catch-up pull step added to the discussion scopes.
- C. A content hash as the identifier, with either replication model.

Recommendation: A for the first step, B only when the read state feature is built. A generated identifier avoids two identical messages sharing one hash. Nothing is decided.

### Question 7: Is the identity handoff about the operator record or a persona per sibling?

The identity tools hold one operator level record per daemon. A persona per sibling needs the personal agents design (BL456, a placeholder that says it needs a real design session first).

Options:

- A. The operator identity record only. Publish the seat-handoff skill as outlined.
- B. A persona per sibling. Hold the skill until the personal agents design exists.

Recommendation: A, with the limit stated in the skill. Nothing is decided.

### Question 8: Does BL446 own the features, or stay separate?

BL446 (federation as a sibling agent mail transport) is not started and has no specification. It is the closest overlap.

Options:

- A. BL446 becomes the single owner of the features for requests 1 to 4.
- B. Separate small Automata under issue 211, with BL446 left as a link.

Recommendation: A, so one transport has one owner. Nothing is decided.

## Proposed order and size

Status: a proposal, not a plan. Nothing is decided and no Automaton is filed by this record. The sizes are rough guesses from reading the code, not measured.

Place in the sequence: all of it comes after the 10.0.0 release work. The sequencing rule of the 2026-10-15 answer stands, and the registry holds a final stage that runs once every 10.0.0 feature and defect is done (row 227 in `docs/plans/README.md`). Nothing here should start before that, and no lane should be filled with it ahead of the operator's decisions above.

Smallest useful first step: the two skills, `agent-inbox-per-seat` (request 5) and `seat-handoff` (request 6). They need no code change and no setting, and they give the requester two of the six requests at once. Rough size: two skill documents and one how-to page each, a few hours of writing and one pass of testing the steps against a running pair of nodes, so about one to two working days. The seat-handoff skill waits for the answer to Question 7.

Next step, if the operator chooses to build: the stable message identifier and the server-set stamp fields (requests 1 and 3), since requests 1, 2 and 3 all depend on them. Rough size: a small feature, about a week including the surfaces listed in the parity section. Receipts (request 1) follow on the receive path, about another week.

Later and larger: the read marker record and the catch-up pull (request 2), the largest part, about two to three weeks and best left until the first two prove out. Wake on arrival (request 4) is small in code, about a few days, and comes with its safety decisions (Questions 2 and 3).

## What the requester must provide

The requester has offered two things. Neither is in this repository, and this pass has not read either, so the list below is what to ask for.

- Their written design from June: the 13 principles and the 12 requirements. They are needed to check this record's reading of the six requests against what the requester meant, and to confirm that the six requests cover all 12 requirements or to show which requirements remain uncovered. Ask for them as a file or a link the operator can read.
- Their change wins merge code and its tests, which they offered. They are needed to answer Question 5. Ask for the source, the licence it is offered under, the language and the test suite, how it takes its clock (wall clock or logical), and whether its tests run without the requester's own tooling.
- Also useful, to be asked at the same time: which of the 12 requirements they would drop if only the first step were built.

## Summary for the issue thread

Thank you for the detailed requests. We checked all six against what datawatch has today and our Automata. None is built.

Two are possible with no code change, as skills: a per-seat inbox using discussion scopes (request 5), and a seat handoff note using the identity and memory tools (request 6). Four need a feature: read-back receipts, per-seat read state with change wins merge, server-stamped writer provenance, and wake on arrival. The operator's 2026-10-15 answer was that this builds on the existing work queue and discussion scopes and is not a parallel messaging layer; whether to extend them or build a separate feature is still an open question for the operator. Wake on arrival is the only one that spends model usage, so we suggest a kill switch and a cooldown, but whether it is in scope at all is also open.

This is a design pass only. Nothing is decided and nothing is built. The decisions are with the operator, and the work follows the 10.0.0 release work.

To help, please send your June design (the 13 principles and 12 requirements) and the change wins merge code and tests you offered, with their licence.
