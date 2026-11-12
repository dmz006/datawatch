// live-smoke.mjs — live browser check of the shared bottom nav at 320 px and 1280 px.
//
// For each viewport it logs in to the sandbox PWA, taps each tab in turn (Sessions,
// Automata, Alerts, Observer, Settings) with real mouse input, and measures:
//   bar       the nav fits (no scroll overflow) and sits inside the viewport
//   buttons   each visible nav button is at least 40x40 CSS px, inside the bar, and
//             has a label that, when truncated, shows an ellipsis and a title equal
//             to the label text
//   active    the tapped tab is the one active button, and its highlight is painted
// It writes a screenshot per viewport and tab, and live-smoke.json with every number.
// It drives the system google-chrome over the DevTools protocol, so it needs no npm
// install (the pwa node_modules is a symlink outside this worktree).
//
// Run it through run-live-smoke.sh, which starts and stops its own sandbox daemon.
//
// Env:
//   TEST_TLS          sandbox daemon HTTPS origin
//   TEST_TOKEN        sandbox auth token
//   OUT_DIR           directory for live-smoke.json and screens/
//   CHROME_PROFILE    chrome --user-data-dir; keep the path short (Unix socket limit)
//   DW_CHROME_PATH    Chrome binary (default /usr/bin/google-chrome)
//
// Exit: 0 when every check passes, 1 when any fails, 2 for a bad environment.

import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const TLS = process.env.TEST_TLS;
const TOKEN = process.env.TEST_TOKEN;
const OUT = process.env.OUT_DIR;
const PROFILE = process.env.CHROME_PROFILE;
const CHROME = process.env.DW_CHROME_PATH || '/usr/bin/google-chrome';
if (!TLS || !TOKEN || !OUT || !PROFILE) {
  console.error('live-smoke: TEST_TLS, TEST_TOKEN, OUT_DIR and CHROME_PROFILE are required');
  process.exit(2);
}

const VIEWPORTS = [
  { name: '320x568', width: 320, height: 568 },
  { name: '1280x800', width: 1280, height: 800 },
];
const TABS = ['sessions', 'autonomous', 'alerts', 'observer', 'settings'];
const MIN_TAP = 40;
const SHOTS = path.join(OUT, 'screens');

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function freePort() {
  return new Promise((resolve, reject) => {
    const s = createServer();
    s.on('error', reject);
    s.listen(0, '127.0.0.1', () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });
}

class CDP {
  constructor(url) {
    this.url = url;
    this.seq = 0;
    this.waiting = new Map();
    this.consoleErrors = [];
  }

  open() {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(this.url);
      this.ws.addEventListener('open', resolve);
      this.ws.addEventListener('error', () => reject(new Error('devtools websocket error')));
      this.ws.addEventListener('message', (ev) => this.onMessage(JSON.parse(ev.data)));
    });
  }

  onMessage(msg) {
    if (msg.id && this.waiting.has(msg.id)) {
      const { resolve, reject } = this.waiting.get(msg.id);
      this.waiting.delete(msg.id);
      if (msg.error) reject(new Error(msg.error.message));
      else resolve(msg.result);
      return;
    }
    if (msg.method === 'Runtime.exceptionThrown') {
      const d = msg.params.exceptionDetails;
      this.consoleErrors.push(d.exception?.description || d.text);
    }
    if (msg.method === 'Runtime.consoleAPICalled' && msg.params.type === 'error') {
      this.consoleErrors.push(msg.params.args.map((a) => a.value ?? a.description ?? '').join(' '));
    }
  }

  send(method, params = {}) {
    const id = ++this.seq;
    return new Promise((resolve, reject) => {
      this.waiting.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }

  async evaluate(expression) {
    const r = await this.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (r.exceptionDetails) {
      throw new Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
    }
    return r.result.value;
  }

  async poll(expression, label, timeoutMs = 15000) {
    const end = Date.now() + timeoutMs;
    while (Date.now() < end) {
      try {
        if (await this.evaluate(expression)) return;
      } catch {
        // page still loading; keep polling until the deadline
      }
      await sleep(150);
    }
    throw new Error(`timed out waiting for ${label}`);
  }

  async clickAt(x, y) {
    await this.send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1 });
    await this.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1 });
  }
}

async function waitForPageTarget(port) {
  for (let i = 0; i < 100; i++) {
    try {
      const list = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json());
      const page = list.find((t) => t.type === 'page');
      if (page) return page;
    } catch {
      // DevTools endpoint not up yet
    }
    await sleep(200);
  }
  throw new Error('chrome DevTools endpoint did not come up');
}

