#!/usr/bin/env node
/**
 * TS-832 — Usage calculator reset-zone row in Settings, on a phone: the input shows the
 * operator's zone, and when the zone is unset it stays empty with the daemon's
 * effective zone as placeholder and the "server zone" note — so the operator can tell
 * a zone they chose from one the daemon picked. Pinning the zone back and forth
 * via the API re-renders the card honestly in both directions.
 * tags: surface:pwa feature:usage-calculator group:usage
 *
 * Runs at a phone viewport (390x844). The Usage calculator settings card sits in
 * Settings, General (the card TS-819 reads its fields from); this story only drives
 * the reset-zone row and the contract behind it (GET /api/config's
 * reset_zone / reset_zone_source / reset_zone_fell_back).
 *
 * Flow: read the card's baseline row, then round-trip the zone through the API —
 *   1. baseline: the input value matches the stored zone; a server-sourced zone
 *      renders empty with the effective zone as placeholder and the server-zone note;
 *      an operator-configured zone renders its value without the note;
 *   2. pin "Europe/Paris" via PUT /api/config; reload; the row shows Paris, no note;
 *   3. unset the zone (empty PUT); reload; the row is empty again, the placeholder
 *      matches the daemon's effective zone from GET /api/config, the note is back;
 *   4. restore the baseline value; the daemon is left exactly as found.
 * The original zone is always restored (the story must not pin the daemon's zone).
 *
 * Needs a sandbox daemon (never the production daemon, never ~/.datawatch); see
 * scripts/e2e-pwa.sh. Skips (exit 2) when Chrome is unavailable or the daemon has no
 * usage endpoints.
 */

import { screenshot, saveLog, env } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const ZONE_KEY = 'usage.calculator.reset_zone';
const NOTE_KEY = 'settings_usage_calculator_reset_zone_server_note';
const CARD = '#gcfg_usage_calculator';

async function api(pathAndQuery, init = {}) {
  const r = await fetch(env.http + pathAndQuery, {
    ...init,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json', ...(init.headers || {}) },
  });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}

const setZone = (zone) => api('/api/config', { method: 'PUT', body: JSON.stringify({ [ZONE_KEY]: zone }) });
const getCalc = async () => ((await api('/api/config')).body || {}).usage?.calculator || {};

// The reset-zone row as the card shows it: control value, placeholder, and the
// per-row note elements (their text, so the note and the hint can be told apart).
const zoneRow = (page) => page.evaluate((card) => {
  const rows = [...document.querySelectorAll(`${card} .settings-row`)];
  const row = rows.find((r) => (r.textContent || '').toLowerCase().includes('time zone'));
  if (!row) return null;
  const input = row.querySelector('input');
  return {
    label: (row.querySelector('.settings-label')?.textContent || '').replace(/\s+/g, ' ').trim(),
    value: input ? input.value : '',
    placeholder: input ? (input.getAttribute('placeholder') || '') : '',
    notes: [...row.querySelectorAll('div')]
      .filter((d) => d.querySelector('input, select') === null && trim(d.textContent).length > 0)
      .map((d) => trim(d.textContent)),
  };
  function trim(s) { return String(s || '').replace(/\s+/g, ' ').trim(); }
}, CARD);

// Does the rendered note carry exactly the bundle's server-zone string?
const noteShown = (page) => page.evaluate((args) => {
  const want = (window._i18n && window._i18n.bundle && window._i18n.bundle[args.noteKey]) || '';
  if (!want) return false;
  return [...document.querySelectorAll(`${args.card} .settings-row`)]
    .some((row) => {
      if (!(row.textContent || '').toLowerCase().includes('time zone')) return false;
      return [...row.querySelectorAll('div')].some((d) => String(d.textContent).replace(/\s+/g, ' ').trim() === want);
    });
}, { card: CARD, noteKey: NOTE_KEY });

