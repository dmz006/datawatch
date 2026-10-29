// TS-833 — PWA Automation sub-tabs on a phone (360×800).
//
// The Automation tab of the Autonomous view carries a second row of exactly
// two sub-tabs:
//   "Overview" — the status/progress cards and the capacity card
//   "Chains"   — the default: the card list with a status badge per card, the
//                filter button, the detail view with back button and strip,
//                and the #automation/<id> route
// There is no "Questions" tab in either row, and the old header "New
// Automation" button (#automationNewBtn) is gone — the round create button
// (#newSessionFab) is the only always-visible create affordance.
//
// The detail page and the #automation/<id> route draw over the whole view
// (the sub-tab rows are not part of them), so the phone fit rule applies
// there to whatever tab buttons a row still shows (there are none), plus the
// no-horizontal-overflow rule everywhere: no horizontal overflow, and every
// .output-tab button in either row keeps ≥40 px height and ≥44 px width,
// clipped inside the viewport.
//
// The story seeds one Automation through the API (name ts-subtabs-<ts>,
// project dir ".", actor operator) so the Chains list, the detail view and
// the route have real data to render, and removes it again at the end.
//
// tags: surface:pwa feature:automation group:automation
//
import { screenshot, saveLog, env } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const STORY = 'TS-833';
const PHONE = { width: 360, height: 800 };
const MIN_TAB = { h: 40, w: 44 };   // phone rule: every output-tab button ≥40 px tall, ≥44 px wide
const TS = Date.now();
const NAME = 'ts-subtabs-' + TS;
const REASON = 'automation sub-tabs story';

const evidence = {
  story: STORY, phone: PHONE, name: NAME, automationId: null,
  topRow: null, subtabs: null, fab: null, chains: null, overview: null,
  detail: null, backToChain: null, route: null, fit: null,
  cleanup: null,
};

// --- API (sandbox: http with the token; 503 = autonomous subsystem off) ----

async function api(path, method = 'GET', body = null) {
  const res = await fetch(env.http + path, {
    method,
    headers: { 'content-type': 'application/json', authorization: 'Bearer ' + env.token },
    body: body ? JSON.stringify(body) : undefined,
  });
  let json = null;
  try { json = await res.json(); } catch { /* 204 etc. */ }
  return { status: res.status, body: json };
}

// --- page helpers ------------------------------------------------------------
// NOTE: anything run via page.evaluate must be self-contained (Playwright
// serializes the function body into the page context; module-scope lets and
// imports are not visible there).

function rowsIn(sel) {
  return Array.from(document.querySelectorAll(sel)).map(b => {
    const r = b.getBoundingClientRect();
    return {
      id: b.getAttribute('data-subtab') || b.getAttribute('data-tab') || null,
      label: (b.textContent || '').trim(),
      active: b.classList.contains('active') || b.getAttribute('aria-selected') === 'true',
      h: Math.round(r.height), w: Math.round(r.width),
      left: Math.round(r.left), right: Math.round(r.right),
    };
  });
}

function measureFit() {
  const row = (sel) => Array.from(document.querySelectorAll(sel)).map(b => {
    const r = b.getBoundingClientRect();
    return {
      id: b.getAttribute('data-subtab') || b.getAttribute('data-tab') || null,
      label: (b.textContent || '').trim(),
      active: b.classList.contains('active') || b.getAttribute('aria-selected') === 'true',
      h: Math.round(r.height), w: Math.round(r.width),
      left: Math.round(r.left), right: Math.round(r.right),
    };
  });
  return {
    page: { innerWidth: window.innerWidth, innerHeight: window.innerHeight,
            scrollWidth: document.documentElement.scrollWidth, scrollHeight: document.documentElement.scrollHeight },
    row1: row('.automata-tabs-bar .output-tab'),
    row2: row('.automation-subtabs .output-tab'),
  };
}

