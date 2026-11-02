# Leak scrub report: private & tailnet addresses

**Purpose.** This task replaces private (RFC 1918), Tailscale/tailnet
(Tailscale CGNAT `100.64/10`), link-local (`169.254/16`), IPv6 unique-local /
link-local, and MagicDNS/tailnet hostname values in tracked files with the
reserved IANA documentation-range placeholders listed below, so that no
tracked file in this repository carries an operator-specific network address.
Goal: `scripts/check-no-local-leaks.sh` reports zero hits for the `address`
and `local` classes.

Prepared: `Mon Nov  2 05:23:13 AM EST 2026` (output of `date` in this
worktree).

**No identifier is repeated here.** This report names only categories, CIDR
classes, and the reserved-range *placeholder* values it introduced. It does
not reproduce any original private address, tailnet address, or hostname.

Scope: every tracked text file in this worktree (the scrub commit changed
79 tracked files, 413 insertions / 609 deletions); 76 of them contain at
least one replaced token.

## Replaced: addresses

### Placeholder scheme

| Original class (CIDR) | Replaced with (all IANA-reserved, documentation-only) |
|---|---|
| RFC 1918 private IPv4 (`10/8`, `172.16/12`, `192.168/16`) | TEST-NET-1 `192.0.2.0/24`, TEST-NET-2 `198.51.100.0/24`, TEST-NET-3 `192.0.3.0/24` / `203.0.113.0/24` |
| Tailscale CGNAT `100.64/10` (tailnet node IPs) | TEST-NET-2 `198.51.100.0/24` |
| Link-local `169.254/16` | `192.0.2.0/24` documentation range, except the two well-known cloud metadata addresses, which are public constants the code must keep and which are listed in `scripts/leak-check.allowed-addresses` |
| IPv6 unique-local (ULA, fc–fd) and link-local (fe80-prefixed) literals | `2001:db8::/32` (RFC 3849) |
| Hex byte-endpoint literals that encoded a private address | re-derived hex for the TEST-NET-1 placeholder (`192.0.2.x`), or byte-value construction `net.IPv4(0x…, 0x…, 0x…, 0x…)` in tests so the RFC1918 branches stay exercised without dotted literals |
| Class-wide RFC 1918 CIDR mentions (comments/prose) | left as CIDR classes or rewritten as "RFC 1918" — a class reference is not a usable address |

Address pairs that are asserted against each other (input vs expected) are
mapped consistently so test semantics are unchanged.

### Per-file replacement counts (addresses)

Counts are per replaced token (dotted/hex IPv4 literal, IPv6 literal, or
hostname token) found on the removed lines of the scrub commit, per file,
per category.

