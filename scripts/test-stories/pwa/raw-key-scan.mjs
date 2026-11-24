#!/usr/bin/env node
/**
 * Raw locale keys on screen — the page-wide scan
 * tags: surface:pwa feature:i18n-raw-keys
 *
 * The locale bundle (/locales/<lang>.json) loads after the page starts. Any string
 * drawn before it is ready, or with no bundle entry, can show a raw key such as
 * pause_badge_paused. This story delays every /locales/*.json response by 3 s,
 * loads the PWA, and walks every string a person can see — each visible text node,
 * the document title, and the title, aria-label, placeholder and alt attributes —
 * and fails on any string that is a locale key: lowercase words joined by
 * underscores, or exactly a key of the English bundle, or such a key inside longer
 * text. A new module cannot ship with the problem.
 *
 * Phases (each in a fresh browser context, so nothing is cached):
 *  1. delayed-3s: load with the locale held 3 s; at 0.5 s and at 2 s scan the page
 *     (header, bottom navigation) and the pause window, then open every main view
 *     from the bottom navigation and scan each.
 *  2. held: load with the locale held until the walk ends; open every main view and
 *     every sub-tab and section in it (found from the page, so a new tab is covered
 *     without a change here) and scan each, then release the locale and scan again.
 *  3. loaded: load, wait for the bundle, then walk every view and sub-tab again.
 * /api/pause is route-fulfilled as paused so the header badge and window show.
 * Findings go to raw-key-findings.json in the evidence directory; the failure
 * message lists the first ones. Sandbox only: the base URL comes from TEST_HTTP.
 * RAW_KEY_ALLOW below lists strings that are allowed to look like keys, each with
 * the reason; keep it empty unless a string is shown on purpose.
 */
import { env } from './lib.mjs';
import { runPerfStory, check, saveJson } from './perf-lib.mjs';
import { COLLECT_SRC, findingsOf, formatFindings } from './raw-key-scan-lib.mjs';

// Strings that are shown on purpose and look like keys. Each entry names the string
// (text, texts or prefix) and, where it matters, the place (a substring of the element
// description), and gives the reason. Text that is API data lists in the page itself
// (the Settings > About MCP tool list) is tagged data-raw-key-ok="<reason>" instead.
const RAW_KEY_ALLOW = [
  { where: 'th.task-routing-type', reason: 'task type ids are the row keys of the task routing table, shown as configured' },
  {
    texts: ['guardrail_warn', 'scope_warning', 'final_gate', 'operator_interactive', 'scheduled_command', 'one_shot'],
    where: 'view=settings tab=automata', reason: 'decision, task and priority-source ids listed verbatim in the Automata settings',
  },
  { text: 'detect_prompt', where: 'view=settings tab=compute', reason: 'detection rule id listed verbatim on the Compute tab' },
  { text: 'device_aliases', where: 'code [view=settings', reason: 'configuration section name inside a code element' },
  { text: 'retry', where: 'automata-event-kind-retry', reason: 'event kind id on a filter chip' },
  {
    texts: ['list_schedule_results', 'autonomous_cache_folder_sweep', 'autonomous_temp_sweep', 'cpu_pct', 'mem_pct', 'disk_pct',
      'gpu_pct', 'uptime_seconds', 'sampled_at_unix_ms', 'plugin_enabled', 'tick_interval_ms', 'process_tree', 'ollama_tap',
      'ebpf_enabled', 'prd_id', 'story_id'],
    where: 'view=observer', reason: 'API field, job and scope names the Observer view shows verbatim',
  },
];

const LOCALE_DELAY_MS = 3000;
const CHECKPOINTS_MS = [500, 2000];
const MAX_TABS_PER_VIEW = 80;
const MAX_TAB_DEPTH = 3;

const PAUSED_STATE = {
  paused: true,
  global: { reason: 'raw key scan', by: 'story', at: '2026-11-24T00:00:00Z' },
  projects: [{ project: 'scan-project', reason: 'raw key scan', by: 'story', at: '2026-11-24T00:00:00Z' }],
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// englishKeys reads the English bundle through the browser (the sandbox daemon's
// plain-HTTP port may redirect to its self-signed TLS port, which node would refuse).
async function englishKeys(page) {
  await page.goto(env.http, { waitUntil: 'domcontentloaded', timeout: 60000 });
  const bundle = await page.evaluate(async () => {
    const res = await fetch('/locales/en.json', { cache: 'reload' });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    return res.json();
  });
  const keys = new Set(Object.keys(bundle));
  check(keys.size > 1000, `the English bundle has only ${keys.size} keys`);
  return keys;
}

// newContext: a cold phone-free desktop context whose locale responses wait for
// `hold()` (a promise-returning function) before they reach the page.
async function newContext(browser, hold) {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: 1280, height: 800 },
    serviceWorkers: 'block', // so the route below sees the locale requests
  });
  await context.addInitScript((token) => { try { localStorage.setItem('cs_token', token); } catch (e) { /* storage unavailable */ } }, env.token);
  await context.route('**/locales/*.json', async (route) => {
    await hold();
    try { await route.continue(); } catch { /* page closed */ }
  });
  await context.route('**/api/pause', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(PAUSED_STATE) });
  });
  return context;
}

