// Story: usage calculator Claude lane stamp
/**
 * TS-840 — Usage calculator Claude lane stamp on a phone: the reading form no longer asks
 * for a Claude lanes count, the what-if control and the running-now line are on the card, and
 * a reading saved from the form is sent with no lanes field.
 * tags: surface:pwa feature:usage-calculator group:usage
 *
 * Runs at a phone viewport (390x844). The calculator card sits on the Automata tab, in the
 * Automation sub-tab, under its Overview second-row tab.
 *
 * Flow: seed an earlier reading through the API so the advice has a previous reading, open the
 * card, then
 *   1. the reading form has the weekly percent, session percent and read-at fields and no
 *      Claude lanes input (the old stepper ids are not inside the form);
 *   2. the what-if control is present, carries its label and hint from the locale bundle, and
 *      says it is not saved;
 *   3. the running-now line is present and shows a count or the not-known wording;
 *   4. a reading saved through the form is posted with no lanes field, and the daemon stores
 *      it, so the lanes it carries are the daemon's own stamp.
 * Readings the story added are removed again at the end.
 *
 * Needs a sandbox daemon (never the production daemon, never ~/.datawatch); see
 * scripts/e2e-pwa.sh. Skips (exit 2) when Chrome is unavailable or the daemon has no
 * usage endpoints.
 */

import { screenshot, saveLog, env } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const CARD = '#automationUsageCard';

async function api(pathAndQuery, init = {}) {
  const r = await fetch(env.http + pathAndQuery, {
    ...init,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json', ...(init.headers || {}) },
  });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}

const squash = (s) => String(s || '').replace(/\s+/g, ' ').trim();

// The ids of the readings the daemon holds now.
const readingIds = async () => new Set((((await api('/api/usage/readings')).body || {}).readings || []).map((r) => r.id));

