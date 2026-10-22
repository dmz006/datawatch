'use strict';
// GH#204 phase 3 — Status-tab Result card.
//
// Regression test for the new renderResultPanelBody /
// renderResultPanelFromBoard / renderResultMarkdownInto functions in app.js
// (the card rendered on the session Status tab when the agent posts a
// structured result via POST /api/sessions/{sid}/result). Covers:
//   1. card hidden when the board carries no result (the whole gate),
//   2. status badge for each of ok / warn / error, and none for unknown,
//   3. hostile strings in title/summary/metrics/tables are HTML-escaped,
//   4. markdown is rendered through DOMPurify.sanitize (raw <script> and
//      <img onerror> are stripped) and degrades to escaped plain text when
//      the markdown libraries can't load (offline/CSP),
//   5. table headers and cells render.
//
// Uses the shared testutil_browser_stub.js harness (real app.js, Node
// vm realm, no browser). Run:
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-result-panel.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const enBundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  for (const name of ['renderResultPanelBody', 'renderResultPanelFromBoard', 'renderResultMarkdownInto']) {
    assert.equal(typeof sandbox[name], 'function', `${name} not found in app.js — renamed?`);
  }
  return sandbox;
}

// Same shape as the card() helper local to renderSessionStatusBoardInner.
const card = (title, body) => `<card title="${title}">${body}</card>`;

test('hidden when the board has no result', () => {
  const s = loadAppJS();
  // The gate itself: any of these must produce no card at all.
  assert.equal(s.renderResultPanelFromBoard(undefined, card), '');
  assert.equal(s.renderResultPanelFromBoard(null, card), '');
  assert.equal(s.renderResultPanelFromBoard({}, card), '');
  assert.equal(s.renderResultPanelFromBoard({ result: null }, card), '');
  assert.equal(s.renderResultPanelFromBoard({ result: 'malformed' }, card), '', 'non-object result must be ignored');

  // And inside the real status board renderer: a board without result
  // must not contain any result-panel markup.
  const area = makeStubElement();
  s.renderSessionStatusBoardInner(area, { state: 'idle' }, 'sess-1');
  assert.ok(area.innerHTML.length > 0, 'status board rendered nothing at all');
  assert.doesNotMatch(area.innerHTML, /result-panel/);
  assert.doesNotMatch(area.innerHTML, /result-markdown/);

  // …and with a result, the card shows up in the board.
  const area2 = makeStubElement();
  s.renderSessionStatusBoardInner(area2, { state: 'idle', result: { title: 'CI', status: 'ok' } }, 'sess-1');
  assert.match(area2.innerHTML, /result-panel/);
  assert.match(area2.innerHTML, /result-status-badge/);
});

for (const status of ['ok', 'warn', 'error']) {
  test(`status badge for ${status}`, () => {
    const s = loadAppJS();
    s._i18n.bundle = enBundle; // exercise the real locale strings
    const html = s.renderResultPanelFromBoard({ result: { title: 'Build', status } }, card);
    assert.match(html, new RegExp('result-status-badge result-status-' + status));
    assert.match(html, /result-status-dot/);
    // Label comes from the locale bundle (en values: ok/warn/error).
    const label = { ok: 'ok', warn: 'warn', error: 'error' }[status];
    assert.match(html, RegExp(label + '\\s*</span>'));
  });
}

test('unknown status gets no badge (not one of the closed set)', () => {
  const s = loadAppJS();
  const html = s.renderResultPanelFromBoard({ result: { title: 'x', status: 'exploded' } }, card);
  assert.doesNotMatch(html, /result-status-badge/);
});

