// scripts/test-stories/pwa/mobile-fit-selftest.mjs
//
// Proves that the mobile-fit detectors in mobile-fit-lib.mjs find exactly
// what they claim on known-good and known-bad pages, and raise no false
// alarms. Self-contained: no daemon, no PWA, no network — each fixture is a
// local static HTML file opened through a file: URL at a 360 by 740
// viewport, plus one inline page that re-states the earlier expectation that
// a 600px wide div next to a 20px button at 360px width still yields
// page-scroll, edge-cross and tap-target findings.
//
// Prints one PASS/FAIL line per scenario and exits 0 only if every scenario
// passes.

import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { launchBrowser } from './lib.mjs';
import { collectFindings } from './mobile-fit-lib.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureDir = path.join(here, 'mobile-fit-fixtures');

const VIEWPORT = { width: 360, height: 740 };

// Each scenario: [name, htmlFileOrInlineHTML (null = fixture file <name>.html),
// expectedCheckNames[] (empty array = page must yield zero findings)]
const SCENARIOS = [
  ['clean', 'clean.html', []],
  ['page-scroll', 'page-scroll.html', ['edge-cross', 'page-scroll']],
  ['edge-cross', 'edge-cross.html', ['edge-cross']],
  ['clipped-text', 'clipped-text.html', ['clipped-text']],
  ['tap-target', 'tap-target.html', ['tap-target']],
  ['overlap', 'overlap.html', ['overlap']],
  ['ellipsis-with-tooltip', 'ellipsis-with-tooltip.html', []],
  ['own-scroll-container', 'own-scroll-container.html', []],
  // Earlier inline expectation, kept true as a regression guard:
  ['inline-wide-block-busy-button',
    '<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" ' +
    'content="width=device-width, initial-scale=1"></head><body>' +
    '<div style="width:600px;background:#3462e0;color:#fff;padding:16px;">' +
    'wide block</div>' +
    '<button style="width:20px;height:20px;padding:0;">b</button>' +
    '</body></html>',
    ['edge-cross', 'page-scroll', 'tap-target']],
];

function sameSet(gotArray, wantArray) {
  const got = new Set(gotArray);
  return got.size === wantArray.length && wantArray.every((c) => got.has(c));
}

async function main() {
  const browser = await launchBrowser({ headless: true });
  let failures = 0;
  try {
    for (const [name, source, expected] of SCENARIOS) {
      const page = await browser.newPage({ viewport: VIEWPORT });
      try {
        let url = null;
        if (source.endsWith('.html')) {
          url = pathToFileURL(path.join(fixtureDir, source)).href;
          await page.goto(url, { waitUntil: 'load' });
        } else {
          await page.setContent(source, { waitUntil: 'load' });
        }
        const findings = await collectFindings(page);
        const got = findings.map((f) => f.check);
        const ok = sameSet(got, expected);
        if (ok) {
          console.log(`PASS ${name}: expected [${expected.join(', ')}], got [${got.join(', ')}]`);
        } else {
          failures += 1;
          console.log(`FAIL ${name}: expected [${expected.join(', ')}], got [${got.join(', ')}]`);
          for (const f of findings) {
            console.log(`  - ${f.check} | ${f.selector} | ${f.detail}`);
          }
        }
      } catch (err) {
        failures += 1;
        console.log(`FAIL ${name}: ${err && err.message ? err.message : String(err)}`);
      } finally {
        await page.close().catch(() => {});
      }
    }
  } finally {
    await browser.close().catch(() => {});
  }
  if (failures > 0) {
    console.log(`${failures} scenario(s) failed`);
    process.exit(1);
  }
  console.log(`all ${SCENARIOS.length} scenarios passed`);
}

main().then(() => {}, (err) => {
  console.error('mobile-fit-selftest crashed:', err && err.stack ? err.stack : err);
  process.exit(1);
});
