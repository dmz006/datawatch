#!/usr/bin/env node
/**
 * Automaton detail tabs at phone width — the eight-tab strip scrolls inside itself, never the page
 * tags: surface:pwa feature:automaton-detail-tabs conflict:pwa-pwa
 *
 * A 390x844 touch context (hasTouch + isMobile) opens the detail view of an
 * Automaton that has scan and rules enabled, so all eight tabs show
 * (Overview, Stories, Decisions, Memory, Subscriptions, Scan, Rules). The
 * detail record is route-fulfilled, so the story needs no daemon data.
 *
 * For each tab: page.tap it, it becomes the one active tab, the tab body's
 * innerHTML differs from the previous tab's, and
 * document.scrollingElement.scrollWidth <= clientWidth. The strip itself must
 * overflow (scrollWidth > clientWidth) with overflow-x auto, and after the
 * last tab is tapped the active tab lies inside the strip's visible bounds.
 * The page-width check repeats at 320 and 430 px, and every Automation
 * sub-tab and every visible bottom-nav item is tapped with the page width
 * held. Sandbox only: the base URL comes from TEST_HTTP via perf-lib/lib.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, openPWA, fulfillEndpoint, check, saveJson,
} from './perf-lib.mjs';

const PRD_ID = 'tabs-mobile-1';
const TAB_KEYS = ['overview', 'stories', 'decisions', 'memory', 'subscriptions', 'scan', 'rules'];

const prd = {
  id: PRD_ID,
  title: 'Phone width detail tabs',
  spec: 'Seeded Automaton with scan and rules enabled so all eight detail tabs render.',
  status: 'needs_review',
  scan_enabled: true,
  rules_enabled: true,
  stories: [{
    id: 's1', title: 'First story', status: 'pending',
    tasks: [{ id: 't1', title: 'First task', status: 'pending', spec: 'Do the first thing.' }],
  }],
  decisions: [{ id: 'd1', kind: 'plan', actor: 'operator', note: 'seeded decision', created_at: '2026-11-05T10:00:00Z' }],
};

// Page width as the story asserts it: the scrolling element must not be wider than its viewport.
const pageFit = (page) => page.evaluate(() => {
  const se = document.scrollingElement;
  return { scrollWidth: se.scrollWidth, clientWidth: se.clientWidth, inner: window.innerWidth };
});

async function assertPageFits(page, label) {
  const f = await pageFit(page);
  check(f.scrollWidth <= f.clientWidth,
    `${label}: page scrolls sideways (scrollWidth ${f.scrollWidth} > clientWidth ${f.clientWidth}, viewport ${f.inner})`);
}

const tabBodyHTML = (page) => page.evaluate(() => {
  const b = document.querySelector('#automataDetailBody .prd-detail-tab-body');
  return b ? b.innerHTML : null;
});

// The active tab's box against the strip's visible box.
const activeInStrip = (page) => page.evaluate(() => {
  const strip = document.querySelector('#automataDetailBody .prd-detail-tabs');
  const active = strip && strip.querySelector('.output-tab.active');
  if (!strip || !active) return null;
  const s = strip.getBoundingClientRect();
  const a = active.getBoundingClientRect();
  return { aLeft: a.left, aRight: a.right, sLeft: s.left, sRight: s.right, label: active.textContent.trim() };
});

// Tap every tab in order and run the per-tab assertions. `full` adds the body-changed check.
async function sweepTabs(page, width, { full }) {
  let prev = await tabBodyHTML(page);
  for (const key of TAB_KEYS) {
    const sel = `#automataDetailBody .prd-detail-tabs .output-tab[onclick="switchAutomataDetailTab('${key}')"]`;
    const btn = await page.$(sel);
    check(btn, `${width}px: no "${key}" tab in the strip`);
    // Overview is the default active tab, so tapping it re-renders identical HTML: no body-change check there.
    const wasActive = await page.$eval(sel, (e) => e.classList.contains('active'));
    await page.tap(sel);
    await page.waitForFunction((k) => {
      const a = document.querySelectorAll('#automataDetailBody .prd-detail-tabs .output-tab.active');
      return a.length === 1 && (a[0].getAttribute('onclick') || '').includes(`'${k}'`);
    }, key, { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(250);

    const actives = await page.$$eval('#automataDetailBody .prd-detail-tabs .output-tab.active',
      (els) => els.map((e) => e.getAttribute('onclick')));
    check(actives.length === 1 && actives[0].includes(`'${key}'`),
      `${width}px: tapping "${key}" left active tabs ${JSON.stringify(actives)}`);

    if (full && !wasActive) {
      const cur = await tabBodyHTML(page);
      check(cur !== null, `${width}px: no tab body after tapping "${key}"`);
      check(cur !== prev, `${width}px: tab body innerHTML did not change after tapping "${key}"`);
      prev = cur;
    } else if (full) {
      prev = await tabBodyHTML(page);
    }
    await assertPageFits(page, `${width}px after tapping "${key}"`);
  }
  // The last tab (Rules) is the furthest right: it must be revealed inside the strip.
  const r = await activeInStrip(page);
  check(r, `${width}px: no active tab found after the last tap`);
  check(r.aLeft >= r.sLeft - 1 && r.aRight <= r.sRight + 1,
    `${width}px: active tab "${r.label}" outside the strip's visible bounds (tab ${r.aLeft.toFixed(1)}..${r.aRight.toFixed(1)}, strip ${r.sLeft.toFixed(1)}..${r.sRight.toFixed(1)})`);
}

const clearToasts = (page) => page.evaluate(() => { document.querySelectorAll('.toast-container').forEach((c) => { c.innerHTML = ''; }); }).catch(() => {});

async function openDetail(page) {
  await page.evaluate(() => window.navigate('autonomous'));
  await page.waitForTimeout(300);
  await page.evaluate((id) => window.renderPRDDetailView(id), PRD_ID);
  await page.waitForSelector('#automataDetailBody .prd-detail-tabs .output-tab', { timeout: 15000 });
}

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  // Phone context: touch, mobile emulation, 390x844 (cold: empty cache and storage).
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  const p = await context.newPage();
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });
    await openPWA(p);

    await fulfillEndpoint(p, `/api/autonomous/prds/${PRD_ID}`, prd);
    await fulfillEndpoint(p, '/api/autonomous/prds', { prds: [prd] });

    await openDetail(p);
    await p.waitForTimeout(500);
    const labels = await p.$$eval('#automataDetailBody .prd-detail-tabs .output-tab', (els) => els.map((e) => e.textContent.trim()));
    check(labels.length === 8, `expected all eight tabs with scan and rules enabled, got ${labels.length}: ${labels.join(', ')}`);

    // The strip overflows itself, scrolls with overflow-x auto, and the page does not widen.
    const strip = await p.evaluate(() => {
      const s = document.querySelector('#automataDetailBody .prd-detail-tabs');
      return { scrollWidth: s.scrollWidth, clientWidth: s.clientWidth, overflowX: getComputedStyle(s).overflowX };
    });
    check(strip.overflowX === 'auto', `strip overflow-x is "${strip.overflowX}", expected auto`);
    check(strip.scrollWidth > strip.clientWidth,
      `strip does not overflow at 390px (scrollWidth ${strip.scrollWidth}, clientWidth ${strip.clientWidth})`);
    await assertPageFits(p, '390px on open');
    await screenshot(p, 'detail-tabs-390-open');

    // 390: the full per-tab assertions.
    await sweepTabs(p, 390, { full: true });
    await screenshot(p, 'detail-tabs-390-last-tab');

    // 320 and 430: the page-width assertion repeats on every tab, and the last tab stays revealed.
    for (const width of [320, 430]) {
      await p.setViewportSize({ width, height: 844 });
      await p.waitForTimeout(300);
      await openDetail(p);
      await p.waitForTimeout(300);
      await assertPageFits(p, `${width}px on open`);
      await sweepTabs(p, width, { full: false });
      await screenshot(p, `detail-tabs-${width}-last-tab`);
    }

    // Back to 390 for the navigation sweeps.
    await p.setViewportSize({ width: 390, height: 844 });
    await p.waitForTimeout(300);

    // Every Automation sub-tab: tap it, it activates, the page keeps its width.
    await p.evaluate(() => window.navigate('autonomous'));
    await p.waitForSelector('.automation-subtabs .output-tab', { timeout: 15000 });
    const subtabs = await p.$$eval('.automation-subtabs .output-tab', (els) => els.map((e) => e.getAttribute('data-subtab')));
    check(subtabs.length > 0, 'no Automation sub-tabs found');
    for (const id of subtabs) {
      const sel = `.automation-subtabs .output-tab[data-subtab="${id}"]`;
      await clearToasts(p);
      await p.tap(sel);
      await p.waitForTimeout(300);
      const cls = await p.$eval(sel, (e) => e.classList.contains('active')).catch(() => false);
      check(cls, `Automation sub-tab "${id}" not active after tap`);
      await assertPageFits(p, `Automation sub-tab "${id}"`);
    }
    await screenshot(p, 'automation-subtabs-390');

    // Every bottom-nav item that is shown.
    const views = await p.$$eval('.nav-btn[data-view]', (els) =>
      els.filter((e) => getComputedStyle(e).display !== 'none').map((e) => e.getAttribute('data-view')));
    check(views.length > 0, 'no visible bottom-nav items found');
    for (const view of views) {
      await clearToasts(p);
      await p.tap(`.nav-btn[data-view="${view}"]`);
      await p.waitForTimeout(400);
      await assertPageFits(p, `bottom-nav "${view}"`);
    }
    await screenshot(p, 'bottom-nav-390');

    check(!dialogs.length, `dialog(s) fired during the story: ${dialogs.join('; ')}`);
    await saveJson('automaton-detail-tabs-mobile', { tabs: labels, strip, subtabs, views });
  } finally {
    await context.close().catch(() => {});
  }
});
