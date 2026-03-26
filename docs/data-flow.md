# Data Flow — claude-signal

Detailed diagrams showing how data moves through the system.

---

## 1. System Data Flow

Top-level component interaction diagram.

```mermaid
graph TD
    subgraph "Signal Infrastructure"
        Phone["Signal Mobile App"]
        Group["Signal Group\n(E2E encrypted)"]
    end

    subgraph "signal-cli (Java subprocess)"
        SCLI["signal-cli\njsonRpc mode"]
    end

    subgraph "claude-signal daemon"
        Bridge["SignalCLIBackend\n(JSON-RPC over stdin/stdout)"]
        Router["Router\n(command parser + dispatcher)"]
        Manager["Session Manager\n(lifecycle + callbacks)"]
        Store["sessions.json\n(flat JSON, mutex-protected)"]
        Monitor1["monitorOutput goroutine\n(session a3f2)"]
        Monitor2["monitorOutput goroutine\n(session b7c1)"]
        HTTPServer["HTTP Server :8080\n(REST + WebSocket)"]
        Hub["WebSocket Hub\n(broadcast to all clients)"]
    end

    subgraph "tmux sessions"
        T1["cs-myhost-a3f2\n(claude-code)"]
        T2["cs-myhost-b7c1\n(claude-code)"]
        L1["logs/myhost-a3f2.log"]
        L2["logs/myhost-b7c1.log"]
    end

    subgraph "PWA clients (Tailscale)"
        PWA1["Phone Browser"]
        PWA2["Tablet Browser"]
    end

    Phone -->|group message| Group
    Group <-->|E2E encrypted| SCLI
    SCLI <-->|stdin/stdout JSON-RPC| Bridge
    Bridge -->|IncomingMessage| Router
    Router -->|Start/Kill/SendInput/Tail| Manager
    Router -->|Send reply| Bridge
    Bridge -->|send JSON-RPC| SCLI
    Manager --> Store
    Manager -->|tmux new-session\nsend-keys| T1
    Manager -->|tmux new-session\nsend-keys| T2
    T1 -->|pipe-pane| L1
    T2 -->|pipe-pane| L2
    Monitor1 -->|poll| L1
    Monitor2 -->|poll| L2
    Monitor1 -->|onStateChange\nonNeedsInput| Manager
    Monitor2 -->|onStateChange\nonNeedsInput| Manager
    Manager -->|NotifyStateChange\nNotifyNeedsInput\nBroadcastOutput| HTTPServer
    HTTPServer --> Hub
    Hub -->|WebSocket push| PWA1
    Hub -->|WebSocket push| PWA2
    PWA1 -->|WebSocket commands| Hub
    Hub -->|command dispatch| Manager
```

---

## 2. New Session Sequence

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group
    participant CLI as signal-cli
    participant Bridge as SignalCLIBackend
    participant Router as Router
    participant Manager as Session Manager
    participant LLM as llm.Backend (claude-code)
    participant Tmux as tmux
    participant Store as sessions.json
    participant Monitor as monitorOutput goroutine
    participant Hub as WebSocket Hub

    User->>Group: "new: add authentication middleware"
    Group->>CLI: message delivered (E2E decrypted)
    CLI->>Bridge: JSON-RPC notification: receive\n{envelope: {dataMessage: {message: "new: add auth..."}}}
    Bridge->>Bridge: filter: not self, correct group
    Bridge->>Router: handleMessage(IncomingMessage{Text: "new: add auth..."})
    Router->>Router: Parse() → CmdNew{Text: "add authentication middleware"}
    Router->>Manager: Start(ctx, "add authentication middleware", groupID)
    Manager->>Manager: crypto/rand → shortID "a3f2"\nfullID = "myhost-a3f2"
    Manager->>Tmux: new-session -d -s cs-myhost-a3f2
    Manager->>Tmux: pipe-pane -o "cat >> ~/.claude-signal/logs/myhost-a3f2.log"
    Manager->>LLM: Launch(ctx, "add authentication middleware", "cs-myhost-a3f2", logFile)
    LLM->>Tmux: send-keys -t cs-myhost-a3f2 'claude "add authentication middleware"' Enter
    Manager->>Store: Save(Session{id:"a3f2", state:"running", ...})
    Manager->>Monitor: go monitorOutput(ctx, sess)
    Manager->>Hub: BroadcastSessions(allSessions)
    Manager-->>Router: Session{ID:"a3f2", TmuxSession:"cs-myhost-a3f2"}
    Router->>Bridge: Send(groupID, "[myhost][a3f2] Started session for:\nadd authentication middleware\nTmux: cs-myhost-a3f2")
    Bridge->>CLI: JSON-RPC send request
    CLI->>Group: message delivered
    Group-->>User: "[myhost][a3f2] Started session for:..."
