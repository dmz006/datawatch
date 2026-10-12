// Plugin widget renderers (BL403 Phase 4): each templated kind renders
// its documented data shape, every plugin-supplied string lands escaped,
// malformed data falls back to the quiet empty state without throwing,
// _pwRenderInto dispatches markdown through the sanitizing
// _renderMarkdownFileInto, and dashboard card ids round-trip.
// Run: `node --test internal/server/web/*.test.js`.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

function count(html, needle) {
  return html.split(needle).length - 1;
}

// Fake element that records every innerHTML write.
function fakeEl() {
  const writes = [];
  let html = '';
  return {
    writes,
    get innerHTML() { return html; },
    set innerHTML(v) { html = v; writes.push(v); },
  };
}

const sandbox = loadAppJS();
const EMPTY = call(sandbox, '_pwEmptyState', 'nodata');
const UNAVAILABLE = call(sandbox, '_pwEmptyState', 'unavailable');

const XSS = [
  '<script>alert(1)</script>',
  '<img src=x onerror=alert(1)>',
  '" onmouseover="x',
];

function assertEscaped(html, label) {
  assert.ok(!html.includes('<script'), `${label}: no raw <script`);
  assert.ok(!html.includes('<img'), `${label}: no raw <img`);
  // onerror= may only survive as part of the escaped '&lt;img ...' text.
  assert.ok(!html.replace(/&lt;img src=x onerror=/g, '').includes('onerror='), `${label}: no unescaped onerror=`);
  assert.ok(!/<[^>]*\sonmouseover=/i.test(html), `${label}: no onmouseover= attribute inside a tag`);
  assert.ok(!html.includes('" onmouseover="'), `${label}: no attribute breakout`);
  assert.ok(html.includes('&lt;'), `${label}: escaped &lt; form present`);
}

// ── (0) empty state ─────────────────────────────────────────────────────────

test('_pwEmptyState renders distinct no-data / loading / unavailable messages', () => {
  const loading = call(sandbox, '_pwEmptyState', 'loading');
  // The stub t() echoes the key, so assert on the i18n keys.
  assert.ok(EMPTY.includes('plugin_widget_no_data'));
  assert.ok(loading.includes('plugin_widget_loading'));
  assert.ok(UNAVAILABLE.includes('plugin_widget_unavailable'));
  assert.notEqual(EMPTY, loading);
  assert.notEqual(EMPTY, UNAVAILABLE);
  assert.equal(call(sandbox, '_pwEmptyState', 'whatever'), EMPTY);
});

// ── (1) valid payloads ──────────────────────────────────────────────────────

test('stat_tile shows value, unit, label and detail', () => {
  const html = call(sandbox, '_pwRenderStatTile', { value: 42, unit: 'msgs', label: 'Enriched today', detail: 'last hour +3' });
  assert.ok(html.includes('42'));
  assert.ok(html.includes('msgs'));
  assert.ok(html.includes('Enriched today'));
  assert.ok(html.includes('last hour +3'));
  assert.ok(call(sandbox, '_pwRenderStatTile', { value: 'ok-ish' }).includes('ok-ish'));
});

test('table renders one <th> per column and one body <tr> per row (array and object rows)', () => {
  const html = call(sandbox, '_pwRenderTable', {
    columns: ['name', 'count', 'flag'],
    rows: [['a', 1, true], ['b', 2, null], { name: 'c', count: 3, flag: false }],
  });
  assert.equal(count(html, '<th style'), 3);
  const body = html.slice(html.indexOf('<tbody>'));
  assert.equal(count(body, '<tr>'), 3);
  assert.equal(count(body, '<td'), 9);
  assert.ok(html.includes('>c<'), 'object row keyed by column name');
  assert.ok(html.includes('>true<') && html.includes('>false<'));
});

test('list renders one entry per item, with optional detail', () => {
  const html = call(sandbox, '_pwRenderList', { items: ['first', { text: 'second', detail: 'more' }, 'third'] });
  assert.ok(html.includes('first') && html.includes('second') && html.includes('third'));
  assert.ok(html.includes('more'));
  assert.equal(count(html, 'margin-bottom:4px;'), 3);
});

test('sparkline emits an <svg> polyline with one point per finite input', () => {
  const html = call(sandbox, '_pwRenderSparkline', { points: [1, 5, NaN, 3, Infinity, 'x', 8, 2], label: 'rate', value: 2 });
  assert.ok(html.includes('<svg'));
  const m = html.match(/<polyline[^>]*points="([^"]*)"/);
  assert.ok(m, 'polyline with points present');
  assert.equal(m[1].trim().split(/\s+/).length, 5, 'non-finite numbers dropped');
  assert.ok(html.includes('rate'));
});

