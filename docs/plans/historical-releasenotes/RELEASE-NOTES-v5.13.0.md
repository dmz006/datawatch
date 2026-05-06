# datawatch v5.13.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.12.0 → v5.13.0
**Closed:** BL180 Phase 2 eBPF kprobes — and with it, **BL180 in its entirety**

## What's new

### BL180 Phase 2 — eBPF kprobes (resumed)

Per the BL292 v5.6.0 commit roadmap: *"will resume in a separate
cycle with `BPF_MAP_TYPE_LRU_HASH` + userspace TTL pruner"*. v5.13.0
ships exactly that.

#### eBPF C (`internal/observer/ebpf/netprobe.bpf.c`)

Two new kprobes alongside the existing tx/rx byte counters:

```c
SEC("kprobe/tcp_connect")
int BPF_KPROBE(kprobe_tcp_connect, struct sock *sk) {
    // record (sock, pid, ts) on outbound conn initiation
}

SEC("kretprobe/inet_csk_accept")
int BPF_KRETPROBE(kretprobe_inet_csk_accept, struct sock *new_sk) {
    // record (sock, pid, ts) on inbound conn acceptance
}
```

Plus the new map:

```c
struct conn_attr_v {
    __u32 pid;
    __u32 _pad;
    __u64 ts_ns;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_ENTRIES);  // 16384
    __type(key, __u64);                // struct sock * cast to u64
    __type(value, struct conn_attr_v);
} conn_attribution SEC(".maps");
```

`BPF_MAP_TYPE_LRU_HASH` is the load-bearing change from the v5.6.0
backed-out attempt: the kernel auto-evicts the oldest entry when the
map fills, so we don't need a userspace pruner just for memory
bounds. The userspace pruner still runs to keep the visible set
fresh.

`vmlinux_amd64/` + `vmlinux_arm64/` BTF headers regenerated cleanly
under clang 20.1.8; the `_x86_bpfel.{go,o}` and `_arm64_bpfel.{go,o}`
artifacts are committed and the CI drift-check (BL177 longer-term,
v5.0.2) verifies they don't drift.

#### Userspace (`internal/observer/ebpf/loader_linux.go`)

Two new methods on `realLinuxKprobeProbe`:

```go
func (r *realLinuxKprobeProbe) ReadConnAttribution() []ConnAttribution
func (r *realLinuxKprobeProbe) PruneConnAttribution(olderThanNs uint64) int
```

`ReadConnAttribution` iterates the BPF map and returns one
`ConnAttribution{Sock, PID, TsNs}` per live entry. Cheap enough to
run every observer tick.

`PruneConnAttribution` walks the map and deletes entries with
`TsNs < olderThanNs`. Returns the count of entries deleted. Combined
with the LRU eviction policy, this gives both memory and freshness
guarantees.

The loader attaches the new probes alongside the existing four
(tcp/udp_sendmsg + tcp/udp_recvmsg returns); failure on the new pair
is non-fatal — partial mode keeps byte counters live.

### Tests

3 new unit tests in `internal/observer/ebpf/conn_attribution_test.go`:

- `TestRealLinuxKprobeProbe_ReadConnAttribution_NilSafe` — Read +
  Prune on an unloaded probe return safely (no panic, sentinel
  values).
- `TestRealLinuxKprobeProbe_NoopAfterClose` — post-Close, both Read
  and Prune become no-ops; double-close is idempotent.
- `TestConnAttributionRow_Shape` — locks down the public type contract.

Real attach + kprobe behaviour requires CAP_BPF + a kernel that
exposes the `tcp_connect` and `inet_csk_accept` symbols. That stays
as the operator-side Thor smoke-test (BL180 design Q6).

Total daemon test count: **1355 passed**.

## What this closes

BL180 Phase 2 in its entirety:

- v5.1.0 — procfs userspace correlator + `Envelope.Callers` shape
- v5.12.0 — cross-host federation correlation (`CorrelateAcrossPeers`,
  `Envelope.ListenAddrs` + `OutboundEdges`)
- v5.13.0 — eBPF kprobe layer (`tcp_connect` + `inet_csk_accept` +
  `conn_attribution` LRU map + userspace pruner)

Operator's two outstanding answers from the BL180 Phase 2 design doc
are both now closed:

- Q1 (eBPF kprobes) — shipped v5.13.0.
- Q5c (cross-host correlation) — shipped v5.12.0.
- Q6 (Thor smoke-test) — operator-side validation; capability gate
  documented.

## Known follow-ups (still open)

- **BL190 expand-and-fill** — extend the `howto-shoot.mjs` recipe map
  per remaining 9 howtos. Mechanical.

## Upgrade path

```bash
datawatch update                                # check + install
datawatch restart                               # apply the new binary

# Verify the new probes attempt to attach (look for tcp_connect /
# inet_csk_accept in the daemon log, or call /api/observer/stats and
# check host.ebpf.kprobes_loaded):
datawatch logs | grep ebpf
curl -sk -H "Authorization: Bearer $(cat ~/.datawatch/token)" \
  https://localhost:8443/api/observer/stats | jq '.host.ebpf'
```

If `kprobes_loaded` is `false` and the message says "no kprobes
attached", the daemon doesn't have CAP_BPF — set it with
`datawatch setup ebpf` (one-time `sudo` for the capability) and
restart.
