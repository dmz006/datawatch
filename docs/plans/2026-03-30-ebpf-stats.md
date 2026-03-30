---
date: 2026-03-30
status: in_progress
---

# Plan: eBPF Per-Session Network & CPU Tracing

## Goal

Use eBPF via `cilium/ebpf` to collect per-session network bytes and CPU time.
Disabled by default. Requires `setcap` for CAP_BPF — configured only via CLI.

## Architecture

```
datawatch start
  └── if ebpf_enabled in config:
       ├── check CAP_BPF on binary
       │   └── if missing: prompt user for sudo password → run setcap → re-exec
       ├── load BPF programs via cilium/ebpf
       │   ├── kprobe: tcp_sendmsg → track TX bytes per PID
       │   ├── kprobe: tcp_recvmsg → track RX bytes per PID
       │   └── tracepoint: sched_switch → track CPU ns per PID
       ├── BPF maps: pid → {rx_bytes, tx_bytes, cpu_ns}
       └── stats collector reads maps every 5s → merges into SessionStat
```

## Implementation Phases

### Phase 1: Setup & Capability Management
- Add `stats.ebpf_enabled: false` to config (default disabled)
- Add `datawatch setup ebpf` CLI command (only way to enable)
- On enable: check if binary has CAP_BPF
  - If not: prompt "eBPF requires elevated privileges. Enter sudo password:"
  - Run `sudo setcap cap_bpf,cap_perfmon+ep <binary_path>`
  - Verify capability was set
  - Save `stats.ebpf_enabled: true` to config
- On daemon start: if ebpf_enabled, verify CAP_BPF exists
  - If missing (binary was replaced by update): warn and disable eBPF
- **NOT configurable from web UI or messaging** — CLI only for security
- Config field hidden from web UI API GET (not exposed)

### Phase 2: BPF Program — Network Bytes
- Add `github.com/cilium/ebpf` dependency
- Write BPF C program (compiled to BPF bytecode):
  ```c
  // kprobe on tcp_sendmsg
  SEC("kprobe/tcp_sendmsg")
  int trace_tcp_send(struct pt_regs *ctx) {
      u32 pid = bpf_get_current_pid_tgid() >> 32;
      struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
      size_t size = PT_REGS_PARM3(ctx);
      // Update per-PID TX counter in BPF map
      update_counter(pid, size, TX);
      return 0;
  }
  ```
- BPF map: `BPF_MAP_TYPE_HASH` keyed by PID, value = {rx_bytes, tx_bytes}
- Go loader: `cilium/ebpf` loads compiled BPF object, attaches kprobes
- Reader: stats collector reads map entries, matches PID → session via pane_pid tree

### Phase 3: BPF Program — CPU Time
- Tracepoint on `sched_switch`:
  ```c
  SEC("tracepoint/sched/sched_switch")
  int trace_sched_switch(struct sched_switch_args *ctx) {
      u32 prev_pid = ctx->prev_pid;
      u64 ts = bpf_ktime_get_ns();
      // Calculate CPU time for prev_pid since last switch
      update_cpu_time(prev_pid, ts);
      return 0;
  }
  ```
- BPF map: PID → cumulative CPU nanoseconds
- Merge into SessionStat alongside network bytes

### Phase 4: Display & Integration
- Add to SessionStat: `NetRxBytes`, `NetTxBytes`, `CPUTimeNs` (per-session, from eBPF)
- Web UI stats dashboard: show per-session network and CPU columns
- Messaging `stats` command: include eBPF metrics when enabled
- Graceful degradation: if eBPF disabled or unavailable, columns show "—"

## Configuration

```yaml
stats:
  ebpf_enabled: false    # only changeable via CLI: datawatch setup ebpf
```

**CLI commands:**
```bash
datawatch setup ebpf          # interactive: check caps, prompt sudo, enable
datawatch setup ebpf --disable # disable eBPF and remove capabilities
```

## Capability Flow

```
User runs: datawatch setup ebpf

1. Check: does binary have CAP_BPF?
   → getcap $(which datawatch) | grep cap_bpf

2. If NO:
   → "eBPF requires CAP_BPF. This needs sudo to set on the binary."
   → "Enter sudo password: " (uses sudo -S)
   → sudo setcap cap_bpf,cap_perfmon+ep $(which datawatch)
   → Verify: getcap again

3. If YES or just set:
   → Save stats.ebpf_enabled: true to config
   → "eBPF enabled. Restart daemon to activate."

On daemon start:
   → if ebpf_enabled && !hasCapBPF():
     warn "eBPF enabled but CAP_BPF missing (binary updated?). Run: datawatch setup ebpf"
     disable eBPF for this run

On auto-restart:
   → capabilities persist on the binary file
   → syscall.Exec inherits capabilities
   → eBPF programs reload automatically
```

## Security

- eBPF disabled by default — must be explicitly enabled via CLI
- NOT configurable from web UI, messaging, or API (prevents remote privilege escalation)
- CAP_BPF + CAP_PERFMON are the minimum capabilities (not full root)
- BPF programs are compiled into the binary (not loaded from external files)
- `setcap` only needs to run once (persists until binary is replaced)
- After binary update (go build / datawatch update): capabilities are lost, user must re-run setup

## Dependencies

- `github.com/cilium/ebpf` v0.12+ — pure Go BPF loader
- Linux kernel 5.8+ recommended (BPF ring buffer, improved BTF)
- BTF data in kernel (most distros since ~5.4)
- `libcap` utils for `setcap`/`getcap` commands

## Estimated Effort

- Phase 1: 3 days (setup, capability management, CLI)
- Phase 2: 1 week (BPF network program, loader, map reader)
- Phase 3: 3 days (BPF CPU program)
- Phase 4: 2 days (UI integration)
- Total: ~2.5 weeks