```

---

## 3. Input Required Sequence

```mermaid
sequenceDiagram
    participant Claude as claude-code process
    participant Log as myhost-a3f2.log
    participant Monitor as monitorOutput goroutine
    participant Manager as Session Manager
    participant Store as sessions.json
    participant Hub as WebSocket Hub
    participant Router as Router
    participant Bridge as SignalCLIBackend
    participant Group as Signal Group
    actor User as User (Signal)

    Note over Claude,Log: claude-code writes a prompt to the terminal
    Claude->>Log: "Found 3 existing test files. Overwrite all? [y/N] "
    Note over Monitor: polls log file every 200ms
    Monitor->>Log: read new line
    Monitor->>Monitor: append to buffer\nreset idle timer
    Note over Monitor: ... 10 seconds pass with no new output ...
    Monitor->>Monitor: idle timer fires
    Monitor->>Monitor: check tmux has-session cs-myhost-a3f2 → alive
    Monitor->>Monitor: last line ends with "[y/N] " → prompt pattern matched
    Monitor->>Manager: setState(sess, "waiting_input")\nLastPrompt = "Found 3 existing test files. Overwrite all? [y/N] "
    Manager->>Store: Save(Session{state:"waiting_input", LastPrompt:...})
    Manager->>Hub: BroadcastNeedsInput("myhost-a3f2", "Found 3 existing test files...")
    Hub->>Hub: send to all WebSocket clients
    Manager->>Router: onNeedsInput(sess, prompt)
    Router->>Bridge: Send(groupID, "[myhost][a3f2] Needs input:\nFound 3 existing test files. Overwrite all? [y/N]\n\nReply: send a3f2: <response>")
    Bridge->>Group: message delivered
    Group-->>User: "[myhost][a3f2] Needs input:..."

    User->>Group: "send a3f2: y"
    Group->>Bridge: JSON-RPC receive notification
    Bridge->>Router: handleMessage()
    Router->>Router: Parse() → CmdSend{SessionID:"a3f2", Text:"y"}
    Router->>Manager: SendInput("myhost-a3f2", "y")
    Manager->>Manager: setState(sess, "running")
    Manager->>Store: Save(Session{state:"running"})
    Manager->>Claude: tmux send-keys -t cs-myhost-a3f2 "y" Enter
    Manager->>Hub: BroadcastSessions(allSessions)
    Router->>Bridge: Send(groupID, "[myhost][a3f2] Input sent.")
    Bridge->>Group: message delivered
    Group-->>User: "[myhost][a3f2] Input sent."
    Note over Claude,Log: claude-code continues execution, writes more output
```

---

## 4. Multi-Machine Sequence

Two hosts sharing one Signal group.

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group

    box hal9000 (192.168.1.10 / 100.100.1.10)
        participant CLI_H as signal-cli
        participant Agent_H as claude-signal
        participant Store_H as sessions.json (hal9000)
    end

    box nas (192.168.1.20 / 100.100.1.20)
        participant CLI_N as signal-cli
        participant Agent_N as claude-signal
        participant Store_N as sessions.json (nas)
    end

    Note over User,Store_N: Both daemons start and connect to the same group
    Agent_H->>Group: "[hal9000] claude-signal started. Listening on group AI Control"
    Agent_N->>Group: "[nas] claude-signal started. Listening on group AI Control"
    Group-->>User: "[hal9000] claude-signal started..."
    Group-->>User: "[nas] claude-signal started..."

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

---

## 5. WebSocket Message Flow

What messages flow on key events.

```mermaid
sequenceDiagram
    participant Browser as Browser (PWA)
    participant HTTP as HTTP Server
    participant Hub as WebSocket Hub
    participant Manager as Session Manager

    Note over Browser,Hub: On connect
    Browser->>HTTP: GET /ws?token=... (Upgrade: websocket)
    HTTP->>Hub: register client
    Hub->>Browser: {"type":"sessions","data":{"sessions":[...]}}

    Note over Browser,Hub: User subscribes to a session's output
    Browser->>Hub: {"type":"subscribe","data":{"session_id":"a3f2"}}

    Note over Manager,Hub: New output lines arrive (from monitor goroutine)
    Manager->>Hub: BroadcastOutput("myhost-a3f2", ["Building...", "Compiling pkg/auth..."])
    Hub->>Browser: {"type":"output","data":{"session_id":"myhost-a3f2","lines":["Building...","Compiling..."]}}

    Note over Manager,Hub: Session state changes
    Manager->>Hub: BroadcastSessions(allSessions)
    Hub->>Browser: {"type":"sessions","data":{"sessions":[{..."state":"complete"}]}}

    Note over Manager,Hub: Session needs input
    Manager->>Hub: BroadcastNeedsInput("myhost-a3f2", "Continue? [y/N]")
    Hub->>Browser: {"type":"needs_input","data":{"session_id":"myhost-a3f2","prompt":"Continue? [y/N]"}}
    Note over Browser: browser notification fires\ninput bar highlighted

    Note over Browser,Hub: User sends input via PWA
    Browser->>Hub: {"type":"send_input","data":{"session_id":"myhost-a3f2","text":"y"}}
    Hub->>Manager: SendInput("myhost-a3f2", "y")
    Manager->>Hub: BroadcastSessions(allSessions)
    Hub->>Browser: {"type":"sessions","data":{"sessions":[{..."state":"running"}]}}

    Note over Browser,Hub: Keepalive (every 30s)
    Hub->>Browser: WebSocket Ping frame
    Browser->>Hub: WebSocket Pong frame