await runPerfStory(async (page) => {
  const probe = await api('/api/config');
  if (probe.status === 404) await skipStory('the daemon has no /api/config; start a sandbox daemon built from this tree');
  check(probe.status === 200, `GET /api/config -> ${probe.status}`);
  const calc0 = (probe.body && (probe.body.usage || {}).calculator) || {};
  if (!('reset_zone' in calc0 && 'reset_zone_source' in calc0)) await skipStory('the daemon predates the reset-zone contract; rebuild it from this tree');

  const baseline = { zone: (calc0.reset_zone || '').replace(/^"|"$/g, ''), source: calc0.reset_zone_source };
  // Normalize: config GET's reset_zone already resolves to the effective zone string.
  const baselineStored = baseline.source === 'server' ? '' : baseline.zone;

  const browser = page.context().browser();
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true });
  const p = await context.newPage();
  const consoleErrors = [];
  p.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));

  const evidence = { viewport: PHONE, baseline };
  try {
    await openPWA(p, { timeout: 60000 });
    await p.evaluate(() => { try { localStorage.setItem('cs_theme', 'dark'); } catch { /* ignore */ } });
    const openSettings = async () => {
      await p.evaluate(() => window.navigate('settings'));
      await p.locator('.settings-tab-btn[data-tab="general"]').click({ timeout: 30000 });
      await p.waitForSelector(`${CARD} .settings-row`, { state: 'attached', timeout: 30000 });
      await p.locator(CARD).scrollIntoViewIfNeeded();
    };

    // --- 1. baseline: the row mirrors the stored zone ----------------------------------------
    await setZone(baselineStored);         // ensure the card and the API agree on the baseline
    await openSettings();
    let row = await zoneRow(p);
    const calcNow = await getCalc();
    check(!!row, 'the settings card has a reset-zone row (label mentions "time zone")');
    check(row.label.length > 0, 'the reset-zone row has a label');
    if (baselineStored) {
      check(row.value === baselineStored, `a configured zone shows its value (want ${baselineStored}, saw "${row.value}")`);
      check(!(await noteShown(p)), 'a configured zone carries no server-zone note');
    } else {
      check(row.value === '', 'an unset zone renders empty (nothing is pinned in the config)');
      check(!!calcNow.reset_zone && row.placeholder === calcNow.reset_zone,
        `the placeholder shows the daemon's effective zone (${calcNow.reset_zone}), saw "${row.placeholder}"`);
      check(await noteShown(p), 'the server-zone note is rendered from the locale bundle');
    }
    evidence.baseline = row;
    await screenshot(p, '01-baseline');

    // --- 2. pin an explicit zone: the row shows it, the note is gone ---------------------------
    await setZone('Europe/Paris');
    await p.reload({ waitUntil: 'domcontentloaded' }).catch(() => {});
    await openSettings();
    row = await zoneRow(p);
    check(row.value === 'Europe/Paris', `an explicit zone renders its value, saw "${row.value}"`);
    check(!(await noteShown(p)), 'an explicit zone carries no server-zone note');
    const calcParis = await getCalc();
    check(calcParis.reset_zone === 'Europe/Paris' && calcParis.reset_zone_source === 'configured',
      'GET /api/config marks the pinned zone as configured');
    evidence.pinned = { row, calc: calcParis };
    await screenshot(p, '02-pinned-paris');

    // --- 3. unset the zone: empty input, effective zone as placeholder, note back -------------
    await setZone('');
    await p.reload({ waitUntil: 'domcontentloaded' }).catch(() => {});
    await openSettings();
    row = await zoneRow(p);
    check(row.value === '', 'the unset zone renders empty again');
    const calcSrv = await getCalc();
    check(calcSrv.reset_zone_source === 'server', `GET /api/config reports the effective zone as server-sourced (saw ${calcSrv.reset_zone_source})`);
    check(!!calcSrv.reset_zone && row.placeholder === calcSrv.reset_zone,
      `the placeholder matches the server's effective zone (${calcSrv.reset_zone}), saw "${row.placeholder}"`);
    check(await noteShown(p), 'the server-zone note is back, rendered from the locale bundle');
    evidence.unset = { row, calc: calcSrv };
    await screenshot(p, '03-unset-server-zone');
  } finally {
    await setZone(baselineStored).catch(() => {});   // leave the daemon exactly as found
    await saveJson('usage-calculator-zone-evidence', evidence).catch(() => {});
    if (consoleErrors.length) await saveLog('console-errors', consoleErrors.join('\n')).catch(() => {});
    await context.close().catch(() => {});
  }
  check(consoleErrors.length === 0, `page errors: ${consoleErrors.join(' | ')}`);
});
