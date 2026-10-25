// scripts/test-stories/pwa/shared-nav-mobile-fit-probe.mjs — report-only mobile-fit probe
// for the shared navigation and every visible tab.
//
// This is a SUBSTITUTE for scripts/test-stories/pwa/shared-nav-mobile-fit.mjs, which is
// not on the branch. It measures the rendered PWA directly with two checks:
//   tap-target          interactive element smaller than 44x44 CSS px
//   horizontal-overflow element whose box extends past the layout viewport width
// Only elements that intersect the viewport vertically at the time of measurement are
// counted, so off-screen list rows are not reported.
//
// It drives the system google-chrome over the DevTools protocol, so it needs no npm
// install (the pwa node_modules is a symlink outside this worktree).
//
// Env:
//   TEST_TLS             sandbox daemon HTTPS base URL (the PWA origin for localStorage)
//   TEST_TOKEN           sandbox auth token
//   PROBE_OUT_DIR        directory for report-only.json and occurrences.json (required)
//   PROBE_CHROME_PROFILE chrome --user-data-dir (required, kept inside the project)
//   PROBE_DAEMON_VERSION daemon version string recorded into the report

import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { writeFile } from 'node:fs/promises';
import path from 'node:path';

const OUT_DIR = process.env.PROBE_OUT_DIR;
const PROFILE = process.env.PROBE_CHROME_PROFILE;
const TLS = process.env.TEST_TLS;
const TOKEN = process.env.TEST_TOKEN;
if (!OUT_DIR || !PROFILE || !TLS || !TOKEN) {
  console.error('shared-nav-mobile-fit-probe: PROBE_OUT_DIR, PROBE_CHROME_PROFILE, TEST_TLS and TEST_TOKEN are required');
  process.exit(2);
}

const CHROME = process.env.DW_CHROME_PATH || '/usr/bin/google-chrome';
const MIN_TARGET = 44;
const VIEWPORTS = [
  { name: '320x568', width: 320, height: 568 },
  { name: '360x740', width: 360, height: 740 },
  { name: '375x667', width: 375, height: 667 },
  { name: '390x844', width: 390, height: 844 },
  { name: '414x896', width: 414, height: 896 },
  { name: '768x1024', width: 768, height: 1024 },
];

// Runs inside the page. Returns one record per finding.
function auditPage(minTarget) {
  const selector = 'a[href], button, input:not([type="hidden"]), select, textarea, [role="button"], [onclick], [data-view]';
  const vw = document.documentElement.clientWidth;
  const vh = window.innerHeight;

  function hiddenByAncestor(el) {
    for (let p = el; p; p = p.parentElement) {
      const s = getComputedStyle(p);
      if (s.display === 'none' || s.visibility === 'hidden') return true;
    }
    return false;
  }

  function describe(el) {
    const id = el.id ? '#' + el.id : '';
    const cls = [...el.classList].slice(0, 3).map((c) => '.' + c).join('');
    const dv = el.getAttribute('data-view') ? `[data-view=${el.getAttribute('data-view')}]` : '';
    return el.tagName.toLowerCase() + id + cls + dv;
  }

  const out = [];
  for (const el of document.querySelectorAll(selector)) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    if (r.bottom <= 0 || r.top >= vh) continue;
    if (hiddenByAncestor(el)) continue;

    const navEl = el.closest('#nav');
    const header = el.closest('header');
    const region = navEl ? 'bottom-nav' : header ? 'header' : 'tab-content';
    const inNavIcon = !!el.closest('.nav-icon');
    const text = (el.getAttribute('aria-label') || el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 40);
    const base = {
      selector: describe(el),
      text,
      region,
      inNavIcon,
      w: Math.round(r.width * 10) / 10,
      h: Math.round(r.height * 10) / 10,
    };

    if (r.width < minTarget || r.height < minTarget) {
      out.push({ ...base, check: 'tap-target' });
    }
    if (r.right > vw + 0.5 || r.left < -0.5) {
      out.push({ ...base, check: 'horizontal-overflow' });
    }
  }

  const pageOverflow = document.documentElement.scrollWidth > document.documentElement.clientWidth + 0.5;
  return { findings: out, pageOverflow };
}

// --- minimal DevTools protocol client ---------------------------------------

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

class CDP {
  constructor(wsUrl) {
    this.ws = new WebSocket(wsUrl);
    this.nextId = 0;
    this.pending = new Map();
    this.listeners = [];
  }

  open() {
    return new Promise((resolve, reject) => {
      this.ws.onopen = () => resolve();
      this.ws.onerror = (e) => reject(new Error('websocket error'));
      this.ws.onmessage = (ev) => this.onMessage(JSON.parse(ev.data));
    });
  }

  onMessage(msg) {
    if (msg.id !== undefined && this.pending.has(msg.id)) {
      const { resolve, reject } = this.pending.get(msg.id);
      this.pending.delete(msg.id);
      if (msg.error) reject(new Error(`${msg.error.message}`));
      else resolve(msg.result);
    } else if (msg.method) {
      for (const l of [...this.listeners]) l(msg);
    }
  }

  send(method, params = {}) {
    const id = ++this.nextId;
    this.ws.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
  }