```

---

## 6. signal-cli JSON-RPC Flow

The full JSON-RPC protocol between claude-signal and signal-cli.

```mermaid
sequenceDiagram
    participant Go as claude-signal (Go)
    participant Pipe as stdin/stdout pipe
    participant SCLI as signal-cli (Java)
    participant Signal as Signal Network

    Note over Go,SCLI: Daemon startup — subscribe to incoming messages
    Go->>Pipe: {"jsonrpc":"2.0","method":"subscribeReceive","id":1}\n
    Pipe->>SCLI: [reads line]
    SCLI->>Signal: open WebSocket to Signal servers
    SCLI->>Pipe: {"jsonrpc":"2.0","result":{},"id":1}\n
    Pipe->>Go: [reads line, resolves pending[1]]

    Note over Go,Signal: Incoming message arrives
    Signal->>SCLI: Signal protocol: data message from +12125551234
    SCLI->>Pipe: {"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+12125551234","dataMessage":{"message":"list","groupInfo":{"groupId":"base64=="}}}}}\n
    Pipe->>Go: [reads line, no id → notification → dispatchNotification]
    Go->>Go: filter self-messages, check group ID\nParse("list") → CmdList\nhandleList() → format reply

    Note over Go,SCLI: Send reply to group
    Go->>Pipe: {"jsonrpc":"2.0","method":"send","params":{"groupId":"base64==","message":"[myhost] Sessions:\n  [a3f2] running  write tests"},"id":2}\n
    Pipe->>SCLI: [reads line]
    SCLI->>Signal: Signal protocol: send group message
    SCLI->>Pipe: {"jsonrpc":"2.0","result":{"timestamp":1711234567890},"id":2}\n
    Pipe->>Go: [reads line, resolves pending[2] → Send() returns nil]

    Note over Go,SCLI: List groups
    Go->>Pipe: {"jsonrpc":"2.0","method":"listGroups","id":3}\n
    Pipe->>SCLI: [reads line]
    SCLI->>Pipe: {"jsonrpc":"2.0","result":[{"id":"base64==","name":"AI Control","active":true}],"id":3}\n
    Pipe->>Go: [reads line, resolves pending[3] → ListGroups() returns [{base64==, AI Control}]]
```

---

## 7. Persistence Flow

When and why data is written to disk.

```mermaid
sequenceDiagram
    participant Main as main.go
    participant Config as config.Load()
    participant Store as session.Store
    participant Disk as sessions.json
    participant Monitor as monitorOutput goroutine
    participant Tmux as tmux pipe-pane
    participant Log as session log file

    Note over Main,Config: Startup — config read once
    Main->>Config: config.Load(~/.claude-signal/config.yaml)
    Config->>Disk: os.ReadFile(config.yaml)
    Disk-->>Config: raw YAML bytes
    Config-->>Main: *Config (merged with defaults)

    Note over Main,Disk: Startup — session store loaded
    Main->>Store: session.NewStore(~/.claude-signal/sessions.json)
    Store->>Disk: os.ReadFile(sessions.json)
    Disk-->>Store: JSON array of Session objects
    Note over Store: in-memory map populated

    Note over Store,Disk: State change — session created
    Store->>Disk: os.WriteFile(sessions.json) [full rewrite]
    Note over Disk: atomic write, 0644 permissions

    Note over Tmux,Log: Session running — output captured
    Tmux->>Log: cat >> logs/myhost-a3f2.log [continuous, via pipe-pane]
    Note over Log: log file grows as claude-code produces output

    Note over Monitor,Disk: State change — waiting_input detected
    Monitor->>Store: Save(session{state:"waiting_input", LastPrompt:...})
    Store->>Disk: os.WriteFile(sessions.json) [full rewrite]

    Note over Store,Disk: State change — session complete
    Monitor->>Store: Save(session{state:"complete"})
    Store->>Disk: os.WriteFile(sessions.json) [full rewrite]

    Note over Main,Disk: Restart — state recovered
    Main->>Store: session.NewStore(sessions.json)
    Store->>Disk: os.ReadFile(sessions.json)
    Disk-->>Store: [{id:"a3f2", state:"complete"}, {id:"b7c1", state:"running"}]
    Note over Main: ResumeMonitors() called\nonly running/waiting_input sessions get new monitor goroutines\nb7c1 monitor goroutine restarted
```
