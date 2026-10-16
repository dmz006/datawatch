# datawatch — threat model for OSS Scanner (DRAFT)

Status: DRAFT for operator review. Items marked **[OPERATOR DECIDES]** are not settled
and must be confirmed by the maintainer before this file is used for enrollment.

## What datawatch is

A Go daemon that lets a person run and supervise LLM coding-agent sessions (claude-code,
opencode, aider, pi and others) on a host, and control them from messaging channels
(Signal, Telegram, Matrix, and others listed in `docs/messaging-backends.md`) or from
a web UI / REST API / MCP. It can also run autonomous multi-step PRD execution that drives
those agents against a project directory.

## Trust boundaries

| Input | Trust level | Notes |
|---|---|---|
| Messages from the configured messaging channel (Signal group, Telegram chat, Matrix room) | **Adversarial if the channel is reachable by others** | Only the configured group/chat/room should be processed. **[OPERATOR DECIDES]** whether any unauthenticated sender path exists and must be fuzzed. |
| HTTP / WebSocket / REST / MCP API (`0.0.0.0` bind by default) | **Adversarial** | Bearer token is the recommended control on both HTTP and MCP; TLS is available via native ACME (Let's Encrypt) for publicly reachable hosts. The operator's stated posture is below. |

## Operator-stated security posture

Stated by the maintainer (2026-10-16). These are the operator's positions, to be confirmed at sign-off:

- **Bearer token is always recommended** on the HTTP API and the MCP server. The operator
  does not run datawatch without a token unless it is reachable only over Tailscale.
- **Running without a token is not itself a finding.** It is a deployment choice, not a
  vulnerability, and reports that only say "no token is set" are out of scope.
- **Access without a token where one is required is a finding.** If an endpoint, MCP tool,
  or control path that should require a token accepts a request without one, that is in
  scope and rated like any other authentication bypass.
- **Misuse of a function that the design allows is the real risk.** A report should show a
  function being used to do something it was not meant to do, not merely that it was
  reachable.
- **Encryption at rest** is a supported security feature: `--secure` mode encrypts sensitive
  data with XChaCha20-Poly1305 and Argon2id key derivation (see `docs/encryption.md`).
  Weaknesses in that encryption, its key handling, or its plaintext-to-encrypted migration
  are in scope.
- **Full ACME support** (`internal/server/acme.go`) issues real certificates for public
  deployments. Certificate issuance, renewal, and domain-validation handling are in scope.
| LLM backend output (agent stdout, tool calls, file contents the agent reads) | **Adversarial** (prompt injection) | Model output can request commands and file writes. Tests should cover parsing and routing of agent output, not the model's behaviour. |
| Project files and task text in a repo the daemon operates on | **Semi-trusted** | Can contain injected instructions. |
| Config files (`config.yaml`), local secrets stores (KeePass, 1Password, env, file provider) | **Trusted** (operator-controlled) | Attacker who can write these already owns the host. Out of scope unless the daemon grants them more than it should. |
| Outbound calls to LLM providers, npm/Go module registries, Signal/Telegram/Matrix servers | **Trusted transport, untrusted content** | Responses should be parsed defensively. |
| **Federation peers and observer peers** (other datawatch instances that register, send envelopes, or receive session calls) | **Authenticated by handshake; authority set per peer** | Each peer completes a handshake, and each side defines a privilege mapping: what the other peer may do and see. Findings are in scope where a peer exceeds its mapping, or where the handshake or mapping can be bypassed or spoofed. The peer's own host is outside this boundary. |
| **Local CLI and `~/.datawatch/` access** | **Trusted** | Anyone who can run the CLI or read `~/.datawatch/` already has host access, so plaintext data they can read is not a finding. The exception is encrypted data: see the `--secure` item below. |
| **Compute nodes** (remote model hosts such as Ollama or other LLM backends used by this instance) | **Untrusted responses** | Model-pull and model-remove actions are control operations on a remote host. |
| **Worker containers** spawned by datawatch (docker, podman, kubectl, and cf drivers; agent images) | **Semi-trusted, nested** | A worker runs agent code inside a container that the parent datawatch launched. The parent is the control plane; a compromised worker must not reach the parent's secrets, config, or other workers. Tailscale sidecars are optional. |
| **Nested sessions** (a session that spawns child sessions, including orchestrator graphs and PRD child tasks) | **Semi-trusted** | Child sessions inherit or receive a subset of parent authority. The inheritance and capability rules are in scope. |
| **Control paths into a running session** (send_input, reply, saved commands, and channel commands) | **Adversarial if the path is reachable by others** | The same channel and token rules apply as for the HTTP API. |

## Attack surface to test (in scope)

1. **Messaging command parsing** — the router that turns channel text into session
   commands. Malformed, oversized, unicode-tricky, or nested input must not crash the
   daemon, bypass the configured-channel check, or execute unintended commands.
2. **HTTP/WebSocket/REST/MCP request handling** — authentication checks (bearer token on
   HTTP and MCP, and the fallback from `mcp.token` to `server.token`), path traversal on
   file endpoints, request size limits, WebSocket message framing, and CSRF/Origin handling
   on the PWA. ACME certificate handling (`/api/acme/*`) is in scope.
3. **Federation and observer peer boundaries** — the handshake, the per-peer privilege
   mapping, envelope ingestion, and session or tool calls accepted from a peer. A peer must
   not gain authority beyond its mapping (`internal/federation/capabilities.go`), and a
   peer's data must not be trusted as local control input.
4. **Worker-container isolation** — how the parent builds a worker's container (image,
    env vars, mounts, network, privileges) and what a worker can reach. Escapes from the
    worker into the parent host or its secrets are in scope. Kubernetes and cloud-driver
    paths are in scope to the extent they run datawatch-generated commands.