test('status_badge maps ok/warn/error/unknown and coerces unexpected status to unknown', () => {
  const ok = call(sandbox, '_pwRenderStatusBadge', { status: 'ok' });
  const warn = call(sandbox, '_pwRenderStatusBadge', { status: 'warn' });
  const err = call(sandbox, '_pwRenderStatusBadge', { status: 'error' });
  const unk = call(sandbox, '_pwRenderStatusBadge', { status: 'unknown' });
  assert.ok(ok.includes('var(--success') && ok.includes('OK'));
  assert.ok(warn.includes('var(--warning') && warn.includes('Warning'));
  assert.ok(err.includes('var(--error') && err.includes('Error'));
  assert.ok(unk.includes('Unknown'));
  assert.equal(call(sandbox, '_pwRenderStatusBadge', { status: 'bogus' }), unk);
  assert.equal(call(sandbox, '_pwRenderStatusBadge', { status: 'toString' }), unk, 'prototype keys are not statuses');
  const labelled = call(sandbox, '_pwRenderStatusBadge', { status: 'ok', label: 'IMAP healthy', detail: '3 accounts' });
  assert.ok(labelled.includes('IMAP healthy') && labelled.includes('3 accounts'));
});

// ── (2) malicious payloads ──────────────────────────────────────────────────

test('stat_tile escapes plugin strings', () => {
  for (const x of XSS) {
    const html = call(sandbox, '_pwRenderStatTile', { value: x + '<b', unit: x, label: x, detail: x });
    assertEscaped(html, `stat_tile ${x}`);
  }
});

test('table escapes column names and cells', () => {
  for (const x of XSS) {
    const html = call(sandbox, '_pwRenderTable', { columns: [x + '<'], rows: [[x], { [x + '<']: x }] });
    assertEscaped(html, `table ${x}`);
  }
});

test('list escapes item text and detail', () => {
  for (const x of XSS) {
    const html = call(sandbox, '_pwRenderList', { items: [x + '<', { text: x + '<', detail: x }] });
    assertEscaped(html, `list ${x}`);
  }
});

test('sparkline escapes label and value; nothing plugin-supplied reaches the SVG', () => {
  for (const x of XSS) {
    const html = call(sandbox, '_pwRenderSparkline', { points: [1, 2], label: x + '<', value: x });
    assertEscaped(html, `sparkline ${x}`);
    const svg = html.slice(html.indexOf('<svg'));
    assert.ok(!svg.includes(x) && !svg.includes('onmouseover') && !svg.includes('alert'));
  }
});

test('status_badge escapes label/detail and never interpolates the status value', () => {
  const evil = 'ok" style="x';
  const html = call(sandbox, '_pwRenderStatusBadge', { status: evil, label: XSS[0], detail: XSS[1] });
  assertEscaped(html, 'status_badge');
  assert.ok(!html.includes('style="x'), 'status value not interpolated');
  assert.ok(html.includes('var(--text2)'), 'unexpected status coerced to unknown');
  for (const x of XSS) {
    assertEscaped(call(sandbox, '_pwRenderStatusBadge', { status: 'warn', label: x + '<', detail: x }), `status_badge ${x}`);
  }
});

// ── (3) wrong-typed / null / undefined data ─────────────────────────────────

const BAD = [null, undefined, 0, 7, '', 'str', true, [], [1, 2], {}, () => 1];

const BAD_SHAPED = {
  _pwRenderStatTile: [{ value: null }, { value: {} }, { value: NaN }, { value: Infinity }, { value: '' }, { value: [1] }],
  _pwRenderTable: [{ columns: 'a', rows: [] }, { columns: [], rows: [['a']] }, { columns: ['a'], rows: 'x' },
    { columns: ['a'], rows: [] }, { columns: ['a'], rows: [1, 'x', null] }],
  _pwRenderList: [{ items: 'x' }, { items: [] }, { items: [null, {}, { text: {} }, [], ''] }, { items: {} }],
  _pwRenderSparkline: [{ points: 'x' }, { points: [] }, { points: [NaN, Infinity, '1', null] }, { points: {} }],
  _pwRenderStatusBadge: [],
};

for (const fn of Object.keys(BAD_SHAPED)) {
  test(`${fn} returns the empty state for wrong-typed/null/undefined data`, () => {
    for (const d of BAD) {
      if (fn === '_pwRenderStatusBadge' && d && typeof d === 'object' && !Array.isArray(d)) continue; // {} -> unknown badge
      if (fn === '_pwRenderStatusBadge' && typeof d === 'function') continue;
      let html;
      assert.doesNotThrow(() => { html = call(sandbox, fn, d); }, `${fn}(${String(d)})`);
      assert.equal(html, EMPTY, `${fn}(${String(d)})`);
    }
    for (const d of BAD_SHAPED[fn]) {
      assert.equal(call(sandbox, fn, d), EMPTY, `${fn}(${JSON.stringify(d)})`);
    }
  });
}