test('hostile strings in title/summary/metrics/tables are escaped', () => {
  const s = loadAppJS();
  const board = {
    result: {
      title: '<script>alert(1)</script>',
      summary: '<img src=x onerror=alert(2)> summary',
      metrics: { jobs: '<b>99</b>', label_with_quote: '"><img src=x onerror=alert(3)>' },
      tables: [
        {
          title: '<i>hostile</i>',
          columns: ['<u>col</u>', '"><script>bad()</script>'],
          rows: [['<a href="#">x</a>', 7], ['"><img src=x onerror=alert(4)>']],
        },
      ],
    },
  };
  const html = s.renderResultPanelFromBoard(board, card);

  // No raw hostile markup survives anywhere in the card.
  assert.doesNotMatch(html, /<script>alert/);
  assert.doesNotMatch(html, /<img src=x onerror/);
  assert.doesNotMatch(html, /<a href/);
  assert.doesNotMatch(html, /<b>99<\/b>/);
  assert.doesNotMatch(html, /<i>hostile<\/i>/);

  // The escaped forms are present (escHtml entity encoding).
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /&lt;img src=x onerror=alert\(2\)&gt;/);
  assert.match(html, /&lt;b&gt;99&lt;\/b&gt;/);

  // The innocent values still render.
  assert.match(html, /&gt; summary<\/div>/);
  assert.match(html, /&gt;col/);
  assert.match(html, />7</);
});

test('metrics render in key order as label/value chips', () => {
  const s = loadAppJS();
  const html = s.renderResultPanelFromBoard({ result: { metrics: { a: 1, b: 2 } } }, card);
  const ia = html.indexOf('result-metric-label');
  const ib = html.indexOf('result-metric-label', ia + 1);
  assert.ok(ia !== -1 && ib !== -1, 'metric chips missing');
  assert.ok(ia < ib, 'metric order not preserved');
});

test('table renders header from columns and escaped rows', () => {
  const s = loadAppJS();
  const html = s.renderResultPanelFromBoard({
    result: { tables: [{ title: 'Builds', columns: ['job', 'status', 'duration'], rows: [['main', 'pass', '3m'], ['ci', 'fail', '1m']] }] },
  }, card);
  assert.match(html, /<table class="result-table"/);
  assert.match(html, /<th class="result-th"[^>]*>job<\/th>/);
  assert.match(html, /<th class="result-th"[^>]*>duration<\/th>/);
  assert.match(html, /<td class="result-td"[^>]*>pass<\/td>/);
  assert.match(html, /<td class="result-td"[^>]*>fail<\/td>/);
});

test('markdown renders through DOMPurify.sanitize (raw script/onerror stripped)', async () => {
  const s = loadAppJS();
  // Minimal stand-ins for the pinned CDN libs: the DOMPurify.sanitize
  // stub strips dangerous tags the way the real one does, the marked
  // stub deliberately passes a raw <script> through (marked does, which
  // is exactly why DOMPurify.sanitize is the security layer).
  s.marked = { parse: (text) => '<p>' + text.replace(/</g, '&lt;').replace(/>/g, '&gt;') + '</p><script>alert(1)</script><img src=x onerror=alert(2)>' };
  s.DOMPurify = { sanitize: (h) => h.replace(/<script[\s\S]*?<\/script>/g, '').replace(/<img[^>]*onerror[^>]*>/g, '') };
  s._markdownLibsPromise = Promise.resolve(true);

  const el = makeStubElement();
  s.renderResultMarkdownInto(el, 'hello **world**');
  await flushAsync();

  assert.match(el.innerHTML, /<p>/, 'markdown was rendered');
  assert.doesNotMatch(el.innerHTML, /<script>/, 'raw script survived sanitize');
  assert.doesNotMatch(el.innerHTML, /onerror/, 'onerror attr survived sanitize');
});

test('markdown degrades to escaped plain text when the libs cannot load', async () => {
  const s = loadAppJS();
  s._markdownLibsPromise = Promise.reject(new Error('offline'));
  const el = makeStubElement();
  s.renderResultMarkdownInto(el, '# <img src=x onerror=alert(1)>');
  await flushAsync();

  assert.match(el.innerHTML, /result-markdown-plain/, 'plain-text fallback not used');
  assert.match(el.innerHTML, /&lt;img src=x onerror=alert\(1\)&gt;/, 'markdown not escaped in fallback');
  assert.doesNotMatch(el.innerHTML, /<img src=x onerror=alert\(1\)>/, 'raw img tag in fallback');
});
