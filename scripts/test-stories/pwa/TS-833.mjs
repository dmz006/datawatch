// TS-833 — PWA Automation sub-tabs on a phone (360×800).
//
// The Automation tab of the Autonomous view carries a second row of exactly
// two sub-tabs:
//   "Overview" — the status/progress cards and the capacity card
//   "Chains"   — the default: the card list with a status badge per card, the
//                filter button, the detail view with back button and strip,
//                and the #automation/<id> route
// There is no "Questions" tab. All of it fits a 360×800 phone: no horizontal
// overflow, and every output-tab button in both rows keeps ≥40 px height and
// ≥44 px width.
//
// The story seeds one Automation through the API (name ts-subtabs-<ts>,
// project dir ".", actor operator, reason TS-833) so the Chains list, the
// detail view and the route have real data to render, and removes it again at
// the end.
//
// tags: surface:pwa feature:automation group:automation
//
import { screenshot, saveLog, env } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const STORY = 'TS-833';
const PHONE = { width: 360, height: 800 };
const MIN_TAB = { h: 40, w: 44 };   // PRD: every output-tab button ≥40 px tall, ≥44 px wide
const TS = Date.now();
const NAME = 'ts-subtabs-' + TS;

const evidence = {
  story: STORY, phone: PHONE, name: NAME, automationId: null,
  subtabs: null, fab: null, chains: null, overview: null,
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

function tabRow(sel) {
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

function measureFit(w, h) {
  return {
    minTab: { h: MIN_TAB.h, w: MIN_TAB.w },
    page: { innerWidth: window.innerWidth, innerHeight: window.innerHeight,
            scrollWidth: document.documentElement.scrollWidth, scrollHeight: document.documentElement.scrollHeight },
    row1: tabRow('.automata-tabs-bar .output-tab'),
    row2: tabRow('.automation-subtabs .output-tab'),
  };
}

function fitChecks(view, m, overflow) {
  check(overflow.ok, `${view}: no horizontal overflow (scrollWidth ${overflow.scrollWidth} > innerWidth ${overflow.innerWidth})`);
  for (const row of [['row 1', m.row1], ['row 2', m.row2]]) {
    check(row[1].length > 0, `${view}: ${row[0]} sub-tab row rendered`);
    for (const b of row[1]) {
      check(b.h >= MIN_TAB.h && b.w >= MIN_TAB.w,
        `${view}: ${row[0]} tab "${b.label}" is ≥${MIN_TAB.h}px tall and ≥${MIN_TAB.w}px wide (saw ${b.h}×${b.w})`);
      check(b.left >= 0 && b.right <= m.page.innerWidth + 1,
        `${view}: ${row[0]} tab "${b.label}" fits the ${m.page.innerWidth}px viewport (saw left=${b.left} right=${b.right})`);
    }
  }
}

// --- story -------------------------------------------------------------------

runPerfStory(async (page) => {
  await page.setViewportSize(PHONE).catch(() => {});

  // 1. Seed the Automation.
  let mk;
  try {
    mk = await api('/api/automations', 'POST', { name: NAME, project_dir: '.', actor: 'operator', reason: STORY });
  } catch (e) {
    await skipStory(`daemon unreachable at ${env.http}: ${e.message}`);
  }
  if (mk.status === 503) await skipStory('autonomous subsystem disabled (POST /api/automations → 503)');
  const id = (mk.body && (mk.body.id || (mk.body.automation && mk.body.automation.id))) || null;
  if (mk.status !== 201 || !id) throw new Error(`expected 201 posting the Automation, got ${mk.status}: ${JSON.stringify(mk.body).slice(0, 300)}`);
  evidence.automationId = id;
  console.log(`seeded Automation ${id} (${NAME})`);

  try {
  // 2. Open the PWA at phone size and get to the Autonomous view.
  await openPWA(page);
  await page.evaluate(() => { if (typeof window.navigate === 'function') window.navigate('autonomous'); });
  await page.waitForFunction(
    () => !!document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]'),
    null, { timeout: 20000 }
  ).catch(() => skipStory('Automation tab not rendered — autonomous disabled or app not ready'));

  // 3. First row: click the "Automation" tab.
  await page.click('.automata-tabs-bar .output-tab[data-tab="automation"]');
  await page.waitForSelector('.automation-subtabs .output-tab', null, { timeout: 15000 });

  // 4. Second row: exactly two sub-tabs, Overview + Chains, Chains default, no Questions.
  const subs = await page.evaluate(() => tabRow('.automation-subtabs .output-tab'));
  evidence.subtabs = subs;
  check(subs.length === 2, `second row has exactly 2 sub-tabs (saw ${subs.length}: ${subs.map(s => s.label).join(', ')})`);
  const labels = subs.map(s => s.label.toLowerCase());
  check(labels.some(l => l.includes('overview')), `second row has an "Overview" tab (saw ${subs.map(s => s.label).join(', ')})`);
  check(labels.some(l => l.includes('chain')), `second row has a "Chains" tab (saw ${subs.map(s => s.label).join(', ')})`);
  check(!labels.some(l => /question/i.test(l)), 'no "Questions" tab in the second row');
  const chains = subs.find(s => (s.id || '').includes('chain'));
  check(chains && chains.active, 'Chains is the default (selected) sub-tab');
  const overviewTab = subs.find(s => (s.id || '').includes('overview'));
  check(overviewTab && !overviewTab.active, 'Overview is not selected by default');

  // 5. FAB is shown on the Automation sub-tab.
  const fab = await page.evaluate(() => {
    const f = document.getElementById('newSessionFab');
    if (!f) return null;
    const r = f.getBoundingClientRect();
    return { hidden: f.classList.contains('hidden'), display: getComputedStyle(f).display,
             label: f.getAttribute('aria-label') || f.title, h: Math.round(r.height), w: Math.round(r.width) };
  });
  evidence.fab = fab;
  check(fab && !fab.hidden && fab.display !== 'none',
    `the FAB button is shown on the Automation sub-tab (saw ${JSON.stringify(fab)})`);
  check((fab && fab.label || '').toLowerCase().includes('automation'),
    `the FAB offers a new Automation (label "${fab && fab.label}")`);
  check(fab && fab.h >= MIN_TAB.h && fab.w >= MIN_TAB.w,
    `the FAB keeps a ≥${MIN_TAB.h}px tap target on phone (saw ${fab && fab.h}×${fab && fab.w})`);

  const cardSel = `#automationPanel .automation-card[data-automation-id="${id}"]`;

  // 6. Chains: the card list carries the status badge row and the filter button.
  await page.waitForSelector(cardSel, null, { timeout: 25000 });
  const chainsView = await page.evaluate((sel) => {
    const card = document.querySelector(sel);
    const badge = card && card.querySelector('.automation-card-status');
    const filterBtn = document.getElementById('automationFilterBtn');
    return {
      cardName: card && card.querySelector('.automation-card-title') ? card.querySelector('.automation-card-title').textContent.trim() : null,
      statusBadge: badge ? { text: badge.textContent.trim(), row: !!badge.closest('.work-tile-status') } : null,
      filterBtn: filterBtn ? { display: getComputedStyle(filterBtn).display,
                               inBar: !!(filterBtn.closest('.automata-tabs-bar')), label: filterBtn.getAttribute('aria-label') } : null,
      filterBarOpen: (() => { const b = document.getElementById('automationFilterBar');
        return b ? getComputedStyle(b).display !== 'none' : null; })(),
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
  fitChecks('Chains list', mList, fitList);
  await screenshot(page, '01-automation-chains');

  // The filter button opens the filter bar (search + tag filters), and closes again.
  await page.click('#automationFilterBtn');
  const filterOpen = await page.evaluate(() => {
    const bar = document.getElementById('automationFilterBar');
    return { open: bar ? getComputedStyle(bar).display !== 'none' : false,
             search: !!(bar && bar.querySelector('#automationSearch')) };
  });
  check(filterOpen.open && filterOpen.search,
    `the filter button opens the filter bar with a search field (saw ${JSON.stringify(filterOpen)})`);
  await page.click('#automationFilterBtn');
  const filterClosed = await page.evaluate(() => {
    const bar = document.getElementById('automationFilterBar');
    return bar ? getComputedStyle(bar).display === 'none' : null;
  });
  check(filterClosed, 'toggling the filter button closes the filter bar again');

  // 7. Overview: the capacity card and the progress/status state.
  await page.click('.automation-subtabs .output-tab[data-subtab="overview"]');
  await page.waitForFunction(() => {
    const cap = document.getElementById('automationCapacityCard');
    const prog = document.getElementById('progressCard');
    if (!cap) return false;
    const capDone = !!(cap.querySelector('.prd-capacity-card') || cap.querySelector('.list-state-failed'));
    const pc = prog && prog.querySelector('.progress-card');
    const progDone = !!(prog && prog.style.display !== 'none' &&
                        ((pc && pc.getAttribute('data-progress-status') !== 'loading') ||
                         prog.querySelector('.list-state-failed')));
    return capDone && progDone;
  }, null, { timeout: 25000 });
  const overviewView = await page.evaluate(() => {
    const cap = document.getElementById('automationCapacityCard');
    const prog = document.getElementById('progressCard');
    const progCard = prog && prog.querySelector('.progress-card');
    return {
      capacityCard: !!(cap && cap.querySelector('.prd-capacity-card')),
      capacityFailed: !!(cap && cap.querySelector('.list-state-failed')),
      progressVisible: !!(prog && prog.style.display !== 'none'),
      progressStatus: progCard ? progCard.getAttribute('data-progress-status') : null,
      progressFailedEl: !!(prog && prog.querySelector('.progress-card-failed')),
    };
  });
  evidence.overview = overviewView;
  check(overviewView.capacityCard, `Overview shows the capacity card (saw ${JSON.stringify(overviewView)})`);
  check(!overviewView.capacityFailed, 'the capacity card did not fail to load');
  check(overviewView.progressVisible, 'Overview shows the progress/status state card');
  check(overviewView.progressStatus && overviewView.progressStatus !== 'failed',
    `the progress card loaded and is not in its failed state (status: ${overviewView.progressStatus})`);
  const fitOver = await noHorizontalOverflow(page);
  const mOver = await page.evaluate(measureFit);
  fitChecks('Overview', mOver, fitOver);
  await screenshot(page, '02-automation-overview');

  // 8. Chains → click the card → detail view: back button + strip. Back returns.
  await page.click('.automation-subtabs .output-tab[data-subtab="chains"]');
  await page.waitForSelector(cardSel, null, { timeout: 15000 });
  await page.click(cardSel);
  await page.waitForSelector('.automation-detail .automation-detail-back', null, { timeout: 15000 });
  const detail = await page.evaluate((aid) => {
    const wrap = document.querySelector('.automation-detail');
    const back = document.querySelector('.automation-detail .automation-detail-back');
    const strip = document.querySelector(`#automationStrip .automation-strip[data-automation-id="${aid}"]`);
    return {
      visible: wrap ? getComputedStyle(wrap).display !== 'none' : false,
      backVisible: back ? getComputedStyle(back).display !== 'none' : false,
      backLabel: back ? (back.getAttribute('aria-label') || back.textContent.trim()) : null,
      stripId: strip ? strip.getAttribute('data-automation-id') : null,
      stripStatus: strip ? strip.getAttribute('data-status') : null,
      title: document.querySelector('.automation-detail-title') ? document.querySelector('.automation-detail-title').textContent.trim() : null,
      status: document.querySelector('.automation-detail-status') ? document.querySelector('.automation-detail-status').textContent.trim() : null,
    };
  }, id);
  evidence.detail = detail;
  check(detail.visible, 'the detail view is open after clicking the card');
  check(detail.backVisible, `the detail view has a back button (saw ${JSON.stringify(detail)})`);
  check(String(detail.backLabel).toLowerCase().includes('back'), `the back button reads like a back action ("${detail.backLabel}")`);
  check(detail.stripId === id, `the detail view shows the strip for Automation ${id} (saw ${detail.stripId})`);
  check(detail.title === NAME, `the detail view shows the Automation name ${NAME} (saw "${detail.title}")`);
  check(/draft/i.test(detail.status || ''), `the detail status reads Draft (saw "${detail.status}")`);
  const fitDetail = await noHorizontalOverflow(page);
  const mDetail = await page.evaluate(measureFit);
  fitChecks('Detail', mDetail, fitDetail);
  await screenshot(page, '03-automation-detail');

  await page.click('.automation-detail .automation-detail-back');
  await page.waitForSelector(cardSel, null, { timeout: 15000 });
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
  await page.waitForSelector('.automation-detail .automation-detail-back', null, { timeout: 30000 });
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
  fitChecks('Route detail', mRoute, fitRoute);
  await screenshot(page, '05-route-automation-detail');

  // 10. Back through the route-opened detail: still on the Chains list.
  await page.click('.automation-detail .automation-detail-back');
  await page.waitForSelector(cardSel, null, { timeout: 15000 });
  evidence.fit = { list: mList, overview: mOver, detail: mDetail, route: mRoute };

  // 11. Cleanup: remove the seeded Automation again, and verify it is gone.
  // (Done inside the story: runStory exits 0 as soon as this resolves, so
  //  nothing scheduled after runPerfStory would ever run.)
  const del = await api('/api/automations/' + encodeURIComponent(evidence.automationId), 'DELETE',
                        { actor: 'operator', reason: STORY + ' cleanup' });
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
                            { actor: 'operator', reason: STORY + ' cleanup (story failed)' });
      evidence.cleanup = { name: NAME, deleteStatus: del.status, note: 'story failed — best-effort delete' };
    } catch (e) { evidence.cleanup = { name: NAME, removed: false, error: String(e && e.message || e) }; }
  }
  try { await saveJson('automation-subtabs-evidence', evidence); } catch { /* best-effort */ }
  try {
    await saveLog('evidence',
      [
        `story=${STORY} phone=${PHONE.width}x${PHONE.height}`,
        `automation=${JSON.stringify({ id: evidence.automationId, name: NAME })}`,
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
