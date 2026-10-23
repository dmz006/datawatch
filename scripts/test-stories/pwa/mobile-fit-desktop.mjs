// scripts/test-stories/pwa/mobile-fit-desktop.mjs — desktop and tablet
// Observer layout baseline for the pre-fix (unfixed) PWA.
//
// Captures the bounding rect (x, y, width, height) of every tracked element
// in the Observer view at fixed desktop/tablet viewports (1280x800,
// 1920x1080, 768x1024), dark theme, 100% zoom, three-peers seed, and records
// it next to this file as `mobile-fit-desktop-baseline.json`.
//
// The comparator (`compare`) is a pure function on two baseline-shaped
// documents: for selectors that sit inside #settings-sec-stats it compares
// x, y, width and height; for every other tracked selector it compares x and
// width only. A value passes when it is within
//   max(tolerance.px, tolerance.pct/100 * |baseline value|)
// of the baseline value. A tracked selector that is absent from the current
// page is always a failure.
//
// Exports (consumed by TS-803.mjs): record, captureRects, compare,
// compareViewport, fieldsFor, allowFor, formatFailure, readBaseline,
// writeBaseline, the VIEWPORTS/STATE/THEME/ZOOM constants and the defaults.
//
// CLI (run from the pwa/ directory with TEST_HTTP/TEST_TOKEN set to a live
// daemon):
//   node mobile-fit-desktop.mjs --record     # capture + write the baseline
//   node mobile-fit-desktop.mjs --compare    # default; capture + compare vs baseline
//   node mobile-fit-desktop.mjs --self-test  # pure comparator self-test, no browser
//
// This file is a read-only measurement harness. It must not modify the
// tracked layout (and must not touch app.js/style.css at all).

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { launchBrowser, connectToPWA, env as libEnv } from './lib.mjs';
import { observerProvider } from './mobile-fit-observer.mjs';
import { clearSeed } from './mobile-fit-seeds.mjs';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

export const VIEWPORTS = [
  { width: 1280, height: 800 },
  { width: 1920, height: 1080 },
  { width: 768, height: 1024 },
];
export const vpKey = (vp) => `${vp.width}x${vp.height}`;

export const STATE = 'three-peers';
export const THEME = 'dark';
export const ZOOM = 100;

export const DEFAULT_TOLERANCE = Object.freeze({ px: 2, pct: 1 });

const HERE = dirname(fileURLToPath(import.meta.url));
export const BASELINE_PATH = join(HERE, 'mobile-fit-desktop-baseline.json');

// Extra settle on top of the observer provider's settle: the per-system
// grid bars animate their width over 0.3s (transition in app.js), and the
// card heights can still grow a beat after the last fetch resolves.
const SETTLE_EXTRA_MS = 800;

// ---------------------------------------------------------------------------
// Tracked selectors
// ---------------------------------------------------------------------------
// Fixed id blocks (all present in the unfixed observer view) plus dynamic
// families: every .settings-section, every .settings-section-title, the
// per-system grid and each direct card of it.

const FIXED_IDS = [
  'statsPanel',
  'ebpfStatusBlock',
  'ebpfNetworkBlock',
  'peerResourceBlock',
  'observerPeersBlock',
  'envelopesBlock',
  'backendHealthBlock',
  'commBackendsBlock',
  'settings-sec-membrowser',
  'memoryBrowserList',
];

// Selectors (or selector prefixes) that live inside #settings-sec-stats in
// the unfixed structure: those compare x, y, width AND height. Everything
// else compares x and width only. Derived from the known unfixed layout
// (stats is the first .settings-section in renderObserverView), so the
// decision stays deterministic and needs no browser.
const STATS_BLOCK_SELECTORS = new Set([
  '#statsPanel',
  '#ebpfStatusBlock',
  '#ebpfNetworkBlock',
  '#peerResourceBlock',
  '#observerPeersBlock',
  '#envelopesBlock',
  '#backendHealthBlock',
  '#commBackendsBlock',
]);
const statsSectionIndex = 0; // stats card is the first settings-section
const inStatsSection = (selector) =>
  selector === `.settings-section[${statsSectionIndex}]`
  || selector === `.settings-section-title[${statsSectionIndex}]`
  || selector.startsWith('#perSystemGrid')
  || STATS_BLOCK_SELECTORS.has(selector);