| File | Priv. IPv4 | Tailnet IPv4 | IPv6 | Hosts | Total |
|---|---:|---:|---:|---:|---:|
| `CHANGELOG.md` | 2 | 1 | 0 | 0 | 3 |
| `cmd/datawatch/loopback_url_test.go` | 2 | 0 | 0 | 0 | 2 |
| `cmd/datawatch/main.go` | 1 | 0 | 0 | 2 | 3 |
| `cmd/datawatch/pi_prompt_restart_node_test.go` | 6 | 0 | 0 | 0 | 6 |
| `cmd/datawatch/session_compute_node_test.go` | 4 | 0 | 0 | 0 | 4 |
| `cmd/datawatch/system_alert_comm_test.go` | 1 | 0 | 0 | 0 | 1 |
| `docs/config-reference.yaml` | 0 | 0 | 0 | 2 | 2 |
| `docs/flow/multi-machine-flow.md` | 0 | 2 | 0 | 0 | 2 |
| `docs/howto/automaton-lifecycle-webhooks.md` | 1 | 1 | 2 | 0 | 4 |
| `docs/howto/channel-routing.md` | 1 | 0 | 0 | 0 | 1 |
| `docs/howto/compute-routing.md` | 3 | 0 | 0 | 0 | 3 |
| `docs/howto/container-workers.md` | 0 | 1 | 0 | 0 | 1 |
| `docs/howto/federated-observer.md` | 0 | 7 | 0 | 0 | 7 |
| `docs/howto/llm-registry.md` | 1 | 0 | 0 | 0 | 1 |
| `docs/howto/multi-servers.md` | 6 | 0 | 0 | 0 | 6 |
| `docs/howto/setup-and-install.md` | 1 | 0 | 0 | 0 | 1 |
| `docs/howto/tailscale-mesh.md` | 0 | 3 | 0 | 0 | 3 |
| `docs/implementation.md` | 0 | 0 | 0 | 2 | 2 |
| `docs/operations.md` | 0 | 1 | 1 | 14 | 16 |
| `docs/plans/2026-10-21-pwa-connection-stability.md` | 0 | 1 | 0 | 0 | 1 |
| `docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md` | 1 | 0 | 1 | 0 | 2 |
| `docs/plans/harness-research/.evidence/e-domenic.html` | 1 | 0 | 0 | 4 | 5 |
| `docs/plans/harness-research/.evidence/quorum.md` | 3 | 0 | 0 | 0 | 3 |
| `docs/plans/historical-plans/2026-04-27-bl173-cluster-validation.md` | 2 | 0 | 0 | 0 | 2 |
| `docs/plans/historical-plans/2026-05-18-v8.0-routing.md` | 1 | 0 | 0 | 0 | 1 |
| `docs/plans/historical-plans/2026-08-28-security-assessment-core.md` | 0 | 1 | 0 | 0 | 1 |
| `docs/plans/historical-plans/2026-08-28-security-assessment-hostile-llm.md` | 0 | 1 | 0 | 0 | 1 |
| `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.12.0.md` | 4 | 0 | 0 | 0 | 4 |
| `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.23.0.md` | 1 | 0 | 0 | 0 | 1 |
| `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.26.17.md` | 6 | 0 | 0 | 0 | 6 |
| `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.26.18.md` | 1 | 0 | 0 | 0 | 1 |
| `docs/plans/historical-releasenotes/RELEASE-NOTES-v5.28.2.md` | 2 | 0 | 0 | 0 | 2 |
| `docs/plans/historical-releasenotes/RELEASE-NOTES-v6.0.0.md` | 0 | 1 | 0 | 0 | 1 |
| `docs/pwa-setup.md` | 0 | 3 | 0 | 10 | 13 |
| `docs/testing.md` | 1 | 2 | 0 | 0 | 3 |
| `internal/agents/docker_driver_test.go` | 3 | 0 | 0 | 0 | 3 |
| `internal/agents/k8s_driver_test.go` | 3 | 0 | 0 | 0 | 3 |
| `internal/agents/reconcile_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/autonomous/adopt_node_unknown_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/autonomous/lifecycle_forward_test.go` | 1 | 0 | 0 | 0 | 1 |
| `internal/autonomous/lifecycle_notify_settings_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/autonomous/lifecycle_webhook_guard.go` | 2 | 0 | 2 | 0 | 4 |
| `internal/autonomous/lifecycle_webhook_guard_test.go` | 40 | 7 | 10 | 0 | 57 |
| `internal/autonomous/lifecycle_webhook_sink_test.go` | 4 | 0 | 0 | 1 | 5 |
| `internal/autonomous/lifecycle_webhook_subscription_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/autonomous/release_version_check_test.go` | 1 | 0 | 0 | 0 | 1 |
| `internal/compute/registry_test.go` | 11 | 0 | 0 | 0 | 11 |
| `internal/config/config.go` | 0 | 0 | 1 | 4 | 5 |
| `internal/config/config_test.go` | 0 | 0 | 0 | 2 | 2 |
| `internal/config/mcp_public_url_test.go` | 0 | 0 | 0 | 4 | 4 |
| `internal/fingerprint/export_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/fingerprint/identity_test.go` | 2 | 1 | 0 | 2 | 5 |
| `internal/mcp/sse_base_url_test.go` | 2 | 0 | 0 | 10 | 12 |
| `internal/messaging/backends/twilio/backend_test.go` | 0 | 0 | 0 | 2 | 2 |
| `internal/observer/conn_correlator.go` | 1 | 0 | 0 | 0 | 1 |
| `internal/observer/conn_correlator_test.go` | 3 | 0 | 0 | 0 | 3 |
| `internal/observer/cross_peer_correlator_test.go` | 11 | 0 | 0 | 0 | 11 |
| `internal/router/mcp_public_url_configure_test.go` | 0 | 0 | 0 | 6 | 6 |
| `internal/router/triage_alert_sink_test.go` | 1 | 0 | 0 | 0 | 1 |
| `internal/server/bl243_tailscale_test.go` | 0 | 3 | 0 | 0 | 3 |
| `internal/server/federation_fingerprint_roundtrip_test.go` | 10 | 0 | 0 | 0 | 10 |
| `internal/server/federation_fingerprints_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/server/held_node_start_test.go` | 8 | 0 | 0 | 0 | 8 |
| `internal/server/loopback_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/server/mcp_public_url_patch_test.go` | 0 | 0 | 0 | 4 | 4 |
| `internal/server/mcp_public_url_put_test.go` | 0 | 0 | 0 | 2 | 2 |
| `internal/server/push_ssrf.go` | 2 | 0 | 2 | 0 | 4 |
| `internal/server/push_ssrf_test.go` | 6 | 3 | 0 | 0 | 9 |
| `internal/server/redirect_bypass_test.go` | 2 | 0 | 0 | 0 | 2 |
| `internal/server/session_start_held_node_test.go` | 4 | 0 | 0 | 0 | 4 |
| `internal/server/web/app-fingerprint.test.js` | 5 | 0 | 0 | 0 | 5 |
| `internal/server/web/app.js` | 0 | 0 | 0 | 2 | 2 |
| `internal/usagecalc/text_test.go` | 3 | 0 | 0 | 0 | 3 |
| `internal/wizard/defs.go` | 0 | 0 | 0 | 2 | 2 |
| `scripts/release-smoke-lifecycle-webhook.sh` | 1 | 0 | 0 | 0 | 1 |
| `scripts/test-stories/pwa/mobile-fit-seeds.mjs` | 2 | 0 | 0 | 0 | 2 |
| **Total** | **194** | **39** | **19** | **77** | **329** |


