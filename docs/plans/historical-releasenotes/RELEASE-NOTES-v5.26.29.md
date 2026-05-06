# datawatch v5.26.29 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.28 → v5.26.29
**Patch release** (no binaries — operator directive).
**Closed:** Pre-release security scan automation (gosec + govulncheck).

## What's new

### `.github/workflows/security-scan.yaml`

The AGENT.md "Pre-release security scan" rule had a documented operator command for years but no CI hook — it was easy to skip on tag day. v5.26.29 wires the same command into Actions so the scan can't be forgotten.

Two jobs:

| Job | Mode | Why |
|------|------|------|
| `govulncheck` | **Blocking** | Reachable Go vulnerabilities are unambiguous. Last regression closed v5.26.3 (transitive `golang.org/x/net` HTTP/2 frame handler — datawatch is client-side, not affected, but bumped anyway for hygiene). |
| `gosec` | **Advisory** (`continue-on-error: true`) | The 55 documented findings in `docs/security-review.md` are accepted with rationale. Making this blocking would require a baseline-diff mechanism we don't have. Operator reviews the diff manually. |

Triggers:

- **Tag push** (`v*`) — natural release boundary.
- **PR to main** — catch new findings before they land.
- **Manual dispatch** — spot checks.

### Command parity with AGENT.md

The CI invocation reads `.gosec-exclude` and applies the same `-severity=high -confidence=medium` filters that the operator runs locally:

```yaml
- name: Run gosec (advisory)
  run: |
    EXCLUDE=$(grep -v '^#' .gosec-exclude | tr '\n' ',' | sed 's/,$//')
    $(go env GOPATH)/bin/gosec -exclude="$EXCLUDE" -severity=high -confidence=medium -fmt text -quiet ./...
```

Same input, same output, same triage workflow. Operator can copy a CI failure straight into a local re-run.

### Why now

The gh-actions audit in v5.26.25 specifically called out "pre-release security scan automation" as a missing piece. v5.26.29 closes that follow-up and removes one more line from `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Configuration parity

No new config knob — pure CI workflow.

## Tests

`govulncheck ./...` validated locally before commit:

```
No vulnerabilities found.
```

## Known follow-ups

Rolled into `docs/plans/2026-04-27-v6-prep-backlog.md`:

- gosec baseline-diff mechanism (would let the gosec job become blocking)
- agent-goose Dockerfile + CI publish
- Kind-cluster smoke workflow
- Pinned action SHAs (supply-chain hardening)
- datawatch-app PWA mirror — issue #10
- v6.0 cumulative release notes
- GHCR past-minor cleanup run

## Upgrade path

```bash
git pull
# No daemon restart needed — CI-only change. The first tag push
# after v5.26.29 will trigger the new workflow.
```
