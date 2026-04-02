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