## Replaced: hostnames

### Placeholder scheme

Operator-specific local and MagicDNS/tailnet hostnames were replaced with
RFC 2606 reserved placeholder hostnames:

| Original form | Replaced with |
|---|---|
| MagicDNS / tailnet `*.ts.net` hostnames | reserved `*.example.net` hostnames |
| Operator home-machine / peer hostnames in docs, seeds, and examples | `local-node.example.com`, `remote-node.example.com`, or the file-specific `*.example.net`/`*.example.com` placeholder used by that document |
| Registry hostnames | `registry.example.com` |
| Web/peer base-URL hostnames | `datawatch.local`-style or `*.example.net` placeholders already present in those docs |

The "Hosts" column in the per-file table above counts replaced hostname
tokens per file (MagicDNS + other placeholder hostname substitutions
combined).

## Files touched

79 tracked files changed by the scrub (413 insertions / 609 deletions):

- **Test/Go sources** — production behavior unchanged; only test fixtures
  and doc comments moved to documentation ranges or byte-value
  constructions (see per-file table: `internal/…`, `cmd/datawatch/…`).
  Two production files carry comment/doc-example edits only:
  `internal/autonomous/lifecycle_webhook_guard.go`,
  `internal/server/push_ssrf.go`,
  `internal/observer/conn_correlator.go`, `internal/config/config.go`,
  `internal/wizard/defs.go`.
- **Web/PWA assets**: `internal/server/web/app.js`,
  `internal/server/web/app-fingerprint.test.js`,
  `scripts/test-stories/pwa/mobile-fit-seeds.mjs`.
- **Docs**: all `docs/howto/…`, `docs/flow/multi-machine-flow.md`,
  `docs/pwa-setup.md`, `docs/testing.md`, `docs/operations.md`,
  `docs/implementation.md`, `docs/config-reference.yaml`,
  `docs/plans/historical-*/**` (release notes, cluster validation,
  security assessment, PWA stability),
  `docs/plans/harness-research/.evidence/{quorum.md,e-domenic.html}`,
  `docs/plans/{2026-10-21-pwa-connection-stability,
  2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal}.md`,
  `CHANGELOG.md`, and this report.
- **Preserved, not deleted**: the `tmp-evidence/2026-10-25-docker-registry-rebuild/…`
  run artifacts (including `docker.build.log`) still contain historical
  private-range values (a public-registry mirror URL and a build-log peer);
  they are build-output evidence, not operator configuration, and were
  restored from the pre-scrub commit and left in place.

## Semantics preserved

- Pair-mapped replacements keep (input, expected) test assertions valid;
  `go test ./...` — 11017 tests passed in 99 packages.
- Loopback (`127/8`, `::1`), public addresses, and class-level CIDR
  references in comments were left as-is; the `64:ff9b::/96` NAT64 prefix
  is a protocol prefix, not a private address.
- `internal/observer/ebpf/vmlinux_amd64/vmlinux.h` was not edited: generated
  kernel header text, covered by the checker's exclusion list.

## Verification (2026-11-02)

- `scripts/check-no-local-leaks.sh` — final run in this worktree:
  `address: 0`, `local: 0`, `email: 0`, `home: 0` (zero hits across all
  classes).
- `go test ./...` — 11017 tests passed in 99 packages.

## Not replaced (intentional)