// The reading form's controls: the ids of its inputs and any element that asks for lanes.
const formControls = (page) => page.evaluate((card) => {
  const percent = document.querySelector(`${card} #usageCalcPercent`);
  const form = percent ? percent.closest('div') : null;
  if (!form) return null;
  return {
    inputIds: [...form.querySelectorAll('input')].map((el) => el.id),
    lanesControls: [...form.querySelectorAll('#usageCalcLanes, #usageCalcLanesMinus, #usageCalcLanesPlus, [id*="Lanes"], [name*="lanes"]')]
      .map((el) => el.id || el.getAttribute('name')),
    text: (form.textContent || '').replace(/\s+/g, ' ').trim(),
  };
}, CARD);

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
    // An earlier reading, sent without a lanes field, so the advice has a previous reading.
    const idsBeforeSeed = await readingIds();
    const before = new Date(Date.now() - 3 * 3600 * 1000).toISOString();
    const seeded = await api('/api/usage/readings', { method: 'POST', body: JSON.stringify({ percent: 40, source: 'typed', at: before }) });
    check(seeded.status === 200 || seeded.status === 201, `seeding an earlier reading -> ${seeded.status}`);
    for (const id of await readingIds()) if (!idsBeforeSeed.has(id)) addedReadings.push(id);

    await openPWA(p, { timeout: 60000 });
    await p.evaluate(() => { try { localStorage.setItem('cs_theme', 'dark'); } catch { /* ignore */ } });

    // Open the Automata tab, Automation sub-tab, Overview second-row tab (the usage card's home),
    // and wait for the usage card to load.
    await p.evaluate(() => {
      window.navigate('autonomous');
      const btn = document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]');
      if (btn && !btn.classList.contains('active')) btn.click();
      const overview = document.querySelector('.automation-subtabs .output-tab[data-subtab="overview"]');
      if (overview && !overview.classList.contains('active')) overview.click();
    });
    await p.waitForSelector(`${CARD} .usage-calc-title`, { state: 'visible', timeout: 60000 });
    await p.waitForSelector(`${CARD} .usage-calc-verdict`, { state: 'visible', timeout: 30000 });
    await p.locator(CARD).scrollIntoViewIfNeeded();
    await screenshot(p, '01-card-opened');

    const bundle = await p.evaluate(() => (window._i18n && window._i18n.bundle) || {});

    // --- 1. the reading form has no Claude lanes input ---------------------------------------
    const form = await formControls(p);
    check(!!form, 'the reading form is on the card');
    check(form.inputIds.includes('usageCalcPercent') && form.inputIds.includes('usageCalcSession') && form.inputIds.includes('usageCalcReadAt'),
      `the form keeps the percent, session and read-at inputs, saw ${form.inputIds.join(', ')}`);
    check(form.lanesControls.length === 0, `no Claude lanes input on the reading form, saw ${form.lanesControls.join(', ')}`);
    const runningNowText = (bundle.usage_calc_lanes_label || 'Claude lanes running now').toLowerCase();
    check(!form.text.toLowerCase().includes(runningNowText), 'the form does not carry the Claude lanes label');
    evidence.form = form;

    // --- 2. the what-if control --------------------------------------------------------------
    const whatIf = await p.evaluate((card) => {
      const el = document.querySelector(`${card} #usageCalcWhatIf`);
      if (!el) return null;
      return {
        set: el.getAttribute('data-whatif-set'),
        text: (el.textContent || '').replace(/\s+/g, ' ').trim(),
        hasMinus: !!el.querySelector('#usageCalcLanesMinus'),
        hasPlus: !!el.querySelector('#usageCalcLanesPlus'),
      };
    }, CARD);
    check(!!whatIf, 'the what-if control is on the card');
    check(whatIf.hasMinus && whatIf.hasPlus, 'the what-if control has the fewer and more buttons');
    check(whatIf.text.includes(squash(bundle.usage_calc_whatif_label)), `the what-if control carries its label "${bundle.usage_calc_whatif_label}"`);
    check(whatIf.text.includes(squash(bundle.usage_calc_whatif_hint)), 'the what-if control carries its hint');
    check(/not saved/i.test(whatIf.text), 'the what-if hint says the value is not saved');
    check(whatIf.set === 'false', `the what-if starts unset, saw data-whatif-set=${whatIf.set}`);
    evidence.whatIf = whatIf;
    await p.locator(`${CARD} #usageCalcWhatIf`).scrollIntoViewIfNeeded();
    await screenshot(p, '02-what-if-and-running-now');

    // --- 3. the running-now line -------------------------------------------------------------
    const running = await p.evaluate((card) => {
      const el = document.querySelector(`${card} .usage-calc-running`);
      return el ? { state: el.getAttribute('data-running-now'), text: (el.textContent || '').replace(/\s+/g, ' ').trim() } : null;
    }, CARD);
    check(!!running, 'the running-now line is on the card');
    if (running.state === 'unknown') {
      check(running.text === squash(bundle.usage_calc_running_unknown), `the running-now line reads the not-known wording, saw "${running.text}"`);
    } else {
      check(/^\d+$/.test(running.state), `the running-now line carries a count, saw "${running.state}"`);
      check(running.text === squash(String(bundle.usage_calc_running_now).replace('%1$s', running.state)),
        `the running-now line reads the bundle wording with the count, saw "${running.text}"`);
    }
    evidence.running = running;

    // --- 4. a reading saved from the form carries no lanes field ------------------------------
    const idsBeforeForm = await readingIds();
    await p.fill(`${CARD} #usageCalcPercent`, '52');
    const posted = p.waitForRequest((r) => r.url().includes('/api/usage/readings') && r.method() === 'POST', { timeout: 20000 });
    const saved = p.waitForResponse((r) => r.url().includes('/api/usage/readings') && r.request().method() === 'POST', { timeout: 20000 });
    await p.locator(`${CARD} button[onclick="usageCalcSubmitReading()"]`).click();
    const sentBody = JSON.parse((await posted).postData() || '{}');
    check((await saved).ok(), 'saving the reading through the form');
    check(sentBody.percent === 52, `the form sent the typed percent, saw ${sentBody.percent}`);
    check(!('lanes' in sentBody), `the form sent no lanes field, saw keys ${Object.keys(sentBody).join(', ')}`);
    await p.waitForSelector(`${CARD} .usage-calc-verdict`, { state: 'visible', timeout: 30000 });
    const fromForm = [...await readingIds()].filter((id) => !idsBeforeForm.has(id));
    check(fromForm.length === 1, `the form added one reading, saw ${fromForm.length}`);
    addedReadings.push(...fromForm);
    evidence.sentBody = sentBody;
    check((await noHorizontalOverflow(p)).ok, 'no horizontal overflow at 390 px after saving a reading');
    await screenshot(p, '03-reading-saved');
  } finally {
    // Put the daemon back as it was: none of the readings the story added.
    for (const id of addedReadings) await api(`/api/usage/readings?id=${encodeURIComponent(id)}`, { method: 'DELETE' }).catch(() => {});
    await saveJson('usage-calculator-lane-stamp-evidence', evidence).catch(() => {});
    if (consoleErrors.length) await saveLog('console-errors', consoleErrors.join('\n')).catch(() => {});
    await context.close().catch(() => {});
  }
  check(consoleErrors.length === 0, `page errors: ${consoleErrors.join(' | ')}`);
});
