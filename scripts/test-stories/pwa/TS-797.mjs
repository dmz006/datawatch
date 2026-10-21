#!/usr/bin/env node
/**
 * TS-797 — PWA schedule-load flood stays bounded after a WS reconnect (20+ sessions)
 * tags: surface:pwa feature:schedules
 *
 * Verifies the /api/schedules request-flood fix in internal/server/web/app.js:
 * with 20+ live sessions, recovering a session-detail view from a WS
 * disconnect must NOT fan out a per-session /api/schedules fetch per update
 * (which on a phone hits the browser's per-origin connection limit and surfaces
 * as `net::ERR_INSUFFICIENT_RESOURCES`). The fixed app.js routes every
 * refresh through `_loadSessionSchedules`, a coalescer (1 s min-interval +
 * in-flight `pending` flag), and only the active detail session refreshes
 * (see `applySessionUpdate`).
 *
 * Setup:
 *   - Seeds ≥ 20 shell sessions via POST /api/sessions/start (never depends on
 *     pre-existing data) and ALWAYS deletes+kills them afterwards, even when
 *     assertions fail (finally-block, best-effort).
 *   - Registers page.on('request'), page.on('requestfailed') and
 *     page.on('response') BEFORE connecting to the PWA in both scenarios.
 *   - The same scenario runs against two origins:
 *       (A) untrusted self-signed TLS origin (TEST_TLS): Playwright context
 *           with ignoreHTTPSErrors; the service worker cannot claim control there.
 *       (B) trusted TLS origin: $TEST_TRUSTED_TLS if provided, else generated on
 *           the fly (openssl throwaway CA + server cert with SAN 127.0.0.1 + a
 *           Node TLS reverse proxy in front of the daemon's TLS port, Chrome
 *           launched with --ignore-certificate-errors-spki-list=<base64(sha256(SPKI))>).
 *           Here `navigator.serviceWorker.controller` is asserted to be set
 *           BEFORE request counting begins (fresh profile → wait for the
 *           registration to activate, reload once, then assert).
 *       If (B) is not feasible on this host (no $TEST_TRUSTED_TLS, no
 *       openssl, or proxy failure) the scenario is recorded as SKIP with a
 *       clear reason (exit 2 + result.txt), never a silent pass.
 *
 * Pass criteria (per scenario):
 *   1. total GET /api/schedules requests ≤ 5 (constant — independent of the
 *      20+ seeded sessions) across the whole run, including the reconnect burst,
 *   2. zero `requestfailed` events whose failure text contains
 *      ERR_INSUFFICIENT_RESOURCES,
 *   3. afterwards /api/sessions and /api/stats still succeed (app-side batch
 *      fetch plus a direct in-page fetch of each endpoint).
 *
 * Reconnect method: page.context().setOffline(true) for 2.5 s (closes the WS
 * and drives the app's scheduleReconnect retry loop), then setOffline(false)
 * and wait for state.connected, then a 4 s settle window for the batch
 * reconnect handlers before assertions.
 *
 * Exit codes: 0 pass · 1 fail · 2 skip ((B) infeasible; see result.txt)
 *
 * Env: TS797_SESSION_COUNT (default 20) · TEST_TRUSTED_TLS (optional trusted
 *      origin for scenario B) · standard story env: TEST_HTTP / TEST_TLS /
 *      TEST_TOKEN / EVIDENCE_DIR / STORY (see lib.mjs)
 *
 * Evidence: EVIDENCE_DIR/TS-797/*.png + logs.jsonl (+ trusted-tls/ key-certs when (B) self-generates)
 */
import { chromium } from 'playwright';
import { env, connectToPWA, screenshot, saveLog, storyEvidenceDir } from './lib.mjs';
import { mkdir, writeFile } from 'node:fs/promises';
import { accessSync, readFileSync } from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import http from 'node:http';
import https from 'node:https';
import tls from 'node:tls';
import { spawnSync } from 'node:child_process';

const CHROME_PATH = '/usr/bin/google-chrome';
const MAX_SCHEDULE_REQS = 5;
const SEED_COUNT = Math.max(20, Number(process.env.TS797_SESSION_COUNT) || 20);
const isInter = !!env.interactive;
const EVID = storyEvidenceDir();
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const daemonBase = new URL(env.http);
const tlsTarget = new URL(env.tls); // proxy target: daemon TLS origin (self-signed)

const httpHeaders = { 'Content-Type': 'application/json' };
if (env.token) httpHeaders.Authorization = `Bearer ${env.token}`;

