# BL180 Phase 2 — eBPF socket-tuple LLM caller attribution

**Status:** Design questions ready — Phase 1 (ollama runtime tap) shipped v4.9.1; Phase 2 (this doc) is structurally unblocked by BL173 task 1 (kprobe loader, v5.0.1) but needs operator alignment on data shape before implementing.

**Filed:** 2026-04-26
**Pre-requisite reads:**
- [`docs/api/observer.md`](../api/observer.md) — current envelope shape + `Caller` / `CallerKind` fields
- [`docs/howto/cross-agent-memory.md`](../howto/cross-agent-memory.md) — example of where caller attribution shows up
- BL180 Phase 1 (closed v4.9.1) — ollama-runtime tap fills `Caller` for ollama envelopes only
- BL173 task 1 (closed v5.0.1) — eBPF kprobe loader now wired

## Goal

Operator scenario (verbatim from the BL): *"on a host where ollama
serves both openwebui and opencode at once, today the GPU/CPU/net
spike rolls up under 'ollama' with no caller attribution. We want
'60% from opencode coding session, 40% from openwebui chat'."*

Phase 1 handles the loaded-model dimension. Phase 2 must answer:
*which **client process** drove the request that loaded that model
or used the GPU just now?*

## Mechanism (sketch — not a decision yet)

Add two more eBPF programs to `netprobe.bpf.c` (or a sibling file):

- `kprobe/__sys_connect` — captures `(client_pid, server_addr,
  server_port, fd)` on every `connect(2)`.
- `kprobe/inet_csk_accept` — captures `(server_pid, client_addr,
  client_port)` when the listener picks up a new connection.

Cross-correlation in userspace: when an envelope's process opens a
TCP connection to a known LLM-server port (ollama 11434, openwebui
3000, etc.), the connect record is the link. The matching accept
record on the server side closes the loop with `server_pid`.

**Result:** an envelope's `Caller` field gets populated with the
client envelope's ID (e.g. `Caller="session:opencode-x1y2"`,
`CallerKind="envelope"`) when the cross-correlation succeeds.

## Questions

### Q1 — Which connection-direction is the source of truth?

- **(a)** Client side only — `kprobe/__sys_connect` from the client
  pid. Cheap; misses listening servers we don't observe (ollama in
  another container).
- **(b)** Server side only — `kprobe/inet_csk_accept` from the server
  pid. Catches every accepted conn; client pid not directly available
  (need a second probe).
- **(c)** Both — definitive correlation but two probes per
  connection.

**Recommendation: (c)**. Two extra kprobes is cheap; both sides
land in a shared map keyed by `(saddr, sport, daddr, dport)` so
the userspace join is O(1).

- Answer: Go with recommendation

### Q2 — Granularity: per-connection or per-byte?

- **(a)** Per-connection — `Caller` is set when the conn is
  established; bytes flowing on the conn inherit attribution.
- **(b)** Per-byte — each `tcp_sendmsg` records the conn tuple,
  userspace pairs each byte to a `Caller`. More accurate when
  multiple clients share a backend.
- **(c)** Per-conn for attribution; per-byte for the existing
  rx/tx counters. Keep the existing `bytes_rx`/`bytes_tx` maps
  unchanged; add a separate `conn_attribution` map keyed by
  `(local_addr, local_port, peer_addr, peer_port)`.

**Recommendation: (c)**. Don't touch the working counters; layer
attribution on top via a separate map.

- Answer - Go with recommendation

### Q3 — What does the envelope look like with multiple callers?

If openwebui + opencode + claude all hit the same ollama at once,
ollama's envelope has three callers in the same tick.

- **(a)** Single `Caller` field — pick the loudest (most bytes).
  Simple but loses information.
- **(b)** New `Callers []CallerAttribution` field with `{caller,
  caller_kind, bytes_rx, bytes_tx}` per attribution.
- **(c)** `Caller` field stays single (back-compat); a new
  `CallerSplit` map surfaces the breakdown when relevant.

**Recommendation: (b)**. Forward-compat for any future per-caller
metric (cost, GPU%, etc.). Existing `Caller`/`CallerKind` fields
become a denormalised "loudest caller" derived from the split, so
existing PWA renders don't break.

- Answer - Go with recommendation; don't worry about existing PWA renders breaking, they should upgrade to latest version

### Q4 — How narrow is the attribution scope?

- **(a)** All TCP conns — every backend gets caller attribution.
  Most general; high cardinality (postgres, redis, etc. all light up).
- **(b)** Only conns to known LLM ports (ollama 11434, openwebui
  3000, openai api.openai.com, etc.). Cheaper; needs a config of
  ports to watch.
- **(c)** Only conns to processes whose envelope is `kind: backend`.
  Attribution flows from "this backend got hit by these clients";
  ignores postgres etc. that aren't classified as backends.

**Recommendation: (c)**. Reuses the existing envelope classifier;
no new config; matches the operator's mental model ("which client
hit which backend").

- Answer - A - but map kind: backend since that is primary purpose. But since there are additoinal connections (postgres and future plugins) all TCP conns from service or clients of service should be mapped

### Q5 — Localhost vs. cross-host

- **(a)** Localhost-only. Simple; misses the case where opencode
  is in one container and ollama in another with different
  network namespaces.
- **(b)** Localhost + same-namespace. Catches container siblings
  on a shared bridge.
- **(c)** Cross-host via federated correlation — operator's primary
  knows about both ends because both are observer peers.

**Recommendation: (b)** to start, **(c)** once the federation
push-out is more battle-tested. Localhost + bridge covers 90 % of
the dev workstation use case; cross-host federation correlation
needs `Source`-aware joins (the field added in v4.8.0) and more
operator-facing UI before it's worth shipping.

- Answer - Follow recommendation.  Also the k8s and docker containers can access this host for testing; use the 192.168 address in testing. don't close until cross-host is working and done

### Q6 — Verification on Thor

Operator noted: *"we will need arm artifact to test this with
local ollama running on thor"*. arm64 artifacts shipped (BL177,
v4.8.22). Phase 2 needs:

- **(a)** A staging deploy on Thor with ollama + datawatch + a
  test client (curl to /api/generate or openwebui).
- **(b)** A unit-test harness that injects synthetic socket events
  into the userspace correlator so we can validate the join logic
  without an arm64 box.
- **(c)** Both — unit tests for the join correctness, Thor
  integration test for end-to-end attribution.

**Recommendation: (c)**. Unit tests gate the merge; Thor smoke
test gates the release.

- Answer - You won't have access to ollama host; i'll have to install so you can run your tests.  So C and let me know how to get thor system running so you can test

## Implementation order (after we align)

1. **Q4 + Q5 first**: get the "what's in scope" answer. Otherwise
   the data shape decisions are theoretical.
2. **Q3**: lock the wire shape (`Callers []CallerAttribution`).
3. **Q1 + Q2**: implement the two new kprobes + the
   `conn_attribution` map. Userspace correlator joins
   client/server records into the new field.
4. **Q6**: unit tests for the join; Thor smoke test for end-to-end.
5. **PWA**: surface the breakdown — `cluster: ollama → split: 60%
   opencode, 40% openwebui` style chip in the federated peers
   card.

## Out of scope for this conversation

- Cost attribution per caller (would inherit from BL117
  orchestrator's session-cost rollup if/when we link sessions to
  callers).
- Non-TCP correlation (UDP, unix sockets) — narrower set of LLM
  servers actually use these; defer.
- Inter-container in different namespaces without a shared bridge —
  needs cgroup or net-ns enumeration; meaningfully larger scope.

## Decision log (filled during the conversation)

_(Empty — populate during the call.)_
