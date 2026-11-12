// Trace card (session view and Automaton view) — PWA side.
//
// window.traceCardHtml(model) draws the card from the bodies of
// GET /api/trace/session/{id} or /api/trace/automaton/{id} and
// GET /api/trace/coverage. The render, the client-side filters, the coverage
// badge text, the session timeline lines, the loader, the tracing.* settings
// and the five locale bundles are covered here with fetch stubbed to fixture
// bodies and a fixed clock. No daemon, session or tmux is started and no
// operator config is read.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const HOSTILE = '<img src=x onerror=alert(1)>';
const LANGS = ['en', 'es', 'fr', 'de', 'ja'];
const FIXED_NOW = Date.parse('2026-11-04T12:00:00Z');

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url) => {
    calls.push(String(url));
    const key = Object.keys(routes || {}).find(k => String(url).split('?')[0] === k);
    if (!key) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    const body = routes[key];
    if (typeof body === 'string') return Promise.resolve({ ok: false, status: 503, statusText: '', text: () => Promise.resolve(body) });
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox.localStorage = { getItem: () => null };
  sandbox.__calls = calls;
  return sandbox;
}

function call(sandbox, expr, value) {
  sandbox._v = value;
  return vm.runInContext(expr, sandbox);
}

const EVENTS = [
  { session_id: 's1', backend: 'pi', kind: 'tool_call', name: 'Read', arg_summary: 'a.go', timestamp: '2026-11-04T10:00:00Z' },
  { session_id: 's1', backend: 'pi', kind: 'tool_result', name: 'Read', outcome: 'ok', duration_ms: 12, timestamp: '2026-11-04T10:00:01Z' },
  { session_id: 's1', backend: 'pi', kind: 'tool_result', name: 'Bash', outcome: 'error', duration_ms: 90, timestamp: '2026-11-04T10:00:02Z' },
  { session_id: 's1', backend: 'ollama', kind: 'model_request', name: 'qwen', tokens_in: 100, tokens_out: 20, timestamp: '2026-11-04T10:00:03Z' },
  { session_id: 's1', backend: 'pi', kind: 'error', name: 'Bash', outcome: 'boom', timestamp: '2026-11-04T10:00:04Z' },
];

function sessionBody(over) {
  return Object.assign({
    session: 's1', total: EVENTS.length, events: EVENTS,
    rollup: {
      dimension: 'session', key: 's1', events: 5, tool_counts: { Read: 2, Bash: 2 }, tool_calls: 4, errors: 2,
      slowest_tools: [{ name: 'Bash', calls: 2, errors: 2, total_ms: 180, max_ms: 90, avg_ms: 90 }, { name: 'Read', calls: 2, errors: 0, total_ms: 24, max_ms: 12, avg_ms: 12 }],
      loop_warnings: [{ tool: 'Bash', arg_summary: 'x', count: 3 }], model_requests: 1, tokens_in: 100, tokens_out: 20,
    },
    index: { session: 's1', backend: 'pi', coverage: 'partial', total: 5 },
  }, over || {});
}

function coverageBody() {
  return {
    sessions: [
      { session: 's1', backend: 'pi', coverage: 'partial', events: 5 },
      { session: 's2', backend: 'ollama', coverage: 'events-only', events: 1 },
      { session: 'other', backend: 'shell', coverage: 'full', events: 9 },
    ],
    by_backend: {},
  };
}

function render(sb, over) {
  return call(sb, 'window.traceCardHtml(_v)', Object.assign({ data: sessionBody(), coverage: coverageBody(), filter: {}, elId: 'sessionTraceCard', scope: 'session', id: 's1' }, over || {}));
}

// ── render ───────────────────────────────────────────────────────────────────

