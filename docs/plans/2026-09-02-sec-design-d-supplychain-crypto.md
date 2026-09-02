# Design D — Supply Chain, At-Rest Crypto & Infra Hardening

- **Date**: 2026-09-02
- **Version at planning**: v8.19.0
- **Status**: Planned (not started; design only — no code changes in this commit). SEC-010/011
  are *already shipped* in v8.14.1 (Go 1.26.6 + cilium/ebpf v0.22.0) — this plan records the
  *verification* (re-run the scans on v8.19.x) and closes the remaining at-rest/supply
  findings.
- **Finds this closes**:
  - **Core:** **SEC-010/011** (verify the v8.14.1 upgrades are intact on v8.19.x),
    **SEC-015** (`/api/agents/ca.pem` 404s with auto-gen TLS → workers can't pin),
    **SEC-018** (discussion throttle `sync.Map` unbounded, no TTL/eviction),
    **SEC-019** (self-update has no checksum/signature verify),
    **SEC-020** (no cosign/sigstore/GH-provenance attestation),
    **SEC-023** (Argon2id below OWASP; no key zeroing; AEAD-error→plaintext fallback),
    **SEC-024** (Helm daemon pod has **no** `securityContext`; `apiToken:""` default =
    unauthenticated in-cluster).
- **Design principles** (AGENT.md Security Rules + Pre-release security scan +
  No-local-environment-leaks + Secrets-Store):
  1. **The supply chain is an actor** (core §2 S) — a compromised upstream or a forged
     release must not yield RCE on `datawatch update`.
  2. **At-rest crypto must meet 2026 guidance** — the keyfile is the *only* thing
     protecting the memory DB, so its KDF params + failure modes carry the full blast radius.
  3. **Infra defaults must be safe** — Helm and container defaults must not ship an
     unauthenticated / root / privileged daemon.

---

## 1. Current state (facts)

| Finding | Where | Problem |
|---|---|---|
| SEC-010/011 | `go.mod`, Dockerfiles | *Shipped fix* (v8.14.1): Go 1.26.6 + `cilium/ebpf v0.22.0`. **Verify** intact on v8.19.x (re-run `govulncheck` + `trivy go.mod` + the 5 Dockerfiles) |
| SEC-015 | `internal/server/agent_api.go:297-303` `handleAgentCAPEM` errors when `cfg.Server.TLSCert == ""` | with **auto-generated** TLS (config `tls_cert` empty, but TLS *is* on — cert written to data dir by `internal/tlsutil/tls.go:48-62`), the handler 404s; workers fall back to `InsecureSkipVerify` (SEC-012) |
| SEC-018 | `internal/server/bl332_discussion_sync.go:159,163` `discussionThrottleMap` (`LoadOrStore(tok, …)`) | one bucket per *caller-supplied* Bearer string, **no TTL/eviction/maxlen** → rotating tokens grow the map until restart (DNS backend's analogous map *has* a cleanup goroutine) |
| SEC-019 | `cmd/datawatch/main.go:5709,5726,5987,6027` (`http.Get` release binary) — no verify; `.goreleaser.yaml:58-59` emits `checksums.txt` the client ignores | compromised GH release ⇒ RCE on every `datawatch update` |
| SEC-020 | `.goreleaser.yaml` (only a `checksum` block; no `sigstore`/`cosign`/GH-provenance) | nothing for a downstream verifier to check against |
| SEC-023 | `internal/config/encrypt.go:22-24` (`argonTime=1, argonMemory=64*1024, argonThreads=4`); `internal/memory/store.go:107-113` (`if len(s.encKey)!=32 || … return plaintext` on no-key **and** on AEAD error); no key zeroing | below OWASP 2026 (t≥3, m≥256MiB); **no key zeroing**; **AEAD-error ⇒ silent plaintext row** (an attacker who can force one decode failure on write can plant a plaintext row) |
| SEC-024 | `charts/datawatch/templates/deployment.yaml` (0 `securityContext`; observer-cluster *does* have one at `observer-cluster.yaml:115-119`); `values.yaml:72 apiToken:""` ("empty = no auth") | `helm install` with defaults = root / no `readOnlyRootFilesystem` / no `drop: [ALL]` / **unauthenticated** daemon in-cluster |

## 2. Design (best fix)

### D1. Verify the shipped govulncheck fixes are intact (SEC-010, SEC-011)

Not new work — a **re-verification gate**, because code has grown since v8.14.1.

- Re-run on v8.19.x: `govulncheck ./...` (expect 0 for the GO-2026-6218/6090/6089/6088/5972/
  5026 + GO-2026-6238 set) and `trivy fs go.mod` (expect no cilium/ebpf CVE). Add a
  **release-gate step** (`.github/workflows/security-scan.yaml`) that **fails** if
  `govulncheck` regresses — this is the guard that "we upgraded, don't let it slide."
- **Acceptance:** `govulncheck` exit 0 on the tip; the scan gate is in CI and green.

### D2. Distribute the auto-generated CA for pinning (SEC-015)

The on-disk auto-gen cert *is* the live server cert (fingerprints match — verified in the
core plan); the handler just doesn't know where to find it.

- `handleAgentCAPEM` (`agent_api.go:297-303`): when `cfg.Server.TLSCert == ""` and
  `TLSEnabled` and the cert is auto-generated, **fall back to the data-dir path**
  (`<data_dir>/tls/<hostname>.crt` — resolve from the same `tlsutil` location) and serve
  *that*. Only 404 if there is genuinely no cert AND no keyfile to derive one from.
- This lets worker agents pin the parent's real cert instead of falling back to
  `InsecureSkipVerify` (and removes half of SEC-012's exposure).
- **Acceptance:** with auto-gen TLS active, `GET /api/agents/ca.pem` returns the live
  server cert (not 404); a worker that pins it verifies successfully; the `ca.pem` on disk
  and the live TLS server share a fingerprint (re-run the core T9 check).

### D3. Bound the discussion throttle map (SEC-018)

- Add a **TTL/eviction** to `discussionThrottleMap` (a bounded LRU, `maxlen` + periodic
  sweep, or a `time.Time` last-seen + a cleanup goroutine on the same pattern as the DNS
  backend's `server.go:248-266`). Cap the map (e.g. 10k buckets) and drop oldest on
  overflow.
- **Acceptance:** 10k distinct Bearer tokens on the discussion-write endpoint no longer
  grow the map unboundedly; after the sweep window the oldest buckets are cleared; RSS
  delta stays small (re-run the core D1 live measurement).

### D4. Self-update & release supply-chain (SEC-019, SEC-020)

This is the **RCE-from-compromised-upstream** class (core P1). Two-sided: produce
attestation, consume verification.

- **Produce (SEC-020):** `.goreleaser.yaml` gains a `cosign`/`sigstore` block
  (or the GH-provenance equivalent) so every release binary carries a verifiable
  signature / provenance. Wire `dependency-review.yaml` and a release job that
  `cosign sign`/`gh attestation` the 5 binaries + `checksums.txt`. Add a `README` note so
  operators know the release is now signed.
- **Consume (SEC-019):** `datawatch update` /
  `/api/update` (`main.go:5709,5726,5987,6027`) must **verify before executing**:
  1. download the release binary **and** `checksums.txt`; verify the SHA-256 matches;
  2. verify the **signature** (cosign/sigstore bundle or a pinned GPG/minisign key) against
     the binary **before** `os.Exec`/extract-and-run.
  3. On verify failure: **refuse the update**, log it, and keep the running binary (no
     partial state). This is the gate that turns "compromised release page ⇒ RCE" into
     "compromised release page ⇒ refused update."
- **Operator opt-in (documented):** for an air-gapped / self-hosted GH runner with no
  cosign key, allow `update.verify="checksums-only"` (SHA-256 from `checksums.txt`) as the
  minimum; default is **signature-verify, refuse-on-fail**.
- **Acceptance (SEC-019):** a tampered binary (bit-flip in a test) is **refused** with a
  logged reason and the old binary untouched; a valid signed+checksummed release is accepted
  and runs. **Acceptance (SEC-020):** the GH release page shows a signature / provenance
  artifact; `cosign verify` (or the equivalent) passes locally against the downloaded
  binary.

### D5. At-rest crypto to 2026 guidance (SEC-023)

The memory DB + secrets keyfile is the *only* thing protecting stored memories.

- **KDF params (OWASP 2026):** `internal/config/encrypt.go:22-24` → raise
  `argonTime` 1→**3** (or 4), `argonMemory` 64MiB→**256MiB**, keep threads 4. This is a
  **parameter change** — existing `secrets.key`-derived keys must still decrypt, so:
  - **Migration, not a breaking flip:** store a **versioned envelope** (e.g. `V2:` prefix
    with the new params; `V1:` / current = the old path). On first decrypt of a
    `V1:`-era key, **re-derive + re-seal** under `V2` and persist, so a one-time
    transparent upgrade happens. Keep the old path as a *fallback reader* for
    pre-migration data (no data loss).
- **Key zeroing:** zero the derived key buffer on all paths (success *and* error) after
  each use — `subtle`/`slices.Clear` on the key slice; ensure the AEAD nonce + IV are also
  cleared before they leave scope.
- **AEAD-error ⇒ plaintext is a downgrade (the real SEC-023 bug):**
  `internal/memory/store.go:107-113` `if len(s.encKey)!=32 || <AEAD err> { return
  plaintext }`. **Change the error path to *refuse to write*** (return an encrypted
  error marker or fail the write loudly) rather than silently planting a plaintext row.
  No-key (first run) can be plaintext-by-design (bootstrap), but **an AEAD *error* on a
  keyed path must not downgrade**. Keep a `--allow-plaintext` dev escape hatch, off by
  default, warned at boot.
- **WAL `ENC:` prefix** (core S3): verify the per-line passthrough can't be *forced*
  plaintext by an attacker who can trigger one decode failure — the D5 error-path change is
  precisely what closes that.
- **Acceptance:** new writes use the `V2` params (re-derived key matches OWASP bounds); a
  forced AEAD error on a keyed write **fails loudly** (no plaintext row), logged + audited
  (C1); an old `V1` row is still readable and is transparently re-sealed to `V2` on next
  access; `secrets.key` perms stay `0600`.

### D6. Helm / container defaults (SEC-024)

- **Daemon pod `securityContext`** (match `observer-cluster.yaml:115-119`): `runAsNonRoot`,
  `allowPrivilegeEscalation:false`, `readOnlyRootFilesystem:true`, `capabilities.drop:[ALL]`,
  a `runAsUser` that owns the writable volume paths only. Keep the data dir on a writable
  `emptyDir`/PVC (the rest read-only).
- **`apiToken` default (SEC-001 in-cluster):** `values.yaml:72 apiToken:""` → **require a
  token** for a non-`hostNetwork`/cluster deployment. Options: (a) `apiToken:""` with a
  **hard error at helm render** unless `allow_empty_token: true` (ties into Design A's
  A1 opt-in), or (b) default to a **generated Secret** the operator must acknowledge.
  Preference: **(a)** — reuses the A1 escape hatch, so the in-cluster daemon inherits the
  same "loopback-or-token" posture as the daemon itself.
- **`observer.ebpf_enabled`** in the chart stays opt-in (it requires CAP_BPF); confirm the
  daemon pod does *not* request the eBPF caps (only the observer-cluster pod does).
- **Acceptance:** `helm install` with defaults renders a daemon pod with `runAsNonRoot +
  drop[ALL] + readOnlyRootFilesystem` and a non-empty `apiToken` (or a render-error
  demanding `allow_empty_token:true`); the observer-cluster pod's caps are unchanged; the
  daemon pod requests no BPF caps.

## 3. Alternatives considered

- **D4: only checksums (no signature):** a SHA-256 from a *compromised* `checksums.txt`
  verifies the *wrong* binary — the checksum is only as good as its source. Signature
  (cosign/sigstore / pinned key) is the control; checksums are the *minimum* for
  air-gapped. Both ship; signature is the default.
- **D4: pinning the GH API TLS + a known-good upstream hash only:** weaker (still
  trusts the upstream hash list) and doesn't survive a compromised-but-validly-signed
  release. Rejected.
- **D5: a new cipher or store (e.g. move to SQLCipher):** out of scope — the current
  AES-256-GCM + Argon2 store is sound; the gap is *params + failure mode*, not the
  algorithm. Re-keying under higher params is the minimal correct fix.
- **D5: hard-fail all writes if crypto is unavailable:** too disruptive (breaks first-run
  bootstrap). The **versioned-envelope migration + refuse-the-error-path** keeps
  bootstrap working while closing the downgrade.
- **D6: just document "set a token / run non-root":** the *default* is the problem — an
  operator who runs `helm install` with defaults gets the insecure pod. Changing the
  default (and erroring on the insecure one) is the fix; documentation is the
  complement, not the control.

## 4. Phases

1. **D1** govulncheck/trivy re-verify on v8.19.x + CI gate (SEC-010/011).
2. **D6** Helm `securityContext` + `apiToken` default (SEC-024) — independent, low-risk.
3. **D2** `ca.pem` auto-gen fallback (SEC-015); **D3** throttle-map TTL (SEC-018) — small,
   independent.
4. **D4** sigstore/cosign produce (SEC-020) + client verify/refuse (SEC-019) — the supply-
   chain pair; land together (verifying against a signature no one produces is a no-op).
5. **D5** Argon2 params + key zeroing + AEAD-error-refuse + envelope migration (SEC-023) —
   the crypto pair with the migration (do not ship the params change without the
   versioned-envelope reader).
6. **Docs + tests:** `docs/security-model.md` (supply chain + at-rest),
   `docs/operations.md` (update verify, helm defaults), config-reference
   (`update.verify`, `server.allow_empty_token` from A), `CHANGELOG`; unit tests:
   `govulncheck` gate, `ca.pem` fallback returns live cert, throttle eviction, update
   verify/refuse (tampered vs valid), `V2` seal/unwrap + refuse-on-AEAD-error, helm
   `securityContext` render.

## 5. Definition of done

- `govulncheck` exit 0 on the tip *and* the CI gate fails on regression (SEC-010/011 held).
- With auto-gen TLS, `GET /api/agents/ca.pem` returns the live server cert; a pinning
  worker verifies (SEC-015 closed).
- 10k rotating Bearer tokens do not grow the discussion throttle map past its cap; the
  sweep evicts old buckets (SEC-018 closed).
- A tampered release binary is **refused** (old binary intact); a signed+checksummed one
  is accepted (SEC-019); the release page carries a verifiable signature (SEC-020).
- New at-rest writes use OWASP-bounds Argon2id (key zeroed on all paths); a forced AEAD
  error **fails the write** (no plaintext row); a pre-migration row is read and re-sealed
  transparently (SEC-023).
- `helm install` with defaults renders a non-root, `drop[ALL]`, `readOnlyRootFilesystem`
  daemon pod with a required token (or a render-error) (SEC-024).

## 6. Sequencing & risk

- **D4 and D5 ship as pairs**: the signature *consume* (D4) is inert without the *produce*
  step, and the params change (D5) breaks readers without the migration. Do not split them.
- **D6 before D1? no** — D6 is Helm-only (render-time), D1 is the repo gate; they're
  independent. Land D6 early since it is a small chart change and removes the default-
  insecure in-cluster daemon.
- **Risk (downstream-review rule):** D5's KDF change is the highest-churn. Mitigate with
  the **versioned envelope** (`V1:` read-only fallback → `V2:` re-seal) and a **dry-run
  migration** (`datawatch security rekey --dry-run`) that reports how many rows would be
  re-sealed before committing — so the migration is reversible, observable, and tested
  against a real data dir (not just unit tests). D4's client-side refuse must be tested
  against a *real* (signed) release in `release-smoke` so a signature/format mismatch
  doesn't accidentally block every update (v8.8.9 lesson: a change that "passes" the
  test suite but breaks the actual operator path is a regression).
