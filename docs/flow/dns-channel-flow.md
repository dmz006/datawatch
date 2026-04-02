# DNS Channel Flow

Command/response cycle for the DNS covert channel backend.

```mermaid
sequenceDiagram
    participant Client as DNS Client
    participant Resolver as DNS Resolver
    participant Server as datawatch DNS Server
    participant Router as Router
    participant Manager as Session Manager

    Note over Client: Encode command as DNS query
    Client->>Client: nonce = random 8 hex chars
    Client->>Client: payload = base64url(command)
    Client->>Client: hmac = HMAC-SHA256(nonce+payload, secret)[:8]
    Client->>Resolver: TXT query: <nonce>.<hmac>.<payload>.cmd.domain.

    Resolver->>Server: Forward TXT query
    Server->>Server: Decode query, verify HMAC
    Server->>Server: Check nonce (replay protection)
    Server->>Router: dispatch command (e.g. "list --active")
    Router->>Manager: Execute command
    Manager-->>Router: Response text
    Router-->>Server: Response string

    Server->>Server: EncodeResponse (fragment into TXT records)
    Server-->>Resolver: TXT reply: ["0/3:chunk0", "1/3:chunk1", "2/3:chunk2"]
    Resolver-->>Client: TXT records
    Client->>Client: DecodeResponse (reassemble + base64url decode)
    Client->>Client: Display plaintext response
```

**Error cases:**
- Invalid HMAC → DNS REFUSED response
- Replayed nonce → DNS REFUSED response
- Command timeout (>10s) → DNS SERVFAIL response

