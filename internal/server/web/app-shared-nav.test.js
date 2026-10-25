// Shared bottom nav — label titles and clipped-label styling.
//
// The bottom nav holds six buttons. At phone widths they do not all fit, so the
// bar scrolls sideways and some labels are cut off. Each nav button needs a title
// attribute equal to its translated label, so the full name stays reachable when the
// label is clipped. The label rule must clip with an ellipsis and must not wrap.
// Both assertions fail on the unmodified tree; the measured evidence is in
// docs/testing/runs/shared-nav-mobile-fit/repro-report.txt.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the English
  // one so the translated label is the real user-visible text.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

// navButtons parses each .nav-btn in index.html: its view, its title attribute
// (from the button's own open tag) and the data-i18n key of its .nav-label.
function navButtons() {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  const start = html.indexOf('<nav class="nav"');
  const end = html.indexOf('</nav>', start);
  const nav = html.slice(start, end).replace(/<!--[\s\S]*?-->/g, '');
  const buttons = [...nav.matchAll(/<button\b([^>]*\bclass="nav-btn[^"]*"[^>]*)>([\s\S]*?)<\/button>/g)];
  return buttons.map(m => {
    const open = m[1];
    const label = m[2].match(/<span class="nav-label"[^>]*data-i18n="([^"]+)"/);
    return {
      view: (open.match(/data-view="([^"]+)"/) || [])[1],
      title: (open.match(/\btitle="([^"]*)"/) || [])[1],
      key: label ? label[1] : undefined,
    };
  });
}

test('every nav button carries a title equal to its translated label', () => {
  const sb = loadApp();
  const buttons = navButtons();
  assert.ok(buttons.length >= 4, 'the bottom nav buttons are found in index.html');
  for (const b of buttons) {
    assert.ok(b.key, `${b.view} has a nav-label with a data-i18n key`);
    const label = sb.t(b.key);
    assert.notEqual(b.title, undefined, `${b.view} nav button has a title attribute`);
    assert.equal(b.title, label, `${b.view} nav button title matches its label`);
  }
});

test('the nav label clips with an ellipsis and does not wrap', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const rule = css.match(/(^|\n)\.nav-label\s*\{([^}]*)\}/);
  assert.ok(rule, 'style.css has a base .nav-label rule');
  assert.match(rule[2], /overflow:\s*hidden\b/, '.nav-label clips its overflow');
  assert.match(rule[2], /text-overflow:\s*ellipsis\b/, '.nav-label shows an ellipsis when clipped');
  assert.match(rule[2], /white-space:\s*nowrap\b/, '.nav-label does not wrap');
});
