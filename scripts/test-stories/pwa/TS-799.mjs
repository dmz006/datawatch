#!/usr/bin/env node
/**
 * TS-799 — PWA connection stability through repeated connection flaps
 * tags: surface:pwa feature:pwa-connection-stability
 *
 * Flow:
 *   1. Create a shell (tmux) session inline via POST /api/sessions/start and open
 *      its session-detail terminal view.
 *   2. Type a marker command through the on-screen #sessionInput; wait for the
 *      marker in the xterm buffer.
 *   3. Register page.on('request' / 'requestfailed' / 'framenavigated'), then flap
 *      the connection 6 times (setOffline(true) ~1 s, setOffline(false) ~1.5 s) and
 *      force-close the WebSocket once (state.ws.close()).
 *
 * Assertions:
 *   1. zero main-frame navigations after the initial load, and a window marker set
 *      before flapping still exists afterwards;
 *   2. GET /api/sessions <= flaps+3, GET /api/schedules <= 5, and no requestfailed
 *      with ERR_INSUFFICIENT_RESOURCES;
 *   3. the terminal still holds the first marker after recovery, and a second
 *      marker typed after recovery appears within 3000 ms;
 *   4. state.reconnectAttempt is never reset between flaps shorter than the
 *      stability window (samples are non-decreasing) and ends > 0.
 * The session is always deleted afterwards.
 */
import { runStory, connectToPWA, screenshot, saveLog, env } from './lib.mjs';

const FLAPS = 6;
const OFFLINE_MS = 1000;
const ONLINE_MS = 1500;
const BUDGET_MS = 3000;
const MAX_SESSIONS_REQS = FLAPS + 3;
const MAX_SCHEDULE_REQS = 5;

async function api(page, path, method, body) {
  return page.evaluate(async ({ path, method, body, token }) => {
    const r = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: body ? JSON.stringify(body) : undefined,
    });
    return r.json().catch(() => ({}));
  }, { path, method, body, token: env.token });
}

async function readTerm(page) {
  return page.evaluate(() => {
    const t = state.terminal;
    if (!t) return '';
    const buf = t.buffer.active;
    const out = [];
    for (let i = 0; i < buf.length; i++) out.push(buf.getLine(i)?.translateToString(true) ?? '');
    return out.join('\n');
  });
}

// Type into the on-screen input and press Enter; return ms until marker appears (null on timeout).
async function typeAndTime(page, command, marker, budget = BUDGET_MS) {
  const input = page.locator('#sessionInput');
  await input.waitFor({ state: 'visible', timeout: 10000 });
  await input.click();
  await input.fill(command);
  const t0 = Date.now();
  await page.keyboard.press('Enter');
  while (Date.now() - t0 < budget) {
    if ((await readTerm(page)).includes(marker)) return Date.now() - t0;
    await page.waitForTimeout(50);
  }
  return null;
}