test('the card shows the roll-up table, the timeline and the loop warning', () => {
  const html = render(loadApp({}));
  assert.ok(html.includes('class="cap-title">Trace<'), html);
  assert.ok(html.includes('trace-rollup-table'), html);
  assert.ok(html.includes('5 events, 4 tool calls, 2 errors, 1 model requests, 100 tokens in, 20 tokens out'), html);
  assert.equal((html.match(/trace-rollup-row/g) || []).length, 2);
  assert.equal((html.match(/class="trace-event/g) || []).length, 5);
  assert.ok(html.includes('Loop warnings: Bash x3'), html);
  assert.ok(html.includes('5 of 5 events shown'), html);
  assert.ok(html.includes('10:00:02'), 'the fixed fixture time is shown in UTC');
});

test('the coverage badge says what each backend records and that missing detail is not no activity', () => {
  const html = render(loadApp({}), { data: sessionBody({ sessions: undefined }) });
  assert.ok(html.includes('pi: partial'), html);
  assert.ok(!html.includes('shell: full'), 'a session that is not on screen adds no badge');
  assert.ok(html.includes('trace-coverage-partial'), html);
  assert.ok(html.includes('Some kinds of events are traced and others are not visible.'), html);
  assert.ok(html.includes('Missing detail is not the same as no activity'), html);
});

test('an Automaton card gives one badge per backend across its sessions, least detail winning', () => {
  const body = sessionBody({ automaton: 'a1', sessions: ['s1', 's2', 's3'] });
  const cov = coverageBody();
  cov.sessions.push({ session: 's3', backend: 'pi', coverage: 'events-only', events: 2 });
  const html = render(loadApp({}), { data: body, coverage: cov, scope: 'automaton', id: 'a1', elId: 'automatonTraceCard' });
  assert.ok(html.includes('pi: events only'), html);
  assert.ok(html.includes('ollama: events only'), html);
  assert.ok(!html.includes('pi: partial'), html);
});

test('without coverage rows the badge falls back to the trace index of the session', () => {
  const html = render(loadApp({}), { coverage: undefined });
  assert.ok(html.includes('pi: partial'), html);
});

test('a session with no trace says so and still carries the missing-detail note', () => {
  const html = render(loadApp({}), { empty: true, data: {} });
  assert.ok(html.includes('No trace events recorded yet.'), html);
  assert.ok(html.includes('Missing detail is not the same as no activity'), html);
  assert.ok(!html.includes('trace-filters'), html);
});

test('tracing off reads as unavailable, not as an empty trace', () => {
  const html = render(loadApp({}), { unavailable: true });
  assert.ok(html.includes('Tracing is off, or the trace store is not available.'), html);
});

test('a missing or malformed model renders an empty card without throwing', () => {
  const sb = loadApp({});
  for (const bad of [undefined, null, 'x', [], {}]) {
    const html = call(sb, 'window.traceCardHtml(_v)', bad);
    assert.ok(html.includes('No trace events recorded yet.'), String(bad));
  }
});

test('hostile values in events, backends and the roll-up are escaped', () => {
  const ev = [{ backend: HOSTILE, kind: 'tool_call', name: HOSTILE, arg_summary: HOSTILE, outcome: HOSTILE, timestamp: '2026-11-04T10:00:00Z' }];
  const body = sessionBody({ events: ev, rollup: { events: 1, tool_counts: { [HOSTILE]: 1 }, slowest_tools: [], loop_warnings: [{ tool: HOSTILE, count: 3 }] }, index: { backend: HOSTILE, coverage: 'full' } });
  const html = render(loadApp({}), { data: body, coverage: { sessions: [] } });
  assert.ok(!html.includes(HOSTILE), 'raw markup leaked: ' + html);
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'));
});

test('more than the row cap of events are cut and the rest counted', () => {
  const many = [];
  for (let i = 0; i < 205; i++) many.push({ kind: 'tool_call', name: 'T' + i, timestamp: '2026-11-04T10:00:00Z' });
  const html = render(loadApp({}), { data: sessionBody({ events: many }) });
  assert.equal((html.match(/class="trace-event/g) || []).length, 200);
  assert.ok(html.includes('5 earlier events not shown'), html);
});

// ── filters ──────────────────────────────────────────────────────────────────

test('the filters narrow the timeline by tool, backend, kind and errors', () => {
  const sb = loadApp({});
  const f = filter => call(sb, 'window.traceFilterEvents(_v.e, _v.f)', { e: EVENTS, f: filter }).length;
  assert.equal(f({}), 5);
  assert.equal(f({ tool: 'Read' }), 2);
  assert.equal(f({ backend: 'ollama' }), 1);
  assert.equal(f({ kind: 'tool_result' }), 2);
  assert.equal(f({ errors: true }), 2);
  assert.equal(f({ tool: 'Bash', errors: true }), 2);
  assert.equal(f({ tool: 'Read', errors: true }), 0);
});

test('a filtered card says how many events show and a filter with no match says so', () => {
  const sb = loadApp({});
  assert.ok(render(sb, { filter: { tool: 'Read' } }).includes('2 of 5 events shown'));
  const none = render(sb, { filter: { tool: 'Read', errors: true } });
  assert.ok(none.includes('No events match the filter.'), none);
});

test('the filter bar offers the tools and backends present and every kind', () => {
  const html = render(loadApp({}));
  assert.ok(html.includes('value="Read"') && html.includes('value="Bash"') && html.includes('value="qwen"'));
  assert.ok(html.includes('value="ollama"') && html.includes('value="pi"'));
  for (const k of ['tool_call', 'tool_result', 'model_request', 'state_change', 'capability_check', 'permission_decision', 'loop_warning', 'error']) {
    assert.ok(html.includes('value="' + k + '"'), k);
  }
  assert.ok(html.includes('Errors only') && html.includes('Clear filters'));
});

test('a selected filter is marked selected', () => {
  const html = render(loadApp({}), { filter: { kind: 'error', errors: true } });
  assert.ok(html.includes('value="error" selected'), html);
  assert.ok(html.includes('class="trace-filter-errors" checked'), html);
});

// ── session timeline lines ───────────────────────────────────────────────────

test('trace events become timeline lines that sort with the daemon lines', () => {
  const sb = loadApp({});
  const lines = call(sb, 'window.traceTimelineLines(_v)', EVENTS);
  assert.equal(lines.length, 5);
  assert.equal(lines[0], '2026-11-04T10:00:00Z | trace | Tool call Read a.go');
  assert.equal(lines[2], '2026-11-04T10:00:02Z | trace | Tool result Bash - error 90 ms');
  assert.deepEqual(call(sb, 'window.traceTimelineLines(_v)', [{ kind: 'tool_call', timestamp: 'nope' }, null, 'x']), []);
});

test('the session timeline panel merges trace lines and survives a missing trace', async () => {
  const panelHolder = { el: null };
  const outputArea = makeStubElement();
  outputArea.insertAdjacentElement = (_pos, el) => { panelHolder.el = el; };
  const created = [];
  const routes = {
    '/api/sessions/timeline': { lines: ['2026-11-04T10:00:01Z | state_change | running'] },
    '/api/trace/session/s1': sessionBody(),
  };
  const sb = loadApp(routes, { outputAreaTmux: outputArea });
  sb.document.createElement = () => { const e = makeStubElement(); created.push(e); return e; };
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'timelinePanel' ? null : byId(id));
  call(sb, 'toggleSessionTimeline("s1")');
  await flushAsync(); await flushAsync(); await flushAsync();
  const panel = created[created.length - 1];
  assert.ok(panel.innerHTML.includes('running'), panel.innerHTML);
  assert.ok(panel.innerHTML.includes('Tool call Read a.go'), panel.innerHTML);
  assert.ok(sb.__calls.some(u => u.startsWith('/api/trace/session/s1')));

  // Same panel when there is no trace for the session: only daemon lines.
  const sb2 = loadApp({ '/api/sessions/timeline': { lines: ['2026-11-04T10:00:01Z | state_change | running'] } }, { outputAreaTmux: outputArea });
  const created2 = [];
  sb2.document.createElement = () => { const e = makeStubElement(); created2.push(e); return e; };
  const byId2 = sb2.document.getElementById;
  sb2.document.getElementById = id => (id === 'timelinePanel' ? null : byId2(id));
  call(sb2, 'toggleSessionTimeline("s1")');
  await flushAsync(); await flushAsync(); await flushAsync();
  assert.ok(created2[0].innerHTML.includes('running'), created2[0].innerHTML);
  assert.ok(!created2[0].innerHTML.includes('trace |'), 'no trace lines');
});

// ── loader and mount ─────────────────────────────────────────────────────────

test('mountTraceCard fetches the trace and the coverage and paints the card', async () => {
  const el = makeStubElement();
  const sb = loadApp({ '/api/trace/session/s1': sessionBody(), '/api/trace/coverage': coverageBody() }, { sessionTraceCard: el });
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  assert.ok(el.innerHTML.includes('trace-rollup-table'), el.innerHTML);
  assert.ok(el.innerHTML.includes('pi: partial'), el.innerHTML);
  assert.ok(sb.__calls.includes('/api/trace/session/s1?limit=500'), sb.__calls.join());
  assert.ok(sb.__calls.includes('/api/trace/coverage'));
  call(sb, 'window._listLoaders.trace_card_sessionTraceCard.stop()');
});

test('an Automaton card reads the Automaton route', async () => {
  const el = makeStubElement();
  const body = sessionBody({ automaton: 'a1', sessions: ['s1'] });
  const sb = loadApp({ '/api/trace/automaton/a1': body, '/api/trace/coverage': coverageBody() }, { automatonTraceCard: el });
  await call(sb, 'window.mountTraceCard("automatonTraceCard", "automaton", "a1")');
  assert.ok(sb.__calls.includes('/api/trace/automaton/a1?limit=500'), sb.__calls.join());
  assert.ok(el.innerHTML.includes('trace-card'), el.innerHTML);
  call(sb, 'window._listLoaders.trace_card_automatonTraceCard.stop()');
});

test('no trace for the session paints the empty card and tracing off paints unavailable', async () => {
  const el = makeStubElement();
  const sb = loadApp({ '/api/trace/session/s1': 'no trace for session s1\n' }, { sessionTraceCard: el });
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  assert.ok(el.innerHTML.includes('No trace events recorded yet.'), el.innerHTML);
  call(sb, 'window._listLoaders.trace_card_sessionTraceCard.stop()');

  const el2 = makeStubElement();
  const sb2 = loadApp({ '/api/trace/session/s1': 'tracing is not available (tracing.mode is off or the trace store could not open)' }, { sessionTraceCard: el2 });
  await call(sb2, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  assert.ok(el2.innerHTML.includes('Tracing is off, or the trace store is not available.'), el2.innerHTML);
  call(sb2, 'window._listLoaders.trace_card_sessionTraceCard.stop()');
});

test('a failed read shows the loader banner with the reason', async () => {
  const el = makeStubElement();
  const sb = loadApp({ '/api/trace/session/s1': 'disk on fire' }, { sessionTraceCard: el });
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  assert.ok(el.innerHTML.includes('Trace failed to load: disk on fire'), el.innerHTML);
  call(sb, 'window._listLoaders.trace_card_sessionTraceCard.stop()');
});

test('changing a filter repaints from the loaded events without another fetch, and clear resets it', async () => {
  const el = makeStubElement();
  const sb = loadApp({ '/api/trace/session/s1': sessionBody(), '/api/trace/coverage': coverageBody() }, { sessionTraceCard: el });
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  const fetches = sb.__calls.length;
  call(sb, 'window.traceCardSetFilter("sessionTraceCard", "tool", "Read")');
  assert.ok(el.innerHTML.includes('2 of 5 events shown'), el.innerHTML);
  call(sb, 'window.traceCardSetFilter("sessionTraceCard", "errors", true)');
  assert.ok(el.innerHTML.includes('No events match the filter.'), el.innerHTML);
  call(sb, 'window.traceCardSetFilter("sessionTraceCard", "clear", "")');
  assert.ok(el.innerHTML.includes('5 of 5 events shown'), el.innerHTML);
  assert.equal(sb.__calls.length, fetches, 'a filter change never refetches');
  call(sb, 'window._listLoaders.trace_card_sessionTraceCard.stop()');
});

test('mounting again keeps the filter for the same target and never stacks a second loader', async () => {
  const el = makeStubElement();
  const sb = loadApp({ '/api/trace/session/s1': sessionBody(), '/api/trace/coverage': coverageBody() }, { sessionTraceCard: el });
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  const first = call(sb, 'window._listLoaders.trace_card_sessionTraceCard');
  call(sb, 'window.traceCardSetFilter("sessionTraceCard", "tool", "Bash")');
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s1")');
  assert.ok(el.innerHTML.includes('2 of 5 events shown'), el.innerHTML);
  assert.notEqual(call(sb, 'window._listLoaders.trace_card_sessionTraceCard'), first);
  assert.equal(first.state.stopped, true, 'the old loader was stopped');
  await call(sb, 'window.mountTraceCard("sessionTraceCard", "session", "s2")');
  assert.deepEqual(JSON.parse(JSON.stringify(call(sb, 'window._traceCards.sessionTraceCard.filter'))), {});
  call(sb, 'window._listLoaders.trace_card_sessionTraceCard.stop()');
});

// ── settings ─────────────────────────────────────────────────────────────────

test('Settings carries the tracing.* controls', () => {
  const sb = loadApp({});
  const fields = JSON.parse(JSON.stringify(vm.runInContext('GENERAL_CONFIG_FIELDS', sb)));
  const all = [];
  (function walk(v) {
    if (Array.isArray(v)) v.forEach(walk);
    else if (v && typeof v === 'object') { if (v.key) all.push(v); Object.values(v).forEach(walk); }
  })(fields);
  const byKey = Object.fromEntries(all.filter(f => /^tracing\./.test(f.key)).map(f => [f.key, f]));
  assert.deepEqual(Object.keys(byKey).sort(), ['tracing.arg_cap_bytes', 'tracing.backends', 'tracing.mode', 'tracing.retention_days']);
  assert.deepEqual(byKey['tracing.mode'].options, ['off', 'summary', 'full']);
  assert.equal(byKey['tracing.arg_cap_bytes'].type, 'number');
  assert.equal(byKey['tracing.retention_days'].type, 'number');
  assert.equal(byKey['tracing.backends'].saveFn, 'saveTracingBackendsSetting');
});

test('the tracing backends box saves an object of booleans and refuses anything else', () => {
  const sb = loadApp({});
  const puts = []; const toasts = [];
  sb.saveGeneralConfigPut = (k, v) => puts.push([k, v]);
  sb.showToast = (m, kind) => toasts.push([m, kind]);
  vm.runInContext('globalThis.saveGeneralConfigPut = saveGeneralConfigPut', sb);
  const save = text => call(sb, 'saveTracingBackendsSetting("tracing.backends", { value: _v })', text);
  save('{"pi": false, "ollama": true}');
  save('');
  assert.deepEqual(JSON.parse(JSON.stringify(puts)), [['tracing.backends', { pi: false, ollama: true }], ['tracing.backends', {}]]);
  for (const bad of ['{"pi": 1}', '[true]', 'nope', '{"": true}', '{"pi": "false"}']) save(bad);
  assert.equal(puts.length, 2, 'bad input is never sent');
  assert.equal(toasts.length, 5);
});

// ── locales ──────────────────────────────────────────────────────────────────

const TRACE_KEYS = Object.keys(bundle('en')).filter(k => /^trace_|^settings_tracing_|^timeline_trace_label$/.test(k));

test('every trace key the card uses exists in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  for (const m of src.matchAll(/t\('((?:trace_|settings_tracing_|timeline_trace_)[a-z_]+)'/g)) used.add(m[1]);
  for (const k of [...used].filter(u => u.endsWith('_'))) used.delete(k);
  for (const k of ['trace_kind_', 'trace_coverage_', 'trace_coverage_note_']) {
    const lv = k === 'trace_kind_' ? ['tool_call', 'tool_result', 'model_request', 'state_change', 'capability_check', 'permission_decision', 'loop_warning', 'error'] : ['full', 'partial', 'events-only', 'unknown'];
    lv.forEach(x => used.add(k + x));
  }
  assert.ok(used.size > 30, 'found the keys in use: ' + used.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of used) assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' is missing ' + k);
  }
});

test('the translated bundles are real translations: no English copy, same placeholders, no internal ids', () => {
  const en = bundle('en');
  const sameOk = new Set(['trace_filter_backend', 'trace_coverage_badge', 'trace_duration_ms', 'trace_col_max_ms']);
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    for (const k of TRACE_KEYS) {
      const ph = s => (s.match(/%\d\$s/g) || []).sort().join();
      assert.equal(ph(b[k]), ph(en[k]), lang + ' placeholders differ for ' + k);
      if (!sameOk.has(k) && en[k].length > 5 && !/^[\s\W\d]*$/.test(en[k])) {
        assert.notEqual(b[k], en[k], lang + ' still has the English text for ' + k);
      }
    }
  }
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of TRACE_KEYS) {
      assert.ok(!/\bPRDs?\b/.test(b[k]), lang + ' says PRD in ' + k);
      assert.ok(!/\bBL\d+\b/.test(b[k]), lang + ' has a tracker id in ' + k);
    }
  }
});

