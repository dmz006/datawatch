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