await runStory(async (page) => {
  await connectToPWA(page);

  const sess = await api(page, '/api/sessions/start', 'POST', {
    task: 'bash --norc',
    name: 'ts-799-conn-stability',
    backend: 'shell',
    project_dir: '/tmp',
    effort: 'quick',
  });
  const fullId = sess.full_id || sess.id;
  if (!fullId) throw new Error(`session start failed: ${JSON.stringify(sess)}`);

  try {
    await page.waitForFunction(
      (id) => state.sessions.some((s) => s.id === id || s.full_id === id),
      sess.id || fullId, { timeout: 15000 });
    const activeId = await page.evaluate((id) => {
      const s = state.sessions.find((x) => x.id === id || x.full_id === id);
      return s ? s.full_id : id;
    }, sess.id || fullId);
    await page.evaluate((id) => navigate('session-detail', id), activeId);
    await page.waitForFunction(() => state.terminal && document.getElementById('sessionInput'),
      null, { timeout: 15000 });
    await page.waitForTimeout(1500);
    await screenshot(page, '01-terminal-open');

    const m1 = 'ts799-' + (7000 + 1);   // 7001
    const ms1 = await typeAndTime(page, 'echo ts799-$((7000+1))', m1);
    if (ms1 === null) throw new Error(`first marker not visible within ${BUDGET_MS} ms before flapping`);

    // Register listeners BEFORE flapping.
    const sessionsReqs = [];
    const scheduleReqs = [];
    const failed = [];
    const navs = [];
    const mainFrame = page.mainFrame();
    page.on('request', (r) => {
      if (r.method() !== 'GET') return;
      const p = new URL(r.url()).pathname;
      if (p === '/api/sessions') sessionsReqs.push(r.url());
      else if (p === '/api/schedules') scheduleReqs.push(r.url());
    });
    page.on('requestfailed', (r) => {
      const f = r.failure();
      failed.push({ url: r.url(), errorText: f ? (f.errorText || '') : '' });
    });
    page.on('framenavigated', (f) => { if (f === mainFrame) navs.push(f.url()); });
    await page.evaluate(() => { window.__ts799Marker = 'no-reload'; });

    // Flap the connection, sampling the reconnect-attempt counter after each cycle.
    const ctx = page.context();
    const attemptSamples = [];
    const sample = async () => attemptSamples.push(await page.evaluate(() => state.reconnectAttempt || 0));
    for (let i = 0; i < FLAPS; i++) {
      await ctx.setOffline(true);
      await page.waitForTimeout(OFFLINE_MS);
      await ctx.setOffline(false);
      await page.waitForTimeout(ONLINE_MS);
      await sample();
      if (i === Math.floor(FLAPS / 2)) {
        await page.evaluate(() => { try { state.ws && state.ws.close(); } catch (_) {} });
        await page.waitForTimeout(ONLINE_MS);
        await sample();
      }
    }
    await screenshot(page, '02-after-flaps');

    // Recovery: connected again, then verify.
    await page.waitForFunction(() => state.connected && state.ws && state.ws.readyState === WebSocket.OPEN,
      null, { timeout: 30000 });
    await page.waitForTimeout(1000);

    const failures = [];
    const insuff = failed.filter((f) => /ERR_INSUFFICIENT_RESOURCES/.test(f.errorText));

    // (1) no reloads / navigations
    if (navs.length > 0) failures.push(`main frame navigated ${navs.length}x during flapping: ${navs.join(', ')}`);
    if (!(await page.evaluate(() => window.__ts799Marker === 'no-reload'))) failures.push('page reloaded (window marker lost)');

    // (2) bounded requests
    if (sessionsReqs.length > MAX_SESSIONS_REQS) failures.push(`GET /api/sessions = ${sessionsReqs.length} (limit ${MAX_SESSIONS_REQS})`);
    if (scheduleReqs.length > MAX_SCHEDULE_REQS) failures.push(`GET /api/schedules = ${scheduleReqs.length} (limit ${MAX_SCHEDULE_REQS})`);
    if (insuff.length > 0) failures.push(`${insuff.length} requestfailed with ERR_INSUFFICIENT_RESOURCES`);

    // (3) terminal content and live updates
    if (!(await readTerm(page)).includes(m1)) failures.push('first marker missing from terminal after recovery');
    const m2 = 'ts799-' + (8000 + 2);   // 8002
    const ms2 = await typeAndTime(page, 'echo ts799-$((8000+2))', m2);
    await screenshot(page, '03-after-recovery-command');
    if (ms2 === null) failures.push(`second marker not visible within ${BUDGET_MS} ms after recovery`);

    // (4) reconnect attempt counter not reset between short flaps
    for (let i = 1; i < attemptSamples.length; i++) {
      if (attemptSamples[i] < attemptSamples[i - 1]) {
        failures.push(`reconnectAttempt reset between flaps: samples=${JSON.stringify(attemptSamples)}`);
        break;
      }
    }
    if (Math.max(0, ...attemptSamples) < 1) failures.push(`reconnectAttempt never incremented: samples=${JSON.stringify(attemptSamples)}`);

    const summary = `navs=${navs.length} sessions=${sessionsReqs.length}/${MAX_SESSIONS_REQS} schedules=${scheduleReqs.length}/${MAX_SCHEDULE_REQS} ` +
      `insufficient=${insuff.length} second=${ms2}ms attempts=${JSON.stringify(attemptSamples)}`;
    console.log('TS-799', summary);
    await saveLog('result', summary);
    if (failures.length) throw new Error(failures.join('\n'));
  } finally {
    await api(page, '/api/sessions/delete', 'POST', { id: fullId }).catch(() => {});
  }
});
