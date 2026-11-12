#!/usr/bin/env node
/**
 * Trace card story — the session view's Trace card shows the coverage badge,
 *                    the roll-up and the event timeline, filters by tool and
 *                    errors, and degrades when tracing is off
 * tags: surface:pwa feature:agent-tracing
 *
 * Route fulfilment (no daemon data): GET /api/trace/session/{id} answers a
 * body with three events (one failed tool result, one whose argument text is
 * hostile HTML) and a roll-up; GET /api/trace/coverage answers a full level
 * for one backend. The card is mounted with window.mountTraceCard into a
 * holder element, as the session view does. It must show the title, the
 * coverage badge, the roll-up and each event line as text (a hostile value is
 * never a live <img>), the "Errors only" filter must cut the timeline to the
 * failed result, and the page must not scroll sideways at phone width. Then
 * the session route answers 404 "no trace for" (the card says nothing is
 * recorded) and finally 503 (the card says tracing is off), never a stale card.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint,
  check, noHorizontalOverflow,
} from './perf-lib.mjs';

const at = (sec) => new Date(Date.UTC(2026, 10, 4, 12, 0, sec)).toISOString();
const SESSION = 'trace-story-1';

// Wire shape of GET /api/trace/session/{id} (internal/server/trace_api.go).
const body = {
  session: SESSION,
  total: 3,
  events: [
    { session_id: SESSION, backend: 'pi', kind: 'tool_call', name: 'Read', arg_summary: '<img src=x onerror=alert(1)>', timestamp: at(1) },
    { session_id: SESSION, backend: 'pi', kind: 'tool_result', name: 'Read', outcome: 'error: no such file', duration_ms: 40, timestamp: at(2) },
    { session_id: SESSION, backend: 'pi', kind: 'tool_call', name: 'Bash', arg_summary: 'go test ./...', timestamp: at(3) },
  ],
  rollup: { key: SESSION, events: 3, tool_calls: 2, errors: 1, model_requests: 0, tokens_in: 0, tokens_out: 0,
    tool_counts: { Read: 1, Bash: 1 }, slowest_tools: [{ name: 'Read', calls: 1, errors: 1, avg_ms: 40, max_ms: 40 }] },
  index: { session: SESSION, backend: 'pi', coverage: 'full', total: 3, errors: 1 },
};
const coverage = {
  sessions: [{ session: SESSION, backend: 'pi', coverage: 'full', events: 3 }],
  by_backend: { pi: { full: 1 } },
};

async function mountHolder(p) {
  await p.evaluate((id) => {
    let h = document.getElementById('traceStoryHolder');
    if (!h) {
      h = document.createElement('div');
      h.id = 'traceStoryHolder';
      document.body.appendChild(h);
    }
    h.innerHTML = '';
    window.mountTraceCard('traceStoryHolder', 'session', id);
  }, SESSION);
}

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });
    await openPWA(p);

    const sessionCtl = await fulfillEndpoint(p, '/api/trace/session/' + SESSION, body);
    const covCtl = await fulfillEndpoint(p, '/api/trace/coverage', coverage);
    await mountHolder(p);
    await p.waitForFunction(() => {
      const h = document.getElementById('traceStoryHolder');
      return h && h.querySelector('.trace-card') && h.querySelector('.trace-timeline');
    }, null, { timeout: 15000 });

    const slot = await p.$('#traceStoryHolder');
    const text = await slot.innerText();
    const html = await slot.innerHTML();
    for (const needle of ['Trace', 'pi: full', 'Read', 'Bash', 'go test ./...', 'error: no such file', '3 of 3 events shown']) {
      check(text.includes(needle), `trace card missing "${needle}" (got: ${text.slice(0, 700)})`);
    }
    check((await p.$$eval('#traceStoryHolder .trace-event', els => els.length)) === 3, 'expected three event rows');
    check(!html.includes('<img src=x'), 'hostile argument text rendered as a live <img>');
    check(!dialogs.length, `dialog(s) fired by a hostile event: ${dialogs.join('; ')}`);
    await screenshot(p, 'trace-card');

    // Errors-only filter keeps just the failed tool result.
    await p.evaluate(() => window.traceCardSetFilter('traceStoryHolder', 'errors', true));
    await p.waitForFunction(() => {
      const h = document.getElementById('traceStoryHolder');
      return h && h.innerText.includes('1 of 3 events shown');
    }, null, { timeout: 15000 });
    const filtered = await (await p.$('#traceStoryHolder')).innerText();
    check(filtered.includes('error: no such file') && !filtered.includes('go test ./...'),
      `errors-only filter kept the wrong events (got: ${filtered.slice(0, 500)})`);
    await screenshot(p, 'trace-card-errors-only');

    // Phone width: the card must not push the page wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `trace card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'trace-card-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // No trace recorded for the session: the card says so.
    sessionCtl.stop();
    await p.route('**/api/trace/session/' + SESSION + '*', (route) =>
      route.fulfill({ status: 404, contentType: 'text/plain', body: 'no trace for session ' + SESSION }));
    await mountHolder(p);
    await p.waitForFunction(() => {
      const h = document.getElementById('traceStoryHolder');
      return h && h.innerText.includes('No trace events recorded yet.');
    }, null, { timeout: 15000 });
    await screenshot(p, 'trace-card-empty');

    // Tracing off: the card says so, never a stale timeline.
    await p.unroute('**/api/trace/session/' + SESSION + '*');
    await p.route('**/api/trace/session/' + SESSION + '*', (route) =>
      route.fulfill({ status: 503, contentType: 'text/plain', body: 'tracing is not available (tracing.mode is off or the trace store could not open)' }));
    await mountHolder(p);
    await p.waitForFunction(() => {
      const h = document.getElementById('traceStoryHolder');
      return h && h.innerText.includes('Tracing is off') && !h.querySelector('.trace-timeline');
    }, null, { timeout: 15000 });
    await screenshot(p, 'trace-card-off');
    covCtl.stop();

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
  } finally {
    await context.close().catch(() => {});
  }
});
