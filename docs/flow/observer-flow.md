# Observer tick pipeline

How one collection tick turns into a `StatsResponse v2` payload + WebSocket broadcast.

```mermaid
flowchart TD
    Tick["Tick: every observer.tick_interval_ms<br/>(default 1000)"]

    subgraph ProcWalk["/proc/ walk"]
        Walk[/"/proc/[pid]/{stat,status,cmdline,cgroup,fd}"/]
        Walk --> Raw[raw ProcRecord array]
    end

    subgraph SessionWire["session attribution"]
        Mgr[session.Manager]
        Mgr -->|RegisterSessionRoot<br/>FullID → pane PID| Roots[session roots]
    end

    Tick --> ProcWalk
    Tick --> SessionWire
    Raw --> Classifier
    Roots --> Classifier

    subgraph Classifier["envelope classifier"]
        Pass1[Pass 1: session subtree<br/>by RootPID]
        Pass2[Pass 2: LLM-backend signatures<br/>claude · ollama · aider · …]
        Pass3[Pass 3: docker container<br/>via cgroup parse]
        Pass4[Pass 4: system<br/>everything else]
        Pass1 --> Pass2 --> Pass3 --> Pass4
    end

    Classifier --> Envelopes[Envelope array rolled up<br/>cpu_pct · rss · fds · net · gpu]

    subgraph HostProbes[Host probes]
        Load["/proc/loadavg · /proc/meminfo · /proc/uptime"]
        Disk["/proc/mounts × syscall.Statfs<br/>(real filesystems)"]
        GPU["nvidia-smi --query-compute-apps<br/>(when present)"]
        EBPF["eBPF net: kprobe/tcp_* · kprobe/udp_*<br/>(when CAP_BPF granted)"]
    end

    Tick --> HostProbes
    Envelopes --> Assemble
    HostProbes --> Assemble[StatsResponse v2 assembled<br/>+ v1 flat-field aliases]

    Assemble --> REST["GET /api/stats?v=2"]
    Assemble --> WS["MsgStats WebSocket<br/>(broadcast 1 s cadence)"]
    Assemble --> Peer["Peer push (federated)<br/>POST /api/observer/peers/{name}/stats"]
```

## Envelope classification order

1. Session subtree (root pid → walkSubtree → all descendants claimed).
2. Backend shallowest match (by `comm` / cmdline[0] basename against `observer.backend_signatures`).
3. Container via `/proc/<pid>/cgroup` fallback.
4. System catch-all.

First-match wins per-PID; session attribution outranks backend so claude-code running inside a tmux session counts as session CPU, not free-floating backend CPU.

## Degradation modes

| Missing component | What happens |
|---|---|
| `/proc` not mounted (non-Linux) | `processes.tree` + `envelopes[]` are empty; host/cpu/mem/disk/gpu/sessions/backends still render |
| `nvidia-smi` not in PATH | `gpu[]` empty; envelopes don't gain `gpu_pct` |
| CAP_BPF not granted | `net.per_process[]` empty; envelopes don't gain `net_rx_bps` / `net_tx_bps`; CPU + memory still roll up |
| Docker socket not reachable | container correlation falls back to `/proc/<pid>/cgroup` parsing; `envelope.image` may be empty |
| Session root PID not registered | session envelope never appears; processes that would have joined fall through to backend/system |
