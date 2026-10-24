#!/usr/bin/env node
/**
 * TS-811 — websocket bytes on a phone: cold connect, reconnect, idle, show history
 * tags: surface:pwa feature:load-performance conflict:pwa-pwa
 *
 * A 390x844 phone viewport, no throttling, opens the PWA against the seeded
 * fixture (3,000+ sessions) while every websocket frame the page receives is
 * counted by its JSON `type`. Four scenarios share one page:
 *   A  cold connect: bytes and the largest frame until the sessions list renders
 *   B  reconnect: the socket drops for 2.5 s; bytes in the 10 s after it is back
 *   C  idle view: one running session's detail view stays open for 60 s
 *   D  show history: toggling it fetches the summary list over REST and adds no
 *      large websocket frame (the REST response size is recorded too)
 * Budgets are BUDGET.wire* in perf-lib.mjs. The PWA announces the window form,
 * so each socket gets at most one `sessions` frame and none while idle. A
 * daemon without the seeded fixture skips (exit 2): the sizes measured here
 * mean nothing on a small set. Evidence: wire-bytes.json.
 */
import { screenshot, env } from './lib.mjs';
import {
  runPerfStory, coldPage, waitForState, openPWA, skipStory, countSessions,
  check, saveJson, BUDGET,
} from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const OUTAGE_MS = 2500;          // websocket offline before the network comes back
const AFTER_RECONNECT_MS = 10000; // measurement window after the reconnect
const SETTLE_MS = 3000;          // detail view's opening burst, not measured
const IDLE_MS = 60000;           // idle measurement window
const HISTORY_SETTLE_MS = 2000;  // frames after the history toggle

// Record every websocket the page opens. Each received frame keeps its socket
// index, arrival time, byte length and the JSON `type`; a frame that does not
// parse is recorded with type null so tally() can count it apart.
function tapWire(page) {
  const tap = { frames: [], sockets: 0, helloAcks: [] };
  page.on('websocket', (ws) => {
    const socket = tap.sockets++;
    ws.on('framereceived', ({ payload }) => {
      const text = typeof payload === 'string' ? payload : payload.toString('utf8');
      const bytes = typeof payload === 'string' ? Buffer.byteLength(payload, 'utf8') : payload.length;
      const frame = { socket, bytes, type: null };
      try {
        const msg = JSON.parse(text);
        if (msg && typeof msg.type === 'string') frame.type = msg.type;
        if (frame.type === 'hello_ack') tap.helloAcks.push((msg.data && msg.data.session_list) || '(none)');
        if (frame.type === 'sessions') {
          const rows = Array.isArray(msg.data) ? msg.data : msg.data && msg.data.sessions;
          frame.rows = Array.isArray(rows) ? rows.length : null;
          frame.total = msg.data && msg.data.total;
          frame.more = msg.data && msg.data.more;
          frame.windowMinutes = msg.data && msg.data.window_minutes;
        }
      } catch { /* not JSON: counted as unparsed */ }
      tap.frames.push(frame);
    });
  });
  return tap;
}

// Totals for a slice of frames: bytes and count per message type, unparsed
// frames apart, the largest single frame, and how many `sessions` frames each
// socket received (the window contract allows one per socket).
function tally(frames) {
  const out = {
    frames: frames.length,
    bytes: 0,
    byType: {},
    unparsed: { frames: 0, bytes: 0 },
    largest: { bytes: 0, type: null },
    sessionsPerSocket: {},
  };
  for (const f of frames) {
    out.bytes += f.bytes;
    if (f.bytes > out.largest.bytes) out.largest = { bytes: f.bytes, type: f.type };
    if (f.type === null) {
      out.unparsed.frames++;
      out.unparsed.bytes += f.bytes;
      continue;
    }
    const t = out.byType[f.type] || (out.byType[f.type] = { frames: 0, bytes: 0 });
    t.frames++;
    t.bytes += f.bytes;
    if (f.type === 'sessions') out.sessionsPerSocket[f.socket] = (out.sessionsPerSocket[f.socket] || 0) + 1;
  }
  out.maxSessionsPerSocket = Math.max(0, ...Object.values(out.sessionsPerSocket));
  out.sessionFrames = (out.byType.sessions && out.byType.sessions.frames) || 0;
  return out;
}

