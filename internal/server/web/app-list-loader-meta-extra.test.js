// listLoader metaExtra — the optional opts.metaExtra(data) callback of the
// shared list-load helper in app.js.
//
// Contract under test:
//   * without metaExtra (or when it returns empty HTML) the meta row markup
//     stays exactly what it was before: a plain <div class="list-state-meta">
//     with the meta spans directly inside — no extra wrapper, no new class.
//   * when metaExtra is given and returns non-empty HTML, the meta row
//     becomes <div class="list-state-meta list-state-meta-row"> holding the
//     existing meta spans wrapped in one <span class="list-state-meta-main">
//     followed by the extra HTML — still inside the .list-state-meta row,
//     before the list body. metaExtra is called with the loader data.
//   * a throwing metaExtra never breaks the paint: it falls back to the
//     default row.
//   * the 1 s tick keeps updating the [data-list-age] span's textContent in
//     place (no innerHTML repaint) on its fast path.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = EN; // t() resolves through the en bundle
  return sandbox;
}

// mkLoader wires one listLoader into a fresh sandbox: a stub element that
// records every innerHTML paint, a fixed successful fetch, and optional
// pieces. metaSrc is the SOURCE of the metaExtra function (evaluated inside
// the context so the callback and its HTML stay in the test realm);
// withClock exposes a controllable now() the test advances by hand.
function mkLoader(metaSrc, withClock) {
  const sandbox = loadApp();
  const paints = [];
  const el = makeStubElement(v => paints.push(v));
  sandbox._el = el;
  sandbox._data = { n: 7 };
  let t = 0;
  if (withClock) sandbox._clock = () => t;
  const parts = [
    'key: "metaextra-test"',
    'url: "/api/metaextra-test"',
    'el: _el',
    'fetch: () => Promise.resolve(_data)',
    'render: d => "body-" + d.n',
  ];
  if (withClock) parts.push('now: _clock');
  if (metaSrc) parts.push('metaExtra: ' + metaSrc);
  const ctl = vm.runInContext('listLoader({ ' + parts.join(', ') + ' })', sandbox);
  return { sandbox, el, paints, setClock: (v) => { t = v; }, ctl };
}

test('without metaExtra the meta row keeps its default markup', async () => {
  const m = mkLoader(null, false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.ok(html, 'loader painted something');
  assert.ok(html.includes('<div class="list-state-meta">'), 'plain meta row: ' + html);
  assert.ok(!html.includes('list-state-meta-row'), 'no new row class: ' + html);
  assert.ok(!html.includes('list-state-meta-main'), 'no new main wrapper: ' + html);
  assert.ok(html.includes('<span class="list-state-age" data-list-age="1">'), 'age span directly in the row: ' + html);
  assert.ok(html.includes('body-7'), 'the rendered body follows the meta row: ' + html);
  m.ctl.stop();
});

test('a metaExtra returning empty HTML keeps the default markup', async () => {
  const m = mkLoader('() => ""', false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.ok(html.includes('<div class="list-state-meta">'), html);
  assert.ok(!html.includes('list-state-meta-row'), html);
  assert.ok(!html.includes('list-state-meta-main'), html);
  m.ctl.stop();
});

test('a throwing metaExtra falls back to the default markup and never breaks the paint', async () => {
  const m = mkLoader('() => { throw new Error("boom"); }', false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.ok(html, 'the paint still happened despite the throwing metaExtra');
  assert.ok(html.includes('<div class="list-state-meta">'), html);
  assert.ok(!html.includes('list-state-meta-row'), html);
  assert.ok(html.includes('body-7'), 'the body still rendered: ' + html);
  m.ctl.stop();
});

test('with metaExtra the extra HTML sits inside the meta row after one main span', async () => {
  const m = mkLoader('d => { window._metaGot = d.n; return \'<a href="#help" class="demo-extra">n\' + d.n + \'</a>\'; }', false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.equal(m.sandbox._metaGot, 7, 'metaExtra was called with the loader data');
  assert.ok(html.includes('<div class="list-state-meta list-state-meta-row">'), 'split meta row: ' + html);
  const mainIdx = html.indexOf('<span class="list-state-meta-main">');
  const ageIdx = html.indexOf('<span class="list-state-age" data-list-age="1">');
  const extraIdx = html.indexOf('class="demo-extra"');
  const bodyIdx = html.indexOf('<div class="list-state-body">');
  assert.ok(mainIdx !== -1 && ageIdx !== -1 && extraIdx !== -1 && bodyIdx !== -1, 'all parts present: ' + html);
  assert.ok(mainIdx < ageIdx && ageIdx < extraIdx, 'the meta spans (incl. the age span) are wrapped in the main span: ' + html);
  assert.ok(extraIdx < bodyIdx, 'the extra HTML is still inside the .list-state-meta row, before the body: ' + html);
  assert.ok(html.includes('>n7</a>'), 'extra HTML was spliced in unescaped: ' + html);
  assert.ok(html.includes('body-7'), 'the body still rendered: ' + html);
  m.ctl.stop();
});

test('the tick updates the age span text in place without repainting', async () => {
  const m = mkLoader(null, true);
  let tickFn = null;
  m.sandbox.setInterval = (fn) => { tickFn = fn; return 0; };
  await m.ctl.load();
  await flushAsync();
  assert.ok(typeof tickFn === 'function', 'the 1 s tick was registered on load');
  const initial = m.el.innerHTML;
  const paintsBefore = m.paints.length;
  // The tick's fast path reads the age span straight off the element; the
  // stub element's querySelector returns it (and no stale marker).
  const span = { textContent: 'stale' };
  m.el.querySelector = (sel) => (sel === '[data-list-age]' ? span : null);
  m.setClock(42000);
  tickFn();
  assert.notEqual(span.textContent, 'stale', 'tick touched the age span');
  assert.equal(span.textContent, 'Last updated 42 s ago', 'age text advanced with the clock: ' + span.textContent);
  assert.equal(m.paints.length, paintsBefore, 'tick must not repaint');
  assert.equal(m.el.innerHTML, initial, 'innerHTML must be untouched by the tick');
  m.ctl.stop();
});