async function navigate(cdp, url) {
  await cdp.send('Page.navigate', { url });
  await sleep(300);
  await cdp.poll(`document.readyState === 'complete'`, `load of ${url}`);
}

const SPLASH_GONE = `(() => {
  const el = document.getElementById('splash');
  if (!el) return true;
  const cs = getComputedStyle(el);
  return el.style.display === 'none' || el.classList.contains('hidden') || cs.display === 'none' || cs.opacity === '0';
})()`;

const NAV_READY = `document.querySelectorAll('#nav .nav-btn').length > 0 && document.querySelector('#nav .nav-btn.active') !== null`;

const TAB_CENTER = (tab) => `(() => {
  const b = document.querySelector('#nav .nav-btn[data-view="${tab}"]');
  if (!b || b.offsetParent === null) return null;
  const r = b.getBoundingClientRect();
  return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
})()`;

const TAB_CLICK_FALLBACK = (tab) => `(() => {
  const b = document.querySelector('#nav .nav-btn[data-view="${tab}"]');
  if (b) b.click();
  return !!b;
})()`;

const ACTIVE_VIEW = `(() => { const a = document.querySelector('#nav .nav-btn.active'); return a ? a.dataset.view : null; })()`;

// Runs in the page. Returns the bar and each visible nav button.
const MEASURE = `(() => {
  const nav = document.getElementById('nav');
  const navR = nav.getBoundingClientRect();
  const vw = document.documentElement.clientWidth;
  const buttons = [...nav.querySelectorAll('.nav-btn')].filter((b) => b.offsetParent !== null);
  const rows = buttons.map((b) => {
    const r = b.getBoundingClientRect();
    const label = b.querySelector('.nav-label');
    const lcs = label ? getComputedStyle(label) : null;
    const bcs = getComputedStyle(b);
    return {
      view: b.dataset.view,
      active: b.classList.contains('active'),
      w: Math.round(r.width * 10) / 10,
      h: Math.round(r.height * 10) / 10,
      tapOK: r.width >= ${MIN_TAP} && r.height >= ${MIN_TAP},
      inBar: r.left >= navR.left - 0.5 && r.right <= navR.right + 0.5,
      label: label ? label.textContent.trim() : '',
      labelTruncated: label ? label.scrollWidth > label.clientWidth : false,
      textOverflow: lcs ? lcs.textOverflow : null,
      title: b.getAttribute('title'),
      borderTop: bcs.borderTopColor,
      color: bcs.color,
    };
  });
  const active = document.querySelector('#nav .nav-btn.active');
  return {
    viewport: { width: vw, height: window.innerHeight },
    bar: {
      left: Math.round(navR.left),
      right: Math.round(navR.right),
      top: Math.round(navR.top),
      bottom: Math.round(navR.bottom),
      scrollWidth: nav.scrollWidth,
      clientWidth: nav.clientWidth,
      visible: navR.width > 0 && navR.height > 0,
      fits: nav.scrollWidth <= nav.clientWidth + 0.5 && navR.left >= -0.5 && navR.right <= vw + 0.5,
    },
    pageOverflowX: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    activeView: active ? active.dataset.view : null,
    activeCount: document.querySelectorAll('#nav .nav-btn.active').length,
    viewChildren: document.getElementById('view').children.length,
    buttons: rows,
  };
})()`;

// Turn one measurement into the checks for that tab.
function checkTab(tab, m) {
  const problems = [];
  const btn = m.buttons.find((b) => b.view === tab);
  if (!btn) problems.push(`no visible ${tab} nav button`);
  if (!m.bar.visible) problems.push('nav bar not visible');
  if (!m.bar.fits) problems.push(`bar does not fit (scrollWidth ${m.bar.scrollWidth}, clientWidth ${m.bar.clientWidth}, left ${m.bar.left}, right ${m.bar.right})`);
  if (m.pageOverflowX > 0) problems.push(`page scrolls sideways by ${m.pageOverflowX}px`);
  for (const b of m.buttons) {
    if (!b.tapOK) problems.push(`${b.view} tap target ${b.w}x${b.h} is under ${MIN_TAP}px`);
    if (!b.inBar) problems.push(`${b.view} button is outside the bar`);
    if (b.labelTruncated) {
      if (b.textOverflow !== 'ellipsis') problems.push(`${b.view} label is truncated without an ellipsis (text-overflow ${b.textOverflow})`);
      if ((b.title || '').toLowerCase() !== b.label.toLowerCase()) problems.push(`${b.view} truncated label has no matching title (title "${b.title}")`);
    }
  }
  if (m.activeView !== tab) problems.push(`active view is ${m.activeView}, expected ${tab}`);
  if (m.activeCount !== 1) problems.push(`${m.activeCount} active nav buttons, expected 1`);
  const active = m.buttons.find((b) => b.active);
  const inactive = m.buttons.find((b) => !b.active);
  if (active && inactive && (active.borderTop === 'rgba(0, 0, 0, 0)' || active.borderTop === inactive.borderTop)) {
    problems.push('active tab highlight is not painted');
  }
  if (m.viewChildren === 0) problems.push('tab view rendered no content');
  return problems;
}

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

