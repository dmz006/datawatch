# datawatch v5.26.5 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.4 → v5.26.5
**Patch release** (no binaries — operator directive: every release until v6.0 is a patch).
**Closed:** Container hygiene runbook + GHCR retention script + datawatch-app#10 catch-up issue + parent-full gap documented

This is the last pre-v6.0 patch. Operator-driven items from the 2026-04-26 audit are all closed; v6.0 cumulative cut is next.

## What's new

### `docs/container-hygiene.md` — new

Day-two operator runbook for the GHCR image inventory. Covers:

- **What CI publishes.** Stage 1 (`agent-base`, `validator`, `stats-cluster`) and Stage 2 (`agent-claude`, `agent-opencode`, `agent-aider`, `agent-gemini`) — including the slash-namespaced `ghcr.io/dmz006/datawatch/agent-base` tag the v5.22.0 fix added so Stage-2 chain consumers resolve.
- **What CI does NOT publish.** Two gaps documented honestly:
  - **`parent-full`** — the Dockerfile exists at `docker/dockerfiles/Dockerfile.parent-full` and `docs/howto/setup-and-install.md` Option D references it, but it isn't in the CI matrix. Operators who want it now build locally; v6.0 lands the CI add-on. Local build command included.
  - **`agent-goose`** — Dockerfile not yet present. Goose works as a host-installed backend; v6.0 lands the image.
- **Retag for a patch.** Bash one-liner that pulls a tag, retags it as `latest`, pushes back. Includes the slash-path `agent-base` requirement.
- **Cleanup — past-minor backlog.** Pointer to the new script (below).
- **Vulnerability scanning.** Trivy / grype / docker scout one-liners. Automation deferred to v6.0.

### `scripts/delete-past-minor-containers.sh` — new

Counterpart to the existing `delete-past-minor-assets.sh`. Same retention algorithm: every major + latest minor + latest patch on latest minor. Untagged dangling layers always pruned.

```bash
# Preview
DRY_RUN=1 GITHUB_TOKEN=<pat> ./scripts/delete-past-minor-containers.sh
# Run
GITHUB_TOKEN=<pat> ./scripts/delete-past-minor-containers.sh
```

Requires a fine-grained PAT with `read:packages + delete:packages` (the default `gh auth login` token is `read` only — same gap the audit flagged). Iterates every datawatch container package.

### `docs/security-review.md` + `docs/container-hygiene.md` wired into `/diagrams.html`

Both new docs added to the **Subsystems** group in the embedded diagrams viewer so operators can browse them without leaving the PWA.

### datawatch-app#10 — filed

`https://github.com/dmz006/datawatch-app/issues/10` — comprehensive catch-up issue covering every PWA addition since v5.3.0:

- BL191 review/approve gate, BL203 LLM overrides, BL191 Q4 child PRDs, BL191 Q5/Q6 verdicts, autonomous full CRUD, BL202 learnings, autonomous WS auto-refresh.
- Channel tab history seeding (v5.26.1).
- Settings → Comms multi-select bind interface, mode-badge drop, docs chips.
- New PRD configured-only backends + Model dropdown.
- Setup howto Helm/Kubernetes section.
- Long-press server-status indicator.
- BL180 cross-host federation, Shape A/B/C peer registry, eBPF kprobes.
- Diagrams page restructure + howto README link rewriting.

Each surface includes the relevant REST/MCP/WS endpoint and a pointer to the datawatch design doc.

## Configuration parity

No new config knob. Pure docs + ops-tooling.

## Tests

1395 still passing. No code changes outside version bumps and the new bash script (syntax-checked with `bash -n`, executable bit set).

## Known follow-ups

All operator-driven items from the 2026-04-26 audit are now closed or have explicit runbooks. Remaining for the v6.0 cut:

- **v6.0 cumulative release notes** — full retrospective covering everything since v5.0.0.
- **CI: add `parent-full` + `agent-goose` to `containers.yaml`.** Currently documented as a gap; v6.0 closes by extending the Stage 2 matrix.
- **CI: pre-release security scan automation** — `gosec` + `govulncheck` runs are manual; v6.0 wires them into a release-gate workflow.

## Upgrade path

```bash
git pull          # patch series — no binary update path
# Operators wanting parent-full now: see docs/container-hygiene.md
# Operators with read:packages+delete:packages PAT can run:
#   GITHUB_TOKEN=<pat> ./scripts/delete-past-minor-containers.sh
```