// The sizes only mean something on the seeded fixture, so a smaller daemon
// skips with a reason rather than passing.
async function requireSeededFixture() {
  const n = await countSessions();
  if (n < BUDGET.seedSessions) {
    await skipStory(`wire story needs the seeded fixture (${BUDGET.seedSessions}+ sessions); daemon has ${n}. Start it on the SeedPerfServer fixture (internal/server/perf_seed_test.go)`);
  }
  return n;
}

// Full id of one running session on the fixture, for the idle scenario.
async function firstRunningSession() {
  const r = await fetch(`${env.http}/api/sessions?view=summary&limit=100000`, {
    headers: { Authorization: `Bearer ${env.token}` },
  });
  if (!r.ok) throw new Error(`GET /api/sessions?view=summary -> ${r.status}`);
  const d = await r.json();
  const row = ((d && d.sessions) || []).find((s) => s.state === 'running');
  check(row, 'seeded fixture has no running session to open');
  return row.full_id || row.id;
}

await runPerfStory(async (page) => {
  const fixtureSessions = await requireSeededFixture();
  const { context, page: p } = await coldPage(page.context().browser(), { viewport: PHONE });
  const tap = tapWire(p); // before the PWA opens, so the cold connect is counted
  const pageErrors = [];
  p.on('pageerror', (e) => pageErrors.push(e.message));
  const result = {
    viewport: PHONE,
    fixtureSessions,
    budgets: {
      frameBytes: BUDGET.wireFrameBytes,
      coldBytes: BUDGET.wireColdBytes,
      reconnectBytes: BUDGET.wireReconnectBytes,
      idleBytesPerMin: BUDGET.wireIdleBytesPerMin,
    },
  };
  try {
    // A — cold connect: everything received until the sessions list renders.
    const coldStart = Date.now();
    await openPWA(p);
    await p.evaluate(() => window.navigate('sessions'));
    await waitForState(p, ['content', 'empty'], 30000);
    result.cold = { renderMs: Date.now() - coldStart, ...tally(tap.frames) };
    await screenshot(p, 'cold-connect').catch(() => {});

    // B — reconnect: drop the network for 2.5 s, then count the 10 s after the
    // socket is back. A new socket is expected; the outage may or may not close
    // the old one, so both cases are recorded.
    const socketsBeforeB = tap.sockets;
    const markB = tap.frames.length;
    await context.setOffline(true);
    await p.waitForTimeout(OUTAGE_MS);
    await context.setOffline(false);
    await p.waitForFunction(() => typeof state !== 'undefined' && state.connected === true, null, { timeout: 30000 });
    const connectedAt = Date.now();
    await p.waitForTimeout(AFTER_RECONNECT_MS);
    result.reconnect = {
      newSocket: tap.sockets > socketsBeforeB,
      windowMs: Date.now() - connectedAt,
      ...tally(tap.frames.slice(markB)),
    };

    // C — idle view: one running session's detail view, left open for 60 s.
    const sessionId = await firstRunningSession();
    await p.evaluate((sid) => window.navigate('session-detail', sid), sessionId);
    await p.waitForFunction(() => typeof state !== 'undefined' && state.activeView === 'session-detail', null, { timeout: 30000 });
    await p.waitForTimeout(SETTLE_MS);
    const idleStart = Date.now();
    const markC = tap.frames.length;
    await p.waitForTimeout(IDLE_MS);
    const idleMs = Date.now() - idleStart;
    const idle = tally(tap.frames.slice(markC));
    result.idle = { sessionId, idleMs, bytesPerMin: Math.round((idle.bytes * 60000) / idleMs), ...idle };
    await screenshot(p, 'idle-detail').catch(() => {});

    // D — show history: the toggle must fetch the summary list over REST and
    // send no large websocket frame. The response is matched on its URL before
    // the click so it cannot be missed.
    await p.evaluate(() => window.navigate('sessions'));
    await waitForState(p, ['content', 'empty'], 30000);
    const markD = tap.frames.length;
    const listResponse = p.waitForResponse((r) => {
      const u = new URL(r.url());
      return r.request().method() === 'GET' && u.pathname === '/api/sessions'
        && u.searchParams.get('view') === 'summary' && u.searchParams.get('limit') === '50';
    }, { timeout: 30000 });
    await p.locator('.btn-toggle-history', { hasText: 'History' }).first().click();
    const resp = await listResponse;
    const restBody = await resp.body();
    await p.waitForTimeout(HISTORY_SETTLE_MS);
    const u = new URL(resp.url());
    result.history = {
      showHistory: await p.evaluate(() => state.showHistory === true),
      rest: {
        path: `${u.pathname}${u.search}`,
        status: resp.status(),
        bytes: restBody.length,
        historyParam: u.searchParams.get('history'),
      },
      ...tally(tap.frames.slice(markD)),
    };
    await screenshot(p, 'show-history').catch(() => {});

    result.helloAcks = tap.helloAcks;
    result.sockets = tap.sockets;
    // What each sessions frame carried: rows, total, more (data.more) and the window.
    result.sessionFrames = tap.frames
      .filter((f) => f.type === 'sessions')
      .map(({ socket, bytes, rows, total, more, windowMinutes }) => ({ socket, bytes, rows, total, more, windowMinutes }));
    result.pageErrors = pageErrors;
    await saveJson('wire-bytes', result);

    check(tap.helloAcks.length > 0 && tap.helloAcks.every((m) => m === 'window'),
      `client did not get the window form: hello_ack session_list ${JSON.stringify(tap.helloAcks)}`);
    for (const [label, t, budget] of [
      ['cold connect', result.cold, BUDGET.wireColdBytes],
      ['reconnect', result.reconnect, BUDGET.wireReconnectBytes],
    ]) {
      check(t.largest.bytes <= BUDGET.wireFrameBytes,
        `${label}: largest frame ${t.largest.bytes} bytes (${t.largest.type}), budget ${BUDGET.wireFrameBytes}`);
      check(t.bytes <= budget, `${label}: ${t.bytes} bytes received, budget ${budget}`);
      check(t.maxSessionsPerSocket <= 1,
        `${label}: ${t.maxSessionsPerSocket} sessions frames on one socket; the window form allows one`);
    }
    check(result.idle.bytesPerMin <= BUDGET.wireIdleBytesPerMin,
      `idle: ${result.idle.bytesPerMin} bytes per minute, budget ${BUDGET.wireIdleBytesPerMin}`);
    check(result.idle.sessionFrames === 0,
      `idle: ${result.idle.sessionFrames} sessions frames while the detail view was open`);
    check(result.idle.largest.bytes <= BUDGET.wireFrameBytes,
      `idle: largest frame ${result.idle.largest.bytes} bytes, budget ${BUDGET.wireFrameBytes}`);
    check(result.history.showHistory, 'Show history did not turn on');
    check(result.history.rest.historyParam === '1', `history list request had history=${result.history.rest.historyParam}, want 1`);
    check(result.history.largest.bytes <= BUDGET.wireFrameBytes,
      `show history: largest frame ${result.history.largest.bytes} bytes (${result.history.largest.type}), budget ${BUDGET.wireFrameBytes}`);
    check(pageErrors.length === 0, `page errors: ${pageErrors.slice(0, 3).join(' | ')}`);
  } finally {
    await context.close().catch(() => {});
  }
});
