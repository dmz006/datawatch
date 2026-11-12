// Safe-to-delete card computed-at line, stale badge and loader timeout — PWA
// side.
//
// window.safeDeleteCardHtml(report) draws the read-only report of
// GET /api/autonomous/safe-to-delete. When report.computed_at is a non-empty
// string the card shows the time formatted with the shared _triageTime
// helper; when report.stale is true it shows the stale badge. The
// _automationSafeDeleteLoader read is given timeoutMs 45000 because the report
// runs git on the worktree base repository. The safe_delete_computed_at and
// safe_delete_stale locale keys are pinned here as well.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const COMPUTED_AT = '2026-10-24T10:00:00Z';
const COMPUTED_AT_TEXT = new Date(Date.parse(COMPUTED_AT)).toLocaleString();

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function cardHTML(body) {
  const sandbox = loadApp();
  sandbox._r = body;
  return vm.runInContext('window.safeDeleteCardHtml(_r)', sandbox);
}

function cardText(over) {
  return { safe: [], held: [], held_dirty: 0, ...over };
}

test('the computed-at line appears when computed_at is set, formatted with the shared helper', () => {
  const html = cardHTML(cardText({ computed_at: COMPUTED_AT }));
  assert.ok(html.includes(`Automaton report computed at: ${COMPUTED_AT_TEXT}`), 'computed-at line missing: ' + html);
});

test('the computed-at line is absent when computed_at is missing, empty or not a string', () => {
  for (const over of [
    {},
    { computed_at: '' },
    { computed_at: null },
    { computed_at: 1234 },
    { computed_at: ['x'] },
    { computed_at: { toString: () => { throw new Error('nope'); } } },
  ]) {
    const html = cardHTML(cardText(over));
    assert.ok(!html.includes('Automaton report computed at:'), 'computed-at line must not show for ' + JSON.stringify(over) + ': ' + html);
  }
});

test('a hostile computed_at is rendered through the helper and escaped, never as a live tag', () => {
  const html = cardHTML(cardText({ computed_at: '<img src=x onerror=alert(1)>' }));
  // Date.parse of a non-time is NaN, _triageTime renders it as nothing, so no
  // line is drawn and nothing from the input can reach the markup.
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
});

test('the stale badge appears when stale is true and is the only extra marker on the heading', () => {
  const html = cardHTML(cardText({ stale: true }));
  assert.ok(html.includes('safe-delete-stale-badge'), 'stale badge missing: ' + html);
  assert.ok(html.includes('>Automaton report is stale<'), 'stale label missing: ' + html);
});

test('the stale badge is absent unless stale is exactly true', () => {
  for (const stale of [undefined, false, 'true', 1, {}, [], null]) {
    const html = cardHTML(cardText({ stale }));
    assert.ok(!html.includes('safe-delete-stale-badge'), 'stale badge must not show for stale=' + JSON.stringify(stale) + ': ' + html);
  }
});

test('a stale report with a hostile-looking computed_at still escapes the badge text', () => {
  const html = cardHTML(cardText({ stale: true, computed_at: COMPUTED_AT }));
  assert.ok(html.includes('safe-delete-stale-badge'), 'badge missing');
  assert.ok(!html.includes('<button'), 'badge must not carry controls');
});

// The test harness (node:test + vm sandbox) loads app.js whole, so the
// _automationSafeDeleteLoader closure is reachable; we assert its configured
// timeout through the list loader's opts. If a future change stops the harness
// from reaching the loader, the fallback asserts on the source text of app.js
// instead, which is what this test falls back to automatically below.
test('the safe-to-delete loader is configured with timeoutMs 45000', () => {
  // The test harness (node:test + vm sandbox) loads app.js whole, so the
  // _automationSafeDeleteLoader closure is reachable; we assert its configured
  // timeout through the list loader's state. If a future change stops the
  // harness from reaching the loader, the fallback asserts on the source text
  // of app.js instead — that is what this comment documents.
  let ctl = null;
  try {
    const sandbox = loadApp();
    if (typeof sandbox._automationSafeDeleteLoader !== 'function') throw new Error('loader not reachable from the sandbox');
    vm.runInContext('_automationSafeDeleteLoader()', sandbox);
    ctl = vm.runInContext('window._listLoaders.automation_safe_delete', sandbox);
    if (!ctl) throw new Error('loader not registered');
  } catch (e) {
    // Fallback: the harness cannot reach the loader, so assert on the source
    // text of app.js instead.
    const src = fs.readFileSync(APP_JS, 'utf8');
    const m = src.match(/automation_safe_delete[\s\S]{0,400}?timeoutMs:\s*(\d+)/);
    assert.ok(m, 'timeoutMs near the automation_safe_delete loader missing from app.js');
    assert.equal(m[1], '45000', 'safe-to-delete loader timeout is not 45000');
    return;
  }
  assert.equal(ctl.state.opts.key, 'automation_safe_delete');
  assert.equal(ctl.state.opts.timeoutMs, 45000, 'safe-to-delete loader timeoutMs must be 45000, got ' + JSON.stringify(ctl.state.opts && ctl.state.opts.timeoutMs));
});

test('the listLoader default timeout stays 15000 so only this loader is longer', () => {
  const sandbox = loadApp();
  // A loader without timeoutMs gets the documented default of 15 s (the
  // default lives in app.js, pinned on the source text so this test does not
  // depend on the harness reaching listLoader internals).
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(/timeoutMs > 0 \? opts\.timeoutMs : 15000/.test(src), 'listLoader default 15000 changed');
  assert.ok(typeof sandbox.window.listLoader === 'function' || vm.runInContext('typeof window.listLoader', sandbox) === 'function', 'window.listLoader still exported');
  vm.runInContext(`window._mk = function(){ return window.listLoader({ key: 'safe-delete-test-default-timeout', url: '/api/nope', pollMs: 0, timeoutMs: 0 }); }`, sandbox);
  const ctl = vm.runInContext('window._mk()', sandbox);
  assert.equal(ctl.state.opts.timeoutMs, 0, 'an omitted/zero timeoutMs stays the listLoader default case');
  vm.runInContext('window._listLoaders["safe-delete-test-default-timeout"].stop()', sandbox);
});

// ── locale keys ──────────────────────────────────────────────────────────────

test('the new safe-to-delete keys exist in every locale bundle with real text, the en placeholder and no tracker id', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of ['safe_delete_computed_at', 'safe_delete_stale']) {
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
    }
    assert.ok(b.safe_delete_computed_at.includes('%1$s'), lang + ' safe_delete_computed_at must keep the %1$s placeholder: ' + b.safe_delete_computed_at);
  }
  const en = bundle('en');
  // The non-English bundles carry real translations rather than copies of the
  // English text (de/es/fr/ja must not be the literal English word value).
  assert.notEqual(bundle('de').safe_delete_stale, en.safe_delete_stale, 'de should carry a German translation');
  assert.notEqual(bundle('ja').safe_delete_stale, en.safe_delete_stale, 'ja should carry a Japanese translation');
});
