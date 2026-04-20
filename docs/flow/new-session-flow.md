# New Session Sequence

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
    Manager->>Tmux: pipe-pane -o "cat >> ~/.datawatch/logs/myhost-a3f2.log"
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
<sub>🔍 <a href="https://mermaid.live/view#pako:eNqNVU1T2zAQ_SsaXwgzcZimp2qmHCC0TYevIXR6wByEtElUZMmVZAKTSX97V5YcTEwLvliS3369XT2vM24EZDRz8LsGzWEi2cKystAEH8a9seSHA0uYi-_BTC40U_sRUDHrJZcV0558taauAi4i4r4POz6dBpBrQDlXsg85slIs4NkVmhwxfg9a9LFXpvYxvbjqI86YZosImYFz0uj2qI89PT0LOKXKUYpIBlyxWkAeeHql6uuyfgwmHt_9rzPkrynExchu9MsZ_UqORstANSLLuLyofVV7sjDIopca-jbf6ruA_wl3M4OpNgdF8h1alR8eNi2gpMg0rChhQhBW-yVo9MF8IKKUQihYMQtFFi0bEzRFzikpMWskighQ8gEsIBsn4xPccvtUeRCJD8SiRewaJd9nF-f51eUx0cbLeYpEiQUO6KQo9Br0AyhTIXYtmGdnMQruyna1k_FoNCqyzWYTw8VAnYhzqbDzNAREotV8SLixGA_pe57BrVWcE0qWTGPtKfhgqrkppV6k_foaHv0_8khVRz8dh5fMOhjsk6Ief_g0JselOIfV1tEb7G92nKYRpThC2PMB94_Dt50MY8HTSUoxOem6azpnDiwW3ybqlsb66SS4_zgf4yAUel4rhSef8ax8Whrn8_Rpx22YfuQdVnkacJILkjvCXd61e9WqkhXkOMlAcoNxsBxyeEj-HIzCTKyY58sDZRbuoONohAf9JPDSUnLKas2X7-YJ471MEc_Q-Rep2kuOXrepOhSC_B6eHMn9Tm1kL-rD20H3yIneCtRz9o1EYJvZAwySPq2loG0zhsR55gH3ttYaBzQc4Rxu-i2OwkFRM16KSCQlNKhng5JB8WYYJjhzPoV3A6ZUu94x6Yx7m-x00kk28JU-0D7Hu0PeXuAZ8jtIoxt6cxOtbm-C2W28Aig-7YzNsUoc0v_zXejYu90c9nfkoBG6rWaFTqNU4b_Q-a64JSXtCWJXNBEWdJe-r4BGS7JhVoItmRT4C15vcFtXOP5wIkL7MjpnysEwwyrN7EnzjHpbQwtKv-qE2vwFRbynzg">View this diagram fullscreen (zoom &amp; pan)</a></sub>