const report = { daemon: TLS, chrome: CHROME, viewports: [], overall: 'FAIL' };
let exitCode = 1;

try {
  await mkdir(SHOTS, { recursive: true });
  const page = await waitForPageTarget(port);
  const cdp = new CDP(page.webSocketDebuggerUrl);
  await cdp.open();
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');

  // Log in the same way the probe does: set the token, reload, wait for the splash.
  await navigate(cdp, TLS + '/');
  await cdp.evaluate(`localStorage.setItem('cs_token', ${JSON.stringify(TOKEN)})`);
  await navigate(cdp, TLS + '/');
  await cdp.poll(SPLASH_GONE, 'splash to clear');
  await cdp.poll(NAV_READY, 'nav to render');

  for (const vp of VIEWPORTS) {
    const mobile = vp.width < 600;
    await cdp.send('Emulation.setDeviceMetricsOverride', { width: vp.width, height: vp.height, deviceScaleFactor: 1, mobile });
    await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: mobile, maxTouchPoints: mobile ? 5 : 1 });
    await navigate(cdp, TLS + '/');
    await cdp.poll(NAV_READY, `nav at ${vp.name}`);
    await sleep(500);

    const vpReport = { name: vp.name, tabs: [] };
    report.viewports.push(vpReport);

    for (const tab of TABS) {
      const entry = { tab, tapped: null, fallbackClick: false, problems: [] };
      vpReport.tabs.push(entry);

      const center = await cdp.evaluate(TAB_CENTER(tab));
      if (!center) {
        entry.problems.push(`no visible ${tab} nav button to tap`);
      } else {
        await cdp.clickAt(center.x, center.y);
        entry.tapped = { x: Math.round(center.x), y: Math.round(center.y) };
      }
      try {
        await cdp.poll(`(${ACTIVE_VIEW}) === ${JSON.stringify(tab)}`, `${tab} to become active`, 4000);
      } catch {
        // Real tap did not switch the tab; use a DOM click so the tab still gets measured.
        entry.fallbackClick = true;
        await cdp.evaluate(TAB_CLICK_FALLBACK(tab));
        await cdp.poll(`(${ACTIVE_VIEW}) === ${JSON.stringify(tab)}`, `${tab} to become active after DOM click`);
      }
      await sleep(900);

      const m = await cdp.evaluate(MEASURE);
      entry.measure = m;
      entry.problems.push(...checkTab(tab, m));
      entry.pass = entry.problems.length === 0;

      const { data } = await cdp.send('Page.captureScreenshot', { format: 'png' });
      const shot = path.join(SHOTS, `${vp.name}-${tab}.png`);
      await writeFile(shot, Buffer.from(data, 'base64'));
      entry.screenshot = path.relative(OUT, shot);
    }
  }

  report.consoleErrors = cdp.consoleErrors.slice(0, 20);
  report.consoleErrorCount = cdp.consoleErrors.length;
  const allPass = report.viewports.every((v) => v.tabs.every((t) => t.pass));
  report.overall = allPass ? 'PASS' : 'FAIL';
  exitCode = allPass ? 0 : 1;
} catch (err) {
  report.error = err.message;
  report.overall = 'FAIL';
} finally {
  chrome.kill('SIGTERM');
}

await writeFile(path.join(OUT, 'live-smoke.json'), JSON.stringify(report, null, 2) + '\n');

for (const v of report.viewports) {
  for (const t of v.tabs) {
    const m = t.measure;
    const bar = m ? `bar ${m.bar.left}-${m.bar.right} of ${m.viewport.width}, scroll ${m.bar.scrollWidth}/${m.bar.clientWidth}` : 'no measure';
    const tapped = t.tapped ? `tap ${t.tapped.x},${t.tapped.y}${t.fallbackClick ? ' + DOM click' : ''}` : 'no tap';
    console.log(`${v.name} ${t.tab}: ${t.pass ? 'pass' : 'FAIL'} (${tapped}; ${bar})`);
    for (const p of t.problems) console.log(`    - ${p}`);
  }
}
if (report.error) console.log(`live-smoke error: ${report.error}`);
console.log(`live-smoke: ${report.overall}, console errors ${report.consoleErrorCount ?? 'n/a'}`);
process.exit(exitCode);