test('the card renders in every language without a raw key showing', () => {
  for (const lang of LANGS) {
    const sb = loadApp({});
    sb._i18n.bundle = bundle(lang);
    const html = render(sb);
    assert.ok(!/trace_[a-z_-]+/.test(html.replace(/trace-[a-z-]+/g, '').replace(/trace_card/g, '')), lang + ': ' + html.match(/trace_[a-z_-]+/));
    assert.ok(html.includes(bundle(lang).trace_card_title), lang);
  }
});

// Monitor Tracing card: the stats card reads the trace_* fields of the system
// stats and shows only while tracing is on.
const STATS_KEYS = ['stats_trace', 'stats_trace_sessions', 'stats_trace_events', 'stats_trace_error_events',
  'stats_trace_appended', 'stats_trace_append_errors', 'stats_trace_pruned', 'stats_trace_last_event_age'];

test('the Monitor tracing card keys exist and are translated in every language', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of STATS_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
      if (lang !== 'en' && k !== 'stats_trace') assert.notEqual(b[k], en[k], lang + ' left ' + k + ' in English');
      assert.ok(!/\bPRDs?\b|\bBL\d+\b/.test(b[k]), lang + ' has an internal term in ' + k);
    }
  }
});

test('the Monitor stats view draws the tracing card only when trace_enabled is reported', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes('if (data.trace_enabled) {'), 'card is gated on trace_enabled');
  assert.ok(src.includes('data-stat="trace"'), 'card has its data-stat hook');
  for (const f of ['trace_sessions', 'trace_events', 'trace_error_events', 'trace_appended_total',
    'trace_append_errors_total', 'trace_pruned_total', 'trace_last_event_age_seconds']) {
    assert.ok(src.includes('data.' + f), 'reads data.' + f);
  }
  for (const k of STATS_KEYS) assert.ok(src.includes("'" + k + "'") || src.includes('`' + k), 'uses ' + k);
});

