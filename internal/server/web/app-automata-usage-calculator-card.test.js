// Usage calculator card — PWA side.
//
// The calculator is one card: a card head (title, "?" help entry, Refresh button)
// above the verdict sentence and the three compact graph tiles (weekly allowance,
// pace line, burn per day) in a responsive grid, one column on phones. The
// empty, loading, stale and error states stay inside the same card, and the burn
// guard is not drawn inside it.
//
// No network call and no wall clock: the bodies come from the hand-written fixture
// and every fetch is filtered out.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody, emptyBody } = require('./usage-calculator-fixture');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  sandbox.window.fetch = (url, init) => {
    const u = String(url);
    if (/(burn-guard|\/api\/sessions)/.test(u)) return Promise.reject(new Error('filtered: ' + u));
    return Promise.reject(new Error('unexpected fetch: ' + u));
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(JSON.stringify(EN));
  delete sandbox.window._listLoaders.automation_usage;
  return sandbox;
}
function frame(sandbox, body, opts) {
  sandbox._body = body;
  sandbox._opts = opts || {};
  return vm.runInContext('window.usageCalcFrameHtml(_body, _opts)', sandbox);
}
const count = (html, re) => (html.match(re) || []).length;

test('the whole calculator is exactly one card with a card head', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody());
  assert.match(html, /^<div class="card usage-calc-frame"/, 'the root is the one card');
  assert.equal(count(html, /class="card usage-calc-frame"/g), 1, 'exactly one card root');
  // The head holds the title, the "?" help entry and the Refresh button.
  assert.ok(html.includes('usage-calc-title'), 'the card head has the title');
  assert.ok(html.includes(EN['usage_calc_title']), 'the title text is localized');
  assert.ok(html.includes('class="usage-help"'), 'the "?" help entry');
  assert.ok(html.includes('usageCalcRefresh()'), 'the Refresh button refetches');
  assert.ok(html.includes(EN['usage_calc_refresh']), 'the Refresh label is localized');
});

test('the verdict sentence and the three graph tiles sit inside the one card', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody());
  assert.equal(count(html, /class="usage-verdict-sentence"/g), 1, 'one verdict sentence');
  for (const id of ['allowance', 'pace', 'burn']) {
    assert.ok(html.includes(`data-card="${id}"`), `the ${id} tile`);
  }
  assert.equal(count(html, /data-card="/g), 3, 'exactly the three graph tiles');
  // The tiles are laid out in the responsive grid inside the card.
  assert.ok(html.includes('class="usage-cards"'), 'the tiles share one grid');
  const tilesEnd = html.lastIndexOf('data-card="burn"');
  assert.ok(tilesEnd < html.lastIndexOf('</div>'), 'the tiles close inside the card root');
  // The sentence is above the tiles, inside the same card.
  assert.ok(html.indexOf('usage-verdict-sentence') < html.indexOf('usage-cards'));
});

test('the tiles are compact fixed-height tiles in a responsive grid, one column on phones', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.match(css, /\.usage-cards \{ display: grid; grid-template-columns: repeat\(auto-fit, minmax\(220px, 1fr\)\);/);
  assert.match(css, /@media\s*\(max-width:\s*600px\)\s*\{[^{]*\.usage-cards\s*\{[^}]*grid-template-columns:\s*1fr/);
  const tile = /^\.usage-card\s*\{([^}]*)\}/m.exec(css);
  assert.match(tile[1], /(^|[\s;])height:\s*\d+px/, 'a tile has a fixed height');
  assert.match(/\.usage-calc-frame\s*\{([^}]*)\}/.exec(css)[1], /border-radius:/, 'the card has a card look');
});

test('the empty, loading and error states stay inside the one card', () => {
  const s = loadApp();
  const empty = frame(s, emptyBody());
  assert.match(empty, /^<div class="card usage-calc-frame"/);
  assert.ok(empty.includes('usage-calc-empty'), 'the empty state');
  assert.ok(!empty.includes('data-card="'), 'no tiles without a reading');

  const loading = frame(s, null, { loading: true });
  assert.match(loading, /^<div class="card usage-calc-frame"/);
  assert.ok(loading.includes('usage-calc-loading'), 'the loading state');

  const error = frame(s, null, { error: 'boom <script>' });
  assert.match(error, /^<div class="card usage-calc-frame"/);
  assert.ok(error.includes('usage-calc-error'), 'the error state');
  assert.ok(error.includes('role="alert"'), 'the error is announced');
  assert.ok(error.includes('boom &lt;script&gt;'), 'the error text is escaped');
  assert.ok(!error.includes('data-card="'), 'no tiles on error');
});

test('a stale copy keeps its marker inside the one card', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({ stale: true, keeper: 'peer-b', copied_at: '2026-10-23T20:00:00-04:00' }));
  assert.match(html, /^<div class="card usage-calc-frame"/);
  assert.ok(html.includes('usage-calc-stale'), 'the stale marker');
  assert.ok(html.includes('data-card="allowance"'), 'the tiles are still drawn');
});

test('the burn guard is not drawn inside the calculator card', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody());
  assert.ok(!html.includes('burnGuard'), 'no burn guard markup in the frame');
  assert.ok(!html.includes('id="burnGuardCard"'), 'no burn guard slot in the frame');
});

test('the settings link stays inside the card', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody());
  assert.ok(html.includes('usage-settings-link'), 'the settings link');
  assert.ok(html.indexOf('usage-settings-link') < html.lastIndexOf('</div>'), 'inside the card root');
});
