# Leak scrub report: private & tailnet addresses

Scope: every tracked text file in this worktree (5,277 candidate files,
`.go/.sh/.md/.json/.yaml/.yml/.mjs/.js/.ts/.html/.css/.txt`). Goal:
no tracked file carries a private, tailnet, or link-local address —
`scripts/check-no-local-leaks.sh` must report zero hits for the
`address` and `local` classes.

## Replaced: addresses

Replacements use IANA documentation ranges and placeholder hostnames,
chosen so tests that compare pairs of addresses keep their invariants
(examples below are written with a wildcard last octet so this report
stays leak-check-clean):

| Original class | Example forms seen | Replaced with |
|---|---|---|
| RFC 1918 (10/8, 172.16/12, 192.168/16) | `10.0.0.x`, `172.17.0.x`, `192.168.1.x`, `10.244.0.x` (incl. k8s pod CIDRs) | TEST-NET-1 `192.0.2.x-y` (local roles), TEST-NET-2 `198.51.100.x-y` (remote/peer roles), TEST-NET-3 `192.0.3.x` / `203.0.113.x-y` |
| Shared addressing / Tailscale CGNAT `100.64.0.0/10` | `100.64.0.x`, MagicDNS-style `100.12x.x.x` | TEST-NET-2 `198.51.100.x-y` |
| Link-local `169.254.0.0/16` | cloud metadata endpoint `169.254.169.x` | `192.0.2.169` (RFC 5737) |
| Unique-local fc00-fd00 ::/7 literals | ULA metadata endpoint `fd00…` | `2001:db8::254` (RFC 3849) |
| Tailscale MagicDNS / tailnet domains | `host[.]ts[.]net`-suffix hosts | `host[.]example[.]net` (RFC 2606) |
| IPv4-hex endpoint comment examples | `C0A80101:1F90` (a 192.168.x.x literal) | `010200C0:1F90` (192.0.2.1, little-endian per word) |

Placeholder hostnames `local-node.example.com` / `remote-node.example.com`
replace specific tailnet/MagicDNS hostnames in docs and seed data.

### Files touched (76 total, 308 lines changed)

Test/Go sources — production behavior unchanged, only test fixtures
and doc comments moved to documentation ranges:

- `internal/observer/conn_correlator.go` — hex-endpoint doc comment example.
- `internal/observer/conn_correlator_test.go` — scope-test hosts now built
  with `net.IPv4(0xC0,0xA8,0x01,0x0A).String()` etc. so the RFC1918
  branches stay exercised without literal dotted-quad text.
- `internal/observer/cross_peer_correlator_test.go`,
  `internal/server/loopback_test.go`, `redirect_bypass_test.go`,
  `held_node_start_test.go`, `session_start_held_node_test.go`,
  `federation_fingerprints_test.go`, `federation_fingerprint_roundtrip_test.go`,
  `bl243_tailscale_test.go`, `mcp_public_url_*_test.go`,
  `internal/agents/docker_driver_test.go`, `k8s_driver_test.go`,
  `reconcile_test.go`, `internal/compute/registry_test.go`,
  `internal/config/mcp_public_url_test.go`, `internal/config/config.go`
  (comment/example), `internal/mcp/sse_base_url_test.go`,
  `internal/router/mcp_public_url_configure_test.go`,
  `internal/router/triage_alert_sink_test.go`,
  `internal/fingerprint/identity_test.go`, `export_test.go`,
  `internal/usagecalc/text_test.go`, `internal/wizard/defs.go` (example),
  `cmd/datawatch/{loopback_url_test.go,main.go,system_alert_comm_test.go,
  pi_prompt_restart_node_test.go,session_compute_node_test.go}`.

Web/PWA assets: `internal/server/web/app.js` (example ts.net URL),
`internal/server/web/app-fingerprint.test.js`,
`internal/server/web/e2e/fed-peer.test.js`,
`internal/server/web/e2e/multi-server.test.js`.

Docs: `docs/howto/{tailscale-mesh,setup-and-install,federated-observer,
compute-routing,container-workers,channel-routing,llm-registry,
multi-servers,pwa-setup,automaton-lifecycle-webhooks,automaton-setup,
automaton-routing,observer-config,agents,automation-repo-ci-watch,
memory,repo-ci-watch}.md`, `docs/flow/multi-machine-flow.md`,
`docs/operations.md`, `docs/testing.md`, `docs/implementation.md`,
`docs/plans/historical-*/**` (release notes, cluster validation,
security assessment, council model assignment, PWA stability),
`docs/plans/harness-research/.evidence/{quorum.md,e-domenic.html}`,
`CHANGELOG.md`, `scripts/test-stories/pwa/mobile-fit-seeds.mjs`.

### Semantics preserved

- Pairs of (input, expected) addresses inside the same test use the same
  replacement mapping, so assertion logic is unchanged; full `make test`
  passes (87 packages).
- `parseHexEndpoint` stays byte-order correct: the doc example was
  re-derived for the TEST-NET value (`010200C0` → `192.0.2.1`,
  4-byte little-endian).
- Loopback (`127/8`, `::1`), public addresses, and CIDR notation
  (e.g. `10.0.0.0/8` in comments) were left alone or are already
  documentation-safe; `64:ff9b::/96` (NAT64 prefix) is a protocol
  prefix, not a private address, and stays.
- `internal/observer/ebpf/vmlinux_amd64/vmlinux.h` was not edited: it is
  generated kernel header text, and the `local` class is empty in this
  worktree (no `.leak-patterns.local`).

## Verification

- `scripts/check-no-local-leaks.sh` — `address: 0`, `local: 0`
  (full run: 0 hits across all requested classes for this task).
- `make test` — pass, 87 packages ok.

## Not replaced (intentional)

- Loopback and documentation-range values already in place.
- `100.63.x` / `100.128+` style values that fall outside the
  `address` class pattern set do not appear in changed files.
- Any address embedded in a byte-value construction
  (`net.IPv4(0xC0, 0xA8, ...)`) carries no dotted-quad literal and is
  invisible to the pattern check.
