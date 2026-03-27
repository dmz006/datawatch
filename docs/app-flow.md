# Application Flow — datawatch

Mermaid diagrams for all major flows through the system.

---

## 1. Daemon Startup Flow

```mermaid
flowchart TD
    A([datawatch start]) --> B[Load config.yaml\nmerge over defaults]
    B --> C{Config valid?\nSignal account set?}
    C -- no signal config --> D[Start HTTP server only\nno Signal listener]
    C -- yes --> E[Connect signal-cli subprocess\njsonRpc mode]
    E --> F{signal-cli started?}
    F -- error --> G([Fatal: log error\ncheck signal-cli in PATH\ncheck Java version])
    F -- ok --> H[Load sessions.json\nNewStore]
    D --> H
    H --> I[Resume monitors\nfor each running/waiting_input session on this host]
    I --> J{tmux session exists?}
    J -- yes --> K[Start monitorOutput goroutine]
    J -- no --> L[Mark session failed\nwrite sessions.json]
    K --> M[Start HTTP server\nbind host:port]
    L --> M
    M --> N{server.enabled?}
    N -- yes --> O[Start WebSocket hub goroutine]
    N -- no --> P[Subscribe to Signal messages\nsubscribeReceive JSON-RPC]
    O --> P
    P --> Q([Ready — listening for Signal messages\nand WebSocket connections])
```

---

## 2. New Session Flow

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group
    participant CLI as signal-cli
    participant Bridge as SignalCLIBackend
    participant Router as Router
    participant Manager as Session Manager
    participant Store as sessions.json
    participant Tmux as tmux
    participant Claude as claude-code

    User->>Group: "new: refactor auth module"
    Group->>CLI: delivers message
    CLI->>Bridge: JSON-RPC receive notification
    Bridge->>Bridge: filter self-messages\nfilter group ID
    Bridge->>Router: handleMessage(IncomingMessage)
    Router->>Router: Parse() → CmdNew\ntext="refactor auth module"
    Router->>Manager: Start(ctx, "refactor auth module", groupID)
    Manager->>Manager: generateID() → "a3f2"\nfullID = "myhost-a3f2"
    Manager->>Manager: create log file path\n~/.datawatch/logs/myhost-a3f2.log
    Manager->>Tmux: new-session -d -s cs-myhost-a3f2
    Manager->>Tmux: pipe-pane -o → logs/myhost-a3f2.log
    Manager->>Tmux: send-keys → claude "refactor auth module"
    Tmux->>Claude: starts claude-code process
    Manager->>Store: Save(session{state:running})
    Manager->>Manager: go monitorOutput(ctx, sess)
    Manager-->>Router: Session{ID:"a3f2", ...}
    Router->>Bridge: Send(groupID, "[myhost][a3f2] Started...")
    Bridge->>Group: JSON-RPC send
    Group-->>User: "[myhost][a3f2] Started session for: refactor auth module\nTmux: cs-myhost-a3f2"
