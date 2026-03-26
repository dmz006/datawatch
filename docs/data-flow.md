# Data Flow

Sequence diagrams for the major flows in `claude-signal`.

---

## 1. New Session Flow

User sends `new: build docker image` in Signal.

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group
    participant Backend as SignalCLIBackend
    participant Router as Router
    participant Manager as Session Manager
    participant Tmux as tmux

    User->>Group: "new: build docker image"
    Group->>Backend: JSON-RPC receive notification
    Backend->>Router: handleMessage(IncomingMessage)
    Router->>Router: Parse() → CmdNew
    Router->>Manager: Start(ctx, "build docker image", groupID)
    Manager->>Manager: generateID() → "a3f2"
    Manager->>Manager: create log file
    Manager->>Tmux: NewSession("cs-myhost-a3f2")
    Manager->>Tmux: PipeOutput("cs-myhost-a3f2", "logs/myhost-a3f2.log")
    Manager->>Tmux: SendKeys("cs-myhost-a3f2", `claude "build docker image"`)
    Manager->>Manager: persist session to sessions.json
    Manager->>Manager: start monitorOutput goroutine
    Manager-->>Router: Session{ID: "a3f2", ...}
    Router->>Backend: Send(groupID, "[myhost][a3f2] Started session...")
    Backend->>Group: message
    Group-->>User: "[myhost][a3f2] Started session for: build docker image\nTmux: cs-myhost-a3f2"
```

---

## 2. Input Required Flow

`claude-code` asks a question; the monitor detects the idle prompt and notifies the user.

```mermaid
sequenceDiagram
    participant Claude as claude-code (tmux)
    participant Log as myhost-a3f2.log
    participant Monitor as monitorOutput goroutine
    participant Manager as Session Manager
    participant Router as Router
    participant Backend as SignalCLIBackend
    participant Group as Signal Group
    actor User as User (Signal)

    Claude->>Log: "Do you want to overwrite? [y/N] "
    Monitor->>Log: reads new line
    Monitor->>Monitor: idleTimeout elapsed, line ends with "[y/N]"
    Monitor->>Manager: setState(waiting_input, prompt)
    Manager->>Manager: save to sessions.json
    Manager->>Router: onNeedsInput(sess, prompt)
    Router->>Backend: Send("[myhost][a3f2] Needs input:\n...")
    Backend->>Group: message
    Group-->>User: "[myhost][a3f2] Needs input:\nDo you want to overwrite? [y/N]\n\nReply with: send a3f2: <response>"

    User->>Group: "send a3f2: y"
    Group->>Backend: JSON-RPC receive notification
    Backend->>Router: handleMessage()
    Router->>Router: Parse() → CmdSend{SessionID:"a3f2", Text:"y"}
    Router->>Manager: SendInput("myhost-a3f2", "y")
    Manager->>Manager: setState(running)
    Manager->>Claude: tmux send-keys "y" Enter
    Router->>Backend: Send("[myhost][a3f2] Input sent.")
    Backend->>Group: message
    Group-->>User: "[myhost][a3f2] Input sent."
```

---

## 3. Status Check Flow

User sends `status a3f2` to see current output.

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group
    participant Backend as SignalCLIBackend
    participant Router as Router
    participant Manager as Session Manager
    participant Log as myhost-a3f2.log

    User->>Group: "status a3f2"
    Group->>Backend: JSON-RPC receive notification
    Backend->>Router: handleMessage()
    Router->>Router: Parse() → CmdStatus{SessionID:"a3f2"}
    Router->>Manager: GetSession("a3f2")
    Manager-->>Router: Session{FullID:"myhost-a3f2", State:running, ...}
    Router->>Manager: TailOutput("myhost-a3f2", 20)
    Manager->>Log: read last 20 lines
    Log-->>Manager: "... output lines ..."
    Manager-->>Router: output string
    Router->>Backend: Send("[myhost][a3f2] State: running\n---\n<output>")
    Backend->>Group: message
    Group-->>User: "[myhost][a3f2] State: running\n---\n<last 20 lines>"
```

---

## 4. Session Complete Flow

`claude-code` finishes; the monitor detects the tmux session is gone.

```mermaid
sequenceDiagram
    participant Claude as claude-code
    participant Tmux as tmux
    participant Monitor as monitorOutput goroutine
    participant Manager as Session Manager
    participant Router as Router
    participant Backend as SignalCLIBackend
    participant Group as Signal Group
    actor User as User (Signal)

    Claude->>Tmux: exits with code 0
    Tmux->>Tmux: session ends
    Monitor->>Tmux: SessionExists("cs-myhost-a3f2") → false
    Monitor->>Manager: setState(complete)
    Manager->>Manager: save to sessions.json
    Manager->>Router: onStateChange(sess, running → complete)
    Router->>Backend: Send("[myhost][a3f2] State: running → complete")
    Backend->>Group: message
    Group-->>User: "[myhost][a3f2] State: running → complete"
```

---

## 5. Startup / Resume Flow

Daemon restarts; existing sessions are re-monitored.

```mermaid
sequenceDiagram
    participant Main as main.go
    participant Manager as Session Manager
    participant Store as sessions.json
    participant Tmux as tmux
    participant Monitor as monitorOutput goroutine
    participant Backend as SignalCLIBackend

    Main->>Manager: NewManager(...)
    Manager->>Store: NewStore(path)
    Store-->>Manager: sessions [{id:"a3f2", state:running}, ...]
    Main->>Manager: ResumeMonitors(ctx)
    loop for each running/waiting_input session on this host
        Manager->>Tmux: SessionExists("cs-myhost-a3f2")?
        alt tmux session alive
            Manager->>Monitor: go monitorOutput(ctx, sess)
        else tmux session gone
            Manager->>Manager: setState(failed)
            Manager->>Store: save
        end
    end
    Main->>Backend: Subscribe(ctx, handleMessage)
    Note over Main: daemon is now running,\nresumes monitoring existing sessions
```
