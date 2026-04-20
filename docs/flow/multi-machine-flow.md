# Multi-Machine Sequence

Two hosts sharing one Signal group.

```mermaid
sequenceDiagram
    actor User as User (Signal)
    participant Group as Signal Group

    box hal9000 (203.0.113.10 / 100.100.1.10)
        participant CLI_H as signal-cli
        participant Agent_H as datawatch
        participant Store_H as sessions.json (hal9000)
    end

    box nas (203.0.113.20 / 100.100.1.20)
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
<sub>🔍 <a href="https://mermaid.live/view#pako:eNqlVl1v2kAQ_CsrPxEJu4Y8xaqQ8lGlkSKrEupTHFXX8wLX2nfUtw6Novz37vkMxWACTZEwMp4b78zOrf0SSJNjkAQWf9WoJd4oMa9EmWngj5BkKvhqsQJh_e9gquZaFGcesBQVKamWQhPcVqZeOpxH-PNMe-B38xsWoriI4xgG4_g8iqPR6DwaxfABRjGfuC8fW95d7uv7u2-fHbdtuENZqH7g5Rw1eWguSKwEyUU_csrSsCVFa5XRNvphjYZBW2dbCup8W4Rm_JaAcVfA-C0B6ekC0pMFpD0CuMb94lNDCOYJfT-H7eoErgwt-FZY8nqwxDcAoXOQRmuUBGSAFghWlAhz31HH1vocTiZNmxPIgofWt8e_hXs-zCO4V5ZQKz0HLrDhgcs7uDaaKlNkwTZp2iFlLe8nbGhC5nOSj9UYRYeXHajCL_GLHHa7co2rBKpag9KEvKWIGwSElmz3NpNJE-4ESm6imKO_1vzH11qfmQklqicEbUjNlGzodluxAX8RlUXI6vHoYgzXZZ7iahfbxj-BqXjCQRugF5UnWSDOZ-MsGLK617Pj3X5w8EdOY2PJOoowM9Ub8vcN4CQ2GdtzId0IS4-70AEfcSHduJD2uSAv8lGfC_vxfHDQ_3GgL6BdW4-Gs1tDJ5jdjZ_4Mb7iCWLd5pYLlD83o9no4rk_zhx5qu0aGPqMvD_HB6N7i3T1PF2Yiu5uBlnQvd_ZupszU7vJdno2CZteuIGRZToMQz5-NDUta5r06finOB5M4GliOMxekBtl3Mzdtm3o3LOHHx8cWcvzuSzdnLaq4Kuua6cn6m0zgmFQYlUKlfNrwcsrn9ZLHn34KVe8W4JkJgqLw0DUZKbPWgYJVTWuQe3rQ4t6_QM0aLMb">View this diagram fullscreen (zoom &amp; pan)</a></sub>