// requireRows names the rows the current view is supposed to keep
// (['row1','row2'] on the tabbed list/Overview views; [] on the detail page
// and the #automation/<id> route, which draw over the whole view and drop
// both rows). A required row must be rendered; any .output-tab that exists
// in either row is held to the size and viewport rule either way.
function fitChecks(view, m, overflow, requireRows) {
  const required = requireRows || [];
  const spec = [['row1', 'row 1'], ['row2', 'row 2']];
  check(overflow.ok, `${view}: no horizontal overflow (scrollWidth ${overflow.scrollWidth} > innerWidth ${overflow.innerWidth})`);
  for (const [key, label] of spec) {
    const tabs = m[key] || [];
    if (required.includes(key)) check(tabs.length > 0, `${view}: ${label} sub-tab row rendered`);
    for (const b of tabs) {
      check(b.h >= MIN_TAB.h && b.w >= MIN_TAB.w,
        `${view}: ${label} tab "${b.label}" is ≥${MIN_TAB.h}px tall and ≥${MIN_TAB.w}px wide (saw ${b.h}×${b.w})`);
      check(b.left >= 0 && b.right <= m.page.innerWidth + 1,
        `${view}: ${label} tab "${b.label}" fits the ${m.page.innerWidth}px viewport (saw left=${b.left} right=${b.right})`);
    }
  }
}

// --- story -------------------------------------------------------------------

// Toasts (e.g. the fresh-daemon "v7 unification" notice) and a lingering
// splash overlay can sit on top of the tab bar and intercept pointer
// events. Clear them before driving the tabs.
async function clearOverlays(page) {
  await page.evaluate(() => {
    const tc = document.querySelector('.toast-container');
    if (tc) tc.innerHTML = '';
    document.querySelectorAll('.toast, .toast-error-msg, .toast-success-msg').forEach((el) => el.remove());
    const s = document.getElementById('splash');
    if (s) { s.style.display = 'none'; s.remove(); }
    const sc = document.getElementById('splashCanvas');
    if (sc) sc.remove();
  });
}

async function clickTab(page, sel) {
  for (let i = 0; i < 3; i++) {
    await clearOverlays(page);
    await page.waitForSelector(sel, { timeout: 15000 });
    try {
      await page.click(sel, { timeout: 15000 });
      return;
    } catch (e) {
      if (i === 2) throw e;
      await new Promise((r) => setTimeout(r, 500));
    }
  }
}