// The Errors-only filter must agree with the daemon's trace.IsError, which
// treats an outcome that starts with "error" or "fail" as a failure.
test('traceEventIsError mirrors the daemon: prefix match on error/fail, exact denied', () => {
  const sb = loadApp({});
  const isErr = (outcome, kind = 'tool_result') => call(sb, 'window.traceEventIsError(_v)', { kind, outcome });
  assert.equal(isErr('error: no such file'), true);
  assert.equal(isErr('failed: exit 1'), true);
  assert.equal(isErr('Error'), true);
  assert.equal(isErr('denied'), true);
  assert.equal(isErr('boom', 'error'), true);
  assert.equal(isErr('ok'), false);
  assert.equal(isErr('denied by policy'), false);
  assert.equal(isErr('timeout'), false);
  assert.equal(isErr(''), false);
});

// ── placement: a Trace sub-tab under Status, not a card above the session ────

test('the session Trace card lives only in the Status panel Trace sub-tab', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const holders = src.match(/id="sessionTraceCard"/g) || [];
  assert.equal(holders.length, 2, 'one holder per Status panel markup, none in the session header');
  for (const m of src.matchAll(/id="sessionTraceCard"/g)) {
    const before = src.slice(Math.max(0, m.index - 140), m.index);
    assert.ok(before.includes('id="statusSubpaneTrace"'), 'the holder sits inside the Trace sub-pane: ' + before);
  }
  assert.equal((src.match(/id="statusSubtabTrace"/g) || []).length, 2, 'a Trace sub-tab button in both Status panels');
  assert.ok(!src.includes("window.mountTraceCard('sessionTraceCard', 'session', sessionId)"), 'opening the session no longer mounts the card');
});

