#!/usr/bin/env bash
# .datawork/verify/schedule-flood-docs-set.sh
#
# Verifies the docs set for the schedule-flood PWA story (TS-797) against the
# code it documents: scripts/e2e-pwa.sh, scripts/e2e-pwa-config.sh,
# scripts/test-stories/pwa/TS-797.mjs, scripts/test-stories/pwa/lib.mjs, the
# Makefile PWA targets and docs/testing/docker-compose.test.yml. Fails (exit 1)
# if any doc claims a behavior the code does not have.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

fail=0
ok()  { printf 'ok: %s\n' "$1"; }
bad() { printf 'FAIL: %s\n' "$1"; fail=1; }

check_has() { # file, fixed-string, description
  if grep -qF -- "$2" "$1"; then ok "$3"; else bad "$3 (missing: $2 in $1)"; fi
}
check_absent() { # file, fixed-string, description
  if grep -qF -- "$2" "$1"; then bad "$3 (present: $2 in $1)"; else ok "$3"; fi
}

# ── runner: EVIDENCE_DIR is clobbered, then defaulted per mode ─────────────
R=scripts/e2e-pwa.sh
check_has $R 'EVIDENCE_DIR=""'                    "runner resets an inherited EVIDENCE_DIR (line 41)"
check_has $R 'EVIDENCE_DIR="${EVIDENCE_DIR:-$RUN_DIR/evidence}"'          "local mode: per-run evidence/ is the default"
check_has $R 'EVIDENCE_DIR="${EVIDENCE_DIR:-/tmp/dw-e2e-pwa-evidence}"'   "external mode: /tmp/dw-e2e-pwa-evidence is the default"
check_has $R 'ARG="${1:-schedule-flood}"'         "runner default story is schedule-flood"
check_has $R 'rm -rf "$RUN_DIR"'                  "cleanup removes the per-run dir"
check_has $R 'keeping evidence + daemon log in: $RUN_DIR' "cleanup keeps the per-run dir on failure"
check_has $R 'TEST_TOKEN:-dw-e2e-pwa-token'       "runner token default is dw-e2e-pwa-token"
check_has $R 'exit $rc'                           "runner exits with the story's own code (0/1/2)"
check_has scripts/e2e-pwa-config.sh 'allowed_project_roots: [\"$(dirname "$RUN_DIR")\"]' \
  "config helper allows the run dir's parent as project root"

# ── lib.mjs: env defaults + evidence naming ─────────────────────────────────
L=scripts/test-stories/pwa/lib.mjs
check_has $L "'/tmp/dw-pwa-evidence'"             "lib.mjs default EVIDENCE_DIR (direct invocation)"
check_has $L "path.join(dir, \`\${name}.txt\`)"   "saveLog appends .txt (doubled .txt.txt)"
check_has $L "path.join(dir, \`\${name}.png\`)"   "screenshot appends .png"
check_has $L "CURRENT_STORY"                      "evidence subdir comes from CURRENT_STORY"

# ── TS-797 story evidence entries ────────────────────────────────────────────
S=scripts/test-stories/pwa/TS-797.mjs
check_has $S "saveLog('failure-A.txt'"            "failure-A.txt (→ failure-A.txt.txt)"
check_has $S "saveLog('failure-B.txt'"            "failure-B.txt (→ failure-B.txt.txt)"
check_has $S "saveLog('SKIP-REASON.txt'"          "SKIP-REASON.txt (→ SKIP-REASON.txt.txt)"
check_has $S "saveLog('verdict.txt'"              "verdict.txt (→ verdict.txt.txt)"
check_has $S "saveLog('trusted-origin'"           "trusted-origin (→ trusted-origin.txt, single suffix)"
check_has $S "path.join(EVID, 'result.txt')"      "result.txt written directly (single .txt)"
check_has $S "'trusted-tls'"                      "trusted-tls/ key-cert dir when (B) self-generates"
check_has $S 'const MAX_SCHEDULE_REQS = 5;'       "cap is 5 /api/schedules requests"
check_has $S "Math.max(20, Number(process.env.TS797_SESSION_COUNT) || 20)" "seed ≥ 20, TS797_SESSION_COUNT raises it"
check_has $S "process.exit(2)"                    "skip path exits 2"
check_has $S "const CHROME_PATH = '/usr/bin/google-chrome';" "TS-797 hardcodes /usr/bin/google-chrome"

