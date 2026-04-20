# System Data Flow

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

    subgraph "datawatch daemon"
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
<sub>🔍 <a href="https://mermaid.live/view#pako:eNqVVVtP2zAU_itWnkAQCt3DUDVN4lINGJeqicQD3YPruKmHY0e2A1SE_75z4qRJWjZGH6rY_r5z-XzO8WvAdMKDUZAami9JfD5TBH62mPuNWRCJVFFJLtXCUOtMwVxh-CzwOPxNllrxhzXwRs-F5OQkz2fBrxb1w-gib1HVcjZTO-PhmHDFzCp3PNldU7hKZmorFluRQyYF2bmiTxTPcqMZt3a3G1J0dn350IWDp99Wq2nOSAb5fuAmoY4-U8eWJKE806pr-tSIJG3TBUenlD2iHUjmKrq7DaeTM6KfuCHWJUIN4F8XbrcnxhR2uAEj_gOpTGcZVQnJqbHA3SOJsDnGwE2fe0MVTStyBGkLrZodtCLFgrMVA_33CKNSziE22-dHThuM33q2PUBhkLuQ1BHMYJ9kENVLCMo6znrXUgWglQAbR2Aj8593hcsLR1INl-qE4mitNk_ol8XwXf7w__jzr-yoz7-I40nEzVOlAS6IX5HR8eHxIXKn4ygGAe75PNJwNxviXxRzIK4PcY2kudE0YVDixGkC0hEoG66c_agmXVa8kEbMbqXEqBCzYbZaautCFKK6aEmLhIfYdv244mEPj4n_E3-N9qVO7aDj4QA2-qjhBgrt9lDvpzW5P2kUIDsxFdJCPfFelwEEQ6j6H_pCP0Ph9nwDAL3HdC5B501Ex683EYbfyxTnAslATijpsh4Taj1AyDcE9WZGWXW7x-CXh3R7jzRtWdbd68H-u_J6qaD9hEpvGr91X6q2WStc5Khxg59CykEE4V8qKNsBilOum3CLAjhieC5Xf_VuEdLG2KZT20SQb9ut7bKqPsWfw7oCoWLQXPjIV7aEEvw0Y-gZ8VEFzkXOw5wqkOS6thUPt05qTjMY_LmWsiU1Pd89eo-lFUjs-NmSqhTHgFa3nCe2EnpD457Jz_A6WtxqJxarPtXvtXTYOm1Ggx9UZWcEeZvturornCiqHjWVn3bY5IVdllXnfIio9UHsBqR-K-C6tjw1z0jzeqyTD_aDjJuMigQe-9c3WBY5vHN8nKCKwWhBpeX7AS2cjlaKBSN46nkDOhcU5kJWo97-AP-3sI8">View this diagram fullscreen (zoom &amp; pan)</a></sub>

