#!/usr/bin/env node
// scripts/perf-measure-tabs.mjs — time to first content for the main web UI tabs.
//
// Measures, against a running daemon (TEST_HTTP / TEST_TOKEN, same variables as
// the PWA stories), how long each tab takes from navigate() to the first
// non-empty, non-"loading" content in #view. Tabs: Sessions, Automata,
// Automation, Observer, Settings. Prints one JSON document on stdout.
//
// Usage:
//   TEST_HTTP=http://127.0.0.1:18080 TEST_TOKEN=... node scripts/perf-measure-tabs.mjs
//   node scripts/perf-measure-tabs.mjs --slow-4g        # also run a throttled pass
//   node scripts/perf-measure-tabs.mjs --slow-4g-only   # throttled pass only
//   node scripts/perf-measure-tabs.mjs --timeout 120000 # per-tab ceiling in ms
//
// Pair it with a daemon started on seeded data (see SeedPerfServer in
// internal/server/perf_seed_test.go) for a repeatable run.

import { launchBrowser, connectToPWA, env } from './test-stories/pwa/lib.mjs';

const TABS = [
  { name: 'sessions', view: 'sessions' },
  { name: 'automata', view: 'autonomous' },
  { name: 'automation', view: 'automation' },
  { name: 'observer', view: 'observer' },
  { name: 'settings', view: 'settings' },
];

// Chrome DevTools Protocol Network.emulateNetworkConditions, "slow 4G"
// (the DevTools preset: 1.6 Mbps down, 750 Kbps up, 150 ms RTT).
const SLOW_4G = {
  offline: false,
  latency: 150,
  downloadThroughput: (1.6 * 1024 * 1024) / 8,
  uploadThroughput: (750 * 1024) / 8,
};

const args = process.argv.slice(2);
const flag = (n) => args.includes(n);
const opt = (n, d) => {
  const i = args.indexOf(n);
  return i >= 0 && args[i + 1] ? args[i + 1] : d;
};
const TIMEOUT = Number(opt('--timeout', '60000'));

// Runs in the page: resolves with ms until #view first holds real content after navigate().
function timeToFirstContent({ view, timeout }) {
  return new Promise((resolve) => {
    const el = document.getElementById('view');
    const t0 = performance.now();
    const ready = () => {
      const txt = (el.innerText || '').trim();
      return txt.length > 0 && !/^\s*(loading|…|\.\.\.)/i.test(txt) && el.children.length > 0;
    };
    let done = false;
    const finish = (timedOut) => {
      if (done) return;
      done = true;
      mo.disconnect();
      clearTimeout(timer);
      resolve({ ms: Math.round(performance.now() - t0), timed_out: timedOut });
    };
    const mo = new MutationObserver(() => { if (ready()) finish(false); });
    mo.observe(el, { childList: true, subtree: true, characterData: true });
    const timer = setTimeout(() => finish(true), timeout);
    el.innerHTML = '';          // drop the previous tab's content so we time the new render
    window.navigate(view);
    if (ready()) finish(false);
  });
}

async function runPass(label, throttle) {
  const browser = await launchBrowser();
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true });
  const page = await ctx.newPage();
  let bytes = 0;
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('Network.enable');
  cdp.on('Network.loadingFinished', (e) => { bytes += e.encodedDataLength || 0; });

  const pass = { profile: label, load: null, tabs: {} };
  try {
    if (throttle) await cdp.send('Network.emulateNetworkConditions', throttle);
    const t0 = Date.now();
    await connectToPWA(page, { timeout: TIMEOUT });
    pass.load = { ms: Date.now() - t0, transferred_bytes: bytes };

    for (const tab of TABS) {
      const before = bytes;
      const exists = await page.evaluate((v) => typeof window.navigate === 'function' && !!document.querySelector(`[data-view="${v}"]`), tab.view);
      if (!exists) {
        pass.tabs[tab.name] = { skipped: 'tab not present in this build/config' };
        continue;
      }
      const r = await page.evaluate(timeToFirstContent, { view: tab.view, timeout: TIMEOUT })
        .catch((e) => ({ error: String(e) }));
      pass.tabs[tab.name] = { ...r, transferred_bytes: bytes - before };
    }
  } catch (e) {
    pass.error = String(e);
  } finally {
    await browser.close();
  }
  return pass;
}

const out = { base: env.http, taken_at: new Date().toISOString(), passes: [] };
if (!flag('--slow-4g-only')) out.passes.push(await runPass('unthrottled', null));
if (flag('--slow-4g') || flag('--slow-4g-only')) out.passes.push(await runPass('slow-4g', SLOW_4G));
console.log(JSON.stringify(out, null, 2));
process.exit(out.passes.some((p) => p.error) ? 1 : 0);
