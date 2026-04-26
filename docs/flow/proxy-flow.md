# Proxy Mode — Remote Server Routing

When proxy mode is configured (`servers:` in config), the router can forward commands to remote instances.

```mermaid
graph LR
    subgraph "Messaging Inputs"
        Signal["Signal"]
        Telegram["Telegram"]
        Web["Web PWA"]
    end

    subgraph "Proxy Instance"
        R["Router"]
        D["RemoteDispatcher"]
        PM["Local Manager"]
    end

    subgraph "Remote A"
        RA["API + Sessions"]
    end

    subgraph "Remote B"
        RB["API + Sessions"]
    end

    Signal --> R
    Telegram --> R
    Web --> R

    R -->|"local session"| PM
    R -->|"session not found locally"| D
    R -->|"new: @remoteA: task"| D
    D -->|"forward via HTTP"| RA
    D -->|"forward via HTTP"| RB

    RA -->|"response"| D
    RB -->|"response"| D
    D -->|"relay response"| R
```
<sub>🔍 <a href="https://mermaid.live/view#pako:eNqNkk1ugzAQha8y8rbJBVhUBVGpkRIJkUhZ1F1MYEKsGhvZpilKcveaQFLoj4QXyDPv43n84MQynRMLWGGwOsAy5Qr8svWua3C2ImuxEKqAhapqZznrkHatRaFQvnLWbTh7-9Y2JMl7lF69bUf6lnZe8k9ItuFdIZVz9WuGxOjPxp9vHaqMhhOk3iPVtSMzMo_bNpXaUSxshS47_ACSlSeWOkMJK1RYDOS_R-jcIBwdHnqTMFnAA6x9SkIrO80lGrlEk1y6iGE-f4T-I91iHfbaPPuya6RteeZMXu9quwM4O_sExkAvgdIO9rpWOVxfkU0Lx2NW0TGAJ3O9TBiAQ_s-oOKe2mtzRJPDh0B42WySFknDCUx0Hz7sMUO28rnQcJbofy2-SxIbGAIpm7GSTIki9z_96eLLusrR0XMunDYs2KO0NGNYO71uVMYCZ2q6QbHANvCeunwBqbn9Wg">View this diagram fullscreen (zoom &amp; pan)</a></sub>

**Routing logic:**
1. Command arrives (e.g. `send a3f2: yes`)
2. Router checks local session manager for `a3f2`
3. If not found locally, asks `RemoteDispatcher.FindSession("a3f2")`
4. Dispatcher checks session discovery cache (refreshes from all remotes every 30s)
5. Returns server name → router forwards command via `ForwardCommand(server, text)`
6. Response relayed back to messaging channel

**Explicit routing:**
- `new: @prod: deploy pipeline` → creates session on remote "prod" directly
- `list` → aggregates sessions from local + all remotes

## Recursive proxying

Proxy mode is transitive. A daemon configured to forward to a
*remote* that is itself configured to forward to *another* remote
will hop through the chain. The dispatcher tracks the per-request
hop count and refuses cycles by inspecting the same request-id it
forwards in the HTTP header.

```mermaid
graph LR
    Op[Operator]
    subgraph "Edge proxy"
        E[Router + RemoteDispatcher]
    end
    subgraph "Mid proxy"
        M[Router + RemoteDispatcher]
    end
    subgraph "Origin"
        O[API + Sessions]
    end

    Op -->|"send a3f2: yes"| E
    E -->|"local? no — forward"| M
    M -->|"local? no — forward"| O
    O -->|"response (Request-ID echoed)"| M
    M -->|"relay"| E
    E -->|"relay"| Op

    M -. "loop guard:<br/>reject if Request-ID<br/>seen in last N s" .- M
    E -. "loop guard:<br/>reject if Request-ID<br/>seen in last N s" .- E
```

**Loop prevention:** every forwarded request carries an
`X-Datawatch-Request-ID` header. Each proxy keeps a small LRU of
recently-seen IDs (default 60 s) and returns a `508 Loop Detected`
when an ID re-enters. Operators get a structured error in the
audit log — no silent infinite loop.

**When to use a chain:**
- **Bastion fan-in** — operator's PWA hits an internet-facing
  edge proxy that holds bearer tokens for several internal
  origins. The internal origins never see the public internet.
- **DR mirror** — a tertiary read-only mirror in another region
  forwards mutating commands back to the primary, while serving
  reads locally to keep the mirror low-latency.
- **Federated PRDs** — orchestrator graphs whose PRD nodes target
  a child cluster get proxied transparently; the parent doesn't
  need direct routes to every child cluster's session API.