export function fieldsFor(selector) {
  return inStatsSection(selector) ? ['x', 'y', 'width', 'height'] : ['x', 'width'];
}

// ---------------------------------------------------------------------------
// Capture
// ---------------------------------------------------------------------------

// Runs in the page: collect { selector: {x, y, width, height} } rounded to a
// single decimal (sub-pixel stability across runs; well inside the tolerance
// band). Missing elements are simply absent from the map — the comparator
// treats "present in baseline, absent now" as a failure.
const CAPTURE = (fixedIds) => {
  const r1 = (v) => Math.round(Number(v) * 10) / 10;
  const rectOf = (el) => {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r1(r.x), y: r1(r.y), width: r1(r.width), height: r1(r.height) };
  };
  const out = {};
  const put = (key, el) => {
    const rect = rectOf(el);
    if (rect) out[key] = rect;
  };

  put('.settings-view', document.querySelector('.settings-view'));

  const sections = Array.from(document.querySelectorAll('.settings-section'));
  sections.forEach((el, i) => put('.settings-section[' + i + ']', el));

  const titles = Array.from(document.querySelectorAll('.settings-section-title'));
  titles.forEach((el, i) => put('.settings-section-title[' + i + ']', el));

  const grid = document.getElementById('perSystemGrid');
  put('#perSystemGrid', grid);

  let cards = [];
  if (grid) {
    const kids = Array.from(grid.children);
    // loadSystemStatsGrid wraps the cards in a single auto-fit grid div;
    // if there is one child, the cards are ITS children.
    cards = kids.length === 1 ? Array.from(kids[0].children) : kids;
  }
  cards.forEach((el, i) => put('#perSystemGrid>card[' + i + ']', el));

  for (const id of fixedIds) put('#' + id, document.getElementById(id));
  return out;
};

export function captureRects(page) {
  return page.evaluate(CAPTURE, FIXED_IDS);
}

// record(page, viewport) — seed + open + settle the observer view at the
// given viewport, then capture the tracked rects. Exports the one a story
// (TS-803) calls with the page it already owns.
export async function record(page, viewport = VIEWPORTS[0]) {
  const provider = observerProvider;
  await page.setViewportSize({ width: viewport.width, height: viewport.height });
  await provider.seed(page, STATE);
  try {
    await provider.open(page);
    await provider.settle(page, STATE, 'settled');
    await page.waitForTimeout(SETTLE_EXTRA_MS);
    const rects = await captureRects(page);
    return rects;
  } finally {
    try { await clearSeed(page); } catch { /* best effort */ }
  }
}

// ---------------------------------------------------------------------------
// Comparator (pure — no browser)
// ---------------------------------------------------------------------------

export function allowFor(baselineValue, tolerance = DEFAULT_TOLERANCE) {
  const base = Number(baselineValue) || 0;
  return Math.max(tolerance.px, Math.abs(base) * tolerance.pct / 100);
}

export function compareViewport(viewport, baselineRects, currentRects, tolerance = DEFAULT_TOLERANCE) {
  const failures = [];
  const nowRects = (currentRects && typeof currentRects === 'object') ? currentRects : {};
  for (const [selector, base] of Object.entries(baselineRects || {})) {
    if (!base || typeof base !== 'object') continue;
    const now = nowRects[selector];
    for (const field of fieldsFor(selector)) {
      const baseValue = base[field];
      if (baseValue == null) continue;
      const nowValue = (now && typeof now === 'object' && now[field] != null) ? now[field] : null;
      if (nowValue == null) {
        failures.push({ viewport, selector, field, baseline: baseValue, now: 'missing' });
        continue;
      }
      const diff = Math.abs(Number(nowValue) - baseValue);
      const allowed = allowFor(baseValue, tolerance);
      if (diff > allowed) {
        failures.push({ viewport, selector, field, baseline: baseValue, now: nowValue, allowed });
      }
    }
  }
  return failures;
}

const viewportsOf = (doc) =>
  (doc && typeof doc === 'object' && doc.viewports && typeof doc.viewports === 'object')
    ? doc.viewports
    : { default: (doc && typeof doc === 'object') ? doc : {} };

