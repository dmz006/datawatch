---
docs:
  index: true
  topics: [testing, ci, scripts, autonomous]
---
# How-to: Testing Against a Running Daemon

Datawatch's two test scripts — `scripts/release-smoke.sh` (functional
smoke) and `scripts/run-tests.sh` (E2E stories) — each default to
spinning up their own fresh, isolated daemon with a throwaway data
directory. Both also support pointing at a daemon you already have
running instead: a CI-deployed kind/k8s instance, a persistent dev
daemon, a remote sandbox. This is a reusable capability, not a
one-off CI hack — any script or app driving datawatch's test surface
can use the same variables.

## Why this exists

`scripts/release-smoke.sh`'s own `kind-smoke` CI job builds a Docker
image, loads it into a `kind` cluster, and waits for it to answer
`/api/health` through a port-forward — the whole point is to catch
chart/image regressions in that *real deployed instance*. But the
script always launched its own separate local daemon anyway, so the
in-kind daemon was never actually exercised; the job silently tested
nothing new. Fixed 2026-10-13 by teaching `release-smoke.sh` to run
its functional checks against the daemon that's actually already
running, when asked to.

## `release-smoke.sh`: `DW_BASE` / `DW_BASE_TOKEN` / `DW_BASE_INSECURE`

```sh
# Point at an already-running daemon instead of starting a new one.
DW_BASE=http://localhost:18080 bash scripts/release-smoke.sh

# With auth and a self-signed/untrusted cert (the common case for a
# test/sandbox/kind-cluster daemon):
DW_BASE=https://sandbox.example:8443 \
  DW_BASE_TOKEN=your-bearer-token \
  bash scripts/release-smoke.sh
```

| Variable | Required | Meaning |
|---|---|---|
| `DW_BASE` | to enable this mode | Full base URL of the already-running daemon (`scheme://host:port`, no trailing slash). Unset = default behavior, spin up a fresh isolated daemon (unchanged). |
| `DW_BASE_TOKEN` | no | Bearer token for that daemon's REST API. Empty = no `Authorization` header (matches a daemon with `server.token: ""`). |
| `DW_BASE_INSECURE` | no (default **on** in this mode) | Whether the `datawatch` CLI's own `-u`/`--url` calls skip TLS certificate verification. Every REST/MCP check in this script already uses `curl -sk` (skip-verify) unconditionally regardless of mode, so this defaults to `1` to match that same posture — the common case (a self-signed test/sandbox/kind-cluster cert) just works. Set `DW_BASE_INSECURE=0` yourself if your external daemon has a real, CA-trusted cert and you want the CLI checks to actually verify it. |

**What's skipped in this mode, and why:** a few checks need to drop a
fixture file where the *daemon* will read it from its own local disk
(the managed-plugin discovery/lifecycle sections, and the evals-suite
smoke check) — that only works when the script and the daemon share
one filesystem, which isn't guaranteed for an external/remote/
in-cluster target. These report `SKIP` with an explicit reason in
this mode, not a false `FAIL`. Everything else — every REST/MCP
check, every CLI round-trip — runs identically against any
already-running daemon, local or external.

## `run-tests.sh`: `--no-daemon` / `TEST_BASE` / `TEST_BASE_HTTP` / `TEST_TOKEN`

The E2E story runner has had its own, independent version of this
same capability since before this doc existed — documented here for
the first time per an operator request that both mechanisms be
discoverable, not just the new one.

```sh
# Skip auto-starting a daemon; run E2E stories against one you already
# have up.
TEST_BASE=https://127.0.0.1:18443 TEST_TOKEN=your-bearer-token \
  bash scripts/run-tests.sh --no-daemon --story=TS-042
```

| Variable | Required | Meaning |
|---|---|---|
| `--no-daemon` (flag) | to enable this mode | Skip the script's own daemon-start step entirely. |
| `TEST_BASE` | yes, with `--no-daemon` | HTTPS base URL of the already-running daemon the stories will target. |
| `TEST_BASE_HTTP` | no | The matching plain-HTTP base URL, for stories that specifically test the HTTP listener/redirect behavior. |
| `TEST_TOKEN` | no | Bearer token for that daemon. Defaults to `dw-test-token-12345` (the fixed token `run-tests.sh`'s own normally-self-started daemon always uses) when unset — override it to match whatever token the external daemon actually requires. |

Without `--no-daemon`, `run-tests.sh` always computes `TEST_BASE`/
`TEST_BASE_HTTP` itself from freshly-allocated ports and starts its
own daemon — unaffected by any of this.

## Why two different mechanisms, not one shared variable name

`release-smoke.sh`'s `DW_BASE` and `run-tests.sh`'s `TEST_BASE` solve
the identical problem independently, because each script's own
daemon-lifecycle code predates this generalization and nothing else
in either script depends on the two sharing one name. If you're
writing a *new* script or app that needs this same "point me at a
daemon I already have" capability, either naming convention is a
reasonable precedent to follow — prefer whichever this doc's sibling
script already uses for the closest-matching case, and document the
new variable here too.

## For other agents and integrations

Both mechanisms are safe to drive from outside a human terminal — an
orchestration script, an automation, or another datawatch Automaton
that needs to functionally verify a daemon it (or something else)
already stood up. Set the env vars documented above and invoke the
script exactly as shown; nothing about either mechanism assumes an
interactive shell.

## See also

- [daemon-operations.md](daemon-operations.md) — general daemon lifecycle (start/stop/restart)
- [docs/testing-tracker.md](../testing-tracker.md) — the TS-### E2E story ledger `run-tests.sh` drives
- `scripts/release-smoke.sh` and `scripts/run-tests.sh` — both scripts' own header comments are the authoritative source; this doc summarizes them
