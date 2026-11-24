// The locale bundle loads after the page starts. A module that paints text on
// its own, before the bundle is ready, must never show a raw locale key (t()
// returns the key itself when the bundle lacks it, and a key is truthy, so
// `t(k) || 'English'` never falls back). This pins:
//   - tOr(key, default): the guard helper, treats a returned key as missing;
//   - onI18nReady(fn): a ready hook run when the bundle has loaded;
//   - the header pause badge and its open window: English default while the
//     bundle is empty, repainted in the active locale when it becomes ready.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function localeBundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp(fetchImpl) {
  const sandbox = buildSandbox();
  if (fetchImpl) sandbox.fetch = fetchImpl;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// bundleFetch serves the locale JSON the way the daemon does.
function bundleFetch(requested) {
  return (url) => {
    const m = /\/locales\/([a-z]+)\.json/.exec(String(url));
    if (!m) return Promise.resolve({ ok: false, status: 404 });
    if (requested) requested.push(m[1]);
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(localeBundle(m[1])) });
  };
}

test('tOr uses the English default while the bundle is empty and the translation once it loads', () => {
  const sb = loadApp();
  assert.equal(sb.window.t('pause_badge_paused'), 'pause_badge_paused', 't() returns the key when the bundle is empty');
  assert.equal(sb.window.tOr('pause_badge_paused', 'Paused'), 'Paused');
  sb.window._i18n.bundle = { pause_badge_paused: 'Pausiert' };
  assert.equal(sb.window.tOr('pause_badge_paused', 'Paused'), 'Pausiert');
  assert.equal(sb.window.tOr('no_such_key_anywhere', 'Fallback'), 'Fallback');
});

test('tOr passes the placeholders to the translation and does not touch the default', () => {
  const sb = loadApp();
  sb.window._i18n.bundle = { attempt_cap_count: 'Attempts started: %1$d' };
  assert.equal(sb.window.tOr('attempt_cap_count', 'Attempts: %1$d', [4]), 'Attempts started: 4');
  sb.window._i18n.bundle = {};
  assert.equal(sb.window.tOr('attempt_cap_count', 'Attempts: 4', [4]), 'Attempts: 4');
});

test('a ready hook runs when the bundle has loaded, and at once when it already has', async () => {
  const sb = loadApp(bundleFetch());
  const seen = [];
  sb.window.onI18nReady(() => seen.push('first:' + sb.window._i18n.ready + ':' + sb.window._i18n.current));
  assert.deepEqual(seen, [], 'nothing runs before the bundle is ready');
  await sb.initI18n();
  assert.deepEqual(seen, ['first:true:en']);
  sb.window.onI18nReady(() => seen.push('late'));
  assert.deepEqual(seen, ['first:true:en', 'late']);
});

test('a hook that throws does not stop the others or the boot', async () => {
  const sb = loadApp(bundleFetch());
  const seen = [];
  sb.window.onI18nReady(() => { throw new Error('boom'); });
  sb.window.onI18nReady(() => seen.push('second'));
  await sb.initI18n();
  assert.deepEqual(seen, ['second']);
});

// ── the pause badge ─────────────────────────────────────────────────────────

function fakeEl() {
  const attrs = {};
  const cls = new Set();
  return {
    textContent: '', innerHTML: '', style: {}, title: '', value: '',
    classList: { add: c => cls.add(c), remove: c => cls.delete(c), toggle: (c, on) => (on ? cls.add(c) : cls.delete(c)), contains: c => cls.has(c) },
    setAttribute: (k, v) => { attrs[k] = v; }, getAttribute: k => (k in attrs ? attrs[k] : null),
  };
}

// loadPause loads app-pause.js the way the page does: t() is the real one's
// behaviour (the key when the bundle lacks it), the bundle starts empty, and the
// DOMContentLoaded listener is captured so the test can fire it.
function loadPause(pausedState) {
  const label = fakeEl();
  const btn = fakeEl();
  const holder = fakeEl();
  const sandbox = buildSandbox({ byId: { headerPauseBtn: btn, headerPauseLabel: label, pauseBodyHolder: holder } });
  const listeners = [];
  sandbox.document.addEventListener = (ev, fn) => listeners.push([ev, fn]);
  sandbox._i18n = { bundle: {}, fallback: {}, ready: false };
  const hooks = [];
  sandbox.onI18nReady = (fn) => { if (sandbox._i18n.ready) fn(); else hooks.push(fn); };
  sandbox.t = (k) => { const b = sandbox._i18n.bundle; return b[k] !== undefined ? b[k] : k; };
  sandbox.tOr = (k, d) => { const s = sandbox.t(k); return s === k ? d : s; };
  sandbox.escHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  sandbox.apiFetch = () => Promise.resolve(pausedState);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-pause.js'), sandbox);
  const becomeReady = (bundle) => { sandbox._i18n.bundle = bundle; sandbox._i18n.ready = true; hooks.forEach(h => h()); };
  return { sandbox, label, btn, holder, listeners, becomeReady };
}

const PAUSED = { paused: true, global: { reason: 'freeze', by: 'cli', at: '2026-11-23T12:00:00Z' } };

test('with the bundle empty the badge shows Paused, never the raw key', async () => {
  const p = loadPause(PAUSED);
  assert.equal(p.sandbox.window.pauseBadgeText(PAUSED), 'Paused');
  await p.sandbox.window.pauseRefresh();
  assert.equal(p.label.textContent, 'Paused');
  assert.equal(p.btn.title, 'New work is paused');
  assert.equal(p.btn.getAttribute('aria-label'), 'New work is paused');
  assert.equal(p.sandbox.window.pauseBadgeText({ ...PAUSED, projects: [{ project: '/a' }] }), 'Paused 2');
});

test('with the bundle empty the pause window shows English text, no raw keys', () => {
  const p = loadPause(PAUSED);
  p.sandbox.window._pause.state = PAUSED;
  const html = p.sandbox.window.pauseBodyHtml();
  assert.doesNotMatch(html, /\bpause_[a-z_]+\b/, 'a raw pause_ key reached the window');
  assert.match(html, /Nothing is paused|All work/);
  assert.match(html, /Resume all/);
});

test('when the bundle becomes ready the badge and an open window repaint in the active locale', async () => {
  const p = loadPause(PAUSED);
  // The page starts polling at DOMContentLoaded, before the bundle has loaded.
  const dcl = p.listeners.find(l => l[0] === 'DOMContentLoaded');
  assert.ok(dcl, 'the module registers its DOMContentLoaded listener');
  dcl[1]();
  await p.sandbox.window.pauseRefresh();
  p.holder.innerHTML = p.sandbox.window.pauseBodyHtml(); // the window is open
  assert.equal(p.label.textContent, 'Paused');
  assert.match(p.holder.innerHTML, /A pause stops new work/);

  p.becomeReady(localeBundle('de'));
  assert.equal(p.label.textContent, 'Pausiert', 'the badge was not repainted in German');
  assert.equal(p.btn.title, 'Neue Arbeit ist pausiert');
  assert.match(p.holder.innerHTML, /Eine Pause stoppt neue Arbeit/, 'the open window was not repainted');
});

test('a bundle that is already ready paints the translation at once', async () => {
  const p = loadPause(PAUSED);
  p.becomeReady(localeBundle('de'));
  await p.sandbox.window.pauseRefresh();
  assert.equal(p.label.textContent, 'Pausiert');
});