```

---

## 3. Output Monitor Flow

```mermaid
flowchart TD
    A([monitorOutput goroutine starts]) --> B[Open log file\nwait if not yet created]
    B --> C[Seek to EOF\nskip history on resume]
    C --> D[Reset idle timer\ninput_idle_timeout seconds]
    D --> E{Read next line\nfrom log file}
    E -- new line --> F[Append to line buffer\ncap at 100 lines]
    F --> G{Was state\nwaiting_input?}
    G -- yes --> H[Set state = running\nnotify callbacks]
    G -- no --> D
    H --> D
    E -- EOF poll --> I[Sleep 200ms]
    I --> J{Context\ncancelled?}
    J -- yes --> K([Goroutine exits\ndaemon shutting down])
    J -- no --> E
    E -- idle timer fires --> L{tmux session\nstill alive?}
    L -- no --> M{Was state\nrunning or\nwaiting_input?}
    M -- yes --> N[Set state = complete or failed\nwrite sessions.json\ncall onStateChange]
    N --> K
    M -- no --> K
    L -- yes --> O{Last line matches\nprompt pattern?\n? [ : > [y/N]}
    O -- no --> D
    O -- yes --> P[Set state = waiting_input\nstore LastPrompt\ncall onNeedsInput\ncall onStateChange]
    P --> D
```

---

## 4. Input Required Flow

```mermaid
sequenceDiagram
    participant Claude as claude-code (tmux)
    participant Log as myhost-a3f2.log
    participant Monitor as monitorOutput goroutine
    participant Manager as Session Manager
    participant Store as sessions.json
    participant Router as Router
    participant Bridge as SignalCLIBackend
    participant Group as Signal Group
    actor User as User (Signal)

    Claude->>Log: "Do you want to overwrite auth.go? [y/N] "
    Note over Monitor: idle timer fires after 10s with no new output
    Monitor->>Monitor: check tmux has-session → alive
    Monitor->>Monitor: last line ends with "[y/N] " → prompt detected
    Monitor->>Manager: setState(waiting_input, prompt)
    Manager->>Store: Save(session{state:waiting_input, LastPrompt:...})
    Manager->>Router: onNeedsInput(sess, prompt)
    Router->>Bridge: Send(groupID, "[myhost][a3f2] Needs input:\nDo you want to overwrite? [y/N]\n\nReply: send a3f2: <response>")
    Bridge->>Group: delivers message
    Group-->>User: "[myhost][a3f2] Needs input:\n..."

    User->>Group: "send a3f2: y"
    Group->>Bridge: JSON-RPC receive notification
    Bridge->>Router: handleMessage()
    Router->>Router: Parse() → CmdSend{SessionID:"a3f2", Text:"y"}
    Router->>Manager: SendInput("myhost-a3f2", "y")
    Manager->>Store: Save(session{state:running})
    Manager->>Claude: tmux send-keys "y" Enter
    Note over Monitor: new output arrives → monitor transitions back to running
    Router->>Bridge: Send(groupID, "[myhost][a3f2] Input sent.")
    Bridge->>Group: delivers message
    Group-->>User: "[myhost][a3f2] Input sent."
```

---

## 5. Session Complete Flow

```mermaid
sequenceDiagram
    participant Claude as claude-code
    participant Tmux as tmux
    participant Log as myhost-a3f2.log
    participant Monitor as monitorOutput goroutine
    participant Manager as Session Manager
    participant Store as sessions.json
    participant Router as Router
    participant Bridge as SignalCLIBackend
    participant Hub as WebSocket Hub
    participant Group as Signal Group
    actor User as User (Signal)
    participant PWA as PWA (Browser)

    Claude->>Tmux: process exits with code 0
    Tmux->>Tmux: session window closes\nsession ends
    Log->>Log: pipe-pane stops writing
    Note over Monitor: idle timer fires — no new output
    Monitor->>Tmux: has-session cs-myhost-a3f2 → false
    Monitor->>Manager: setState(complete)
    Manager->>Store: Save(session{state:complete})
    Manager->>Router: onStateChange(sess, running → complete)
    Manager->>Hub: NotifyStateChange(sess)
    Router->>Bridge: Send(groupID, "[myhost][a3f2] complete ✓")
    Bridge->>Group: delivers message
    Group-->>User: "[myhost][a3f2] complete ✓"
    Hub->>PWA: {"type":"session_state","data":{"session":{..."state":"complete"}}}
    Note over PWA: session card updates to green "complete" badge
```

---

## 6. PWA WebSocket Flow

```mermaid
sequenceDiagram
    participant Browser as Browser / PWA
    participant HTTP as HTTP Server (:8080)
    participant Hub as WebSocket Hub
    participant Manager as Session Manager
    participant Store as sessions.json

    Browser->>HTTP: GET /ws (Upgrade: websocket)
    Note over HTTP: auth middleware checks ?token= if configured
    HTTP->>Hub: register new client
    Hub->>Browser: {"type":"sessions","data":{"sessions":[...]}}
    Note over Browser: session list rendered

    Browser->>Hub: {"type":"subscribe","data":{"session_id":"a3f2"}}
    Note over Hub: client marked as subscribed to a3f2

    Note over Manager: new output lines arrive from monitor goroutine
    Manager->>Hub: BroadcastOutput("a3f2", ["line1","line2"])
    Hub->>Browser: {"type":"output","data":{"session_id":"a3f2","lines":["line1","line2"]}}
    Note over Browser: output panel updates in real time

    Note over Manager: session enters waiting_input
    Manager->>Hub: BroadcastNeedsInput("a3f2", "Continue? [y/N]")
    Hub->>Browser: {"type":"needs_input","data":{"session_id":"a3f2","prompt":"Continue? [y/N]"}}
    Note over Browser: input bar highlighted\nbrowser notification fired

    Browser->>Hub: {"type":"send_input","data":{"session_id":"a3f2","text":"y"}}
    Hub->>Manager: SendInput("a3f2", "y")
    Manager->>Store: Save(session{state:running})
    Hub->>Browser: {"type":"sessions","data":{"sessions":[...updated...]}}
```

---

## 7. QR Linking Flow

Two paths for linking a Signal device.

```mermaid
sequenceDiagram
    participant User as User

    box Terminal path
        participant CLI as datawatch CLI
        participant SignalCLI as signal-cli subprocess
        participant Terminal as Terminal (QR render)
    end

    box PWA path
        participant PWA as PWA Browser
        participant HTTP as HTTP Server
        participant SSE as SSE stream (/api/link/stream)
        participant SignalCLI2 as signal-cli subprocess
    end

    participant Phone as Signal Mobile App

    Note over User,Terminal: Terminal path
    User->>CLI: datawatch link
    CLI->>SignalCLI: exec signal-cli link -n myhost
    SignalCLI->>SignalCLI: generate key pair\nregister as linked device
    SignalCLI->>CLI: stdout: sgnl://linkdevice?uuid=...
    CLI->>Terminal: render QR code from sgnl:// URI
    User->>Phone: Signal → Settings → Linked Devices → Link New Device
    Phone->>Phone: scan QR code
    Phone->>SignalCLI: Signal protocol: confirm link
    SignalCLI->>CLI: exit 0
    CLI->>User: "Device linked successfully"

    Note over User,SSE: PWA path
    User->>PWA: Settings → Link Device → Start Linking
    PWA->>HTTP: POST /api/link/start
    HTTP->>PWA: {"stream_id": "abc123"}
    PWA->>SSE: GET /api/link/stream?id=abc123
    HTTP->>SignalCLI2: exec signal-cli link -n myhost
    SignalCLI2->>HTTP: stdout: sgnl://linkdevice?uuid=...
    HTTP->>SSE: event: qr\ndata: {"uri":"sgnl://..."}
    SSE->>PWA: qr event with URI
    PWA->>PWA: render QR code in browser
    User->>Phone: Signal → Settings → Linked Devices → Link New Device
    Phone->>Phone: scan QR code
    Phone->>SignalCLI2: Signal protocol: confirm link
    SignalCLI2->>HTTP: exit 0
    HTTP->>SSE: event: linked\ndata: {"success":true}
    SSE->>PWA: linked event
    PWA->>User: "Device linked successfully"
```

---

## 8. Multi-Machine Message Flow

Two hosts (`hal9000` and `nas`) both connected to the same Signal group.

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group

    box hal9000
        participant CLI_H as signal-cli (hal9000)
        participant Agent_H as datawatch daemon (hal9000)
        participant Sessions_H as Sessions on hal9000
    end

    box nas
        participant CLI_N as signal-cli (nas)
        participant Agent_N as datawatch daemon (nas)
        participant Sessions_N as Sessions on nas
    end

    User->>Group: "list"

    Group->>CLI_H: message delivered
    CLI_H->>Agent_H: JSON-RPC receive notification
    Agent_H->>Agent_H: Parse() → CmdList
    Agent_H->>Sessions_H: list local sessions
    Agent_H->>Group: "[hal9000] Sessions:\n  [a3f2] running  refactor auth module"

    Group->>CLI_N: message delivered
    CLI_N->>Agent_N: JSON-RPC receive notification
    Agent_N->>Agent_N: Parse() → CmdList
    Agent_N->>Sessions_N: list local sessions
    Agent_N->>Group: "[nas] Sessions:\n  [b7c1] waiting_input  deploy to staging"

    Group-->>User: "[hal9000] Sessions:\n  [a3f2] running  refactor auth module"
    Group-->>User: "[nas] Sessions:\n  [b7c1] waiting_input  deploy to staging"

    Note over User: User replies to nas session
    User->>Group: "send b7c1: yes"

    Group->>CLI_H: message delivered
    CLI_H->>Agent_H: JSON-RPC receive notification
    Agent_H->>Agent_H: GetByShortID("b7c1") → not found on hal9000
    Note over Agent_H: hal9000 ignores the command silently

    Group->>CLI_N: message delivered
    CLI_N->>Agent_N: JSON-RPC receive notification
    Agent_N->>Agent_N: GetByShortID("b7c1") → found
    Agent_N->>Agent_N: SendInput("nas-b7c1", "yes")
    Agent_N->>Group: "[nas][b7c1] Input sent."
    Group-->>User: "[nas][b7c1] Input sent."
```
