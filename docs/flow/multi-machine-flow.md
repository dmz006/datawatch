# Multi-Machine Sequence

Two hosts sharing one Signal group.

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group

    box hal9000 (192.168.1.10 / 100.100.1.10)
        participant CLI_H as signal-cli
        participant Agent_H as datawatch
        participant Store_H as sessions.json (hal9000)
    end

    box nas (192.168.1.20 / 100.100.1.20)
        participant CLI_N as signal-cli
        participant Agent_N as datawatch
        participant Store_N as sessions.json (nas)
    end

    Note over User,Store_N: Both daemons start and connect to the same group
    Agent_H->>Group: "[hal9000] datawatch started. Listening on group AI Control"
    Agent_N->>Group: "[nas] datawatch started. Listening on group AI Control"
    Group-->>User: "[hal9000] datawatch started..."
    Group-->>User: "[nas] datawatch started..."

    User->>Group: "new: run integration tests"
    Group->>CLI_H: message
    CLI_H->>Agent_H: receive notification
    Agent_H->>Agent_H: Parse → CmdNew
    Agent_H->>Store_H: Save(session{id:"a3f2",...})
    Agent_H->>Group: "[hal9000][a3f2] Started session for: run integration tests"

    Group->>CLI_N: same message
    CLI_N->>Agent_N: receive notification
    Agent_N->>Agent_N: Parse → CmdNew
    Agent_N->>Store_N: Save(session{id:"c9d1",...})
    Agent_N->>Group: "[nas][c9d1] Started session for: run integration tests"

    Group-->>User: "[hal9000][a3f2] Started..."
    Group-->>User: "[nas][c9d1] Started..."

    Note over User: User wants to check hal9000 only
    User->>Group: "status hal9000-a3f2"
    Group->>CLI_H: message
    CLI_H->>Agent_H: receive
    Agent_H->>Agent_H: GetByShortID("hal9000-a3f2") → found
    Agent_H->>Group: "[hal9000][a3f2] State: running\n---\n<output>"
    Group->>CLI_N: same message
    CLI_N->>Agent_N: receive
    Agent_N->>Agent_N: GetByShortID("hal9000-a3f2") → not found on nas
    Note over Agent_N: nas ignores command silently

    Group-->>User: "[hal9000][a3f2] State: running\n---\n<output>"
```

