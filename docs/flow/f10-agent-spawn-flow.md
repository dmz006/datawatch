# F10 Agent Spawn Flow — sprints 3-5

End-to-end sequence covering the F10 ephemeral-agent lifecycle as
shipped through Sprint 3 + 4 + Sprint 5 (4 of 6 stories): profile
selection → token mint → container spawn → worker self-registration
→ TLS-pinned bootstrap → git clone → session run → terminate +
revoke + sweep.

This diagram covers the parent-side and worker-side responsibilities
in a single picture; refer to [docs/agents.md](../agents.md) for the
prose surface.

```mermaid
sequenceDiagram
    participant Op as Operator (CLI / chat / MCP / UI)
    participant API as Parent REST<br/>/api/agents
    participant Mgr as Agent manager
    participant Broker as Token broker
    participant GH as GitHub (via gh CLI)
    participant Drv as Driver (docker / kubectl)
    participant Pod as Worker container/Pod
    participant Bootstrap as Parent bootstrap<br/>/api/agents/bootstrap

    Op->>API: POST /api/agents { project, cluster, task }
    API->>Mgr: Spawn(project, cluster, task)
    Mgr->>Mgr: validate Project + Cluster Profiles
    Mgr->>Mgr: mint Agent record + 32-byte hex bootstrap token
    Mgr->>Broker: MintForWorker(workerID, repo, ttl)
    Broker->>GH: gh auth token + gh api /repos/<repo>
    GH-->>Broker: PAT + access OK
    Broker->>Broker: persist TokenRecord + audit "mint"
    Broker-->>Mgr: { token, expiresAt }
    Mgr->>Drv: Spawn(Agent)
    Drv->>Pod: docker run / kubectl apply<br/>env: BOOTSTRAP_URL/TOKEN/AGENT_ID<br/>+ DEADLINE_SECONDS<br/>+ PARENT_CERT_FINGERPRINT
    Drv-->>Mgr: container/Pod created
    Mgr-->>API: Agent record (state=starting)
    API-->>Op: 201 Created { id, state: starting }

    Note over Pod: Worker boots in container
    Pod->>Pod: read DATAWATCH_BOOTSTRAP_* env
    Pod->>Bootstrap: POST /api/agents/bootstrap<br/>{ agent_id, token }<br/>(TLS pinned to PARENT_CERT_FINGERPRINT)
    Bootstrap->>Mgr: ConsumeBootstrap(token, agent_id)
    Mgr->>Mgr: burn bootstrap token, transition state=ready
    Mgr-->>Bootstrap: Agent record + Project Profile
    Bootstrap->>Mgr: GetProjectFor(id) + GetGitTokenFor(id)
    Bootstrap-->>Pod: BootstrapResponse { agent_id, project, cluster, task,<br/>git: { url, branch, token, provider }, env: {...} }

    Pod->>Pod: ApplyBootstrapEnv(resp)
    Pod->>Pod: CloneOnBootstrap → /workspace/<repo><br/>(token in URL, then scrubbed via remote set-url)
    Pod->>Pod: continue runStart with config.DefaultConfig()
    Pod->>Pod: start session in /workspace/<repo>

    Note over Pod: Worker runs claude-code / opencode session

    Op->>API: GET /api/output?id=<session>
    API->>API: forwardSessionToAgent (S3.6)
    API->>Pod: /api/proxy/agent/{id}/api/output?id=...
    Pod-->>API: session output
    API-->>Op: forwarded body

    Op->>API: DELETE /api/agents/{id}
    API->>Mgr: Terminate(id)
    Mgr->>Drv: Terminate (docker rm -f / kubectl delete pod)
    Drv-->>Mgr: container removed
    Mgr->>Broker: RevokeForWorker(workerID)
    Broker->>Broker: mark RevokedAt + audit "revoke"
    Mgr->>Mgr: state=stopped, StoppedAt=now
    Mgr-->>API: ok
    API-->>Op: 204 No Content

    loop Every 5 min (background)
        Note over Broker: SweepOrphans(agentMgr.ActiveIDs())
        Broker->>Broker: revoke + delete tokens whose worker is gone or expired
        Broker->>Broker: audit "sweep" with note: orphaned|expired
    end
```

## Notes

- **Bootstrap is the only unauthenticated parent endpoint.** The
  worker has no session token yet; the bootstrap-token + agent-ID
  pair is the auth signal, single-use, burned on first acceptance.
- **TLS pinning happens before bootstrap.** The parent injects its
  leaf-cert SHA-256 fingerprint into the spawn env so the worker's
  bootstrap HTTP client refuses any cert that doesn't match — no
  fallback to system trust store, no TOFU.
- **Git token never lands on disk.** It travels in the bootstrap
  response over the pinned-TLS connection, gets injected into the
  HTTPS clone URL with `x-access-token` username, then `git remote
  set-url origin <url-without-token>` strips it from `.git/config`.
- **Termination is idempotent.** If the parent missed the explicit
  Terminate (crash, missed signal), the periodic SweepOrphans
  catches it within 5 min using `agentMgr.ActiveIDs()` as the
  source of truth.
- **Failure mode visibility.** Mint failure, container failure,
  bootstrap rejection — all surface via `Agent.FailureReason`
  (visible in `/api/agents/{id}` JSON), via the broker's
  `audit.jsonl`, and via the daemon log.

## Pending steps in this flow

- **S5.4** — after the session ends, the worker would `gh pr
  create` against `git.url` so the changes land as a PR back on
  the project repo. Token gets revoked the moment the session
  terminates, so the PR open has to happen *before* Terminate;
  plan is to wire it via `Manager.SetOnSessionEnd`.
- **S5.2** — bootstrap token replaced with a PQC-secured envelope
  (Cloudflare CIRCL ML-KEM 768 + ML-DSA 65). Same flow shape; the
  token field becomes structured.

See [docs/agents.md](../agents.md) for the surface API, MCP, CLI,
and comm-channel commands that wrap each REST call above.
