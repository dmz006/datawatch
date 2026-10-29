// Header Tags menu shell — placement, open/close state, outside click, Escape and the active count.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function makeEl() {
  const el = makeStubElement();
  const own = { innerHTML: '', textContent: '' };
  // The shared stub defines these as fixed accessors; plain ones let a test read back what was written.
  const plain = Object.create(el);
  Object.defineProperty(plain, 'innerHTML', { get() { return own.innerHTML; }, set(v) { own.innerHTML = v; }, configurable: true });
  Object.defineProperty(plain, 'textContent', { get() { return own.textContent; }, set(v) { own.textContent = v; }, configurable: true });
  plain.attrs = {};
  plain.hidden = true;
  plain.focused = false;
  plain.kids = [];
  plain.setAttribute = (k, v) => { plain.attrs[k] = String(v); };
  plain.getAttribute = k => (k in plain.attrs ? plain.attrs[k] : null);
  plain.focus = () => { plain.focused = true; };
  plain.contains = other => other === plain || plain.kids.includes(other);
  return plain;
}

function loadApp(stored = {}) {
  const btn = makeEl();
  const chip = makeEl();
  const panel = makeEl();
  const outside = makeEl();
  const handlers = {};
  const sandbox = buildSandbox({ byId: { headerTagsBtn: btn, headerTagsCount: chip, headerTagsMenu: panel } });
  sandbox.document.addEventListener = (type, fn) => { (handlers[type] = handlers[type] || []).push(fn); };
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ tags: [] }), text: () => Promise.resolve('{"tags":[]}') });
  for (const k of Object.keys(stored)) sandbox.localStorage.setItem(k, stored[k]);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const fire = (type, ev) => (handlers[type] || []).forEach(fn => fn(ev));
  return { sb: sandbox, btn, chip, panel, outside, handlers, fire };
}

const run = (h, code) => vm.runInContext(code, h.sb);

test('the Tags button sits before the status dot in the header', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  const btn = html.indexOf('id="headerTagsBtn"');
  const dot = html.indexOf('id="statusDot"');
  const install = html.indexOf('id="headerInstallBtn"');
  assert.ok(btn > install && btn < dot, 'button is after the install button and before statusDot');
  const tag = html.slice(html.lastIndexOf('<button', btn), html.indexOf('</button>', btn));
  for (const needle of ['type="button"', 'aria-haspopup="dialog"', 'aria-expanded="false"', 'aria-controls="headerTagsMenu"', 'class="header-tags-btn"']) {
    assert.ok(tag.includes(needle), needle);
  }
  const icon = tag.slice(tag.indexOf('<svg'), tag.indexOf('</svg>') + '</svg>'.length);
  assert.ok(icon, 'button holds an svg icon');
  assert.match(icon, /<svg[^>]*class="header-tags-icon"/, 'the icon svg carries the header-tags-icon class');
  assert.match(icon, /aria-hidden="true"/, 'the icon svg is aria-hidden');
  assert.ok(!/stroke\s*=\s*"currentColor"/.test(icon), 'no old currentColor outline stroke');
  assert.ok(!icon.includes('fill="none"'), 'no old unfilled outline drawing');
  assert.ok(!icon.includes('stroke-width="1.' ), 'no old outline stroke-width attribute');
  assert.ok(!tag.includes('>Tags<'), 'button has no visible Tags text');
  assert.ok(!tag.includes('data-i18n'), 'button carries no data-i18n');
  assert.match(html, /<div id="headerTagsMenu"[^>]*role="dialog"[^>]*aria-labelledby="headerTagsBtn"[^>]*hidden/);
  assert.ok(html.indexOf('id="headerTagsMenu"') > html.indexOf('</header>'));
});

test('opening shows the panel and closing hides it', async () => {
  const h = loadApp();
  assert.equal(h.panel.hidden, true);
  h.sb.toggleTagsMenu();
  assert.equal(h.btn.getAttribute('aria-expanded'), 'true');
  await flushAsync();
  assert.equal(h.panel.hidden, false);
  h.sb.toggleTagsMenu();
  assert.equal(h.btn.getAttribute('aria-expanded'), 'false');
  assert.equal(h.panel.hidden, true);
});

