# datawatch-app parity issue: classification verdicts

To be filed by the supervisor at merge against `dmz006/datawatch-app`. Not filed from this branch (no `gh` here).

**Title:** Parity for classification verdicts: Verification mode selector, route and score vector on the verdict

## What changed in the PWA

1. **Verification mode selector** on the LLM edit view (`llm_field_verification_mode`, `llm_verification_mode_default|generate|classify`, `llm_verification_mode_hint`). Sets `verification_mode` on the LLM entry; empty means generate.
2. **Route and score vector on the verdict** (`classif_verdict_title`, `classif_mode`, `classif_route`, `classif_route_path`, `classif_scores`, `classif_pass_probability`, `classif_threshold`, `classif_severity_scores`, `classif_category`, `classif_low_confidence`). Reads `route` (`node`, `model`, `path`) and `scores` (`ok`, `severity`, `pass_probability`, `threshold`) from the task verification and the verdict records.

## API the app needs

`GET /api/llm/classifier` (modes, probes, calibrations, allowlist), `GET|POST /api/llm/classifier/publishers` (POST needs `confirm: true`), `GET /api/llm/classifier/verdicts`.

## Acceptance

Selector saves and round-trips; a classify verdict shows route and scores; a low-confidence block is marked and can be decided; strings exist in en, de, es, fr and ja.