const bundleReady = (page) => page.evaluate(() => !!(window._i18n && window._i18n.ready)).catch(() => false);

const stats = { scans: 0, strings: 0, preReadyScans: 0, views: new Set(), tabs: new Set() };
const findings = [];

async function scan(page, enKeys, at) {
  const ready = await bundleReady(page);
  const items = await page.evaluate(COLLECT_SRC);
  stats.scans++;
  stats.strings += items.length;
  if (!ready) stats.preReadyScans++;
  const f = findingsOf(items, enKeys, { at, ready, allowList: RAW_KEY_ALLOW });
  findings.push(...f);
  return f;
}

// visibleViews: the data-view of every bottom-navigation button that is shown.
const visibleViews = (page) => page.evaluate(() => [...document.querySelectorAll('.nav-btn[data-view]')]
  .filter((b) => (typeof b.checkVisibility === 'function' ? b.checkVisibility() : b.offsetWidth > 0))
  .map((b) => b.getAttribute('data-view')));

const openView = (page, view) => page.evaluate((v) => {
  const b = document.querySelector(`.nav-btn[data-view="${v}"]`);
  if (b) b.click();
}, view);

// pageTabs: the sub-tabs and sections shown in the current view, as
// { sig, text, strip, active }. sig identifies a tab across re-renders: its onclick, else
// data-tab, else its text. strip numbers the tab strips in page order (the first strip is
// the outermost), and active says whether the tab is the selected one in its strip.
const pageTabs = (page) => page.evaluate(() => {
  const vis = (b) => (typeof b.checkVisibility === 'function' ? b.checkVisibility() : b.offsetWidth > 0);
  const sigOf = (b) => b.getAttribute('onclick') || (b.getAttribute('data-tab') ? 'tab:' + b.getAttribute('data-tab') : (b.textContent || '').trim());
  const strips = [];
  return [...document.querySelectorAll('#view .output-tab, #view .settings-tab-btn, #view [role="tab"]')].filter(vis).map((b) => {
    let k = strips.indexOf(b.parentElement);
    if (k < 0) { strips.push(b.parentElement); k = strips.length - 1; }
    return { sig: sigOf(b), text: (b.textContent || '').trim().slice(0, 30), strip: k, active: b.classList.contains('active') || b.getAttribute('aria-selected') === 'true' };
  });
});

const clickTab = (page, sig) => page.evaluate((s) => {
  const b = [...document.querySelectorAll('#view .output-tab, #view .settings-tab-btn, #view [role="tab"]')]
    .find((x) => (x.getAttribute('onclick') || (x.getAttribute('data-tab') ? 'tab:' + x.getAttribute('data-tab') : (x.textContent || '').trim())) === s);
  if (!b) return false;
  b.click();
  return true;
}, sig);

// walkTabs reads every sub-tab and section of one view, and the tabs those open. It works
// from a queue of click paths: for each path the view is opened afresh and the tabs of the
// path are clicked in order (a click re-renders the view, so a list taken before it would
// go stale), the page is scanned, and the tabs that show up there and were not seen before
// are queued. The path to a tab is the selected tab of every outer strip, then the tab
// itself, so a second-row tab is reached through the first-row tab it belongs to.
async function walkTabs(page, enKeys, at, view, settle) {
  const seen = new Set();
  const names = new Map();
  const queue = [[]];
  let paths = 0;
  while (queue.length && paths < MAX_TABS_PER_VIEW) {
    const tabPath = queue.shift();
    paths++;
    await openView(page, view);
    await sleep(settle);
    let reached = true;
    for (const sig of tabPath) {
      if (!(await clickTab(page, sig))) { reached = false; break; }
      await sleep(settle);
    }
    if (!reached) {
      if (process.env.RAW_KEY_VERBOSE) console.log(`  ${at} path ${tabPath.map((sg) => names.get(sg) || sg).join('>')} could not be reached`);
      continue;
    }
    if (tabPath.length) {
      const label = `${at}>${tabPath.map((sg) => names.get(sg) || sg).join('>')}`;
      stats.tabs.add(label);
      await scan(page, enKeys, label);
    }
    if (tabPath.length >= MAX_TAB_DEPTH) continue;
    const here = await pageTabs(page);
    for (const tab of here) {
      if (seen.has(tab.sig)) continue;
      seen.add(tab.sig);
      names.set(tab.sig, tab.text || tab.sig);
      const outer = [];
      for (let k = 0; k < tab.strip; k++) {
        const sel = here.find((x) => x.strip === k && x.active);
        if (sel) outer.push(sel.sig);
      }
      queue.push([...outer, tab.sig]);
    }
  }
}

// walkViews opens every main view from the bottom navigation and scans it; with
// withTabs it also walks every sub-tab and section of each.
async function walkViews(page, enKeys, at, { settle, withTabs }) {
  for (const view of await visibleViews(page)) {
    await openView(page, view);
    await sleep(settle);
    stats.views.add(view);
    await scan(page, enKeys, `${at}/${view}`);
    if (withTabs) await walkTabs(page, enKeys, `${at}/${view}`, view, settle);
  }
}

