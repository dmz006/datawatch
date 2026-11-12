#!/usr/bin/env node
/**
 * TS-819 — Usage calculator on a phone: verdict chip, three variants with the same numbers,
 * a policy change that moves only the advice, extra credit that moves the ceiling, and the
 * settings card
 * tags: surface:pwa feature:usage-calculator group:usage
 *
 * Runs at a phone viewport (390x844). The calculator card sits on the Automata tab, in the
 * Automation sub-tab (the capacity card's neighbour); its settings card sits in Settings,
 * General. Settings has no Monitor tab any more (its cards moved to the Observer view), so
 * the story takes the calculator's real place in the app.
 *
 * Flow: seed an earlier reading through the API so the recent burn has a previous reading,
 * open the card, add a reading through the form, then
 *   1. the verdict chip reads "Within budget" or "Over budget" and agrees with the response
 *      body the page drew;
 *   2. switch between the three variants; the nine shared reading fields, the verdict chips
 *      and the headline are the same text in all three, and equal the response body;
 *   3. choose each policy in turn; the reading numbers do not move and only the policy
 *      line, the headline and the notes do;
 *   4. save extra credit; the ceiling line reads 100 plus the extra points;
 *   5. open Settings, General and read the Usage calculator settings card.
 * Readings and extra credit the story added are removed again at the end.
 *
 * Needs a sandbox daemon (never the production daemon, never ~/.datawatch); see
 * scripts/e2e-pwa.sh. Skips (exit 2) when Chrome is unavailable or the daemon has no
 * usage endpoints.
 */

import { screenshot, saveLog, env } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const VARIANTS = ['pace-gauge', 'lane-planner', 'burn-ledger'];
const VARIANT_BODY = { 'pace-gauge': '.usage-calc-pace-gauge', 'lane-planner': '.usage-calc-lane-planner', 'burn-ledger': '.usage-calc-burn-ledger' };
const POLICIES = ['use-all', 'pace', 'finish-early'];
const POLICY_LABEL = { 'use-all': 'Run until used up', 'pace': 'Pace to the week', 'finish-early': 'Run flat out' };
const CARD = '#automationUsageCard';
const EXTRA_POINTS = 10;
// Reading fields 0..5 (reading, days, pace line, burns, projected end, run out) are numbers of the
// reading; field 6 holds the verdicts, field 7 the lanes and policy, field 8 the advice.
const NUMBER_FIELDS = [0, 1, 2, 3, 4, 5, 6];

async function api(pathAndQuery, init = {}) {
  const r = await fetch(env.http + pathAndQuery, {
    ...init,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json', ...(init.headers || {}) },
  });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}

const squash = (s) => String(s || '').replace(/\s+/g, ' ').trim();

// The nine reading fields as the card shows them, in order, as text.
const readFields = (page) => page.evaluate((card) => {
  const rows = [...document.querySelectorAll(`${card} [data-field]`)];
  return rows.sort((a, b) => Number(a.dataset.field) - Number(b.dataset.field))
    .map((el) => el.textContent.replace(/\s+/g, ' ').trim());
}, CARD);

const readVerdictCard = (page) => page.evaluate((card) => {
  const text = (sel) => (document.querySelector(`${card} ${sel}`)?.textContent || '').replace(/\s+/g, ' ').trim();
  const chips = [...document.querySelectorAll(`${card} .usage-calc-chips .usage-calc-chip`)]
    .map((el) => ({ cls: el.classList.contains('over') ? 'over' : 'within', label: el.textContent.trim() }));
  return {
    chips,
    headline: text('.usage-calc-headline'),
    notes: [...document.querySelectorAll(`${card} .usage-calc-notes li`)].map((li) => li.textContent.replace(/\s+/g, ' ').trim()),
  };
}, CARD);

// The response body the page drew last (window._usageCalc.body is the card's own state).
const drawnBody = (page) => page.evaluate(() => JSON.parse(JSON.stringify(window._usageCalc.body)));

const num = (v, d = 1) => Number(v).toFixed(d);

// The ids of the readings the daemon holds now.
const readingIds = async () => new Set((((await api('/api/usage/readings')).body || {}).readings || []).map((r) => r.id));

async function waitForVerdict(page) {
  await page.waitForSelector(`${CARD} .usage-calc-verdict`, { state: 'visible', timeout: 30000 });
}

