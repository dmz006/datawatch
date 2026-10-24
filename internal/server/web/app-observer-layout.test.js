// Observer tab layout ratchet: the markup renderObserverView generates must
// stay fluid (no fixed-pixel widths, no fixed-pixel grid columns) and the
// phone-width rules must live in a max-width media query in style.css.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function renderObserver() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const view = { innerHTML: '' };
  sandbox.document.getElementById = (id) => (id === 'view' ? view : null);
  try { vm.runInContext('renderObserverView()', sandbox); } catch (e) { /* loaders may touch absent nodes */ }
  return view.innerHTML;
}

test('observer markup has no fixed pixel widths of 60px or more', () => {
  const html = renderObserver();
  assert.ok(html.length > 1000, 'observer view did not render');
  const bad = [];
  for (const m of html.matchAll(/(?<![-\w])(min-width|width)\s*:\s*(\d+(?:\.\d+)?)px/g)) {
    // 7px/8px status dots and other tiny glyph boxes are below the threshold.
    if (parseFloat(m[2]) >= 60) bad.push(m[0]);
  }
  assert.deepEqual(bad, []);
});

test('observer markup has no fixed pixel grid columns', () => {
  const html = renderObserver();
  const bad = [...html.matchAll(/grid-template-columns\s*:\s*[^;"]*\d+px[^;"]*/g)].map(m => m[0]);
  assert.deepEqual(bad, []);
});

test('observer markup uses the layout classes', () => {
  const html = renderObserver();
  for (const cls of ['obs-row', 'obs-filter-row', 'obs-scope-row', 'obs-maint-grid', 'obs-log', 'obs-tap']) {
    assert.ok(html.includes(cls), 'missing class ' + cls);
  }
});

test('style.css keeps the observer classes inside a max-width media query', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const idx = css.lastIndexOf('.obs-row {');
  assert.ok(idx >= 0, 'base observer rules missing');
  const tail = css.slice(idx);
  const m = tail.match(/@media \(max-width: (\d+)px\)(?:, \(max-height: \d+px\))? \{([\s\S]*)\n\}\s*$/);
  assert.ok(m, 'no max-width media query after the observer rules');
  assert.ok(Number(m[1]) <= 768, 'breakpoint must not affect widths above 768px');
  for (const sel of ['.obs-tap', '.obs-maint-grid', '.obs-scope-input', '.obs-log']) {
    assert.ok(m[2].includes(sel), sel + ' not inside the max-width media query');
  }
  assert.match(m[2], /\.obs-maint-grid\s*\{\s*grid-template-columns:\s*1fr;/);
});
