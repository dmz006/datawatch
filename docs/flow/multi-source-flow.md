# Multi-Source Message Flow

```mermaid
flowchart LR
    subgraph Sources
        S[Signal]
        D[Discord]
        SL[Slack]
        W[Web PWA]
    end
    subgraph Routing
        SR[Signal Router]
        DR[Discord Router]
        SLR[Slack Router]
        API[HTTP API]
    end
    subgraph "Local Sessions"
        M[Session Manager]
    end
    subgraph Backends
        CC[Claude Code]
        OL[Ollama]
        OW[OpenWebUI]
    end

    S --> SR --> M
    D --> DR --> M
    SL --> SLR --> M
    W --> API --> M

    M --> CC
    M --> OL
    M --> OW

    M -->|state change| SR
    M -->|state change| DR
    M -->|state change| SLR
    M -->|state change| W
```

## With Proxy Mode (Remote Servers)

When remote servers are configured, the routing layer can forward commands to remote
instances if the session is not found locally.

```mermaid
flowchart LR
    subgraph Sources
        S[Signal]
        D[Discord]
        W[Web PWA]
    end
    subgraph "Proxy Instance"
        SR[Router]
        RD[RemoteDispatcher]
        LM[Local Manager]
    end
    subgraph "Remote: prod"
        RM1[Session Manager]
        CC1[Claude Code]
    end
    subgraph "Remote: pi"
        RM2[Session Manager]
        CC2[Aider]
    end

    S --> SR
    D --> SR
    W --> SR

    SR -->|local| LM
    SR -->|"session not found / @server"| RD
    RD -->|HTTP forward| RM1
    RD -->|HTTP forward| RM2
    RM1 --> CC1
    RM2 --> CC2

    RM1 -->|response| RD
    RM2 -->|response| RD
    RD -->|relay| SR
    SR -->|reply| S
    SR -->|reply| D
    SR -->|reply| W
```