test('renderers survive hostile objects (throwing getters) with the empty state', () => {
  const hostile = {};
  for (const k of ['value', 'columns', 'rows', 'items', 'points', 'status', 'label']) {
    Object.defineProperty(hostile, k, { get() { throw new Error('boom'); } });
  }
  for (const fn of ['_pwRenderStatTile', '_pwRenderTable', '_pwRenderList', '_pwRenderSparkline', '_pwRenderStatusBadge']) {
    assert.equal(call(sandbox, fn, hostile), EMPTY, fn);
  }
});

// ── (4) _pwRenderInto dispatch ──────────────────────────────────────────────

function withMarkdownSpy(sb) {
  const calls = [];
  sb.__mdSpy = (el, text) => calls.push({ el, text });
  vm.runInContext(`
    globalThis.marked = globalThis.marked || { parse: (s) => s };
    globalThis.DOMPurify = globalThis.DOMPurify || { sanitize: (s) => s };
    _renderMarkdownFileInto = __mdSpy;
  `, sb);
  return calls;
}

test('_pwRenderInto markdown ({text}) goes through _renderMarkdownFileInto, not innerHTML', () => {
  const sb = loadAppJS();
  const calls = withMarkdownSpy(sb);
  const el = fakeEl();
  call(sb, '_pwRenderInto', el, { kind: 'markdown', value: { data: { text: '# Hello <script>x</script>' } } });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].el, el);
  assert.equal(calls[0].text, '# Hello <script>x</script>');
  assert.equal(el.writes.length, 0, 'no direct innerHTML write');
});

test('_pwRenderInto markdown accepts a bare JSON string', () => {
  const sb = loadAppJS();
  const calls = withMarkdownSpy(sb);
  const el = fakeEl();
  call(sb, '_pwRenderInto', el, { kind: 'markdown', value: { data: '**bold**' } });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].text, '**bold**');
  assert.equal(el.writes.length, 0);
});

test('_pwRenderInto with null value / missing data renders the no-data state', () => {
  for (const view of [{ kind: 'stat_tile', value: null }, { kind: 'table' }, { kind: 'list', value: { data: null } }, null, undefined]) {
    const el = fakeEl();
    call(sandbox, '_pwRenderInto', el, view);
    assert.equal(el.innerHTML, EMPTY, JSON.stringify(view));
  }
});

test('_pwRenderInto with an unknown kind renders the unavailable state', () => {
  for (const kind of ['chart', 'iframe', '__proto__', 'toString', undefined]) {
    const el = fakeEl();
    call(sandbox, '_pwRenderInto', el, { kind, value: { data: { value: 1 } } });
    assert.equal(el.innerHTML, UNAVAILABLE, String(kind));
  }
});

test('_pwRenderInto renders templated kinds and an escaped error hint', () => {
  const el = fakeEl();
  call(sandbox, '_pwRenderInto', el, { kind: 'stat_tile', value: { data: { value: 9, label: 'n' }, error: XSS[1] } });
  assert.ok(el.innerHTML.includes('9'));
  assert.ok(el.innerHTML.includes('⚠'));
  assertEscaped(el.innerHTML, 'error hint');
  assert.doesNotThrow(() => call(sandbox, '_pwRenderInto', null, { kind: 'list' }));
});

// ── (5) card ids ────────────────────────────────────────────────────────────

test('_pwParseCardId parses plugin card ids; widget id may contain ":"', () => {
  const parsed = call(sandbox, '_pwParseCardId', 'plugin:imap-mcp:enrich:status');
  assert.deepEqual({ ...parsed }, { plugin: 'imap-mcp', widget: 'enrich:status' });
  for (const bad of ['tree', 'plugin:', 'plugin:x', 'plugin::w', 'plugin:x:', null, undefined, 5]) {
    assert.equal(call(sandbox, '_pwParseCardId', bad), null, String(bad));
  }
});

test('_pwCardId round-trips through _pwParseCardId', () => {
  const id = call(sandbox, '_pwCardId', 'imap-mcp', 'enrich:status');
  assert.equal(id, 'plugin:imap-mcp:enrich:status');
  assert.deepEqual({ ...call(sandbox, '_pwParseCardId', id) }, { plugin: 'imap-mcp', widget: 'enrich:status' });
});