runPerfStory(async (page) => {
  await page.setViewportSize(PHONE).catch(() => {});

  // 1. Seed the Automation.
  let mk;
  try {
    mk = await api('/api/automations', 'POST', { name: NAME, project_dir: '.', actor: 'operator', reason: REASON });
  } catch (e) {
    await skipStory(`daemon unreachable at ${env.http}: ${e.message}`);
  }
  if (mk.status === 503) await skipStory('autonomous subsystem disabled (POST /api/automations → 503)');
  const id = (mk.body && (mk.body.id || (mk.body.automation && mk.body.automation.id))) || null;
  if (mk.status !== 201 || !id) throw new Error(`expected 201 posting the Automation, got ${mk.status}: ${JSON.stringify(mk.body).slice(0, 300)}`);
  evidence.automationId = id;
  console.log(`seeded Automation ${id} (${NAME})`);

  try {
  // 2. Open the PWA at phone size and get to the Automation tab of the Autonomous view.
  await openPWA(page);
  await clearOverlays(page);
  await page.evaluate(() => { if (typeof window.navigate === 'function') window.navigate('autonomous'); });
  await page.waitForFunction(
    () => !!document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]'),
    null, { timeout: 20000 }
  ).catch(() => skipStory('Automation tab not rendered — autonomous disabled or app not ready'));

  // 3. First row: click the "Automation" tab; the second row lands on Chains.
  await clickTab(page, '.automata-tabs-bar .output-tab[data-tab="automation"]');
  await page.waitForSelector('.automation-subtabs .output-tab', { timeout: 15000 });

  // 4. Sub-tab rows: exactly two tabs, Overview + Chains, Chains default,
  //    no Questions tab in either row, and the removed header create button absent.
  const topRow = await page.evaluate(rowsIn, '.automata-tabs-bar .output-tab');
  const subs = await page.evaluate(rowsIn, '.automation-subtabs .output-tab');
  evidence.topRow = topRow;
  evidence.subtabs = subs;
  check(subs.length === 2, `second row has exactly 2 sub-tabs (saw ${subs.length}: ${subs.map(s => s.label).join(', ')})`);
  const labels = subs.map(s => s.label.toLowerCase());
  check(labels.some(l => l.includes('overview')), `second row has an "Overview" tab (saw ${subs.map(s => s.label).join(', ')})`);
  check(labels.some(l => l.includes('chain')), `second row has a "Chains" tab (saw ${subs.map(s => s.label).join(', ')})`);
  check(!topRow.some(t => /question/i.test(t.label)),
    `no "Questions" tab in the first row (saw ${topRow.map(t => t.label).join(', ')})`);
  check(!subs.some(s => /question/i.test(s.label)),
    `no "Questions" tab in the second row (saw ${subs.map(s => s.label).join(', ')})`);
  const chains = subs.find(s => (s.id || '').includes('chain'));
  check(chains && chains.active, 'Chains is the default (selected) sub-tab');
  const overviewTab = subs.find(s => (s.id || '').includes('overview'));
  check(overviewTab && !overviewTab.active, 'Overview is not selected by default');

  const noNewBtn = await page.evaluate(() => !document.getElementById('automationNewBtn'));
  check(noNewBtn, 'no #automationNewBtn on the Automation tab (the round create button is the create path)');

  // 5. The round create button is present and visible on the Automation sub-tab.
  const fab = await page.evaluate(() => {
    const f = document.getElementById('newSessionFab');
    if (!f) return null;
    const r = f.getBoundingClientRect();
    return { hidden: f.classList.contains('hidden'), display: getComputedStyle(f).display,
             label: f.getAttribute('aria-label') || f.title, h: Math.round(r.height), w: Math.round(r.width) };
  });
  evidence.fab = fab;
  check(fab && !fab.hidden && fab.display !== 'none',
    `the round create button is present and visible (saw ${JSON.stringify(fab)})`);

  const cardSel = `#automationPanel .automation-card[data-automation-id="${id}"]`;

  // 6. Chains: the card list, the status badge row, the filter button and search.
  await page.waitForSelector(cardSel, { timeout: 25000 });
  const chainsView = await page.evaluate((sel) => {
    const card = document.querySelector(sel);
    const badge = card && card.querySelector('.automation-card-status');
    const filterBtn = document.getElementById('automationFilterBtn');
    return {
      cardName: card && card.querySelector('.automation-card-title') ? card.querySelector('.automation-card-title').textContent.trim() : null,
      statusBadge: badge ? { text: badge.textContent.trim(), row: !!badge.closest('.work-tile-status') } : null,
      filterBtn: filterBtn ? { display: getComputedStyle(filterBtn).display,
                               inBar: !!(filterBtn.closest('.automata-tabs-bar')), label: filterBtn.getAttribute('aria-label') } : null,
    };
  }, cardSel);
  evidence.chains = chainsView;
  check(chainsView.cardName === NAME, `the Chains list renders the seeded card named ${NAME} (saw "${chainsView.cardName}")`);
  check(chainsView.statusBadge && chainsView.statusBadge.row && /draft/i.test(chainsView.statusBadge.text),
    `the card shows its status in the badge row (want Draft, saw ${JSON.stringify(chainsView.statusBadge)})`);
  check(chainsView.filterBtn && chainsView.filterBtn.display !== 'none',
    `the filter button is shown on Chains (saw ${JSON.stringify(chainsView.filterBtn)})`);
  const fitList = await noHorizontalOverflow(page);
  const mList = await page.evaluate(measureFit);
  fitChecks('Chains list', mList, fitList, ['row1', 'row2']);
  await screenshot(page, '01-automation-chains');

  // The filter button opens the filter bar (search), and closes again.
  await clickTab(page, '#automationFilterBtn');
  const filterOpen = await page.evaluate(() => {
    const bar = document.getElementById('automationFilterBar');
    return { open: bar ? getComputedStyle(bar).display !== 'none' : false,
             search: !!(bar && bar.querySelector('#automationSearch')) };
  });
  check(filterOpen.open && filterOpen.search,
    `the filter button opens the filter bar with a search field (saw ${JSON.stringify(filterOpen)})`);
  await clickTab(page, '#automationFilterBtn');
  const filterClosed = await page.evaluate(() => {
    const bar = document.getElementById('automationFilterBar');
    return bar ? getComputedStyle(bar).display === 'none' : null;
  });
  check(filterClosed, 'toggling the filter button closes the filter bar again');

  // 7. Overview: the capacity card, the progress card in any state the app
  //    allows (loaded, failed or empty), no #automationPanel, no header create button.
  await clickTab(page, '.automation-subtabs .output-tab[data-subtab="overview"]');
  // Wait until the capacity slot has painted (loaded, or its failed/empty
  // state); treat a stall as evidence, not as a failure — the spec holds the
  // failed/empty states as showing the card.
  await page.waitForFunction(() => {
    const cap = document.getElementById('automationCapacityCard');
    return cap && !!(cap.querySelector('.prd-capacity-card') ||
                     cap.querySelector('.list-state-failed') ||
                     cap.querySelector('.list-state-empty'));
  }, null, { timeout: 25000 }).catch(() => {});
  const overviewView = await page.evaluate(() => {
    const cap = document.getElementById('automationCapacityCard');
    const prog = document.getElementById('progressCard');
    const progCard = prog && prog.querySelector('.progress-card');
    const panel = document.getElementById('automationPanel');
    return {
      capacityCardPresent: !!cap,
      capacityLoaded: !!(cap && cap.querySelector('.prd-capacity-card')),
      capacityFailedOrEmpty: !!(cap && (cap.querySelector('.list-state-failed') || cap.querySelector('.list-state-empty'))),
      progressCardPresent: !!prog,
      progressStatus: progCard ? progCard.getAttribute('data-progress-status') : null,
      progressFailedEl: !!(prog && prog.querySelector('.progress-card-failed')),
      automationPanel: panel ? { inDom: true, shown: getComputedStyle(panel).display !== 'none' } : { inDom: false, shown: false },
      newBtn: !!document.getElementById('automationNewBtn'),
    };
  });
  evidence.overview = overviewView;
  check(overviewView.capacityCardPresent, `Overview shows the capacity card (saw ${JSON.stringify(overviewView)})`);
  check(overviewView.capacityLoaded || overviewView.capacityFailedOrEmpty,
    `Overview rendered the capacity card (loaded, or its failed/empty state; saw ${JSON.stringify(overviewView)})`);
  check(overviewView.progressCardPresent,
    `Overview shows the progress card or its failed/empty state (saw ${JSON.stringify(overviewView)})`);
  check(!overviewView.automationPanel.inDom || !overviewView.automationPanel.shown,
    `clicking Overview hides the Chains list panel #automationPanel (saw ${JSON.stringify(overviewView.automationPanel)})`);
  check(!overviewView.newBtn, 'no #automationNewBtn on Overview');
  const fitOver = await noHorizontalOverflow(page);
  const mOver = await page.evaluate(measureFit);
  fitChecks('Overview', mOver, fitOver, ['row1', 'row2']);
  await screenshot(page, '02-automation-overview');

  // 8. Back to Chains: the list returns; click the card → detail view
  //    (back button + pipeline strip). Back returns to the list.
  await clickTab(page, '.automation-subtabs .output-tab[data-subtab="chains"]');
  await page.waitForSelector(cardSel, { timeout: 15000 });

  await clickTab(page, cardSel); // overlay-safe: clear + retry (a sticky toast may sit on the card)
  await page.waitForSelector('.automation-detail .automation-detail-back', { timeout: 15000 });
  // The detail opens on a loading shell and fills in asynchronously — wait for
  // the real header (title) and the strip container before probing.
  await page.waitForSelector('.automation-detail .automation-detail-title', { timeout: 15000 });
  await page.waitForSelector('.automation-detail #automationStrip', { timeout: 15000 });
  const detail = await page.evaluate((aid) => {
    const wrap = document.querySelector('.automation-detail');
    const back = document.querySelector('.automation-detail .automation-detail-back');
    const strip = document.querySelector(`#automationStrip .automation-strip[data-automation-id="${aid}"]`);
    return {
      visible: wrap ? getComputedStyle(wrap).display !== 'none' : false,
      backVisible: back ? getComputedStyle(back).display !== 'none' : false,
      backLabel: back ? (back.getAttribute('aria-label') || back.textContent.trim()) : null,
      stripId: strip ? strip.getAttribute('data-automation-id') : null,
      title: document.querySelector('.automation-detail-title') ? document.querySelector('.automation-detail-title').textContent.trim() : null,
      status: document.querySelector('.automation-detail-status') ? document.querySelector('.automation-detail-status').textContent.trim() : null,
    };
  }, id);
  evidence.detail = detail;
  check(detail.visible, 'the detail view is open after clicking the card');
  check(detail.backVisible, `the detail view has a back button (saw ${JSON.stringify(detail)})`);
  check(String(detail.backLabel).toLowerCase().includes('back'), `the back button reads like a back action ("${detail.backLabel}")`);
  check(detail.stripId === id, `the detail view shows the pipeline strip for Automation ${id} (saw ${detail.stripId})`);
  check(detail.title === NAME, `the detail view shows the Automation name ${NAME} (saw "${detail.title}")`);
  check(/draft/i.test(detail.status || ''), `the detail status reads Draft (saw "${detail.status}")`);
  const fitDetail = await noHorizontalOverflow(page);
  const mDetail = await page.evaluate(measureFit);
  // The detail page draws over the whole view: neither sub-tab row is part
  // of it, so no row is required — only the overflow rule and, for the tabs
  // a view keeps, the size/viewport rule.
  fitChecks('Detail', mDetail, fitDetail, []);
  await screenshot(page, '03-automation-detail');

  await clickTab(page, '.automation-detail .automation-detail-back');
  await page.waitForSelector(cardSel, { timeout: 15000 });
  const backToChain = await page.evaluate((sel) => {
    const card = document.querySelector(sel);
    return { noDetail: !document.querySelector('.automation-detail'),
             cardVisible: card ? getComputedStyle(card).display !== 'none' : false,
             statusBadge: card && card.querySelector('.automation-card-status') ? card.querySelector('.automation-card-status').textContent.trim() : null };
  }, cardSel);
  evidence.backToChain = backToChain;
  check(backToChain.noDetail && backToChain.cardVisible,
    `back from detail returns to the Chains list with the card (saw ${JSON.stringify(backToChain)})`);
  check(/draft/i.test(backToChain.statusBadge || ''), `the card keeps its Draft badge after coming back (saw "${backToChain.statusBadge}")`);
  await screenshot(page, '04-back-to-chain');

  // 9. The #automation/<id> route opens the detail (and keeps the back path).
  await page.evaluate((aid) => { window.location.hash = '#automation/' + encodeURIComponent(aid); }, id);
  await page.reload({ waitUntil: 'domcontentloaded' });
  await page.waitForFunction(() => {
    const el = document.getElementById('splash');
    return !el || getComputedStyle(el).display === 'none' || el.classList.contains('hidden');
  }, null, { timeout: 25000 });
  await page.waitForSelector('.automation-detail .automation-detail-back', { timeout: 30000 });
  // Rendered from the route: the detail fills in asynchronously on a loading
  // shell, so wait until the header (and strip) is real before probing.
  await page.waitForSelector('.automation-detail .automation-detail-title', { timeout: 20000 });
  await page.waitForSelector('.automation-detail #automationStrip', { timeout: 20000 });
  const route = await page.evaluate((aid) => ({
    hash: location.hash,
    detailOpen: !!document.querySelector('.automation-detail'),
    backVisible: (() => { const b = document.querySelector('.automation-detail .automation-detail-back');
      return b ? getComputedStyle(b).display !== 'none' : false; })(),
    stripId: (() => { const s = document.querySelector(`#automationStrip .automation-strip[data-automation-id="${aid}"]`);
      return s ? s.getAttribute('data-automation-id') : null; })(),
  }), id);
  evidence.route = route;
  check(route.hash === '#automation/' + id, `the page sits on #automation/<id> (saw "${route.hash}")`);
  check(route.detailOpen, `route #automation/<id> opens the detail view (saw ${JSON.stringify(route)})`);
  check(route.backVisible, 'the route-opened detail keeps its back button');
  check(route.stripId === id, `the route-opened detail shows the strip for ${id} (saw ${route.stripId})`);
  const fitRoute = await noHorizontalOverflow(page);
  const mRoute = await page.evaluate(measureFit);
  fitChecks('Route detail', mRoute, fitRoute, []);
  await screenshot(page, '05-route-automation-detail');

  // 10. Back through the route-opened detail: still on the Chains list.
  await clickTab(page, '.automation-detail .automation-detail-back');
  await page.waitForSelector(cardSel, { timeout: 15000 });
  evidence.fit = { list: mList, overview: mOver, detail: mDetail, route: mRoute };

  // 11. Cleanup: remove the seeded Automation again, and verify it is gone.
  // (Done inside the story: runStory exits 0 as soon as this resolves, so
  //  nothing scheduled after runPerfStory would ever run.)
  const del = await api('/api/automations/' + encodeURIComponent(evidence.automationId), 'DELETE',
                        { actor: 'operator', reason: REASON + ' cleanup' });
  const after = await api('/api/automations/' + encodeURIComponent(evidence.automationId));
  evidence.cleanup = { name: NAME, deleteStatus: del.status, afterGetStatus: after.status,
                       removed: del.status === 204 && after.status === 404 };
  console.log(`cleanup: DELETE ${del.status}, GET ${after.status}`);
  check(evidence.cleanup.removed,
    `cleanup removed the seeded Automation ${evidence.automationId} (DELETE ${del.status}, GET ${after.status})`);
  console.log(`${STORY}: Automation sub-tabs (Overview/Chains) OK on ${PHONE.width}×${PHONE.height}`);
} finally {
  // Evidence + last-chance cleanup (best-effort, never masks the story result).
  if (!evidence.cleanup && evidence.automationId) {
    try {
      const del = await api('/api/automations/' + encodeURIComponent(evidence.automationId), 'DELETE',
                            { actor: 'operator', reason: REASON + ' cleanup (story failed)' });
      evidence.cleanup = { name: NAME, deleteStatus: del.status, note: 'story failed — best-effort delete' };
    } catch (e) { evidence.cleanup = { name: NAME, removed: false, error: String(e && e.message || e) }; }
  }
  try { await saveJson('automation-subtabs-evidence', evidence); } catch { /* best-effort */ }
  try {
    await saveLog('evidence',
      [
        `story=${STORY} phone=${PHONE.width}x${PHONE.height}`,
        `automation=${JSON.stringify({ id: evidence.automationId, name: NAME })}`,
        `topRow=${JSON.stringify(evidence.topRow)}`,
        `subtabs=${JSON.stringify(evidence.subtabs)}`,
        `fab=${JSON.stringify(evidence.fab)}`,
        `chains=${JSON.stringify(evidence.chains)}`,
        `overview=${JSON.stringify(evidence.overview)}`,
        `detail=${JSON.stringify(evidence.detail)}`,
        `backToChain=${JSON.stringify(evidence.backToChain)}`,
        `route=${JSON.stringify(evidence.route)}`,
        `fit=${JSON.stringify(evidence.fit)}`,
        `cleanup=${JSON.stringify(evidence.cleanup)}`,
      ].join('\n'));
  } catch { /* best-effort */ }
}  });
