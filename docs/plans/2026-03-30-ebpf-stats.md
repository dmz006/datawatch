---
date: 2026-03-30
status: planned
---

# Plan: eBPF Per-Session Network & CPU Tracing

## Goal

Use eBPF (extended Berkeley Packet Filter) to collect fine-grained per-session
network bytes, CPU time, and syscall counts — data that can't be obtained from
/proc alone because tmux child processes may spawn subprocesses dynamically.

## Current State

- Per-session memory (RSS) is collected via `/proc/<pane_pid>/statm` + child summing
- Network stats are system-wide from `/proc/net/dev` (no per-process breakdown)
- CPU is system-wide load average, not per-session
- No root access required for current stats

## What eBPF Enables

| Metric | Current | With eBPF |
|--------|---------|-----------|
| Network bytes per session | Not available | Per-cgroup or per-PID tracking |
| CPU time per session | Not available | Per-PID CPU accounting via sched_process_exec |
| Syscall counts | Not available | Per-PID syscall frequency |
| File I/O per session | Not available | Per-PID read/write bytes |

## Prerequisites

- Linux kernel 4.15+ (for BPF CO-RE)
- Root or CAP_BPF capability to load BPF programs
- `libbpf` or a Go eBPF library (e.g. `cilium/ebpf`, `aquasecurity/libbpfgo`)

## Architecture

```
datawatch daemon
  └── eBPF loader (root or CAP_BPF)
       ├── kprobe: tcp_sendmsg → track TX bytes per PID
       ├── kprobe: tcp_recvmsg → track RX bytes per PID
       ├── tracepoint: sched_process_exec → track new processes in session cgroup
       └── BPF map: pid → session_id → {rx_bytes, tx_bytes, cpu_ns}

Stats collector (every 5s)
  └── reads BPF map → aggregates per session → merges into SessionStat
```

## Phases

### Phase 1: Research & Feasibility (1 week)
- Evaluate `cilium/ebpf` vs `libbpfgo` for Go integration
- Prototype: load a simple BPF program that counts TCP bytes per PID
- Test on the target kernel version
- Determine if CAP_BPF is sufficient or if full root is needed
- Document kernel version requirements

### Phase 2: Per-Session Network Tracking (1-2 weeks)
- BPF program: kprobe on tcp_sendmsg/tcp_recvmsg
- Map PID → session by looking up tmux pane PID → child tree
- Aggregate RX/TX bytes per session
- Add to SessionStat struct
- Display in web UI stats dashboard

### Phase 3: Per-Session CPU Tracking (1 week)
- BPF program: tracepoint on sched_switch or sched_process_exec
- Track CPU nanoseconds per PID
- Aggregate per session
- Display in stats dashboard

### Phase 4: Optional Enhancements
- Syscall counting per session
- File I/O (read/write bytes) per session
- Network connection tracking (source/dest IP:port per session)
- Historical graphs in web UI

## Configuration

```yaml
stats:
  ebpf_enabled: false      # requires root or CAP_BPF
  collection_interval: 5s  # how often to read BPF maps
```

## Security Considerations

- eBPF requires elevated privileges (root or CAP_BPF)
- BPF programs run in kernel space — must be carefully vetted
- Use CO-RE (Compile Once, Run Everywhere) for portability
- Graceful degradation: if eBPF is not available, fall back to /proc stats

## Dependencies

- `github.com/cilium/ebpf` — Go library for loading/managing BPF programs
- Kernel headers or BTF (BPF Type Format) for CO-RE
- No external daemon needed — BPF programs loaded by datawatch itself

## Estimated Effort

- Phase 1: 1 week (research + prototype)
- Phase 2: 1-2 weeks (network tracking)
- Phase 3: 1 week (CPU tracking)
- Total: 3-4 weeks

## Decision: Not implementing now

eBPF integration is deferred because:
1. Current /proc stats provide useful session memory and system metrics
2. eBPF requires root/CAP_BPF which complicates deployment
3. The value (per-session network) is nice-to-have, not critical
4. Focus should be on stability and bug fixes first
