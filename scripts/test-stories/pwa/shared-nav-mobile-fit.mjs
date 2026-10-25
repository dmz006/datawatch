// scripts/test-stories/pwa/shared-nav-mobile-fit.mjs — shared navigation phone-width scenario.
//
// The scenario the PWA runner's shared-nav group, `make test-e2e-shared-nav` and the
// release-smoke navigation section all run. It delegates to the mobile-fit probe in gate
// mode: the shared bottom nav is measured at 320, 360 and 390 px in each theme, and any
// nav finding (clipped text, a tap target under 44 px, or overlapping items) fails.
//
// The probe is the stand-in for the named checker, which is not on any git ref (see
// docs/plans/2026-10-25-shared-nav-mobile-fit.md). Its gate measures the nav only, so
// findings in the other tabs do not fail this scenario.
//
// Exit codes, as the runner reads them:
//   0  pass
//   1  fail: a nav finding at phone widths, or the probe could not complete
//   2  skip: only when no browser (Chrome) or no daemon is available
//
// Env (the release-smoke and e2e-pwa.sh runners set these):
//   TEST_TLS / TEST_HTTP    daemon origin; the first one that answers /api/health is used
//   TEST_TOKEN              daemon auth token
//   EVIDENCE_DIR            evidence root; reports go to <EVIDENCE_DIR>/shared-nav-mobile-fit
//   SHARED_NAV_GATE_THEMES  themes to gate, comma-separated (default light,dark)
//   DW_CHROME_PATH          Chrome binary (default /usr/bin/google-chrome, as the probe)

import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = path.dirname(fileURLToPath(import.meta.url));
const probe = path.join(dir, 'shared-nav-mobile-fit-probe.mjs');
const chrome = process.env.DW_CHROME_PATH || '/usr/bin/google-chrome';

function skip(reason) {
  console.log(`shared-nav-mobile-fit: ${reason}, skipped`);
  process.exit(2);
}

if (!existsSync(chrome)) skip(`no browser at ${chrome}`);

const origin = [process.env.TEST_TLS, process.env.TEST_HTTP]
  .filter(Boolean)
  .find((base) => spawnSync('curl', ['-skf', '--max-time', '5', '-o', '/dev/null', `${base}/api/health`]).status === 0);
if (!origin) skip('no daemon answers at TEST_TLS or TEST_HTTP');

const outDir = path.join(process.env.EVIDENCE_DIR || tmpdir(), 'shared-nav-mobile-fit');
mkdirSync(outDir, { recursive: true });

const res = spawnSync(process.execPath, [probe], {
  env: {
    ...process.env,
    TEST_TLS: origin,
    SHARED_NAV_MODE: 'gate',
    SHARED_NAV_GATE_THEMES: process.env.SHARED_NAV_GATE_THEMES || 'light,dark',
    PROBE_OUT_DIR: outDir,
    PROBE_CHROME_PROFILE: path.join(outDir, 'chrome-profile'),
  },
  stdio: 'inherit',
});

// The probe exits 2 for a bad environment (for example a missing token). That is a
// failure to run the check, not a missing browser or daemon, so it is not a skip.
process.exit(res.status === 0 ? 0 : 1);