# ── Testing README: the EVIDENCE_DIR + evidence-name claims ─────────────────
RDM=docs/testing/README.md
check_has    $RDM '/tmp/dw-e2e-pwa-evidence'      "README: external-mode default /tmp/dw-e2e-pwa-evidence"
check_has    $RDM 'resets any inherited `EVIDENCE_DIR`' "README: runner resets inherited EVIDENCE_DIR"
check_has    $RDM 'honored only when invoking the story directly' "README: env EVIDENCE_DIR honored only for direct invocation"
check_has    $RDM 'SKIP-REASON.txt.txt'           "README: SKIP-REASON.txt.txt (doubled)"
check_has    $RDM 'trusted-origin.txt` plus `trusted-tls/' "README: trusted-origin.txt (single .txt)"
check_has    $RDM 'verdict.txt.txt'               "README: verdict.txt.txt (doubled)"
check_absent $RDM 'trusted-origin.txt.txt'        "no stale trusted-origin.txt.txt"
check_absent $RDM 'is honored in both modes'      "no wrong 'honored in both modes' claim"
check_has    $RDM 'make test-e2e-pwa` is the docker-compose route, not the local one' "README: make root vs local route"
[ -f docs/testing/runs/observer-mobile-fit/repro-report.txt ] && ok "repro-report.txt exists" || bad "repro-report.txt missing"

# ── howto navigation-mobile-fit: accurate clobber wording ───────────────────
H=docs/howto/navigation-mobile-fit.md
check_has    $H 'resets it (it sets `EVIDENCE_DIR=""` before'   "howto: runner resets inherited EVIDENCE_DIR"
check_has    $H '/tmp/dw-e2e-pwa-evidence`, which persists'     "howto: external default persists"
check_has    $H 'honors the inherited `EVIDENCE_DIR` (lib.mjs default' "howto: direct invocation honors EVIDENCE_DIR"
check_absent $H 'is honored (the run dir is only the default)'  "no stale 'honored' claim in howto"

# ── testing.md: the N6 record stays as a historical note ────────────────────
T=docs/testing.md
check_has    $T 'Fixing the script is a separate change.' "testing.md: EVIDENCE_DIR script fix still noted as separate change"
check_absent $T 'That script fix is now in the current tree' "no wrong 'fix is in the tree' claim"

# ── Makefile: local vs compose roots ─────────────────────────────────────────
M=Makefile
check_has $M 'docker compose -f $(E2E_COMPOSE) run --rm playwright' "make test-e2e-pwa runs the compose playwright root"
check_has $M 'bash scripts/e2e-pwa.sh load'                        "make test-e2e-pwa-load is the local load group"
check_has $M 'bash scripts/e2e-pwa.sh TS-817'                      "make test-e2e-mobile-fit runs TS-817 locally"
check_has docs/testing/docker-compose.test.yml 'EVIDENCE_DIR=/tmp/dw-pwa-evidence' "compose playwright root: fixed EVIDENCE_DIR"
check_has docs/testing/docker-compose.test.yml 'TEST_HTTP=http://datawatch:8080'   "compose playwright root: compose-network daemon origin"

# ── testing-tracker: the current-state sentence ─────────────────────────────
TRK=docs/testing-tracker.md
check_has $TRK 'The local, non-docker route is `bash scripts/e2e-pwa.sh
schedule-flood`' "tracker: local single-story route is scripts/e2e-pwa.sh"
check_has $TRK 'docker-compose route that runs the \*\*full\*\* story
suite in the `playwright` container' "tracker: make test-e2e-pwa is the compose full-suite root"
check_absent $TRK 'is the local, non-docker route
(`scripts/e2e-pwa.sh' "no stale 'make test-e2e-pwa is the local route' sentence"

# ── link targets named in the TS-797 section must exist ─────────────────────
for f in scripts/e2e-pwa.sh scripts/e2e-pwa-config.sh tests/test-e2e-pwa-config.sh \
         scripts/test-stories/pwa/schedule-flood.mjs scripts/test-stories/pwa/run-all.mjs \
         docs/testing/docker-compose.test.yml docs/howto/testing-against-a-running-daemon.md \
         scripts/test-stories/pwa/perf-lib.mjs scripts/test-stories/pwa/TS-816.mjs \
         scripts/test-stories/pwa/TS-817.mjs scripts/test-stories/pwa/TS-818.mjs \
         scripts/test-stories/pwa/mobile-fit-desktop-baseline.json; do
  [ -e "$f" ] && ok "link target exists: $f" || bad "missing link target: $f"
done

if [ "$fail" -eq 0 ]; then echo 'schedule-flood docs set: all checks passed'; else
  echo 'schedule-flood docs set: FAILURES'; exit 1; fi