// pauseWindow opens the pause window from the header, scans it, and closes it.
async function pauseWindow(page, enKeys, at) {
  const opened = await page.evaluate(() => { const b = document.getElementById('headerPauseBtn'); if (!b) return false; b.click(); return true; });
  if (!opened) return;
  await sleep(250);
  await scan(page, enKeys, `${at}/pause-window`);
  await page.keyboard.press('Escape').catch(() => {});
  await page.evaluate(() => { const m = document.getElementById('pauseModal'); if (m && m.parentElement) m.remove(); }).catch(() => {});
}

async function waitReady(page, timeout = 30000) {
  await page.waitForFunction(() => window._i18n && window._i18n.ready === true, null, { timeout });
  await page.waitForFunction(() => {
    const el = document.getElementById('splash');
    return !el || getComputedStyle(el).display === 'none' || getComputedStyle(el).opacity === '0' || el.classList.contains('hidden');
  }, null, { timeout });
  await sleep(500);
}

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const enKeys = await englishKeys(page);

  // Phase 1: the locale held 3 s; scan at 0.5 s and 2 s after the page starts.
  for (const ms of CHECKPOINTS_MS) {
    const context = await newContext(browser, () => sleep(LOCALE_DELAY_MS));
    const p = await context.newPage();
    try {
      const t0 = Date.now();
      await p.goto(env.http, { waitUntil: 'commit', timeout: 60000 });
      await p.waitForSelector('.nav-btn[data-view]', { state: 'attached', timeout: 20000 });
      const wait = ms - (Date.now() - t0);
      if (wait > 0) await sleep(wait);
      const at = `delayed-3s/${ms / 1000}s`;
      await scan(p, enKeys, `${at}/page`);
      await pauseWindow(p, enKeys, at);
      await walkViews(p, enKeys, at, { settle: 120, withTabs: false });
    } finally {
      await context.close().catch(() => {});
    }
  }

  // Phase 2: the locale held until every view and tab has been walked.
  {
    let release;
    const gate = new Promise((r) => { release = r; });
    const context = await newContext(browser, () => Promise.race([gate, sleep(120000)]));
    const p = await context.newPage();
    try {
      await p.goto(env.http, { waitUntil: 'commit', timeout: 60000 });
      await p.waitForSelector('.nav-btn[data-view]', { state: 'attached', timeout: 20000 });
      await sleep(500);
      await scan(p, enKeys, 'held/page');
      await pauseWindow(p, enKeys, 'held');
      await walkViews(p, enKeys, 'held', { settle: 250, withTabs: true });
      // The last tap made while the bundle was held is not lost: the boot opens that view.
      const lastTapped = await p.evaluate(() => localStorage.getItem('cs_active_view'));
      release();
      await waitReady(p);
      const shownView = await p.evaluate(() => (document.querySelector('.nav-btn.active') || {}).dataset?.view || '');
      check(lastTapped && shownView === lastTapped, `a tap on "${lastTapped}" before the bundle loaded was lost: the page opened "${shownView}"`);
      await scan(p, enKeys, 'held/after-release/page');
      await pauseWindow(p, enKeys, 'held/after-release');
    } finally {
      release && release();
      await context.close().catch(() => {});
    }
  }

  // Phase 3: the bundle loaded; walk everything again.
  {
    const context = await newContext(browser, () => sleep(LOCALE_DELAY_MS));
    const p = await context.newPage();
    try {
      await p.goto(env.http, { waitUntil: 'commit', timeout: 60000 });
      await waitReady(p);
      await scan(p, enKeys, 'loaded/page');
      await pauseWindow(p, enKeys, 'loaded');
      await walkViews(p, enKeys, 'loaded', { settle: 300, withTabs: true });
    } finally {
      await context.close().catch(() => {});
    }
  }

  const summary = {
    scans: stats.scans, strings_checked: stats.strings, scans_before_bundle_ready: stats.preReadyScans,
    views: [...stats.views], tab_scans: stats.tabs.size, tabs: [...stats.tabs], findings: findings.length,
  };
  await saveJson('raw-key-findings', { summary, findings });
  console.log('raw-key scan:', JSON.stringify({ ...summary, tabs: undefined }));
  if (process.env.RAW_KEY_VERBOSE) console.log('tabs scanned:\n' + summary.tabs.join('\n'));

  // The scan must have looked at the page before the bundle was ready, or it proves nothing.
  check(stats.preReadyScans >= 4, `only ${stats.preReadyScans} scans ran before the locale bundle was ready; the delay did not hold`);
  check(stats.views.size >= 3, `only ${stats.views.size} main views were opened: ${[...stats.views].join(', ')}`);
  check(stats.tabs.size >= 5, `only ${stats.tabs.size} sub-tab scans ran`);
  check(findings.length === 0, `${findings.length} raw locale key(s) on screen:\n${formatFindings(findings)}`);
});