test('the panel is filled from _tagsMenuHTML when that function exists', async () => {
  const h = loadApp();
  run(h, 'globalThis._tagsMenuHTML = () => "<p>content</p>";');
  h.sb.toggleTagsMenu();
  await flushAsync();
  assert.equal(h.panel.innerHTML, '<p>content</p>');
});

test('a press outside closes the menu; a press inside does not', async () => {
  const h = loadApp();
  h.panel.kids = [h.outside];
  h.sb.toggleTagsMenu();
  await flushAsync();
  h.fire('pointerdown', { target: h.panel });
  assert.equal(h.panel.hidden, false);
  h.fire('pointerdown', { target: h.btn });
  assert.equal(h.panel.hidden, false);
  h.fire('pointerdown', { target: makeEl() });
  assert.equal(h.panel.hidden, true);
  assert.equal(h.btn.getAttribute('aria-expanded'), 'false');
});

test('Escape closes the menu and returns focus to the button', async () => {
  const h = loadApp();
  h.sb.toggleTagsMenu();
  await flushAsync();
  h.fire('keydown', { key: 'a' });
  assert.equal(h.panel.hidden, false);
  h.fire('keydown', { key: 'Escape' });
  assert.equal(h.panel.hidden, true);
  assert.equal(h.btn.focused, true);
});

test('document listeners are registered once, not on every open', async () => {
  const h = loadApp();
  for (let i = 0; i < 3; i++) { h.sb.toggleTagsMenu(); await flushAsync(); h.sb.toggleTagsMenu(); }
  assert.equal((h.handlers.pointerdown || []).length, 1);
  assert.equal((h.handlers.keydown || []).length, 1);
});

test('the button shows the active count: watch is one, plus each selected tag', () => {
  const h = loadApp();
  assert.equal(run(h, '_tagsMenuActiveCount()'), 0);
  h.sb._tagsMenuRefresh();
  assert.equal(h.chip.textContent, '');
  assert.equal(h.btn.getAttribute('aria-label'), h.sb.t('tags_menu_button'));
  assert.equal(h.btn.getAttribute('title'), h.sb.t('tags_menu_button'));
  run(h, 'state.sessionWatchFilter = true;');
  assert.equal(run(h, '_tagsMenuActiveCount()'), 1);
  run(h, "_tagBrowse.session.sel = ['aaaa0001', 'aaaa0002'];");
  assert.equal(run(h, '_tagsMenuActiveCount()'), 3);
  h.sb._tagsMenuRefresh();
  assert.equal(h.chip.textContent, '3');
  assert.match(h.btn.getAttribute('aria-label'), /3/);
  assert.equal(h.btn.getAttribute('aria-label'), h.sb.t('tags_menu_button') + ' (3)');
  assert.equal(h.btn.getAttribute('title'), h.sb.t('tags_menu_button'));
  run(h, 'state.sessionWatchFilter = false;');
  assert.equal(run(h, '_tagsMenuActiveCount()'), 2);
  h.sb._tagsMenuRefresh();
  assert.equal(h.chip.textContent, '2');
  assert.equal(h.btn.getAttribute('aria-label'), h.sb.t('tags_menu_button') + ' (2)');
});

test('a tag selection restored from storage shows its count once the page has loaded', async () => {
  const h = loadApp({ cs_tag_browse_session: JSON.stringify({ view: 'list', sel: ['aaaa0001', 'aaaa0002'], mode: '' }) });
  assert.equal(run(h, '_tagsMenuActiveCount()'), 2);
  assert.equal(h.chip.textContent, '', 'nothing has refreshed the button before the page loads');
  for (const fn of h.handlers.DOMContentLoaded || []) { try { fn(); } catch (_) { /* unrelated startup code */ } }
  await flushAsync();
  assert.equal(h.chip.textContent, '2');
  assert.match(h.btn.getAttribute('aria-label'), /2/);
  assert.equal(h.btn.getAttribute('aria-expanded'), 'false');
});

test('the new strings exist in every locale and the visible label has no PRD wording', () => {
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    assert.ok(b.tags_menu_button && b.tags_menu_close, l);
    assert.doesNotMatch(b.tags_menu_button + b.tags_menu_close, /PRD/);
  }
});