// compare(baselineDoc, currentDoc, tolerance) — docs are
//   { viewports: { '<w>x<h>': { '<selector>': {x,y,width,height} } } }
// or a bare rect map (treated as viewport 'default'). Returns the flat
// failure list; empty means everything tracked is within tolerance.
export function compare(baseline, current, tolerance = DEFAULT_TOLERANCE) {
  const baseVps = viewportsOf(baseline);
  const nowVps = viewportsOf(current);
  const failures = [];
  for (const [vp, baseRects] of Object.entries(baseVps)) {
    failures.push(...compareViewport(vp, baseRects, nowVps[vp], tolerance));
  }
  return failures;
}

export function formatFailure(f) {
  return [String(f.viewport), String(f.selector), String(f.field),
          String(f.baseline), '->', String(f.now)].join(' ');
}

// ---------------------------------------------------------------------------
// Baseline I/O
// ---------------------------------------------------------------------------

export function readBaseline(path = BASELINE_PATH) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

export function writeBaseline(doc, path = BASELINE_PATH) {
  writeFileSync(path, JSON.stringify(doc, null, 2) + '\n', 'utf8');
}

// ---------------------------------------------------------------------------
// Self-test (pure comparator, no browser)
// ---------------------------------------------------------------------------
// Exits 0 IFF: (a) values drifting within tolerance pass, and (b) values
// shifted by 5% fail for both a stats-section element and a non-stats one.
function runSelfTest() {
  const tolerance = { px: 2, pct: 1 };
  const base = {
    '.settings-view': { x: 0, y: 24, width: 1080, height: 1600 },
    '.settings-section[0]': { x: 12, y: 40, width: 1056, height: 900 },
    '.settings-section-title[0]': { x: 24, y: 52, width: 1032, height: 20 },
    '#perSystemGrid': { x: 20, y: 80, width: 1040, height: 300 },
    '#perSystemGrid>card[0]': { x: 28, y: 96, width: 510, height: 120 },
    '#statsPanel': { x: 20, y: 380, width: 1040, height: 200 },
    '#settings-sec-membrowser': { x: 12, y: 1200, width: 1056, height: 300 },
    '#memoryBrowserList': { x: 16, y: 1350, width: 1048, height: 80 },
  };

  // (a) within-tolerance drift: +1 px on every field.
  const within = {};
  for (const [k, v] of Object.entries(base)) {
    within[k] = { x: v.x + 1, y: v.y + 1, width: v.width + 1, height: v.height + 1 };
  }
  // (b) 5% shifts (well outside max(2px, 1%) for these magnitudes).
  const shifted = JSON.parse(JSON.stringify(base));
  shifted['#statsPanel'].width = +(base['#statsPanel'].width * 1.05).toFixed(1);
  shifted['#memoryBrowserList'].width = +(base['#memoryBrowserList'].width * 1.05).toFixed(1);

  const withinFailures = compare({ viewports: { st: base } }, { viewports: { st: within } }, tolerance);
  const shiftFailures = compare({ viewports: { st: base } }, { viewports: { st: shifted } }, tolerance);

  const hasStatsWidth = shiftFailures.some(
    (f) => f.selector === '#statsPanel' && f.field === 'width');
  const hasNonStatsWidth = shiftFailures.some(
    (f) => f.selector === '#memoryBrowserList' && f.field === 'width');

  console.log('mobile-fit-desktop self-test (pure comparator)');
  console.log('  within-tolerance drift: ' + (withinFailures.length === 0 ? 'PASS (0 failures)' : 'FAIL'));
  for (const f of withinFailures) console.log('    ' + formatFailure(f));
  console.log('  5% shift stats-section width:   '
    + (hasStatsWidth ? 'PASS (flagged: ' + formatFailure(shiftFailures.find(f => f.selector === '#statsPanel')) + ')' : 'FAIL'));
  console.log('  5% shift non-stats width:       '
    + (hasNonStatsWidth ? 'PASS (flagged: ' + formatFailure(shiftFailures.find(f => f.selector === '#memoryBrowserList')) + ')' : 'FAIL'));

  const ok = withinFailures.length === 0 && hasStatsWidth && hasNonStatsWidth;
  console.log(ok ? '  RESULT: OK' : '  RESULT: BROKEN');
  return ok;
}