async function api(p, method = 'GET', body) {
  const r = await fetch(daemonBase.origin + p, {
    method,
    headers: httpHeaders,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  if (!r.ok) throw new Error(`api ${method} ${p} -> ${r.status}: ${text.slice(0, 300)}`);
  try { return text ? JSON.parse(text) : null; } catch { return text; }
}

function whichCmd(cmd) {
  for (const d of (process.env.PATH || '').split(':')) {
    if (!d) continue;
    const f = path.join(d, cmd);
    try { accessSync(f); return f; } catch { /* keep looking */ }
  }
  return null;
}

// ── Session seeding / guaranteed cleanup ────────────────────────────────────
const seededIds = [];

async function seedSessions() {
  for (let i = 0; i < SEED_COUNT; i += 5) {
    const batch = [];
    for (let j = 0; j < 5 && i + j < SEED_COUNT; j++) {
      const n = i + j;
      batch.push(
        api('/api/sessions/start', 'POST', {
          task: `ts-797 flood seed #${n} (echo ts797-${n} && sleep 240)`,
          name: `ts-797-seed-${n}`,
          backend: 'shell',
          project_dir: '/tmp',
          effort: 'quick',
        }).then(d => { if (d && d.id) seededIds.push(d.id); })
          .catch(e => { throw new Error(`seed ${n} failed: ${e.message}`); }),
      );
    }
    await Promise.all(batch);
  }
  if (seededIds.length < SEED_COUNT) {
    throw new Error(`only ${seededIds.length}/${SEED_COUNT} sessions seeded — cannot verify a ≥20-session flood story`);
  }
}

async function cleanupSessions() {
  await Promise.all(seededIds.map(async (id) => {
    try { await api('/api/sessions/delete', 'POST', { id }); } catch { /* best-effort */ }
    try { await api('/api/sessions/state', 'POST', { id, state: 'killed' }); } catch { /* best-effort */ }
  }));
  console.log(`[TS-797] cleanup: disposed ${seededIds.length} seeded session(s)`);
}

// ── Scenario (one origin) ───────────────────────────────────────────────────
async function runScenario(page, { label, base, trusted }) {
  const counters = {
    schedules: [],                       // every /api/schedules request while counting
    failed: [],                          // every requestfailed event
    okSessionsAfter: false,              // app-side /api/sessions success post-reconnect
    okStatsAfter: false,                 // app-side /api/stats success post-reconnect
    pageErrors: [],
    before: 0,
    after: 0,
    swController: null,
    liveness: { sessions: false, stats: false },
  };

  // Listeners BEFORE connecting to the PWA — capture the whole lifecycle.
  const failedHandler = (rf) => {
    const f = rf.failure();
    counters.failed.push({ url: rf.url(), errorText: f ? (f.errorText || '') : '' });
  };
  const pageErrorHandler = (e) => counters.pageErrors.push(String(e).slice(0, 300));
  const respHandler = (resp) => {
    const u = resp.url();
    if (resp.status() >= 400) return;
    if (u.includes('/api/schedules')) return;
    if (u.includes('/api/sessions')) counters.okSessionsAfter = true;
    if (u.includes('/api/stats')) counters.okStatsAfter = true;
  };
  page.on('requestfailed', failedHandler);
  page.on('pageerror', pageErrorHandler);
  page.on('response', respHandler);

  // Devtools-style concurrency instrumentation, active from the first byte:
  // peak in-flight request count per phase (load vs reconnect window).
  // "all" = everything sharing the per-origin pool (HTTP/1.1: 6 concurrent
  // per origin — the phone/cellular case); "schedules" = /api/schedules only.
  const IS_SCHED = (url) => (url || '').includes('/api/schedules');
  const concurrency = {
    all:       { load: { peak: 0 }, reconnect: { peak: 0 } },
    schedules: { load: { peak: 0 }, reconnect: { peak: 0 } },
  };
  let phase = 'load';
  let active = 0;
  let activeSched = 0;
  const bumpPeaks = () => {
    if (active > concurrency.all[phase].peak) concurrency.all[phase].peak = active;
    if (activeSched > concurrency.schedules[phase].peak) concurrency.schedules[phase].peak = activeSched;
  };
  const retireRequest = (r) => {
    active = Math.max(0, active - 1);
    if (IS_SCHED(r.url())) activeSched = Math.max(0, activeSched - 1);
  };
  page.on('request', (r) => {
    active += 1;
    if (IS_SCHED(r.url())) activeSched += 1;
    bumpPeaks();
  });
  page.on('requestfinished', retireRequest);
  page.on('requestfailed', retireRequest);

  let reqHandler = null;
  let counting = false;
  const installCounting = () => {
    if (counting) return;
    counting = true;
    reqHandler = (r) => {
      if (r.url().includes('/api/schedules')) {
        counters.schedules.push(`${r.method()} ${new URL(r.url()).pathname}${new URL(r.url()).search}`);
      }
    };
    page.on('request', reqHandler);
  };
  if (!trusted) installCounting(); // untrusted origin: count from first byte

  await connectToPWA(page, { base });

  if (trusted) {
    // Fresh browser profile: the app's registerServiceWorker() runs during the
    // first load but the client is not in control until the next navigation.
    await page.waitForFunction(async () => {
      if (!('serviceWorker' in navigator)) return false;
      const regs = await navigator.serviceWorker.getRegistrations();
      return regs.length > 0 && !!regs[0].active;
    }, null, { timeout: 25000 });
    await page.reload({ waitUntil: 'domcontentloaded' });
    // SW must be controlling the page BEFORE we start counting requests.
    await page.waitForFunction(
      () => 'serviceWorker' in navigator && !!navigator.serviceWorker && !!navigator.serviceWorker.controller,
      null,
      { timeout: 25000 },
    );
    counters.swController = true;
    // Re-wait for the WS to re-establish across the reload.
    await page.waitForFunction(
      () => typeof state !== 'undefined' && state.connected === true && state.sessions && state.sessions.length > 0,
      null,
      { timeout: 30000 },
    );
    installCounting();
  }

  // Open the first session card → its detail view.
  await page.waitForSelector('.session-card[data-full-id]', { timeout: 20000 });
  const fullId = await page.evaluate(() => document.querySelector('.session-card[data-full-id]').dataset.fullId);
  console.log(`[TS-797][${label}] opened session detail: ${fullId}`);
  await page.evaluate((id) => navigate('session-detail', id), fullId);
  await page.waitForFunction(
    () => typeof state !== 'undefined' && state.activeView === 'session-detail' && !!state.activeSession,
    null,
    { timeout: 15000 },
  );
  await sleep(2500); // let the initial detail schedule fetch settle
  counters.before = counters.schedules.length;

  // ── Flood window: WS disconnect → recover ────────────────────────────────
  const ctx = page.context();
  phase = 'reconnect';
  await ctx.setOffline(true);
  await sleep(2500);
  await ctx.setOffline(false);
  await page.waitForFunction(() => typeof state !== 'undefined' && state.connected === true, null, { timeout: 30000 });
  await sleep(4000); // batch reconnect handlers + settle
  counters.after = counters.schedules.length;

  counters.concurrency = concurrency;
  // One stable log line so before/after comparisons can be diffed directly.
  console.log(
    `[TS-797][${label}] concurrency peaks — ` +
    `load: all=${concurrency.all.load.peak} schedules=${concurrency.schedules.load.peak} · ` +
    `reconnect: all=${concurrency.all.reconnect.peak} schedules=${concurrency.schedules.reconnect.peak} · ` +
    `total /api/schedules=${counters.schedules.length} ` +
    `(page-load+detail=${counters.before}, reconnect-window=${counters.after - counters.before})`,
  );

  // App endpoints still succeed after the reconnect: direct in-page fetches
  // (go through the SW on the trusted origin — same path as the app).
  counters.liveness = await page.evaluate(async () => {
    const h = { 'Authorization': `Bearer ${localStorage.getItem('cs_token') || ''}` };
    const out = { sessions: false, stats: false };
    try { const r = await fetch('/api/sessions', { headers: h }); out.sessions = r.ok; } catch { /* counted as fail */ }
    try { const r = await fetch('/api/stats', { headers: h }); out.stats = r.ok; } catch { /* counted as fail */ }
    return out;
  });

  return counters;
}

function assertScenario(label, c) {
  const failures = [];
  const insuff = c.failed.filter((f) => /ERR_INSUFFICIENT_RESOURCES/.test(f.errorText));
  if (c.schedules.length > MAX_SCHEDULE_REQS) {
    failures.push(`${label}: /api/schedules requests = ${c.schedules.length} (limit ${MAX_SCHEDULE_REQS}; ${SEED_COUNT} sessions seeded; before-reconnect=${c.before}, reconnect-window=${c.after - c.before}) — schedule-load flood not bounded`);
  }
  if (insuff.length > 0) {
    failures.push(`${label}: ${insuff.length} requestfailed event(s) with ERR_INSUFFICIENT_RESOURCES, e.g. ${insuff.slice(0, 3).map((f) => f.url).join(', ')}`);
  }
  if (!c.okSessionsAfter && !c.liveness.sessions) failures.push(`${label}: /api/sessions no longer succeeds after reconnect`);
  if (!c.liveness.sessions) failures.push(`${label}: direct fetch of /api/sessions after reconnect was not OK`);
  if (!c.liveness.stats) failures.push(`${label}: direct fetch of /api/stats after reconnect was not OK`);
  if (c.swController === false) failures.push(`${label}: navigator.serviceWorker.controller was not set on the trusted origin`);
  if (failures.length > 0) throw new Error(failures.join('\n'));
}

// ── Scenario (B) trusted origin: throwaway CA + TLS reverse proxy ───────────
async function startTrustedProxy() {
  const openssl = whichCmd('openssl');
  if (!openssl) throw new Error('no TEST_TRUSTED_TLS and no openssl on PATH');

  const dir = path.join(EVID, 'trusted-tls');
  await mkdir(dir, { recursive: true });
  const run = (args) => {
    const r = spawnSync(openssl, args, { encoding: 'utf8' });
    if (r.status !== 0) throw new Error(`openssl ${args.join(' ')} failed: ${(r.stderr || '').slice(0, 400)}`);
    return r.stdout;
  };
  run(['genpkey', '-algorithm', 'RSA', '-out', dir + '/ca.key']);
  run(['req', '-x509', '-new', '-key', dir + '/ca.key', '-sha256', '-days', '2', '-subj', '/CN=TS797-local-CA', '-out', dir + '/ca.crt']);
  run(['genpkey', '-algorithm', 'RSA', '-out', dir + '/server.key']);
  run(['req', '-new', '-key', dir + '/server.key', '-subj', '/CN=127.0.0.1', '-out', dir + '/server.csr']);
  await writeFile(dir + '/ext.cnf', 'subjectAltName=IP:127.0.0.1,DNS:localhost\n');
  run(['x509', '-req', '-in', dir + '/server.csr', '-CA', dir + '/ca.crt', '-CAkey', dir + '/ca.key',
       '-CAcreateserial', '-days', '2', '-sha256', '-extfile', dir + '/ext.cnf', '-out', dir + '/server.crt']);

  const key = readFileSync(dir + '/server.key');
  const cert = readFileSync(dir + '/server.crt');
  // Chrome --ignore-certificate-errors-spki-list (Chrome 155) takes the SHA-256
  // of the cert's SPKI DER as base64 (see net::HashValue::FromString).
  const spkiDer = crypto.createPublicKey(cert).export({ type: 'spki', format: 'der' });
  const spki = crypto.createHash('sha256').update(spkiDer).digest('base64');

  const targetHeaders = (req) => ({ ...req.headers, host: `${tlsTarget.hostname}:${tlsTarget.port}` });
  const server = https.createServer({ key, cert }, (req, res) => {
    const up = https.request(
      {
        host: tlsTarget.hostname, port: Number(tlsTarget.port),
        path: new URL(req.url, 'http://x').pathname + new URL(req.url, 'http://x').search,
        method: req.method, headers: targetHeaders(req), rejectUnauthorized: false,
      },
      (upRes) => {
        res.writeHead(upRes.statusCode, upRes.headers);
        upRes.pipe(res);
      },
    );
    up.on('error', () => { try { res.writeHead(502, { 'content-type': 'text/plain' }); res.end('TS797 proxy error'); } catch { /* closed */ } });
    req.pipe(up);
  });

  // WebSocket relay via a RAW tls socket. (Node's https.request 'upgrade' event
  // is unreliable for 101 responses — the client socket can receive a plain
  // 'response' first, which would break the WS handshake before the browser
  // sees the 101.) We rewrite the client request head, and once the upstream
  // replies, splice both directions verbatim.
  server.on('upgrade', (req, clientSocket) => {
    console.log(`[TS-797][proxy] WS upgrade: ${req.method} ${req.url}`);
    const p = new URL(req.url, 'http://x').pathname + new URL(req.url, 'http://x').search;
    const up = tls.connect({
      host: tlsTarget.hostname, port: Number(tlsTarget.port),
      servername: tlsTarget.hostname, rejectUnauthorized: false,
    });
    clientSocket.on('error', () => { try { up.destroy(); } catch { /* closed */ } });
    up.on('error', (e) => {
      console.log(`[TS-797][proxy] relay error: ${e && e.message}`);
      try {
        clientSocket.write(`HTTP/1.1 502 TS797 relay error\r\ncontent-type: text/plain\r\n\r\n${e ? String(e.message).slice(0, 100) : 'upstream error'}`);
      } catch { /* socket gone */ }
      try { clientSocket.end(); up.destroy(); } catch { /* closed */ }
    });
    let sentHead = false;
    up.on('secureConnect', () => {
      if (sentHead) return;
      sentHead = true;
      const lines = [`${req.method} ${p} HTTP/1.1`, `host: ${tlsTarget.hostname}:${tlsTarget.port}`];
      for (let i = 0; i < req.rawHeaders.length; i += 2) {
        const k = req.rawHeaders[i].toLowerCase();
        if (k === 'host') continue;
        if (k === 'origin') { lines.push(`origin: https://${tlsTarget.hostname}:${tlsTarget.port}`); continue; }
        if (k === 'referer') { lines.push(`referer: https://${tlsTarget.hostname}:${tlsTarget.port}/`); continue; }
        lines.push(`${req.rawHeaders[i]}: ${req.rawHeaders[i + 1]}`);
      }
      up.write(lines.join('\r\n') + '\r\n\r\n');
    });
    let acc = Buffer.alloc(0);
    let rel = false;
    up.on('data', (d) => {
      if (rel) { if (!clientSocket.destroyed) clientSocket.write(d); return; }
      acc = Buffer.concat([acc, d]);
      const i = acc.indexOf('\r\n\r\n');
      if (i === -1) return;
      const head = acc.subarray(0, i + 4);
      const rest = acc.subarray(i + 4);
      acc = null;
      rel = true;
      if (!/^HTTP\/1\.[01] 101 /i.test(head.toString('latin1'))) {
        console.log(`[TS-797][proxy] upstream rejected WS: ${head.toString('latin1').split('\r\n')[0]}`);
        // Not a 101 (auth reject etc): surface it, then close both legs.
        clientSocket.end(head.toString('binary'));
        up.destroy();
        return;
      }
      // Splice: pipe the 101 head, then transparently relay frames both ways.
      console.log(`[TS-797][proxy] 101 relayed for ${p}`);
      clientSocket.write(head, () => {
        if (rest.length) clientSocket.write(rest);
        up.removeAllListeners('data');
        up.on('data', (dd) => { if (!clientSocket.destroyed) clientSocket.write(dd); });
        clientSocket.on('data', (dd) => { if (!up.destroyed) up.write(dd); });
        clientSocket.on('end', () => { try { up.end(); } catch { /* closed */ } });
        up.on('end', () => { try { clientSocket.end(); } catch { /* closed */ } });
        clientSocket.on('error', () => { try { up.destroy(); } catch { /* closed */ } });
        up.on('error', () => { try { clientSocket.destroy(); } catch { /* closed */ } });
      });
    });
  });

  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const port = server.address().port;
  return {
    base: `https://127.0.0.1:${port}`,
    spki,
    stop: () => new Promise((resolve) => { server.closeAllConnections?.(); server.close(() => resolve()); }),
  };
}

// ── Scenario drivers ────────────────────────────────────────────────────────
const BASE_ARGS = [
  '--no-sandbox',
  '--disable-dev-shm-usage',
  '--no-first-run',
  '--no-default-browser-check',
  '--disable-features=TranslateUI,Translate',
  '--disable-extensions',
  '--disable-sync',
  '--disable-background-networking',
  '--disable-default-apps',
  '--disable-component-update',
];
const CONTEXT_OPTS = {
  viewport: { width: 1330, height: 800 },
  deviceScaleFactor: 2,
  userViewport: { width: 1330, height: 800 },
  reducedMotion: 'reduce',
};

async function launchChrome(extraArgs = [], ignoreHttps = true) {
  const browser = await chromium.launch({ executablePath: CHROME_PATH, args: [...BASE_ARGS, ...extraArgs], headless: !isInter });
  const ctx = await browser.newContext({ ignoreHTTPSErrors: ignoreHttps, ...CONTEXT_OPTS });
  ctx.setDefaultTimeout(15000);
  return { browser, ctx };
}

const results = [];
const failures = [];
let scenarioBSkip = null;

async function scenarioAUntrusted() {
  const label = 'A-untrusted-tls';
  const { browser, ctx } = await launchChrome([], true);
  const page = await ctx.newPage();
  try {
    const c = await runScenario(page, { label, base: env.tls, trusted: false });
    await screenshot(page, `ts797-${label}`);
    assertScenario(label, c);
    results.push({ label, ok: true, schedules: c.schedules.length });
    console.log(`[TS-797] PASS [${label}] /api/schedules=${c.schedules.length} (limit ${MAX_SCHEDULE_REQS}) failed-events=${c.failed.length}`);
  } finally {
    await browser.close().catch(() => {});
  }
  // NOTE: on throw, the browser is closed and the caller records the failure.
}

async function scenarioBTrusted() {
  const label = 'B-trusted-tls';
  // Resolve the trusted origin: explicit, or self-generated on this host.
  let trustedUrl = (process.env.TEST_TRUSTED_TLS || '').trim();
  let extraArgs = [];
  let proxy = null;
  if (!trustedUrl) {
    try {
      proxy = await startTrustedProxy();
      extraArgs = [`--ignore-certificate-errors-spki-list=${proxy.spki}`];
      trustedUrl = proxy.base;
      await saveLog('trusted-origin', JSON.stringify({ generated: true, base: trustedUrl, spki: proxy.spki }, null, 2));
    } catch (e) {
      throw { skip: `no TEST_TRUSTED_TLS and self-generation failed: ${e.message}` };
    }
  }
  const { browser, ctx } = await launchChrome(extraArgs, false);
  const page = await ctx.newPage();
  try {
    const c = await runScenario(page, { label, base: trustedUrl, trusted: true });
    await screenshot(page, `ts797-${label}`);
    assertScenario(label, c);
    results.push({ label, ok: true, schedules: c.schedules.length, swController: c.swController });
    console.log(`[TS-797] PASS [${label}] sw-controller=${c.swController} /api/schedules=${c.schedules.length} (limit ${MAX_SCHEDULE_REQS}) failed-events=${c.failed.length}`);
  } finally {
    await browser.close().catch(() => {});
    if (proxy) await proxy.stop().catch(() => {});
  }
}

// ── Main ────────────────────────────────────────────────────────────────────
console.log(`[TS-797] env: http=${env.http} tls=${env.tls} evidence=${EVID} seed=${SEED_COUNT} sessions`);
await mkdir(EVID, { recursive: true });
await seedSessions();
console.log(`[TS-797] seeded ${seededIds.length} sessions: ${seededIds.slice(0, 5).join(', ')}…`);

try {
  try {
    await scenarioAUntrusted();
  } catch (e) {
    failures.push(e instanceof Error ? e.message : String(e));
    await saveLog('failure-A.txt', String(e && e.message || e));
    console.error(`[TS-797] FAIL [A-untrusted-tls]: ${e && e.message || e}`);
  }
  try {
    await scenarioBTrusted();
  } catch (e) {
    if (e && e.skip) {
      scenarioBSkip = e.skip;
      await saveLog('SKIP-REASON.txt', `scenario B (trusted origin): ${e.skip}`);
      console.log(`[TS-797] SKIP [B-trusted-tls]: ${e.skip}`);
    } else {
      failures.push(e instanceof Error ? e.message : String(e));
      await saveLog('failure-B.txt', String(e && e.message || e));
      console.error(`[TS-797] FAIL [B-trusted-tls]: ${e && e.message || e}`);
    }
  }
} finally {
  await cleanupSessions().catch((e) => console.error(`[TS-797] cleanup error (ignored): ${e.message}`));
}

if (failures.length > 0) {
  await saveLog('verdict.txt', JSON.stringify({ verdict: 'FAIL', failures, schedules: results, seeded: seededIds.length }, null, 2));
  process.exit(1);
}
if (scenarioBSkip) {
  // Scenario A passed; B was infeasible on this host → record skip (rc 2)
  // rather than silently passing; run_pwa_story writes result.txt on rc 2.
  const why = scenarioBSkip;
  try { await writeFile(path.join(EVID, 'result.txt'), `SKIP: scenario B (trusted origin) infeasible — ${why}\n`); } catch { /* best-effort */ }
  await saveLog('verdict.txt', JSON.stringify({ verdict: 'SKIP', reason: why, scenarios: results }, null, 2));
  process.exit(2);
}
await saveLog('verdict.txt', JSON.stringify({ verdict: 'PASS', scenarios: results, seeded: seededIds.length }, null, 2));
console.log('[TS-797] PASS — schedule-load flood bounded on both origins; no ERR_INSUFFICIENT_RESOURCES; API healthy after reconnect');
process.exit(0);
