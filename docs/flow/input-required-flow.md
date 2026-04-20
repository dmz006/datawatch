# Input Required Sequence

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
<sub>🔍 <a href="https://mermaid.live/view#pako:eNqtVu9P40YQ_VdG_hSkOOW4T7V6nHRwbak4QKTVfcCoWuyJs8Xedb1rIEL87327601yMUH9hZBw8Js3b-fNzOY5KXTJSZYY_rNnVfCpFFUnmlwRflrRWVnIVihLJ7XoSyZhqPBPqQukttMFGzOGn-vKYZvVUhubiveLo1mtqzHui1bS6s5jw-Nlb9veUqU73Vup-JUYoUTFPmaO5FKr-K8xdg5Gr9oEpJn9YbQa437u7xzqK9_NdXHP_h9j1DUkhcThaYz41Mmy8gnnslKiPjk_-yRAqMox9iecsN1Aw-cAE4Urym8mJPN_JwF1kA_qL7Rl0g94E6yZouTZN-Y8dtKyIeFcalpLVpNdMkF3I8EUaEJwenzsw_PkR92rkt4TP0mD8leAG0sLWbOZ0SXSeVYSdf2RblbfXdwiZlfQYGpGra5rQzDeExDj5YqODg-boWMGYMzesShJ8SPVa983iDWpaFuU053mrl8sYEKuOjawTJbIYWUTfXlF0Gw2o3eHaIZCq9LADWPoUdolKe0T69B8gO1Nv8mCQyHxXmCx5OKebNM_0VKYdGhAKky6NRWU90fvvj9CPeXD_iPXAh64ohA72V5xnsTyR47B5lZYWKyoERYSyhFpGJUMRbBzKyxPnLIpeB6FdI7_LhVqkCcHKOw5El8F2g__qTmGrMjvJzKjuXjgyTC-z8bpyEYKprTJn8GTl4NdMkxphpnToiyAvGAuzZmLneTJVpEd09vi4XcykIMyEpuhz3Cerc1Q1JKVNbtSwkbISKstGaG0wZiBP-AQEFYFKoEsk8rN_tmp03kTlN_eOOm35NnIVySDI__YAsTg95rbejWcyPFm9AN6t8VC5OP10YMiSPObKKMG6nE6Ktl1Zxd7yb9NAXN7KXtbsa9sXFkOv2HPky01q9gogXxTnV_mlxfp9dUJlkPBUIFJtXIhC2Fl3ONr2dGCpVAY0i9B_WS37hF1JTqDt3F6TprSOfE89OTZKfoxNs-v_ITiJxD5skO2niYXu7f1VusSb_pl_xx2vVJw9pWYt2ZnHTWekrDjs7CLXNHTe14ZSu1oGTml9Fmtr7Z9kzYkNxP0WXz-l_3ti-ZU2dn_3YjfUP_dWxM3A-aqx8XJT1z0rs2m8Spt3JeJcEck0wRXQCNkiW9Pzy_42LclnPhcuj2bZAtRG54mord6vlJFktmu5wgavmUNqJe_AFwQP1c">View this diagram fullscreen (zoom &amp; pan)</a></sub>