// ---------------------------------------------------------------------------
// CLI: --record / --compare / --self-test
// ---------------------------------------------------------------------------

function baselineFromViewports(viewports) {
  return {
    tolerance: { ...DEFAULT_TOLERANCE },
    state: STATE,
    theme: THEME,
    zoom: ZOOM,
    viewports,
  };
}

async function collectViewports() {
  const http = process.env.TEST_HTTP || libEnv.http;
  const token = process.env.TEST_TOKEN || libEnv.token;
  if (!token) throw new Error('TEST_TOKEN is required (run against a live daemon)');
  const viewports = {};
  const browser = await launchBrowser();
  try {
    for (const vp of VIEWPORTS) {
      const context = await browser.newContext({
        ignoreHTTPSErrors: true,
        viewport: { width: vp.width, height: vp.height },
        deviceScaleFactor: 1, // 100% zoom
      });
      await context.addInitScript(([t]) => {
        try { localStorage.setItem('cs_theme', t); } catch { /* ignore */ }
      }, THEME); // dark theme from first paint
      const page = await context.newPage();
      try {
        await connectToPWA(page);
        viewports[vpKey(vp)] = await record(page, vp);
      } finally {
        await context.close();
      }
    }
  } finally {
    await browser.close();
  }
  return { http, viewports };
}

function assertBaselineShape(viewports) {
  for (const [key, rects] of Object.entries(viewports)) {
    const n = Object.keys(rects).length;
    if (n < 8) throw new Error(`viewport ${key}: only ${n} tracked selectors (need >= 8)`);
    if (!(rects && '#perSystemGrid' in rects)) {
      throw new Error(`viewport ${key}: #perSystemGrid not captured`);
    }
    console.log(`  ${key}: ${n} tracked selectors, ${rects['#perSystemGrid'].width.toFixed(1)}px wide grid`);
  }
}

async function runRecord() {
  console.log('mobile-fit-desktop: recording baseline ('
    + `seed=${STATE} theme=${THEME} zoom=${ZOOM})`);
  const { http, viewports } = await collectViewports();
  assertBaselineShape(viewports);
  writeBaseline(baselineFromViewports(viewports));
  console.log(`wrote ${BASELINE_PATH} (daemon ${http}, tolerance `
    + `${DEFAULT_TOLERANCE.px}px/${DEFAULT_TOLERANCE.pct}%)`);
}

async function runCompare() {
  console.log('mobile-fit-desktop: comparing current layout vs baseline');
  const { viewports: now } = await collectViewports();
  const baseline = readBaseline(BASELINE_PATH);
  const tolerance = baseline.tolerance && typeof baseline.tolerance === 'object'
    ? baseline.tolerance : DEFAULT_TOLERANCE;
  const failures = compare(baseline, { viewports: now }, tolerance);
  for (const f of failures) console.log('  FAIL  ' + formatFailure(f));
  if (failures.length) {
    console.log(`RESULT: FAIL — ${failures.length} value(s) outside tolerance`);
    return 1;
  }
  console.log(`RESULT: PASS — all tracked values within ${tolerance.px}px / ${tolerance.pct}%`);
  return 0;
}

async function main() {
  const mode = process.argv[2] || '--compare';
  if (mode === '--record') { await runRecord(); return 0; }
  if (mode === '--compare') return runCompare();
  if (mode === '--self-test') return runSelfTest() ? 0 : 1;
  console.error('usage: mobile-fit-desktop.mjs [--record | --compare | --self-test]');
  return 2;
}

const IS_MAIN = (() => {
  try {
    if (!process.argv[1]) return false;
    const base = (p) => p.split(/[\\/]/).filter(Boolean).pop();
    return base(process.argv[1]) === base(fileURLToPath(import.meta.url));
  } catch { return false; }
})();

if (IS_MAIN) {
  main()
    .then((code) => process.exit(typeof code === 'number' ? code : 0))
    .catch((err) => {
      console.error('mobile-fit-desktop: ' + (err && err.message ? err.message : err));
      process.exit(1);
    });
}
