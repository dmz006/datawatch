# Security review — pre-v6.0

Last run: 2026-04-27 (v5.26.3 patch).
Tools: `gosec` (severity HIGH, confidence MEDIUM+) and `golang.org/x/vuln/cmd/govulncheck`.
Scope: `./...` from the repo root.

This document is the answer to the AGENT.md release-discipline rule that requires a pre-release security pass. It triages every gosec finding (real → fix; false-positive → annotate) and tracks the dependency vulnerability state from govulncheck.

## govulncheck

```
govulncheck ./...     →   No vulnerabilities found.
```

Two transitive flags closed in this pass by bumping `go.mod`:

| ID | Module | Pre-bump | Post-bump | Reachable from datawatch? |
|----|--------|----------|-----------|---------------------------|
| GO-2026-4503 | filippo.io/edwards25519 | v1.1.0 | v1.2.0 | No (govulncheck symbol-resolution: not called) |
| GO-2026-4559 | golang.org/x/net | v0.50.0 | v0.53.0 | No (HTTP/2 server frame handler — datawatch is the client side) |

Re-run cadence: every release until v6.0 (patches included). The full
go.sum diff lives in the v5.26.3 commit; routine `go get -u` +
`go mod tidy` is preferred over selective bumps so we don't hide a
dep that picks up a new vuln next month.

## gosec — HIGH severity, MEDIUM+ confidence

55 findings across 4 rule families. Triage below — every entry is either fixed in v5.26.3, suppressed with a `//#nosec` annotation explaining the reason, or accepted as a documented false-positive.

### G115 — integer-overflow conversions (19 findings)

All conversions are between Go integer types where the arithmetic context guarantees no overflow (timeouts, port numbers, list lengths, byte counts that come from controlled sources). gosec flags any `int → int64` / `int → uint32` regardless of context.

**Decision:** accept. Reviewed line-by-line; none of the call sites accept attacker-controlled magnitudes that approach the conversion boundary. Adding 19 `//#nosec` comments would be churn for no signal. Re-evaluate if any of these expand to take user-supplied size hints from REST.

### G118 — goroutine context misuse (2 findings)

Both inside `internal/llm/backends/opencode/acpbackend.go` (`Launch` and `LaunchResume`). The goroutines watch a long-lived `opencode serve` subprocess: they wait for the server to come up, create a session, stream events, and tick a healthcheck. They intentionally outlive the request that started them.

The goroutines already respect `ctx.Done()` for cancellation in addition to a private `bgCtx`, so cancellation propagates from the parent.

**Decision:** false positive. The goroutine *is* cancellable from the parent context; gosec just sees the `context.Background()` call and assumes the context is detached, which it isn't in practice (we select on both).

### G122 — race-prone path in WalkDir callback (1 finding)

`internal/autonomous/security.go:74` — a path is computed inside the callback and used to read a file. There's a TOCTOU window (file replaced between Walk visiting it and us reading), but the directory we're walking is the daemon's own `~/.datawatch/autonomous/` tree, not operator-supplied paths. The attacker model would need write access to the daemon's data directory, at which point reading replaced files is the least of the concerns.

**Decision:** accept. Treat as low-risk false-positive given the trust boundary.

### G123 — VerifyPeerCertificate without VerifyConnection (1 finding)

`internal/agents/tls.go:96`. Paired with the G402 below — this is the canonical "self-signed-with-pinning" pattern: `InsecureSkipVerify=true` plus a `VerifyPeerCertificate` callback that compares the cert SHA-256 to a pinned fingerprint. gosec doesn't recognize this as safe because it expects `VerifyConnection` (added in Go 1.15) to also be set. The pinning logic is enforced regardless.

**Decision:** false positive. The pinning is correct and tested in `TestPinnedTLSConfig_RoundTrip`.

### G402 — TLS InsecureSkipVerify=true (7 findings)

| File:Line | Context | Verdict |
|-----------|---------|---------|
| `internal/agents/tls.go:95` | parent↔worker pinned mTLS — paired with VerifyPeerCertificate fingerprint check | False positive |
| `internal/agents/client.go:124` | parent↔worker mTLS client — same pinning pattern | False positive |
| `internal/observerpeer/client.go:96` | observer-peer push — pinning planned in S15; currently opt-in via config (default off) | Accept (documented) |
| `internal/observer/cluster_k8s.go:139,142` | k8s API server cert verification skipped for cluster-internal traffic via in-cluster ServiceAccount | Accept (k8s convention) |
| `cmd/datawatch/main.go:6250,6392` | self-update binary fetch + datawatch-app heartbeat probe — both gated behind operator opt-in | Accept (documented) |

All five "accept" entries get a `//#nosec G402 -- <reason>` annotation in the v5.26.3 patch. The two pinning-pattern entries get `//#nosec G402 G123 -- pinned via VerifyPeerCertificate; see TestPinnedTLSConfig_RoundTrip`.

### G702 — command-injection via taint analysis (7 findings)

Every G702 finding is one of two shapes:

1. **`syscall.Exec(selfPath, os.Args, os.Environ())`** — daemon hot-restart. `selfPath` resolves through `os.Executable()` + `filepath.EvalSymlinks`; `os.Args` is the literal argv vector that started this process. Both come from the kernel, not user input. Crucially, `syscall.Exec` takes an argv list, not a shell string — there is no shell parsing.
2. **`exec.Command("git", "-C", dir, …)`** — argv-list invocation of git. `dir` is a project-directory path that came from the operator's config or from the `datawatch session start --project-dir` flag. `exec.Command` again takes an argv list, not a shell string.

In both shapes the "tainted" data flows into the *argv* of an exec, not into a shell. There is no way to inject a second command via argv parameters; that requires `sh -c` or string concatenation, neither of which we use.

**Decision:** false positive across the board. Each site gets `//#nosec G702 -- argv-list invocation, not shell` in v5.26.3.

### G703 / G704 — taint-flow noise (12 + 6 findings)

These are downstream of G702 — gosec's taint propagation considers the same "tainted" inputs after they've flowed through other functions. Same root cause, same disposition.

**Decision:** accept as part of the G702 triage.

## Fixes shipped in v5.26.3

- govulncheck — bumped `golang.org/x/net`, `filippo.io/edwards25519`, and the routine `go mod tidy` cascade. Both flagged advisories cleared.
- This document — written so the next pre-release pass can diff against it and only review *new* findings.

## Procedural changes

- Run `gosec -severity high -confidence medium ./...` and `govulncheck ./...` before every patch. The audit doc is updated only when triage changes (new findings or new mitigations).
- New `//#nosec` annotations must include the rule ID *and* a one-line reason. PRs adding bare `//#nosec` get rejected by review.
- New `InsecureSkipVerify=true` sites must be paired with pinning (`VerifyPeerCertificate` + a fingerprint check) or get an entry in the G402 table above with an "accept (documented)" verdict and a config gate.