test('opening the Trace sub-tab mounts the card and leaving it drops the registration', async () => {
  const mk = () => { const e = makeStubElement(); return e; };
  const els = {
    statusSubtabStatus: mk(), statusSubtabStats: mk(), statusSubtabTrace: mk(),
    statusSubpaneStatus: mk(), statusSubpaneStats: mk(), statusSubpaneTrace: mk(), sessionTraceCard: mk(),
  };
  const sb = loadApp({ '/api/trace/session/s1': sessionBody(), '/api/trace/coverage': coverageBody() }, els);
  call(sb, 'state.activeSession = "s1"');
  call(sb, 'window.switchStatusSubtab("trace")');
  await flushAsync();
  assert.equal(els.statusSubpaneTrace.style.display, '', 'the Trace pane is shown');
  assert.equal(els.statusSubpaneStatus.style.display, 'none');
  assert.equal(els.statusSubpaneStats.style.display, 'none');
  assert.ok(els.sessionTraceCard.innerHTML.includes('trace-card'), els.sessionTraceCard.innerHTML);
  assert.ok(call(sb, '!!window._traceCards.sessionTraceCard'), 'the card is registered while the sub-tab is open');
  call(sb, 'window.switchStatusSubtab("status")');
  assert.equal(els.statusSubpaneTrace.style.display, 'none');
  assert.equal(els.statusSubpaneStatus.style.display, '');
  assert.ok(call(sb, '!window._traceCards.sessionTraceCard'), 'leaving the sub-tab drops the registration so the loader stops');
  call(sb, 'window._listLoaders.trace_card_sessionTraceCard.stop()');
});

test('the Automaton detail has a Trace tab and no always-on Trace card', () => {
  const sb = loadApp({}, {});
  const strip = call(sb, '_renderDetailTabStrip({}, "trace")');
  assert.ok(strip.includes("switchAutomataDetailTab('trace')"), strip);
  assert.ok(call(sb, '_renderDetailTraceTab()').includes('id="automatonTraceCard"'));
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.equal((src.match(/id="automatonTraceCard"/g) || []).length, 1, 'the holder exists only in the Trace tab body');
});
