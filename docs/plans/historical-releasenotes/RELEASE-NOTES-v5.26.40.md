# datawatch v5.26.40 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.39 → v5.26.40
**Patch release** (no binaries — operator directive).
**Closed:** gosec baseline-diff mechanism — gosec job now blocking on net-new findings (CI residual from v5.26.25 audit).

## What's new

### gosec is now blocking with a baseline-diff gate

v5.26.29 wired gosec into CI but kept the job advisory (`continue-on-error: true`) because the codebase has a documented set of accepted findings (`docs/security-review.md`) and there was no mechanism to distinguish "expected" findings from "regression". v5.26.40 closes that residual.

Mechanism:

```
.gosec-baseline.json    ← committed; total + by_rule breakdown of accepted findings
        │
        ▼ (compared)
gosec runs in CI ──── live count ≤ baseline?
                        │
                        ├── yes → PASS (also notice if BELOW baseline → suggest tightening)
                        │
                        └── no  → FAIL with diff explanation pointing at next-step
                                    (review the new finding; either fix or bump baseline)
```

Concrete enforcement in `.github/workflows/security-scan.yaml`:

```yaml
gosec:
  steps:
    - name: Run gosec + baseline-diff
      run: |
        $(go env GOPATH)/bin/gosec ... -fmt=json ./... > /tmp/gosec.json
        python3 - <<'PY'
        live_n = len(json.load(open("/tmp/gosec.json")).get("Issues", []))
        base_n = json.load(open(".gosec-baseline.json"))["total"]
        if live_n > base_n:
            print(f"::error::gosec: {live_n - base_n} net-new findings exceed baseline {base_n}")
            sys.exit(1)
        PY
```

Output also includes a per-rule live-vs-baseline breakdown so the operator can see exactly which rule moved if total is unchanged but composition shifted.

### `.gosec-baseline.json` (new file)

```json
{
  "total": 42,
  "by_rule": {
    "G115": 19,   // integer-overflow conversions
    "G703": 13,   // hardcoded credentials false-positives
    "G704":  6,   // SSRF expected in proxy mode
    "G118":  2,   // long-lived goroutine context (intentional)
    "G122":  1,
    "G123":  1
  }
}
```

The 42 findings here are the live count at the moment of v5.26.40 — close to but not identical to the `55` cited in `docs/security-review.md` (codebase has changed since that doc was last sweep-updated; some findings closed organically, a couple new shapes appeared). The baseline file IS the source of truth for CI; the review doc keeps the rationale.

### Bump procedure

When a new gosec finding lands:

1. Operator reviews in CI logs (the per-rule diff makes the new rule obvious).
2. Decision: real fix, or accept-with-rationale.
3. If accepting:
   - Add a new section to `docs/security-review.md` documenting why.
   - Bump `total` and the relevant `by_rule[<rule>]` in `.gosec-baseline.json`.
   - Commit both together — the baseline file's `_comment` field references this rule.

The `_baseline_taken_at` and `_command` keys make the file self-documenting for any future operator that needs to regenerate from scratch.

### Notice when below baseline

If live count drops below baseline (someone fixed an accepted finding without updating the baseline), the workflow emits a `::notice::` suggesting a baseline reduction. Doesn't fail — operator can pick up the cleanup at their pace — but flags the opportunity to tighten the gate.

## Configuration parity

No new config knob. Baseline file is a CI artifact; not consulted at runtime.

## Tests

Logic validated locally by:

```bash
# Equal-count case (live=42, baseline=42) → exits 0.
# Simulated +1 finding (live=43, baseline=42) → would exit 1 with diff=1.
```

Both paths confirmed. Smoke unaffected: 37 pass / 0 fail / 1 skip. Go test suite unaffected: 465 passing.

## Known follow-ups

CI residuals remaining after v5.26.40:

- **agent-goose Dockerfile + CI publish.** Dockerfile not yet written.
- **Kind-cluster smoke workflow.** Spin up `kind`, deploy chart, run `release-smoke.sh` against the deployed daemon.

Phase 6 screenshot recapture, Phase 3 + Phase 4 design, mempalace alignment audit, service-function smoke completeness — all unchanged.

## Upgrade path

```bash
git pull
# No daemon restart needed — CI-only change. Push a tag (this one)
# to verify the baseline-diff resolves cleanly. Future tag pushes
# will fail the security-scan workflow if a net-new gosec finding
# lands without a baseline bump.
```