  waitForEvent(method, timeout = 30000) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`timeout waiting for ${method}`)), timeout);
      const listener = (msg) => {
        if (msg.method !== method) return;
        clearTimeout(timer);
        this.listeners = this.listeners.filter((x) => x !== listener);
        resolve(msg.params);
      };
      this.listeners.push(listener);
    });
  }

  async evaluate(expression) {
    const r = await this.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (r.exceptionDetails) {
      throw new Error(`evaluate failed: ${r.exceptionDetails.exception?.description || r.exceptionDetails.text}`);
    }
    return r.result.value;
  }

  async poll(expression, timeout = 20000) {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      if (await this.evaluate(expression)) return;
      await sleep(200);
    }
    throw new Error(`poll timed out: ${expression.slice(0, 80)}`);
  }

  async navigate(url) {
    const loaded = this.waitForEvent('Page.loadEventFired');
    await this.send('Page.navigate', { url });
    await loaded;
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// --- run ---------------------------------------------------------------------

const port = await freePort();
const chrome = spawn(CHROME, [
  '--headless=new',
  '--no-sandbox',
  '--disable-gpu',
  '--disable-dev-shm-usage',
  '--ignore-certificate-errors',
  '--no-first-run',
  '--no-default-browser-check',
  '--disable-background-networking',
  `--remote-debugging-port=${port}`,
  `--user-data-dir=${PROFILE}`,
  'about:blank',
], { stdio: 'ignore' });

const occurrences = [];
const pageLevel = [];
const viewsSeen = {};

try {
  // Wait for the DevTools endpoint, then pick the first page target.
  let targets = null;
  for (let i = 0; i < 100 && !targets; i++) {
    try {
      const list = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json());
      targets = list.filter((t) => t.type === 'page');
      if (targets.length === 0) targets = null;
    } catch {
      await sleep(200);
    }
  }
  if (!targets) throw new Error('chrome DevTools endpoint did not come up');

  const cdp = new CDP(targets[0].webSocketDebuggerUrl);
  await cdp.open();
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');

  // Log in the same way lib.mjs connectToPWA does: set the token, reload, wait for splash.
  await cdp.navigate(TLS + '/');
  await cdp.evaluate(`localStorage.setItem('cs_token', ${JSON.stringify(TOKEN)})`);
  await cdp.navigate(TLS + '/');
  await cdp.poll(`(() => {
    const el = document.getElementById('splash');
    if (!el) return true;
    const cs = getComputedStyle(el);
    return el.style.display === 'none' || el.classList.contains('hidden') || cs.display === 'none' || cs.opacity === '0';
  })()`);

  for (const vp of VIEWPORTS) {
    const mobile = vp.width < 600;
    await cdp.send('Emulation.setDeviceMetricsOverride', {
      width: vp.width,
      height: vp.height,
      deviceScaleFactor: 1,
      mobile,
    });
    await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: mobile });
    await sleep(400);

    const views = await cdp.evaluate(
      `[...document.querySelectorAll('.nav-btn')].filter(b => b.offsetParent !== null).map(b => b.dataset.view)`,
    );
    viewsSeen[vp.name] = views;

    for (const view of views) {
      await cdp.evaluate(`document.querySelector('[data-view="${view}"]').click()`);
      await cdp.poll(`document.querySelector('[data-view="${view}"]')?.classList.contains('active')`, 5000);
      await sleep(400);
      const { findings, pageOverflow } = await cdp.evaluate(`(${auditPage.toString()})(${MIN_TARGET})`);
      for (const f of findings) {
        occurrences.push({ viewport: vp.name, tab: view, ...f });
      }
      pageLevel.push({ viewport: vp.name, tab: view, pageOverflow });
    }
  }
  cdp.ws.close();
} finally {
  chrome.kill();
}

// Aggregate: one group per (check, selector, region, inNavIcon).
const groups = new Map();
for (const o of occurrences) {
  const key = [o.check, o.selector, o.region, o.inNavIcon].join('|');
  if (!groups.has(key)) {
    groups.set(key, {
      check: o.check,
      selector: o.selector,
      region: o.region,
      inNavIcon: o.inNavIcon,
      count: 0,
      viewports: new Set(),
      tabs: new Set(),
      sizes: new Set(),
      sampleText: o.text,
    });
  }
  const g = groups.get(key);
  g.count += 1;
  g.viewports.add(o.viewport);
  g.tabs.add(o.tab);
  g.sizes.add(`${o.w}x${o.h}`);
}

const groupList = [...groups.values()]
  .map((g) => ({
    check: g.check,
    selector: g.selector,
    region: g.region,
    inNavIcon: g.inNavIcon,
    count: g.count,
    viewports: [...g.viewports].sort(),
    tabs: [...g.tabs].sort(),
    sizes: [...g.sizes].sort(),
    sampleText: g.sampleText,
  }))
  .sort((a, b) => b.count - a.count);

const report = {
  story: 'shared-nav-mobile-fit',
  mode: 'report-only',
  substitute_for: 'scripts/test-stories/pwa/shared-nav-mobile-fit.mjs (absent from this branch)',
  status: 'run',
  daemon_version: process.env.PROBE_DAEMON_VERSION || 'unknown',
  checks: {
    'tap-target': `interactive element under ${MIN_TARGET}x${MIN_TARGET} CSS px`,
    'horizontal-overflow': 'element box extends past the viewport width',
  },
  viewports: VIEWPORTS.map((v) => v.name),
  views_per_viewport: viewsSeen,
  total_occurrences: occurrences.length,
  page_horizontal_scroll: pageLevel.filter((p) => p.pageOverflow).map((p) => `${p.viewport}:${p.tab}`),
  findings: groupList,
};

await writeFile(path.join(OUT_DIR, 'report-only.json'), JSON.stringify(report, null, 2) + '\n');
await writeFile(path.join(OUT_DIR, 'occurrences.json'), JSON.stringify(occurrences, null, 2) + '\n');
console.log(`shared-nav-mobile-fit-probe: ${occurrences.length} occurrences in ${groupList.length} groups`);