5. **Nested session and orchestrator authority** — whether a child session or graph node
    can perform actions its parent or the human operator did not authorise, including
    privilege carried through PRD child tasks and MCP tool calls.
6. **Compute-node and model-management operations** — pulling and removing models on a
    remote host, and what the remote host's responses can make datawatch do.
7. **Encryption at rest (`--secure` mode)** — key derivation, cipher use, nonce handling,
   the plaintext-to-encrypted migration on first `--secure` start, and whether secrets
   reach disk or logs unencrypted.
8. **Session and process management** — how user-supplied task text, project paths, and
   backend names are turned into subprocess arguments (command injection, argument
   injection, `--add-dir` scope escape).
9. **Autonomous PRD execution** — story/task definitions from PRD files or API requests;
   the path from a task definition to file writes and shell commands.
10. **Secrets handling** — the secrets vault and provider code (`internal/secrets`):
   secrets must not be written to session logs (`~/.datawatch/logs/`), echoed to
   channels, or returned by list endpoints without authorisation.
11. **Agent-output parsing** — code that interprets LLM/agent output as structured data
   (JSON tool calls, markdown rendering for messaging channels).
12. **Dependency-level issues reachable from datawatch code** — `govulncheck` reachability,
   not just presence in `go.sum`.

## Out of scope

- The security of the LLM models themselves, or whether an agent behaves well when
  given adversarial instructions (that is an agent-policy concern, not a datawatch bug),
  unless datawatch itself fails to enforce a documented boundary.
- Host hardening, tmux/claude/opencode binary vulnerabilities, Tailscale, and signal-cli
  internals.
- Findings that require the attacker to already have write access to `~/.datawatch/`
  or the config file.
- Plaintext data readable by anyone with local CLI or `~/.datawatch/` access. Encrypted
  data is different: a bypass that reads `--secure` data without the key is in scope.
- Deliberate test/dev tooling: `Dockerfile.dev`, E2E harnesses, and the `.shrink-test/`,
  `testdata/`, and `.zap/` directories.
- Denial of service via unbounded LLM spend or long-running agent sessions, unless a
  single unauthenticated request can trigger it.

## Severity rubric (draft)

- **Critical** — unauthenticated remote code execution or arbitrary file write on the host
  through any in-scope input, or secret disclosure to an unauthorised channel sender.
- **High** — authentication or channel-allowlist bypass; command or argument injection
  reachable from channel or API input; path traversal outside the configured project dir.
- **Medium** — information disclosure of non-secret host or session data; crashes reachable
  from remote input without auth; missing size limits on remote input.
- **Low** — hardening gaps with no demonstrated attacker path; issues requiring a
  pre-authenticated, trusted operator.

Token-less operation is not rated on its own (see the posture above). Confirm these
thresholds before sign-off.

## Reporting expectations (draft)

- Reproduction steps against a build from `main`, with the config used (secrets redacted).
- Which trust boundary in the table above is crossed.
- A proposed patch is welcome but not required; patches must keep the
  `SignalBackend` and `LLMBackend` interfaces stable (see `AGENT.md`).
- Do not include real secrets, tokens, or Signal/Telegram account identifiers in reports.

**[OPERATOR DECIDES]** Whether reports go only to the primary contact or also to the
GitHub advisory process named in `SECURITY.md`.

## Deduplication guidance (draft)

- `docs/security-review.md` already triages gosec and govulncheck results. Do not
  re-report findings listed there as accepted; cite the existing triage entry.
- Accepted gosec false positives (G115 integer conversions, G118 goroutine context in
  the opencode backend) are documented there with reasons.

## Open questions for the operator

1. ~~Should the scanner treat a daemon running with no bearer token on `0.0.0.0` as
   in scope?~~ **Answered 2026-10-16:** running without a token is not a finding. Access
   that a token should have blocked is a finding, rated normally as an authentication
   bypass.
2. Is there any messaging path that accepts messages from senders outside the
   configured group/chat/room? If yes, that path moves up in priority.
3. Which messaging backends are actually enabled in production (affects which parsers
   are in scope)?
4. Should the autonomous PRD runner be in scope as an attack surface, or treated as
   trusted operator tooling?
5. Confirm the severity thresholds and reporting destination (see above).
6. ~~**Federation auth:** how does a peer prove its identity?~~ **Answered 2026-10-16:**
   a handshake, with a privilege mapping defined by each peer for the other. The exact
   handshake mechanism still needs to be read from the code and documented before peer
   findings can be rated.
7. **Worker containers:** are workers ever launched with privileged mode, host networking,
   or host mounts? Which driver (docker, podman, kubectl, cf) is the production default?
8. **Severity for nested and peer findings:** should a peer or worker reaching the parent's
   control plane be rated like the same bug reached from the HTTP API, or lower because a
   peer already holds some authority?
