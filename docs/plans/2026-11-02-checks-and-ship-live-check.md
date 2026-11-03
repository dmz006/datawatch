# Checks and ship live check — 2026-11-02

Run by `scripts/checks-and-ship-live-check.sh` against a **sandbox daemon** (generated config, loopback ports, own data directory), source revision `f983f830e`. The release number is not recorded here.

**Result: 25 of 25 checks passed.**

- **PASS** — GET /api/autonomous/checks answers a JSON object
  - {"policy":{"pre_start_verifier":"on","block_when_missing":true,"plan_rules":"on","after_done":"on","registry_readme":"on"},"findings":[]}
- **PASS** — GET /api/autonomous/ship answers a JSON object
  - {"policy":{"push":"on","tag":"on","ci_watch":"on","tag_format":"v{version}","remote":"origin","ci_workflows":[]},"findings":[]}
- **PASS** — pre_start_verifier reads the default mode on
- **PASS** — ship push reads the default mode on
- **PASS** — tag_format reads the default v{version}
- **PASS** — PUT /api/config with the new keys answers 200
  - status 200
- **PASS** — checks.pre_start_verifier reads back shadow
- **PASS** — checks.after_done reads back off
- **PASS** — ship.tag_format reads back rel-{version}
- **PASS** — ship.remote reads back upstream
- **PASS** — ship.ci_workflows reads back as a list
  - ["release","ci"]
- **PASS** — a bad check mode is rejected with 400
  - status 400
- **PASS** — a tag_format without {version} is rejected with 400
  - status 400
- **PASS** — the rejected values changed nothing
- **PASS** — checks route shows the shadow mode just set
  - {"policy":{"pre_start_verifier":"shadow","block_when_missing":true,"plan_rules":"on","after_done":"off","registry_readme":"on"},"findings":[]}
- **PASS** — ship route shows the new tag format
  - {"policy":{"push":"shadow","tag":"shadow","ci_watch":"on","tag_format":"rel-{version}","remote":"upstream","ci_workflows":["release","ci"]},"findings":[]}
- **PASS** — checks route accepts prd_id for an unknown id
- **PASS** — checks route refuses a write with 405
- **PASS** — ship route refuses a write with 405
- **PASS** — datawatch autonomous checks prints the modes
  - checks: pre_start_verifier=shadow block_when_missing=true plan_rules=on after_done=off registry_readme=on
no findings recorded
- **PASS** — datawatch autonomous ship prints the tag format
  - ship: push=shadow tag=shadow ci_watch=on tag_format=rel-{version} remote=upstream ci_workflows=release,ci
no findings recorded
- **PASS** — served app.js carries the checks card
- **PASS** — served app.js carries the ship card
- **PASS** — all five served locale bundles carry the checks and ship keys
- **PASS** — the sandbox daemon is gone

## Not run live

- The ship step itself (push, annotated tag, CI watch) and the pre-start block need a finished or started Automaton driven by a model, which a sandbox has none of. They are covered in-process by `TestChecksShipIntegration` (`cmd/datawatch/checks_ship_integration_test.go`) against a local bare remote, and are **not** validated on a live daemon.
- The MCP tools and the chat verbs are covered by unit tests only; the PWA cards are covered by node tests and by the served-code check above, not by opening a browser.