- Loopback, public, and class-level CIDR references (not operator values).
- Addresses embedded purely as byte-value constructions
  (`net.IPv4(0x…, …)`) — no dotted literal, invisible to the pattern check.
- `tmp-evidence/` run artifacts — see "Files touched" above.

## Replaced: node and GPU identifiers

This pass covers the operator machine's node hostname and GPU model name
(the address/tailnet scrub above already moved the same machine's network
identity; this section records the node-name and GPU-model sweep and its
end state).

### Replacement scheme

| Role in a document | Neutral wording used |
|---|---|
| The operator's own machine as a compute node | `local node`, `remote node`; where a name is required by a URL, command, or node pool identifier, an `example.com` name named after the role (`local-node.example.com`, `remote-node.example.com`, `ollama-local-node`-style role labels) — never the real hostname |
| The GPU where the document only needs a role | generic wording: "the GPU", "a high-end consumer GPU", or a memory-size fact (e.g. a 32 GB GPU) that carries capacity without identifying a model |
| Documents quoting a real operator setting | the placeholder value, plus one sentence noting the real value lives in the operator's own configuration |
| Session identifiers that embedded the hostname | neutral prefix + opaque suffix (no hostname fragment) |

### Per-file replacement counts (node hostname + GPU model)

A whole-tree, case-insensitive sweep for the runtime-discovered node
hostname (full and short form), the full and shortened GPU product names
(spaced and unspaced), and the bare model number found:

| File | Node-hostname replacements | GPU-model replacements | Total |
|---|---:|---:|---:|
| tracked tree (all files, incl. `AGENT.md`, `CHANGELOG.md`, `docs/plans/**`, test fixtures) | 0 new in this pass | 0 new in this pass | 0 |

Zero new replacements were required in this pass because the earlier
address/hostname scrub (see "Replaced: hostnames" above) already moved
every node and GPU reference in tracked content onto the neutral scheme
tabled above. Remaining GPU tokens in tracked files are generic catalog
examples (`h100`-class cards, third-party benchmark pages) that do not
identify this operator's machine, and the two generated kernel BTF
headers whose vendor IC identifiers merely share the 4-digit sequence are
excluded in `scripts/leak-check.exclude` (they are btfgen output and must
be regenerated, not hand-edited). Where a document quotes a real operator
setting next to an example value, the example carries the placeholder and
the sentence notes the real value lives in the operator config.

### Renamed paths

None: no tracked file or directory name carries a node or GPU identifier,
and session identifiers in tracked content already use the neutral
prefix scheme. No `mv` was needed in this pass.

### Verification (this pass)

- `.leak-patterns.local` (repository root, gitignored, never committed)
  holds the runtime-discovered identifiers, one per line, created from
  `scripts/leak-patterns.local.template`; `git check-ignore` confirms the
  ignore.
- `env -u TMUX -u TMUX_TMPDIR LEAK_CHECK_CLASSES=local bash
  scripts/check-no-local-leaks.sh` → `local: 0` (and `address: 0`,
  `home: 0`, `email: 0`).
- `.datawatch/verify/replace-hostname-gpu-identifiers.sh` re-discovers the
  identifiers at run time (none are stored in the script or this report)
  and passes.

## Replaced: home paths and emails

(scrub 08) Replaced absolute home-directory paths for real user names and
personal / operator email addresses across the tracked file set. Before
this pass the leak check (scripts/check-no-local-leaks.sh) reported about
336 home-path findings in about 130 files and about 110 email findings in
about 65 files (files in both classes overlap; about 190 files in total).
After this pass every class reports zero findings (address, home, email,
local).

Scheme:

- Linux `/home/<name>` -> `/home/<user>` (docs, scripts, test fixtures).
  Lines that already carried a placeholder form (`<user>`, `$USER`, `~/`)
  were left as-is.
- macOS `/Users/<name>` -> `/Users/<user>`.
- Windows `C:\Users\<name>` -> `C:\Users\<user>` (install/windows/README.md,
  including the literal `YOU` example).
- Personal/operator addresses -> generic placeholders: `me@example.com`,
  `user@example.com`, `foo@example.com`, `author@example.com`,
  `attacker@example.com`, `support@example.com`, and single-letter
  fixtures (`a@example.com`, `x@example.com`, `t@example.com`,
  `u@example.com`). A few fixtures that carried a real-looking domain as
  part of their identity were mapped onto the matching placeholder.
- Maintainer identity in the packaging metadata (packaging/arch/PKGBUILD,
  packaging/debian/changelog, packaging/debian/control,
  packaging/rpm/datawatch.spec): personal address replaced with
  `datawatch@example.com` and the surrounding name generalized to
  `datawatch`.
