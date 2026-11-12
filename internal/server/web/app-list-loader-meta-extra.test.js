// listLoader metaExtra — the optional opts.metaExtra(data) callback of the
// shared list-load helper in app.js.
//
// Contract under test:
//   * while the data is fresh and uncached the meta row is omitted entirely:
//     no "Last updated N s ago" line, no .list-state-meta div at all — just
//     the rendered body.
//   * when metaExtra is given and returns non-empty HTML, the meta row
//     becomes <div class="list-state-meta list-state-meta-row"> holding the
//     existing meta spans (none while fresh) wrapped in one
//     <span class="list-state-meta-main"> followed by the extra HTML — still
//     inside the .list-state-meta row, before the list body. metaExtra is
//     called with the loader data.
//   * a throwing metaExtra never breaks the paint: it falls back to the
//     default markup.
//   * load() starts no one-second tick and keeps no tick timer on state:
//     the per-second age counter is gone.

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

test('without metaExtra fresh data paints the body with no meta row and no age line', async () => {
  const m = mkLoader(null, false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.ok(html, 'loader painted something');
  assert.ok(!html.includes('list-state-age'), 'no age line: ' + html);
  assert.ok(!html.includes('list-state-meta'), 'no meta row while fresh and uncached: ' + html);
  assert.ok(html.includes('body-7'), 'the rendered body is present: ' + html);
  m.ctl.stop();
});

test('a metaExtra returning empty HTML keeps the default markup', async () => {
  const m = mkLoader('() => ""', false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.ok(!html.includes('list-state-meta-row'), html);
  assert.ok(!html.includes('list-state-meta-main'), html);
  assert.ok(html.includes('body-7'), html);
  m.ctl.stop();
});

test('a throwing metaExtra falls back to the default markup and never breaks the paint', async () => {
  const m = mkLoader('() => { throw new Error("boom"); }', false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.ok(html, 'the paint still happened despite the throwing metaExtra');
  assert.ok(!html.includes('list-state-meta-row'), html);
  assert.ok(html.includes('body-7'), 'the body still rendered: ' + html);
  m.ctl.stop();
});

test('with metaExtra the extra HTML sits inside the meta row after the main span', async () => {
  const m = mkLoader('d => { window._metaGot = d.n; return \'<a href="#help" class="demo-extra">n\' + d.n + \'</a>\'; }', false);
  await m.ctl.load();
  await flushAsync();
  const html = m.el.innerHTML;
  assert.equal(m.sandbox._metaGot, 7, 'metaExtra was called with the loader data');
  assert.ok(html.includes('<div class="list-state-meta list-state-meta-row">'), 'split meta row: ' + html);
  const mainIdx = html.indexOf('<span class="list-state-meta-main">');
  const extraIdx = html.indexOf('class="demo-extra"');
  const bodyIdx = html.indexOf('<div class="list-state-body">');
  assert.ok(mainIdx !== -1 && extraIdx !== -1 && bodyIdx !== -1, 'all parts present: ' + html);
  assert.ok(mainIdx < extraIdx, 'the main span precedes the extra HTML: ' + html);
  assert.ok(extraIdx < bodyIdx, 'the extra HTML is still inside the .list-state-meta row, before the body: ' + html);
  assert.ok(html.includes('>n7</a>'), 'extra HTML was spliced in unescaped: ' + html);
  assert.ok(html.includes('body-7'), 'the body still rendered: ' + html);
  m.ctl.stop();
});

test('load() starts no one-second tick and keeps no tick timer on state', async () => {
  const m = mkLoader(null, true);
  const intervals = [];
  m.sandbox.setInterval = (fn, ms) => { intervals.push(ms); return 0; };
  await m.ctl.load();
  await flushAsync();
  assert.ok(intervals.every(ms => ms !== 1000), 'no 1 s interval registered during load: ' + JSON.stringify(intervals));
  assert.equal('tickTimer' in m.ctl.state, false, 'state has no tick timer');
  const html = m.el.innerHTML;
  assert.ok(!html.includes('list-state-age'), 'no age line in the paint: ' + html);
  assert.ok(!html.includes('Last updated'), 'no counter text in the paint: ' + html);
  m.ctl.stop();
});