async function showVariant(page, id) {
  const index = VARIANTS.indexOf(id);
  const buttons = page.locator(`${CARD} .usage-calc-switch`);
  check((await buttons.count()) === VARIANTS.length, `the switcher shows ${VARIANTS.length} variants (setting "all")`);
  await buttons.nth(index).click();
  await page.waitForSelector(`${CARD} ${VARIANT_BODY[id]}`, { state: 'attached', timeout: 10000 });
}

async function choosePolicy(page, id) {
  const advice = page.waitForResponse((r) => r.url().includes('/api/usage/advice') && r.url().includes(`policy=${id}`), { timeout: 20000 });
  await page.locator(`${CARD} input[name="usageCalcPolicy"][data-policy="${id}"]`).click();
  await advice;
  await page.waitForFunction((arg) => {
    const radio = document.querySelector(`${arg.card} input[name="usageCalcPolicy"][data-policy="${arg.id}"]`);
    return radio && radio.checked && !!document.querySelector(`${arg.card} .usage-calc-verdict`);
  }, { card: CARD, id }, { timeout: 20000 });
}

await runPerfStory(async (page) => {
  const probe = await api('/api/usage/readings');
  if (probe.status === 404) await skipStory('the daemon has no /api/usage/readings; start a sandbox daemon built from this tree');
  check(probe.status === 200, `GET /api/usage/readings -> ${probe.status}`);

  const browser = page.context().browser();
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true });
  const p = await context.newPage();
  const consoleErrors = [];
  p.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));

  const addedReadings = [];
  const evidence = { viewport: PHONE };
  try {
    // An earlier reading, so the recent burn has a previous reading to work from.
    const idsBeforeSeed = await readingIds();
    const before = new Date(Date.now() - 3 * 3600 * 1000).toISOString();
    const seeded = await api('/api/usage/readings', { method: 'POST', body: JSON.stringify({ percent: 40, lanes: 2, source: 'typed', at: before }) });
    check(seeded.status === 200 || seeded.status === 201, `seeding an earlier reading -> ${seeded.status}`);
    for (const id of await readingIds()) if (!idsBeforeSeed.has(id)) addedReadings.push(id);

    await openPWA(p, { timeout: 60000 });
    await p.evaluate(() => { try { localStorage.setItem('cs_theme', 'dark'); } catch { /* ignore */ } });

    // Open the Automata tab, Automation sub-tab, and wait for the usage card to load.
    await p.evaluate(() => {
      window.navigate('autonomous');
      const btn = document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]');
      if (btn && !btn.classList.contains('active')) btn.click();
    });
    await p.waitForSelector(`${CARD} .usage-calc-title`, { state: 'visible', timeout: 60000 });
    await p.locator(CARD).scrollIntoViewIfNeeded();
    await screenshot(p, '01-card-opened');

    // --- add a reading through the form ------------------------------------------------------
    const idsBeforeForm = await readingIds();
    await p.fill(`${CARD} #usageCalcPercent`, '52');
    const saved = p.waitForResponse((r) => r.url().includes('/api/usage/readings') && r.request().method() === 'POST', { timeout: 20000 });
    await p.locator(`${CARD} button[onclick="usageCalcSubmitReading()"]`).click();
    check((await saved).ok(), 'saving the reading through the form');
    await waitForVerdict(p);
    const fromForm = [...await readingIds()].filter((id) => !idsBeforeForm.has(id));
    check(fromForm.length === 1, `the form added one reading, saw ${fromForm.length}`);
    addedReadings.push(...fromForm);

    // --- 1. the verdict chip ------------------------------------------------------------------
    let body = await drawnBody(p);
    check(!body.advice.needs, 'the advice has a reading to work from');
    check(body.advice.percent === 52, `the card drew the saved reading (${body.advice.percent})`);
    let card = await readVerdictCard(p);
    check(card.chips.length === 2, `two verdict chips (recent burn and week average), saw ${card.chips.length}`);
    const wording = { over: 'Over budget', within: 'Within budget' };
    check(card.chips[0].label === wording[body.advice.verdict], `recent-burn chip "${card.chips[0].label}" matches verdict ${body.advice.verdict}`);
    check(card.chips[1].label === wording[body.advice.verdict_avg], `week-average chip "${card.chips[1].label}" matches verdict ${body.advice.verdict_avg}`);
    check(card.headline === squash(body.advice.advice.headline), 'the headline is the response headline');
    check((await noHorizontalOverflow(p)).ok, 'no horizontal overflow at 390 px after adding a reading');
    evidence.verdict = { chips: card.chips, headline: card.headline };
    await screenshot(p, '02-verdict-chip');

    // --- 2. the three variants show the same numbers -----------------------------------------
    const shared = {};
    for (const id of VARIANTS) {
      await showVariant(p, id);
      shared[id] = { fields: await readFields(p), card: await readVerdictCard(p) };
      check(shared[id].fields.length === 9, `${id}: nine reading fields, saw ${shared[id].fields.length}`);
      const o = await noHorizontalOverflow(p);
      check(o.ok, `${id}: no horizontal overflow at 390 px (scrollWidth ${o.scrollWidth})`);
      await screenshot(p, `03-variant-${id}`);
    }
    for (const id of VARIANTS.slice(1)) {
      check(JSON.stringify(shared[id].fields) === JSON.stringify(shared[VARIANTS[0]].fields),
        `${id} shows different shared fields than ${VARIANTS[0]}:\n${shared[id].fields.join('\n')}\n--\n${shared[VARIANTS[0]].fields.join('\n')}`);
      check(JSON.stringify(shared[id].card) === JSON.stringify(shared[VARIANTS[0]].card), `${id} shows a different verdict card than ${VARIANTS[0]}`);
    }
    const f = shared[VARIANTS[0]].fields;
    const a = body.advice;
    check(f[0].includes(`${num(a.percent, 0)}%`), `the percent field shows ${a.percent}`);
    check(f[1].includes(num(a.days_since_reset, 2)), `the days field shows ${num(a.days_since_reset, 2)}`);
    check(f[2].includes(`${num(a.pace_line)}%`), `the pace line field shows ${num(a.pace_line)}%`);
    check(f[3].includes(num(a.recent_burn_per_24h)) && f[3].includes(num(a.week_avg_burn_per_24h)), 'the burn field shows both burns');
    check(f[4].includes(`${num(a.projected_end_recent)}%`) && f[4].includes(`${num(a.projected_end_avg)}%`), 'the projected-end field shows both ends');
    check(f[7].includes(String(a.lanes)), 'the lanes field shows the lane count');
    check(f[8].endsWith(squash(a.advice.headline)), 'the advice field ends with the response headline');
    evidence.shared = shared[VARIANTS[0]];

    // --- 3. the policy changes only the advice ----------------------------------------------
    await showVariant(p, 'pace-gauge');
    const byPolicy = {};
    for (const id of POLICIES) {
      await choosePolicy(p, id);
      const drawn = await drawnBody(p);
      check(drawn.policy === id, `the response carries policy ${id}`);
      byPolicy[id] = { fields: await readFields(p), card: await readVerdictCard(p), lanesSuggested: drawn.advice.advice.lanes_suggested };
      check(byPolicy[id].lanesSuggested >= 1, `${id}: the advice names at least one lane`);
      check(byPolicy[id].fields[7].includes(POLICY_LABEL[id]), `${id}: the lanes line names the policy "${POLICY_LABEL[id]}"`);
      await screenshot(p, `04-policy-${id}`);
    }
    for (const id of POLICIES.slice(1)) {
      for (const n of NUMBER_FIELDS) {
        check(byPolicy[id].fields[n] === byPolicy[POLICIES[0]].fields[n], `policy ${id} moved reading field ${n}: "${byPolicy[POLICIES[0]].fields[n]}" -> "${byPolicy[id].fields[n]}"`);
      }
      check(JSON.stringify(byPolicy[id].card.chips) === JSON.stringify(byPolicy[POLICIES[0]].card.chips), `policy ${id} moved a verdict chip`);
    }
    const signature = (id) => `${byPolicy[id].card.headline} | ${byPolicy[id].card.notes.join(' | ')}`;
    check(new Set(POLICIES.map(signature)).size === POLICIES.length, `each policy has its own advice text:\n${POLICIES.map(signature).join('\n')}`);
    evidence.policies = Object.fromEntries(POLICIES.map((id) => [id, { headline: byPolicy[id].card.headline, notes: byPolicy[id].card.notes, lanesSuggested: byPolicy[id].lanesSuggested }]));

    // --- 4. extra credit moves the ceiling -----------------------------------------------------
    await choosePolicy(p, 'pace');
    await p.fill(`${CARD} #usageCalcExtra`, String(EXTRA_POINTS));
    const extra = p.waitForResponse((r) => r.url().includes('/api/usage/extra-credit') && r.request().method() === 'PUT', { timeout: 20000 });
    await p.locator(`${CARD} button[onclick="usageCalcSaveExtraCredit()"]`).click();
    check((await extra).ok(), 'saving extra credit');
    const ceiling = 100 + EXTRA_POINTS;
    await p.waitForFunction((arg) => {
      const el = document.querySelector(`${arg.card} .usage-calc-extra-line`);
      return el && el.textContent.includes(`${arg.ceiling}%`);
    }, { card: CARD, ceiling }, { timeout: 20000 });
    body = await drawnBody(p);
    check(body.advice.extra_credit_points === EXTRA_POINTS && body.advice.ceiling === ceiling, `the response carries ceiling ${ceiling} (saw ${body.advice.ceiling})`);
    const line = squash(await p.textContent(`${CARD} .usage-calc-extra-line`));
    check(line === `Ceiling ${ceiling}% (100 plus ${EXTRA_POINTS} extra-credit points)`, `the ceiling line reads "${line}"`);
    card = await readVerdictCard(p);
    check(card.chips[0].label === wording[body.advice.verdict], 'the verdict chip follows the raised ceiling');
    check((await noHorizontalOverflow(p)).ok, 'no horizontal overflow with extra credit');
    evidence.extraCredit = { line, ceiling: body.advice.ceiling, verdict: body.advice.verdict };
    await screenshot(p, '05-extra-credit');

    // --- 5. the settings card ------------------------------------------------------------------
    await p.evaluate(() => window.navigate('settings'));
    await p.locator('.settings-tab-btn[data-tab="general"]').click({ timeout: 30000 });
    await p.waitForSelector('#gcfg_usage_calculator .settings-row', { state: 'attached', timeout: 30000 });
    await p.locator('#gcfg_usage_calculator').scrollIntoViewIfNeeded();
    const settings = await p.evaluate(() => [...document.querySelectorAll('#gcfg_usage_calculator .settings-row')].map((row) => {
      const control = row.querySelector('select, input');
      return {
        label: (row.querySelector('.settings-label')?.textContent || '').replace(/\s+/g, ' ').trim(),
        tag: control ? control.tagName.toLowerCase() : '',
        value: control ? control.value : '',
        options: control && control.tagName === 'SELECT' ? [...control.options].map((o) => o.value) : [],
        placeholder: control ? control.getAttribute('placeholder') || '' : '',
      };
    }));
    check(settings.length === 10, `the settings card lists 10 fields, saw ${settings.length}`);
    check(settings.every((s) => s.label && s.tag), 'every settings row has a label and a control');
    const config = (await api('/api/config')).body || {};
    const calc = (config.usage && config.usage.calculator) || {};
    const byLabel = (needle) => settings.find((s) => s.label.toLowerCase().includes(needle));
    const variant = byLabel('layout');
    check(JSON.stringify(variant.options) === JSON.stringify(VARIANTS.concat('all')), `the layout options are ${variant.options.join(', ')}`);
    check(variant.value === (calc.variant || variant.options[0]), `the layout reads ${variant.value}`);
    const policy = byLabel('policy');
    check(JSON.stringify(policy.options) === JSON.stringify(POLICIES), `the policy options are ${policy.options.join(', ')}`);
    check(policy.value === (calc.policy || POLICIES[0]), `the policy reads ${policy.value}`);
    const safety = byLabel('safety margin');
    check(safety.value === (calc.safety_points === undefined ? '' : String(calc.safety_points)) && safety.placeholder === '5', 'the safety margin shows the configured value, default 5');
    check((await noHorizontalOverflow(p)).ok, 'no horizontal overflow on the settings card');
    evidence.settingsCard = settings;
    await screenshot(p, '06-settings-card');
  } finally {
    // Put the daemon back as it was: no extra credit, none of the readings the story added.
    await api('/api/usage/extra-credit', { method: 'PUT', body: JSON.stringify({ points: 0 }) }).catch(() => {});
    for (const id of addedReadings) await api(`/api/usage/readings?id=${encodeURIComponent(id)}`, { method: 'DELETE' }).catch(() => {});
    await saveJson('usage-calculator-evidence', evidence).catch(() => {});
    if (consoleErrors.length) await saveLog('console-errors', consoleErrors.join('\n')).catch(() => {});
    await context.close().catch(() => {});
  }
  check(consoleErrors.length === 0, `page errors: ${consoleErrors.join(' | ')}`);
});