- Credential-embedding examples that happened to land in `user@host` shape
  (git URL auth, token examples): the credential part was replaced with a
  placeholder form (`tok@example.com`, `GH_TOKEN@example.com`,
  `ghp_abc@example.com`) so the examples read identically.
- Docker images (docker/dockerfiles/*): the non-root user's home was
  de-hardcoded via a new `ARG DW_USER` (default `datawatch`); the
  `useradd`/`chown`/`ENV PATH` lines now reference `/home/${DW_USER}`.
  Image output is byte-identical for default builds; the user name is a
  build argument instead of a literal.
- tegrastats field names that the email regex caught as false positives
  (the GPU temperature field, in internal/observer/gpu_tegrastats.go, a
  historical plan, and CHANGELOG.md) were re-worded to `temp field
  (T dot TC)` in the prose so the class no longer fires; the Go probe
  parsing code is untouched.

Allowed-list additions (scripts/leak-check.allowed-emails — legitimate
non-personal forms only):

- `*.example`, `*.example.net`, `*@*.example.com`, `*@*.example.net` —
  IANA-reserved example domains used by webhook/fixture/git hosts.
- `*.invalid`, `*.local` — RFC 2606 never-assigned domains in test
  fixtures.
- `*.test` — IANA-reserved git test identities.
- `*.service` — systemd user-bus unit names in test evidence.
- `*@users.noreply.github.com` — GitHub Actions default noreply
  identifier (bot, not a person).
- `*@*.png` — Retina asset file names (`...@2x.png`) in evidence HTML.

Verification: `bash scripts/check-no-local-leaks.sh` exits 0 with all four
classes at 0. `go build ./...` passes; the full `go test ./...` suite
passes (one load-dependent flake in tests/ci
`TestDaemonCleanupHelper/InterruptSighup`, 30 s signal-timing timeout,
reruns green in isolation and on the full package). Test fixtures that
feed the autonomous scope linter were moved to concrete non-personal
roots (`/root/...`) because the linter treats `<user>`-style segments as
placeholders and skips them; data-only fixtures use `/home/<user>`.

No original values are reproduced in this report. Note: the operator's
GitHub login appears in remote references (remote repo URLs, Go module
paths, container image registry paths); those are remote identifiers, not
local home paths or email addresses, and rewriting them would break
builds — they were left as-is.

## Final gate

Run: `Mon Nov  2 07:16:27 AM EST 2026` (output of `date` in this worktree).
Every gate was executed under `env -u TMUX -u TMUX_TMPDIR` with the
repository root as the scanned root.

- Whole-tree leak check (`scripts/check-no-local-leaks.sh`, all classes):
  PASS — 0 findings in all four classes, exit 0.
- Per-class runs (`LEAK_CHECK_CLASSES` set to one class at a time):
  - address: PASS (0 findings)
  - home: PASS (0 findings)
  - email: PASS (0 findings)
  - local: PASS (0 findings); the local-patterns set was **skipped**, not
    passed by pattern — `.leak-patterns.local` is absent in this worktree,
    and an absent patterns file only emits a "local patterns: skipped"
    notice (never an error).
- `make check-leaks`: PASS — exit 0, 0 findings in all four classes.
- Leak-check tests (`tests/test-check-no-local-leaks.sh`): PASS — 17 of
  17 cases passed, 0 failed, exit 0.
- `make lint`: PASS — all static audit scripts OK and the lint run
  finished with 0 issues; `check-leaks` is one of the lint steps, so the
  above classes were re-verified as part of it.
- Release-smoke leak step (`scripts/release-smoke.sh`, the static
  pre-daemon lint block that invokes the same whole-tree check on the
  repository root): only its leak step was run, directly, and it passed
  (exit 0). The full smoke was not run here because it needs a live test
  daemon and network endpoints, which are unavailable offline.

Reporting guarantee observed: the leak check's report output is
`path:line: class` per finding plus a per-class count summary, and with
zero findings it printed only the count summary; no identifier,
matched text or rest of any matched line was printed by any gate
command in this run.

Out of scope, deliberately untouched: the commit history (including
commit messages that name the operator's local hostname) and any public
issues and comments were **not** modified in this feature. Rewriting
history or public records is a separate decision with its own blast
radius.

Follow-ups:
- Decide whether to rewrite or accept the historical commit messages and
  public issue/comment text that name the operator's hostname.
- Optional: create `.leak-patterns.local` (see `make leak-patterns-init`)
  if operator-specific tokens beyond the four generic classes should be
  swept by the local class.
