#!/usr/bin/env node
/**
 * TS-818 — Desktop and tablet Observer layout regression guard (pre-fix baseline)
 * tags: surface:pwa feature:pwa-mobile-fit
 *
 * Flow: runStory owns the browser; connect the PWA; for each desktop/tablet
 *       viewport (1280x800, 1920x1080, 768x1024) the mobile-fit-desktop
 *       module seeds the three-peers state, opens and settles the Observer
 *       view (dark theme, 100% zoom) and captures the bounding rects of the
 *       tracked elements; compare() checks every tracked value against
 *       mobile-fit-desktop-baseline.json and a mobile-fit-report-style text
 *       is written into the story evidence dir.
 *
 * Assertions:
 *   - all three viewports are captured with at least 8 tracked selectors each
 *     (including #perSystemGrid)
 *   - every tracked value is within max(2px, 1% of the baseline value) of the
 *     baseline recorded for the unfixed layout; a tracked selector that is
 *     absent from the page fails the story
 *
 * The mobile-fit-desktop module and the baseline it records are read-only
 * measurement harnesses; this story changes no tracked layout.
 */

import { runStory, connectToPWA, saveLog } from './lib.mjs';
import {
  VIEWPORTS, vpKey, STATE, THEME, ZOOM,
  record, compare, formatFailure, readBaseline, BASELINE_PATH,
} from './mobile-fit-desktop.mjs';

await runStory(async (page) => {
  const baseline = readBaseline(BASELINE_PATH);
  if (!baseline || !baseline.viewports) throw new Error('baseline viewports missing');
  const tolerance = baseline.tolerance && typeof baseline.tolerance === 'object'
    ? baseline.tolerance
    : { px: 2, pct: 1 };

  // Dark theme for the whole story.
  await page.evaluate(() => {
    try { localStorage.setItem('cs_theme', 'dark'); } catch { /* ignore */ }
  }).catch(() => {});

  await connectToPWA(page);
  await page.waitForTimeout(300);

  const now = { viewports: {} };
  const perVp = [];
  for (const vp of VIEWPORTS) {
    const key = vpKey(vp);
    const rects = await record(page, vp);
    now.viewports[key] = rects;
    const n = Object.keys(rects).length;
    if (n < 8) throw new Error(`viewport ${key}: only ${n} tracked selectors captured (need >= 8)`);
    if (!rects['#perSystemGrid']) throw new Error(`viewport ${key}: #perSystemGrid not captured`);
    perVp.push({ key, n, grid: rects['#perSystemGrid'] });
    console.log(`[TS-818] ${key}: ${n} tracked selectors; grid ${rects['#perSystemGrid'].width.toFixed(1)}px wide`);
  }

  const failures = compare(baseline, now, tolerance);

  // mobile-fit-report-style text listing -> story evidence dir
  const lines = [];
  lines.push('mobile-fit desktop/tablet baseline report');
  lines.push('generated_at: ' + new Date().toISOString());
  lines.push(`meta.story: TS-818`);
  lines.push(`meta.seed: ${STATE}`);
  lines.push(`meta.view: observer`);
  lines.push(`meta.theme: ${THEME}`);
  lines.push(`meta.zoom: ${ZOOM}`);
  lines.push(`meta.tolerance: ${tolerance.px}px | ${tolerance.pct}% of baseline`);
  lines.push('');
  lines.push(`totals: ${failures.length} out-of-tolerance value(s) across ${perVp.length} viewport(s)`);
  for (const entry of perVp) {
    const vpFailures = failures.filter((f) => f.viewport === entry.key).length;
    const status = vpFailures === 0 ? 'PASS' : 'FAIL';
    lines.push(`  ${entry.key}: ${status} (${entry.n} tracked selectors, ${vpFailures} outside tolerance)`);
  }
  if (failures.length) {
    lines.push('');
    lines.push('failures:');
    for (const f of failures) {
      lines.push(`  [${f.viewport}] ${f.selector} ${f.field} ${f.baseline} -> ${f.now}`
        + (f.allowed != null ? ` (allowed ${f.allowed})` : ''));
    }
  }
  lines.push('');
  lines.push(failures.length === 0
    ? 'result: PASS - all tracked values within tolerance of the unfixed-layout baseline'
    : `result: FAIL - ${failures.length} tracked value(s) outside tolerance`);
  const text = lines.join('\n') + '\n';
  for (const line of lines) console.log(line);
  await saveLog('desktop-baseline-report', text);
  console.log(`[TS-818] baseline: ${BASELINE_PATH}`);

  if (failures.length > 0) {
    throw new Error('Observer desktop/tablet layout drifted outside tolerance: '
      + failures.map(formatFailure).join(' | '));
  }
  console.log(`[TS-818] all ${Object.keys(now.viewports).length} viewports within tolerance`);
});
