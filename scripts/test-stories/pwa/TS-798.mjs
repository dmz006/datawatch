#!/usr/bin/env node
/**
 * TS-798 — PWA terminal view live-updates, before and after a WebSocket reconnect
 * tags: surface:pwa feature:terminal-live-update
 *
 * Regression guard for the PWA terminal live-update stall (pane_capture frames
 * dropped client-side by the app.js throttle). Flow:
 *   1. Create a shell (tmux) session inline via POST /api/sessions/start.
 *   2. Open its session-detail terminal view in the PWA.
 *   3. Type a command into the on-screen #sessionInput and press Enter (the real
 *      input path, not a REST shortcut); assert its output shows up in the xterm
 *      buffer within 3000 ms, with no page reload.
 *   4. Force-close the page's WebSocket (state.ws.close()), wait for the PWA to
 *      reconnect on its own, then send a second command the same way and assert
 *      its output shows up within 3000 ms, still without a reload.
 * The echoed command line never contains the expected marker (it is produced by
 * shell arithmetic), so only real command output can satisfy the assertion.
 * The session is always deleted afterwards.
 */
import { runStory, connectToPWA, screenshot, saveLog, env } from './lib.mjs';

const BUDGET_MS = 3000;

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

// Type into the on-screen input and press Enter; return ms until marker appears.
async function typeAndTime(page, command, marker) {
  const input = page.locator('#sessionInput');
  await input.waitFor({ state: 'visible', timeout: 10000 });
  await input.click();
  await input.fill(command);
  const t0 = Date.now();
  await page.keyboard.press('Enter');
  while (Date.now() - t0 < BUDGET_MS) {
    if ((await readTerm(page)).includes(marker)) return Date.now() - t0;
    await page.waitForTimeout(50);
  }
  return null;
}

await runStory(async (page) => {
  await connectToPWA(page);
  await page.evaluate(() => { window.__ts798Marker = 'no-reload'; });

  const sess = await api(page, '/api/sessions/start', 'POST', {
    task: 'bash --norc',
    name: 'ts-798-terminal-live',
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
    // let the shell prompt settle
    await page.waitForTimeout(1500);
    await screenshot(page, '01-terminal-open');

    const m1 = 'ts798-' + (4000 + 1);   // 4001
    const ms1 = await typeAndTime(page, 'echo ts798-$((4000+1))', m1);
    await screenshot(page, '02-after-first-command');
    if (ms1 === null) throw new Error(`first command output not visible within ${BUDGET_MS} ms`);

    // Force the page's session WebSocket closed; the PWA must reconnect itself.
    const oldWs = await page.evaluate(() => { window.__ts798OldWs = state.ws; state.ws.close(); return true; });
    await page.waitForFunction(
      () => state.ws && state.ws !== window.__ts798OldWs && state.ws.readyState === WebSocket.OPEN && state.connected,
      null, { timeout: 20000 });
    await page.waitForTimeout(500);

    const m2 = 'ts798-' + (5000 + 2);   // 5002
    const ms2 = await typeAndTime(page, 'echo ts798-$((5000+2))', m2);
    await screenshot(page, '03-after-reconnect-command');
    if (ms2 === null) throw new Error(`second command output not visible within ${BUDGET_MS} ms after WS reconnect`);

    const noReload = await page.evaluate(() => window.__ts798Marker === 'no-reload');
    if (!noReload) throw new Error('page reloaded during the test');

    const summary = `first=${ms1}ms after-reconnect=${ms2}ms budget=${BUDGET_MS}ms`;
    console.log('TS-798', summary);
    await saveLog('result', summary);
  } finally {
    await api(page, '/api/sessions/delete', 'POST', { id: fullId }).catch(() => {});
  }
});
